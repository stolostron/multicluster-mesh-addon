# User Guide

End-to-end walkthrough for setting up a multi-cluster service mesh.
For a quick overview of what the addon does, see the [architecture](architecture.md).
New to OCM? See the [OCM concepts docs][ocm-concepts] for background on terms like ManagedCluster and ManifestWork.

This guide uses predefined sample manifests suitable for development and testing.
For production, use your own configurations (e.g., your organization's CA instead of the self-signed example).

> **Note:** Commands use `kubectl`. On OpenShift, `oc` is a drop-in replacement for the `kubectl` commands in this guide.

## Prerequisites

- An OCM hub cluster with managed clusters registered
- [cert-manager] installed on the hub cluster (mints per-cluster intermediate CAs for mTLS trust)
- A service mesh operator installed on the managed clusters. This guide uses [OperatorPolicy] from the [governance-policy-framework][gpf]; any other installation method is fine.
- [`ManifestWorkReplicaSet`][mwrs] feature gate enabled on the hub's `ClusterManager`

## Step 1: Install the Addon

```bash
helm repo add multicluster-mesh-addon https://stolostron.github.io/multicluster-mesh-addon
helm repo update
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --namespace multicluster-mesh-system \
  --create-namespace
```

For more install options, see the [Helm chart docs](../chart/README.md).

## Step 2: Set Up Clusters

Ensure your clusters are registered as ManagedClusters with the hub:

```bash
clusteradm get clusters
```

> **Note:** `cluster1` and `cluster2` are example ManagedCluster names.
> Run `clusteradm get clusters` to list the actual names in your environment.

### Network identity (optional)

In a multi-network mesh, each cluster needs a network identity.
By default the addon uses the managed cluster name.
To override it, label the ManagedCluster on the hub before creating the mesh:

```bash
kubectl label managedcluster cluster1 topology.istio.io/network=network-a
kubectl label managedcluster cluster2 topology.istio.io/network=network-b
```

The addon applies this value as `topology.istio.io/network` on the control plane namespace on the managed cluster.
Your Istio CR and east-west gateway must use the same value (see [Step 8](#step-8-configure-istio)).

## Step 3: Create a Trust Chain

The addon uses cert-manager to distribute intermediate CAs to each cluster, implementing Istio's [Plug-in CA] pattern.
All clusters in a mesh share the same root CA, enabling mTLS across cluster boundaries.

You need to create a cert-manager issuer that acts as the Root CA.
Use an `Issuer` (namespace-scoped) for per-mesh trust isolation, or a `ClusterIssuer` for a shared root CA across meshes in different namespaces.

A self-signed trust chain (for testing):

```bash
kubectl create namespace mesh-system
kubectl apply -n mesh-system -f samples/cert-manager-issuer.yaml
```

This creates a self-signed `Issuer`, a root CA `Certificate`, and a root CA-backed `Issuer` that the addon will use to mint intermediate certificates.

> **Note:** For production, replace the self-signed root with your preferred root CA.

## Step 4: Create a Placement

A [Placement] selects which clusters join the mesh.
Placements only consider clusters from [ManagedClusterSets][clusterset] that are bound to the Placement's namespace, so you need three things in order:

1. **ClusterSet membership** - each cluster must belong to a ClusterSet (via the `cluster.open-cluster-management.io/clusterset` label).
2. **ManagedClusterSetBinding** - the ClusterSet must be bound to the mesh namespace.
3. **Placement** - selects from the bound ClusterSets using predicates.

### Create and bind a ClusterSet

Create a dedicated [ManagedClusterSet] for the mesh and assign your clusters to it:

```bash
kubectl apply -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: mesh-clusters
EOF

kubectl label managedcluster cluster1 cluster.open-cluster-management.io/clusterset=mesh-clusters
kubectl label managedcluster cluster2 cluster.open-cluster-management.io/clusterset=mesh-clusters
```

Then bind the ClusterSet to the mesh namespace (the binding name must match the ClusterSet name):

```bash
kubectl apply -n mesh-system -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: mesh-clusters
spec:
  clusterSet: mesh-clusters
EOF
```

> **Tip:** For quick testing on ACM, you can bind the `global` ClusterSet instead - it automatically includes
> all managed clusters and doesn't require labeling. For production, use a dedicated ClusterSet to limit
> which clusters the Placement can select.

### Create the Placement

The Placement must be in the same namespace as the `MultiClusterMesh` resource.

```bash
kubectl apply -n mesh-system -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: mesh-placement
spec:
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchLabels:
            mesh-role: member
EOF
```

Label your clusters to match the Placement's selector:

```bash
kubectl label managedcluster cluster1 mesh-role=member
kubectl label managedcluster cluster2 mesh-role=member
```

> **Note:** To select all the clusters from a specific clusterSet, you can specify spec.clusterSets and remove spec.predicates.

Verify the Placement selected your clusters:

```bash
kubectl get placementdecision -n mesh-system -l cluster.open-cluster-management.io/placement=mesh-placement
```

> **Warning:** An empty selection uninstalls the mesh. If the Placement exists and its PlacementDecisions agree that zero clusters are selected (`status.numberOfSelectedClusters` is 0), the addon deletes the plumbing for every cluster: control plane namespace ManifestWorks (and the namespaces they created, including any Istio you installed there), intermediate CA Certificates, cacerts, ManagedServiceAccounts, and remote secrets.
>
> Deleting the Placement does not do this. The mesh reports `PlacementNotFound` and leaves the existing plumbing in place until the Placement exists again.
>
> A short mismatch does not uninstall anything. If PlacementDecisions are briefly empty while `status.numberOfSelectedClusters` is still greater than zero, the addon waits until the counts agree.
>
> This empty selection usually comes from an adjacent change: deleting the ManagedClusterSetBinding, removing a cluster's clusterset label, or editing Placement predicates so they no longer match.

## Step 5: Install the Service Mesh Operator

The service mesh operator must be installed on managed clusters before creating a mesh.
The recommended approach uses [OperatorPolicy] from the governance-policy-framework, which distributes the operator to managed clusters using the same Placement-based targeting.

The samples bind to a Placement named `mesh-operator-placement`.
Point that binding at `mesh-placement` from Step 4 before applying the sample.
Enable OperatorPolicy on each cluster's config-policy-controller ManagedClusterAddOn with the `operator-policy-disabled=false` annotation.
Verify that the Placement selects the intended clusters and each has a compliant policy entry before creating the mesh; an aggregate result alone can describe earlier membership.

Kubernetes (Sail):

```bash
sed 's/name: mesh-operator-placement/name: mesh-placement/' samples/operator-policy-sail.yaml \
  | kubectl apply -n mesh-system -f -
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-sail-operator -n mesh-system --timeout=15m
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl wait --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_NAME}\")].compliant}=Compliant" \
    policy/policy-sail-operator -n mesh-system --timeout=15m
done
```

OpenShift (OSSM):

```bash
sed 's/name: mesh-operator-placement/name: mesh-placement/' samples/operator-policy-ossm.yaml \
  | kubectl apply -n mesh-system -f -
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-ossm-operator -n mesh-system --timeout=15m
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl wait --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_NAME}\")].compliant}=Compliant" \
    policy/policy-ossm-operator -n mesh-system --timeout=15m
done
```

## Step 6: Create a MultiClusterMesh

See [samples/complete.yaml](../samples/complete.yaml) for a fully annotated example. The following creates `basic-mesh` with an explicit discovery duration, avoiding the current [nested-defaulting limitation](api-reference.md#discovery-defaulting) in YAML that omits `security.discovery`.

```bash
kubectl apply -n mesh-system -f - <<EOF
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: basic-mesh
spec:
  placementRef:
    name: mesh-placement
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
    discovery:
      tokenValidity: 360h
EOF
```

Update `spec.placementRef.name` to match your Placement name.
See the [API reference](api-reference.md) for all available fields.

## Step 7: Verify the Setup

Check mesh status:

```bash
kubectl wait --for=condition=Ready=True multiclustermesh/basic-mesh \
  -n mesh-system --timeout=15m
kubectl get multiclustermesh -n mesh-system -o yaml
```

When trust distribution is complete on every resolved cluster, the mesh shows `Ready=True` with reason `AllClustersReady`.
With no issuer configured, successful hub reconciliation for at least one resolved cluster is enough; no spoke application is checked on that path.
The auto-derived control plane namespace is reported in `status.controlPlaneNamespace`.
See the [API reference](api-reference.md#status-conditions) for all reported conditions.

If something isn't working, see the [troubleshooting guide](troubleshooting.md).

## Step 8: Configure Istio

The addon creates the control plane namespace, distributes trust, and handles endpoint discovery (remote secrets), but you configure the mesh control plane on each cluster yourself.
Both OSSM 3.x and upstream Sail use the `sailoperator.io` API group.
See the [Sail operator multicluster docs][sail-multicluster] for the full procedure.

For a policy-managed setup, the PoC [`meshctl scaffold`](meshctl.md) command
generates an ACM Policy bundle that resolves mesh identity and network values
from hub state, then installs Istio and the east-west Gateway on selected
spokes. The base bundle works with compatible Sail and OSSM 3.x operators. When
the selected clusters require Istio CNI, generate a separate bundle with
`meshctl scaffold istiocni` and pass its policy name to
`meshctl scaffold istio --depends-on-istiocni`; the latter waits for the CNI
readiness check. Validate the operator and Gateway API versions before using
the PoC outside the demo. Both commands are offline; apply their output to the
hub or commit them to GitOps.

To get started quickly, use the provided sample manifests from [samples/istio/](../samples/istio/).

The following commands run against the spoke clusters (not the hub).
Make sure your kubeconfig has the contexts for each managed cluster.

The east-west gateway sample uses [Gateway API]. Check the CRDs on each spoke, and install a version supported by your Istio/operator release when they are absent. The demo uses v1.2.1:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  if ! kubectl --context "$CLUSTER_NAME" get crd \
    gateways.gateway.networking.k8s.io gatewayclasses.gateway.networking.k8s.io >/dev/null 2>&1; then
    kubectl --context "$CLUSTER_NAME" apply --server-side \
      -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
  fi
done
```

Notes:

* `MESH_NAME` must be the `MultiClusterMesh` name. Step 6 creates `basic-mesh`, and that name is the trust domain.
* `CP_NAMESPACE` must be `status.controlPlaneNamespace` from that mesh. The Istio samples substitute it into the control plane namespace.
* `NETWORK` must match the `topology.istio.io/network` label on the control plane namespace (defaults to the cluster name).
* `IstioCNI` is required on OpenShift and for ambient mode. Create its `istio-cni` namespace and apply `samples/istio/istiocni.yaml` on those clusters before installing Istio; it can be skipped for sidecar mode on vanilla Kubernetes.
* The Istio sample creates the cluster-scoped resource `Istio/default`. Use this example for one mesh per spoke. Multiple meshes require distinct Istio resource names and appropriate revision/injection and discovery-selector configuration.

For OpenShift or ambient mode, prepare CNI on the spokes first:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl --context "$CLUSTER_NAME" apply -f hack/demo/istiocni-namespace.yaml
  kubectl --context "$CLUSTER_NAME" apply -f samples/istio/istiocni.yaml
done
```

Then install each control plane and gateway:

```bash
export MESH_NAME=basic-mesh
export CP_NAMESPACE="$(kubectl get multiclustermesh "$MESH_NAME" -n mesh-system -o jsonpath='{.status.controlPlaneNamespace}')"
test -n "$CP_NAMESPACE" || { echo "Mesh control plane namespace is not available yet" >&2; exit 1; }

for CLUSTER_NAME in cluster1 cluster2; do
  export CLUSTER_NAME
  # Read the label on the hub, falling back to the managed cluster name.
  NETWORK="$(kubectl get managedcluster "$CLUSTER_NAME" -o jsonpath='{.metadata.labels.topology\.istio\.io/network}')"
  export NETWORK="${NETWORK:-$CLUSTER_NAME}"

  envsubst < samples/istio/istio.yaml | kubectl apply --context "$CLUSTER_NAME" -f -
  kubectl --context "$CLUSTER_NAME" wait --for=create \
    deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
  kubectl --context "$CLUSTER_NAME" wait --for=condition=Available \
    deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
  envsubst < samples/istio/eastwest-gateway.yaml | kubectl apply --context "$CLUSTER_NAME" -f -
done
```

### Next Steps

Once the mesh is running, you can use [Fleet Service Mesh][fleet-mesh] for multi-cluster mesh observability.
Fleet Service Mesh is an OpenShift console plugin and requires an OpenShift cluster with ACM (Advanced Cluster Management).
See the [Fleet Service Mesh Dev Preview Guide][fleet-mesh] for setup instructions.

## Step 9: Cleanup

First remove the user-owned Istio custom resources on the spokes, or disable their GitOps reconciliation, so the operator stops recreating control-plane resources. For the `Istio/default` example above:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl --context "$CLUSTER_NAME" delete istio.sailoperator.io default --ignore-not-found
done
```

Deleting the `MultiClusterMesh` requests deletion of its ManifestWorks and ManagedServiceAccounts. Kubernetes garbage collection removes its owned Certificates and ManifestWorkReplicaSet; OCM applies the resulting spoke cleanup asynchronously. The addon does not delete the external operator, OperatorPolicies, Placements, bindings, issuers, or cluster-scoped Istio/IstioCNI custom resources.

> **Warning:** Deleting a mesh also deletes the control plane namespace on each spoke cluster, including any Istio resources you deployed there.
> Back up your Istio configuration before deleting.

```bash
kubectl delete multiclustermesh -n mesh-system <mesh-name>
```

<!-- Reference links -->
[cert-manager]: https://cert-manager.io/
[clusteradm]: https://open-cluster-management.io/docs/getting-started/installation/start-the-control-plane/
[Gateway API]: https://gateway-api.sigs.k8s.io/
[fleet-mesh]: https://github.com/kiali/openshift-servicemesh-plugin/blob/main/docs/fleet-mesh/DEV-PREVIEW-GUIDE.md
[gpf]: https://open-cluster-management.io/docs/getting-started/integration/policy-controllers/
[kind]: https://kind.sigs.k8s.io/
[mwrs]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestworkreplicaset/
[clusterset]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/
[ManagedClusterSet]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/
[OperatorPolicy]: https://open-cluster-management.io/docs/getting-started/integration/policy-controllers/operator-policy/
[Placement]: https://open-cluster-management.io/docs/concepts/content-placement/placement/
[ocm-concepts]: https://open-cluster-management.io/docs/concepts/
[Plug-in CA]: https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/
[sail-multicluster]: https://github.com/istio-ecosystem/sail-operator/blob/main/docs/deployment-models/multicluster.adoc#multi-primary---multi-network
