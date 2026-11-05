# Configure Istio (Manual)

Configure Istio on each managed cluster using direct kubectl access.

> **Tip:** The recommended approach configures all clusters from the hub using ACM policies with no spoke access needed. See [Step 8 of the user guide](user-guide.md#step-8-configure-istio) for that path.

This page covers the manual approach using `envsubst` and direct spoke kubeconfig contexts. It is useful when the governance-policy-framework is not available, or when you prefer to run commands against the spokes directly.

Both OSSM 3.x and upstream Sail use the `sailoperator.io` API group.
See the [Sail operator multicluster docs][sail-multicluster] for the full procedure.

## Prerequisites

- The MultiClusterMesh must be created and reporting `status.controlPlaneNamespace`. See the [user guide](user-guide.md#step-6-create-a-multiclustermesh).
- Your kubeconfig must have contexts for each managed cluster.
- The sample manifests from [samples/istio/](../samples/istio/).

Run the examples below in the same Bash session with the current kubeconfig context set to the hub.

List the ManagedCluster names and their kubeconfig contexts in the same order. Replace the example context names with those from your kubeconfig:

```bash
CLUSTER_NAMES=(cluster1 cluster2)
CLUSTER_CONTEXTS=("context-for-cluster1" "context-for-cluster2")
```

## Install Gateway API CRDs

The east-west gateway sample uses [Gateway API]. Check the CRDs on each spoke, and install a compatible version supported by your Istio/operator release when they are absent:

```bash
for CONTEXT in "${CLUSTER_CONTEXTS[@]}"; do
  if ! kubectl --context "$CONTEXT" get crd \
    gateways.gateway.networking.k8s.io gatewayclasses.gateway.networking.k8s.io >/dev/null 2>&1; then
    kubectl --context "$CONTEXT" apply --server-side \
      -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
  fi
done
```

## Variable reference

The sample manifests use four variables that must be set per cluster:

* `MESH_NAME` -- the `MultiClusterMesh` name. This is also the CA trust domain.
* `CP_NAMESPACE` -- `status.controlPlaneNamespace` from the MultiClusterMesh. The Istio samples substitute it into the control plane namespace.
* `CLUSTER_NAME` -- the ManagedCluster name.
* `NETWORK` -- the `topology.istio.io/network` label on the control plane namespace (defaults to the cluster name).

## Install IstioCNI (OpenShift / ambient)

IstioCNI is required on OpenShift and for ambient mode. Create its namespace and apply the sample on those clusters before installing Istio. It can be skipped for sidecar mode on vanilla Kubernetes.

```bash
for CONTEXT in "${CLUSTER_CONTEXTS[@]}"; do
  kubectl --context "$CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
  kubectl --context "$CONTEXT" apply -f samples/istio/istiocni.yaml
done
```

## Install the control plane and gateway

Export the mesh-level variables, then loop over each cluster:

```bash
export MESH_NAME=basic-mesh
export CP_NAMESPACE="$(kubectl get multiclustermesh "$MESH_NAME" -n mesh-system -o jsonpath='{.status.controlPlaneNamespace}')"
test -n "$CP_NAMESPACE" || { echo "Mesh control plane namespace is not available yet" >&2; exit 1; }

for i in "${!CLUSTER_NAMES[@]}"; do
  CLUSTER_NAME="${CLUSTER_NAMES[$i]}"
  CONTEXT="${CLUSTER_CONTEXTS[$i]}"
  export CLUSTER_NAME
  NETWORK="$(kubectl get managedcluster "$CLUSTER_NAME" -o jsonpath='{.metadata.labels.topology\.istio\.io/network}')" || {
    echo "Failed to read network label for ManagedCluster $CLUSTER_NAME" >&2
    exit 1
  }
  export NETWORK="${NETWORK:-$CLUSTER_NAME}"

  envsubst < samples/istio/istio.yaml | kubectl apply --context "$CONTEXT" -f -
  kubectl --context "$CONTEXT" wait --for=create \
    deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
  kubectl --context "$CONTEXT" wait --for=condition=Available \
    deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
  envsubst < samples/istio/eastwest-gateway.yaml | kubectl apply --context "$CONTEXT" -f -
done
```

> **Tip:** The `samples/istio/istio.yaml` file is a minimal template. To configure additional Istio fields (discovery selectors, pilot settings, proxy resource limits, etc.), edit `spec.values` in the sample before substituting variables. See the [Sail operator API reference][sail-api] for all available fields.

## Notes

* The Istio sample creates the cluster-scoped resource `Istio/default`. Use this example for one mesh per spoke. Multiple meshes require distinct Istio resource names and appropriate revision/injection and discovery-selector configuration.
* `NETWORK` is read from the `topology.istio.io/network` label on the ManagedCluster. If the label is absent, the cluster name is used as the network identity. Each cluster on a separate network needs a unique value.

[sail-api]: https://docs.sailoperator.io/docs/api-reference/sailoperator.io/
[sail-multicluster]: https://docs.sailoperator.io/docs/multicluster/multi-primary-multi-network
[Gateway API]: https://gateway-api.sigs.k8s.io/
