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

For detailed architecture and design decisions, see [docs/design.md](docs/design.md).

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

#### Local Kind+OCM Dev Environment

Provisions a complete multi-cluster topology (1 hub + 2 managed clusters) using Kind and OCM, then builds and deploys the addon controller:

```bash
make dev-env
```

Individual targets are also available:

| Target                      | Description                                            |
|-----------------------------|--------------------------------------------------------|
| `make create-clusters`      | Create three Kind clusters (hub, cluster1, cluster2)   |
| `make install-olm`          | Install OLM on managed clusters                        |
| `make install-cert-manager` | Install cert-manager on the hub cluster                |
| `make init-ocm`             | Initialize hub as OCM control plane                    |
| `make join-clusters`        | Register managed clusters and create ManagedClusterSet |
| `make deploy-addon`         | Build and deploy addon to the hub Kind cluster         |
| `make setup-mesh`           | Create cert-manager trust chain and MultiClusterMesh CR|
| `make dev-clean`            | Destroy clusters and remove `.kube/`                   |
| `make dev-clean-meshes`     | Delete mesh resources only (re-run `setup-mesh` to recreate) |

**Configuration (override via environment or command-line):**

```bash
make dev-env K8S_VERSION=v1.31.0 OLM_VERSION=v0.42.0
```

##### Known Issues

**"Too many open files" on Linux:**
Kind clusters may fail to start or pods may crash with `too many open files` errors due to low inotify limits. Increase them on the host:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

To persist across reboots, add to `/etc/sysctl.conf`:

```
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
```

See the [Kind known issues](https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files) documentation for more details.

### Usage

#### Quick Start

1. Create a namespace for your mesh resources:
```bash
kubectl create namespace mesh-system
```

2. Create the cert-manager trust chain (ClusterIssuer, root CA Certificate, and CA-backed Issuer):
```bash
kubectl apply -f samples/cert-manager-issuer.yaml
```

3. Deploy a basic MultiClusterMesh:
```bash
kubectl apply -f samples/basic.yaml
```

> **Note:** The `basic.yaml` sample uses `clusterSet: mesh-cluster-set`. Update this field to match your actual ManagedClusterSet name as needed.

For more configuration options, see the [samples](./samples/) directory:

- **[basic.yaml](./samples/basic.yaml)** - Minimal configuration using defaults
- **[complete.yaml](./samples/complete.yaml)** - All available fields with documentation
- **[openshift.yaml](./samples/openshift.yaml)** - OpenShift-specific configuration
- **[pinned-version.yaml](./samples/pinned-version.yaml)** - Version pinning with manual approval
- **[cert-manager-issuer.yaml](./samples/cert-manager-issuer.yaml)** - cert-manager trust chain (ClusterIssuer + root CA + Issuer)

#### What the Addon Does

The addon will:
1. Install the Sail Operator on all clusters in the referenced ManagedClusterSet
2. Generate and distribute intermediate CA certificates to establish trust
3. Exchange discovery tokens between clusters for endpoint resolution
4. Manage automatic rotation of certificates and tokens

Users must then create `Istio` Custom Resources on each spoke cluster to configure the mesh control plane.
