# Samples

Example manifests for common configurations.

## Hub (MultiClusterMesh)

| Manifest | Use when |
|----------|----------|
| [cert-manager-issuer.yaml](cert-manager-issuer.yaml) | Self-signed trust chain for testing |
| [openshift.yaml](openshift.yaml) | OpenShift with user-installed OSSM |
| [basic.yaml](basic.yaml) | Minimal mesh with a user-installed service mesh operator |
| [complete.yaml](complete.yaml) | Fully annotated field reference |

Apply hub samples on the hub cluster in your mesh namespace (for example `mesh-system`).
Update `spec.placementRef.name` to match your Placement name.

## Spoke (Istio)

| Manifest | Purpose |
|----------|---------|
| [istio/istio.yaml](istio/istio.yaml) | Istio control plane (`MESH_NAME`, `CLUSTER_NAME`, `NETWORK`) |
| [istio/eastwest-gateway.yaml](istio/eastwest-gateway.yaml) | East-west Gateway API gateway |
| [istio/istiocni.yaml](istio/istiocni.yaml) | IstioCNI (required on OpenShift and ambient) |

Install the service mesh operator yourself, then apply spoke samples on each managed cluster.
See the [User Guide](../docs/user-guide.md#step-7-configure-istio) for the full procedure.
