# User Guide

End-to-end walkthrough for setting up a multi-cluster service mesh.
For a quick overview of what the addon does, see the [architecture](architecture.md).
New to OCM? See the [OCM concepts docs][ocm-concepts] for background on terms like ManagedCluster and ManifestWork.

This guide uses predefined sample manifests suitable for development and testing.
For production, use your own configurations (e.g., your organization's CA instead of the self-signed example).

> **Note:** Commands use `kubectl`. On OpenShift, `oc` is a drop-in replacement for every command in this guide.

## Prerequisites

- An OCM hub cluster with managed clusters registered
- [cert-manager] installed on the hub cluster (mints per-cluster intermediate CAs for mTLS trust)
- [OLM] installed on managed clusters (installs the service mesh operator)
- A [ManagedClusterSet] with `ExclusiveClusterSetLabel` selector (the default type, where each cluster belongs to exactly one set).
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

If you don't already have a ManagedClusterSet, create one and assign your clusters using [`clusteradm`][clusteradm]:

```bash
clusteradm create clusterset mesh-cluster-set
clusteradm clusterset set mesh-cluster-set --clusters cluster1,cluster2,...
```

Verify the clusters are in the set:

```bash
clusteradm get clustersets
```

### Network identity (optional)

In a multi-network mesh, each cluster needs a network identity.
By default the addon uses the managed cluster name.
To override it, label the ManagedCluster on the hub before creating the mesh:

```bash
kubectl label managedcluster cluster1 topology.istio.io/network=network-a
kubectl label managedcluster cluster2 topology.istio.io/network=network-b
```

The addon applies this value as `topology.istio.io/network` on the control plane namespace.
Your Istio CR and east-west gateway must use the same value (see [Step 6](#step-6-configure-istio)).

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

## Step 4: Create a MultiClusterMesh

The CRD defaults target OSSM on OpenShift (`servicemeshoperator3` from `redhat-operators`).
For vanilla Kubernetes clusters, override `spec.operator` fields to use Sail.
See [samples/basic.yaml](../samples/basic.yaml) for a Sail example and [samples/openshift.yaml](../samples/openshift.yaml) for OSSM.

OpenShift (OSSM):

```bash
kubectl apply -n mesh-system -f samples/openshift.yaml
```

Kubernetes (Sail):

```bash
kubectl apply -n mesh-system -f samples/basic.yaml
```

Update `spec.clusterSet` to match your ManagedClusterSet name.
See the [API reference](api-reference.md) for all available fields, or [samples/complete.yaml](../samples/complete.yaml) for a fully annotated example.

## Step 5: Verify the Setup

Check mesh status:

```bash
kubectl get multiclustermesh -n mesh-system -o yaml
```

When all operators are installed, the mesh shows `Ready=True`.
That means operator installation succeeded, not that cross-cluster traffic works yet.
See the [API reference](api-reference.md#status-conditions) for all reported conditions.

If something isn't working, see the [troubleshooting guide](troubleshooting.md).

## Step 6: Configure Istio

The addon installs the operator and distributes trust, but you configure the mesh control plane on each cluster yourself.
Both OSSM 3.x and upstream Sail use the `sailoperator.io` API group.
See the [Sail operator multicluster docs][sail-multicluster] for the full procedure.

> **Note:** Automated remote secret distribution is not yet implemented.
> For now, follow the [manual remote secret exchange][sail-multicluster] procedure from the Sail docs.
> This step will be removed once the addon handles it automatically.

To get started quickly, use the provided sample manifests from [samples/istio/](../samples/istio/).

The following commands run against the spoke clusters (not the hub).
Make sure your kubeconfig has the contexts for each managed cluster.

The east-west gateway sample uses [Gateway API].
On OCP 4.19+ the CRDs ship by default, while on [kind] or vanilla K8s clusters they need to be installed:

```bash
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/experimental-install.yaml
```

Notes:
* `MESH_NAME` should match the `MultiClusterMesh` CR name (used as CA trust domain).
* `NETWORK` must match the `topology.istio.io/network` label on the control plane namespace (defaults to the cluster name).
* `IstioCNI` is required on OpenShift and for ambient mode, otherwise you can skip it.

```bash
export MESH_NAME=my-mesh

for CLUSTER_NAME in cluster1 cluster2; do
  export CLUSTER_NAME
  # If you set a custom network label in Step 2, override NETWORK here to match it.
  export NETWORK=$CLUSTER_NAME

  kubectl apply --context $CLUSTER_NAME -f samples/istio/istiocni.yaml
  envsubst < samples/istio/istio.yaml | kubectl apply --context $CLUSTER_NAME -f -
  envsubst < samples/istio/eastwest-gateway.yaml | kubectl apply --context $CLUSTER_NAME -f -
done
```

## Step 7: Cleanup

Deleting the `MultiClusterMesh` CR removes all addon-managed resources: operator ManifestWorks (if no other mesh needs the operator on that cluster), CA certificate secrets, and ManagedServiceAccounts.

> **Warning:** Deleting a mesh also deletes the control plane namespace on each spoke cluster, including any Istio resources you deployed there.
> Back up your Istio configuration before deleting.

```bash
kubectl delete multiclustermesh -n mesh-system <mesh-name>
```

<!-- Reference links -->
[cert-manager]: https://cert-manager.io/
[clusteradm]: https://open-cluster-management.io/docs/getting-started/installation/start-the-control-plane/
[Gateway API]: https://gateway-api.sigs.k8s.io/
[kind]: https://kind.sigs.k8s.io/
[ManagedClusterSet]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/
[mwrs]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestworkreplicaset/
[ocm-concepts]: https://open-cluster-management.io/docs/concepts/
[OLM]: https://olm.operatorframework.io/
[Plug-in CA]: https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/
[sail-multicluster]: https://github.com/istio-ecosystem/sail-operator/blob/main/docs/deployment-models/multicluster.adoc#multi-primary---multi-network
