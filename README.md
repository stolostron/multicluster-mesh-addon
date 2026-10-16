# Multi Cluster Mesh Add On

Automates [multi-cluster Istio service mesh][sail] setup on [Open Cluster Management (OCM)][OCM] (or [Red Hat Advanced Cluster Management][ACM], which includes OCM).

The addon creates the control plane namespace, distributes CA certificates for mTLS trust, and exchanges endpoint-discovery credentials.
Registered managed clusters and a service mesh operator are prerequisites. OperatorPolicy is the supported way to install the operator.
Istio itself must be installed manually on each managed cluster for now. Automating that installation will be addressed in a future PR.

## Documentation

### For Users

- [Quick Start](#quick-start) - Get up and running in minutes
- [Architecture](docs/architecture.md) - How the addon works
- [User Guide](docs/user-guide.md) - Detailed setup walkthrough with explanations
- [OCP ACM and OSSM Demo](docs/demo.md) - Manual customer demo walkthrough
- [API Reference](docs/api-reference.md) - `MultiClusterMesh` CRD fields and examples
- [Troubleshooting](docs/troubleshooting.md) - Common issues and resolutions
- [Fleet Service Mesh (Dev Preview)][fleet-mesh] - Multi-cluster mesh observability
- [Helm Chart](chart/README.md) - Installation options
- [Samples](samples/) - Example manifests for common configurations

### For Contributors

- [Contributing](CONTRIBUTING.md) - PR process, DCO, development workflow, doc-sync requirements
- [Development](docs/dev/README.md) - Building, dev environment, testing, design

## Quick Start

> **Note:** Requires an OCM hub with [cert-manager] and the [governance-policy-framework][gpf], plus [OLM] on the managed clusters. OperatorPolicy must be enabled on each spoke (`operator-policy-disabled=false` on the config-policy-controller add-on) or the policy wait below does not succeed. See [prerequisites](docs/user-guide.md#prerequisites) for details.

> **Note:** Commands use `kubectl`; on OpenShift, `oc` is a drop-in replacement for the `kubectl` commands.

```bash
# Install the addon
helm repo add multicluster-mesh-addon https://stolostron.github.io/multicluster-mesh-addon
helm repo update
helm install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
  --namespace multicluster-mesh-system \
  --create-namespace

# Set up trust chain
kubectl create namespace mesh-system
kubectl apply -n mesh-system -f samples/cert-manager-issuer.yaml

# Bind a ClusterSet to the mesh namespace so the Placement can select clusters.
# This example uses the ACM "global" set for simplicity; for production, create
# a dedicated ManagedClusterSet (see user guide).
kubectl apply -n mesh-system -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSetBinding
metadata:
  name: global
spec:
  clusterSet: global
EOF

# Create a Placement that selects all clusters in the bound ClusterSet
kubectl apply -n mesh-system -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: mesh-placement
spec: {}
EOF

# Install the mesh operator before creating the mesh.
# The Sail sample binds to a Placement named mesh-operator-placement.
kubectl apply -n mesh-system -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: mesh-operator-placement
spec: {}
EOF
kubectl apply -n mesh-system -f samples/operator-policy-sail.yaml
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-sail-operator -n mesh-system --timeout=15m

# Create the mesh (uses mesh-placement to determine target clusters)
kubectl apply -n mesh-system -f samples/basic.yaml
```

> **Note:** For OpenShift, use `samples/operator-policy-ossm.yaml` and `samples/openshift.yaml` instead of the Sail policy and `samples/basic.yaml`.
> Wait for `policy/policy-ossm-operator` to become Compliant before creating the mesh.

This example uses the ACM `global` ClusterSet, which includes all managed clusters.
For production, create a dedicated [ManagedClusterSet] with only the clusters that should participate in the mesh and bind it to the mesh namespace.
See the [User Guide](docs/user-guide.md) for a complete walkthrough including prerequisites, dedicated ClusterSet setup, verification, and configuring Istio on the spoke clusters.

[ManagedClusterSet]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/

<!-- Reference links -->
[ACM]: https://www.redhat.com/en/technologies/management/advanced-cluster-management
[cert-manager]: https://cert-manager.io/
[gpf]: https://open-cluster-management.io/docs/governance/policy/policy-framework/
[OCM]: https://open-cluster-management.io/
[OLM]: https://olm.operatorframework.io/
[fleet-mesh]: https://github.com/kiali/openshift-servicemesh-plugin/blob/main/docs/fleet-mesh/DEV-PREVIEW-GUIDE.md
[sail]: https://github.com/istio-ecosystem/sail-operator
