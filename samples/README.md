# Samples

Example manifests for common configurations.

## Hub (MultiClusterMesh)

| Manifest | Use when |
|----------|----------|
| [cert-manager-issuer.yaml](cert-manager-issuer.yaml) | Self-signed trust chain for testing |
| [openshift.yaml](openshift.yaml) | OpenShift / OSSM (matches CRD defaults) |
| [basic.yaml](basic.yaml) | kind / vanilla Kubernetes with Sail |
| [complete.yaml](complete.yaml) | Fully annotated field reference |
| [pinned-version.yaml](pinned-version.yaml) | Pin the operator CSV version |

Apply hub samples on the hub cluster in your mesh namespace (for example `mesh-system`).
Update `spec.clusterSet` to match your ManagedClusterSet.

## Spoke (Istio)

| Manifest | Purpose |
|----------|---------|
| [istio/istio.yaml](istio/istio.yaml) | Istio control plane (`MESH_NAME`, `CLUSTER_NAME`, `NETWORK`) |
| [istio/eastwest-gateway.yaml](istio/eastwest-gateway.yaml) | East-west Gateway API gateway |
| [istio/istiocni.yaml](istio/istiocni.yaml) | IstioCNI (required on OpenShift and ambient) |

Apply spoke samples on each managed cluster.
See the [User Guide](../docs/user-guide.md#step-6-configure-istio) for the full procedure.
