# Multi Cluster Mesh Add On

Automates [multi-cluster Istio service mesh][sail] setup on [Open Cluster Management (OCM)][OCM].

The addon installs service mesh operators on managed clusters and distributes CA certificates for mTLS trust.
You bring the clusters and configure Istio; the addon handles the rest.

## Documentation

### For Users

- [Quick Start](#quick-start) - Get up and running in minutes
- [Architecture](docs/architecture.md) - How the addon works
- [User Guide](docs/user-guide.md) - Detailed setup walkthrough with explanations
- [API Reference](docs/api-reference.md) - `MultiClusterMesh` CRD fields and examples
- [Troubleshooting](docs/troubleshooting.md) - Common issues and resolutions
- [Helm Chart](chart/README.md) - Installation options
- [Samples](samples/) - Example manifests for common configurations

### For Contributors

- [Contributing](CONTRIBUTING.md) - PR process, DCO, development workflow, doc-sync requirements
- [Development](docs/dev/README.md) - Building, dev environment, testing, design

## Quick Start

> **Note:** Requires an OCM hub with [cert-manager] and managed clusters with [OLM]. See [prerequisites](docs/user-guide.md#prerequisites) for details.

> **Note:** Commands use `kubectl`; on OpenShift, `oc` is a drop-in replacement.

```bash
# Install the addon
helm repo add multicluster-mesh-addon https://stolostron.github.io/multicluster-mesh-addon
helm repo update
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --namespace multicluster-mesh-system \
  --create-namespace

# Create a ClusterSet and assign clusters
clusteradm create clusterset mesh-cluster-set
clusteradm clusterset set mesh-cluster-set --clusters cluster1,cluster2

# Set up trust chain and create a mesh
kubectl create namespace mesh-system
kubectl apply -n mesh-system -f samples/cert-manager-issuer.yaml
kubectl apply -n mesh-system -f samples/basic.yaml
```

> **Note:** For OpenShift, use `samples/openshift.yaml` instead of `samples/basic.yaml`.

This installs the operator and distributes trust. For a working multi-cluster mesh setup, see the [User Guide](docs/user-guide.md) for prerequisites, what each step does, verification, and next steps (configuring Istio).

<!-- Reference links -->
[cert-manager]: https://cert-manager.io/
[OCM]: https://open-cluster-management.io/
[OLM]: https://olm.operatorframework.io/
[sail]: https://github.com/istio-ecosystem/sail-operator
