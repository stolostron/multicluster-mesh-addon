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
The recommended approach uses [OperatorPolicy] from the governance-policy-framework, which distributes the operator to managed clusters using the same Placement-based targeting. This requires the governance-policy-framework and config-policy-controller add-ons on each managed cluster, with OperatorPolicy enabled.

Apply the sample, pointing the PlacementBinding at `mesh-placement` from Step 4. Kubernetes (Sail):

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

For the full walkthrough, including governance framework setup, version pinning, spoke verification, and adding clusters later, see [Install the Service Mesh Operator](operator-installation.md).

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

The recommended approach uses ACM policies to configure all clusters from the hub. No direct spoke access is needed. The policy samples read the control plane namespace from the MultiClusterMesh status using the `lookup` hub template function.

> **Note:** The east-west gateway uses [Gateway API]. Make sure the Gateway API CRDs are installed on each spoke before applying the policy. See the [manual guide](istio-manual.md) for the install command.

> **Warning:** The policy sample creates the cluster-scoped `Istio/default` resource. This means one mesh per spoke cluster. Running two meshes on the same cluster requires distinct Istio resource names plus revision, injection, discovery-selector, and gateway ownership configuration.

For OpenShift or ambient mode, install IstioCNI before applying the control plane policy. IstioCNI is a cluster-level prerequisite (like the operator), not a per-mesh resource. Apply it once with a Placement covering all clusters that need CNI, typically the same Placement used for the operator:

```bash
sed 's/<CNI_PLACEMENT_NAME>/mesh-placement/g' samples/policy-istiocni.yaml \
  | kubectl apply -n mesh-system -f -
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-istiocni -n mesh-system --timeout=10m
```

Then apply the control plane policy. Replace the placeholders with your values before applying:

```bash
sed -e 's/<MESH_NAMESPACE>/mesh-system/g' -e 's/<MESH_NAME>/basic-mesh/g' \
  -e 's/<PLACEMENT_NAME>/mesh-placement/g' \
  samples/policy-istio-control-plane.yaml \
  | kubectl apply -n mesh-system -f -
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-istio-basic-mesh -n mesh-system --timeout=15m
```

Verify per-cluster compliance:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl wait \
    --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_NAME}\")].compliant}=Compliant" \
    policy/policy-istio-basic-mesh -n mesh-system --timeout=15m
done
```

When new clusters are added to the Placement, the policies propagate to them automatically.

> **Tip:** The policy sample is a minimal starting point. To configure additional Istio fields (discovery selectors, pilot settings, proxy resource limits, etc.), edit the Istio `objectDefinition` under `spec.values` in the sample before applying. See the [Sail operator API reference][sail-api] for all available fields.

For direct spoke-cluster access without ACM policies, see [Configure Istio (Manual)](istio-manual.md).
For GitOps workflows with Kustomize, see [Configure Istio with PolicyGenerator](istio-policygenerator.md).

### Next Steps

Once the mesh is running, you can use [Fleet Service Mesh][fleet-mesh] for multi-cluster mesh observability.
Fleet Service Mesh is an OpenShift console plugin and requires an OpenShift cluster with ACM (Advanced Cluster Management).
See the [Fleet Service Mesh Dev Preview Guide][fleet-mesh] for setup instructions.

## Step 9: Cleanup

If you used PolicyGenerator, follow its [cleanup instructions](istio-policygenerator.md#cleanup) to remove the generated policy before deleting the MultiClusterMesh below.

First remove the Istio configuration. If you used the policy-based approach from Step 8, delete the policy and its binding on the hub:

```bash
kubectl delete policy policy-istio-basic-mesh -n mesh-system
kubectl delete placementbinding binding-istio-basic-mesh -n mesh-system
```

The sample policies set `pruneObjectBehavior: DeleteIfCreated`, so deleting them also removes the Istio CR and east-west gateway they created on the spokes. The same applies to the IstioCNI policy from Step 8: deleting it removes the `IstioCNI` CR and the `istio-cni` namespace it created. Pruning is asynchronous and only covers objects the policy created.

If you used the [manual approach](istio-manual.md), delete the Istio CR on each spoke directly:

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
[sail-api]: https://docs.sailoperator.io/docs/api-reference/sailoperator.io/
[sail-multicluster]: https://github.com/istio-ecosystem/sail-operator/blob/main/docs/deployment-models/multicluster.adoc#multi-primary---multi-network
