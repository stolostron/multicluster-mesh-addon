# Multi Cluster Mesh Add On

A pluggable addon working on [Open Cluster Management (OCM)](https://open-cluster-management.io/).

## What is Multi Cluster Mesh Add On?

The Multi Cluster Mesh Add On automates the complex setup of multi-cluster service mesh deployments using the [Sail Operator](https://github.com/istio-ecosystem/sail-operator/).
It eliminates the manual, error-prone process of establishing trust between clusters, installing mesh components, and sharing credentials for endpoint discovery.

The addon acts as an infrastructure provisioner and federation broker, automating:

- **Trust Distribution**: Establishes mTLS trust between clusters using cert-manager to distribute intermediate CAs
- **Operator Lifecycle**: Installs and manages the Sail Operator (Istio) on managed clusters via OLM
- **Endpoint Discovery**: Automates secure, rotatable token exchange between control planes using ManagedServiceAccount

## Architecture

The addon leverages OCM's hub-and-spoke architecture and integrates with core OCM components:

- **ManagedClusterSet**: Defines cluster membership in the mesh
- **ManifestWork**: Distributes certificates and configurations to spoke clusters
- **ManagedServiceAccount**: Provides short-lived discovery tokens for cross-cluster endpoint resolution
- **cert-manager**: Manages Root CA lifecycle and mints intermediate certificates for each cluster

The addon manages the "plumbing" (trust and connectivity) while users configure their mesh control planes using GitOps or the Istio CR directly.

## Getting Started

### Prerequisites

- OCM hub cluster
- [cert-manager](https://cert-manager.io/) installed on the hub cluster
- Managed clusters registered with OCM

### Installation

#### Development Deployment

**Prerequisites:**
- Valid kubeconfig pointing to an existing cluster
- ACM or OCM (Advanced Cluster Management or Open Cluster Management) installed on the cluster
- `kubectl` CLI installed
- `make` and Go toolchain installed
- Push access to the container registry (default: `quay.io/sail-dev`, override with `REGISTRY_BASE`)

To build and deploy the controller to your cluster:

```bash
# Build, push image, and deploy to cluster
make deploy

# Or run individual steps:
make images      # Build container image
make push        # Push to registry
make deploy      # Apply CRDs and deployment
```

The `deploy` target will:
1. Build and push the container image to the registry (default: `quay.io/sail-dev`)
2. Apply the CRD manifests from `config/crd/`
3. Deploy the controller with the built image

**Configuration:**
- `REGISTRY_BASE`: Override the image registry (default: `quay.io/sail-dev`)
- `IMG`: Full image reference (default: `${REGISTRY_BASE}/multicluster-mesh-addon:${GIT_VERSION}`)

To remove the deployment:

```bash
make undeploy
```

#### Running Locally

To run the controller from localhost for development:

```bash
# Generate and install CRDs
make gen-crds
kubectl apply -f config/crd/

# Build the binary
make build

# Run with leader election disabled (no namespace/RBAC requirements). It's necessary to specify the kubeconfig explicitly.
./bin/multicluster-mesh-addon controller --leader-elect=false --kubeconfig=/path/to/kubeconfig
```

**Prerequisites:**
- CRDs must be installed in the cluster (via `kubectl apply -f config/crd/`)
- Valid kubeconfig pointing to your cluster (specify with `--kubeconfig` flag)

The controller runs against the specified kubeconfig context. Leader election is disabled to avoid requiring the `multicluster-mesh-system` namespace and associated RBAC permissions during local development.

If you need to test with leader election enabled:

```bash
# Create the required namespace
kubectl create namespace multicluster-mesh-system

# Run with leader election (default)
./bin/multicluster-mesh-addon controller --kubeconfig=/path/to/kubeconfig
```

### Usage

Create a namespace to host your multi cluster mesh resources.
Inside the namespace, create an `Issuer` resource that will act as the Root CA.

Create a `MultiClusterMesh` resource on the hub cluster:

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: prod-mesh
  namespace: mesh-ns
spec:
  clusterSet: finance-prod
  controlPlane:
    namespace: istio-system
  operator:
    channel: "stable"
    source: redhat-operators
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-issuer
    discovery:
      tokenValidity: "1w"
```

The addon will:
1. Install the Sail Operator on all clusters in the `finance-prod` ManagedClusterSet
2. Generate and distribute intermediate CA certificates to establish trust
3. Exchange discovery tokens between clusters for endpoint resolution
4. Manage automatic rotation of certificates and tokens

Users must then create `Istio` Custom Resources on each spoke cluster to configure the mesh control plane.
