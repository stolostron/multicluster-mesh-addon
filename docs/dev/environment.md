# Development Environment

## Local Environment (Recommended)

The fastest and simplest way to get a working environment is the [kind]-based setup.
It provisions a complete multi-cluster topology (1 hub + 2 managed clusters) with OCM, OLM, cert-manager, and the addon deployed:

```bash
make dev-env
```

This takes ~5 minutes.
When you're done with it, tear it down with:

```bash
make dev-clean
```

Run `make help` to see all available targets, including individual steps if you need to re-run only part of the setup.

## Bring Your Own Clusters (OCP)

If you have an existing OCP hub with OCM and managed clusters, you can build and deploy the addon directly:

```bash
make deploy
```

This builds the container image, pushes it to the registry, and installs the controller via Helm.

To use a different registry or tag:

```bash
make deploy HUB=quay.io/myorg TAG=dev-latest
```

**Prerequisites:**
- Valid kubeconfig pointing to your OCP hub cluster
- OCM installed on the cluster
- Push access to the container registry

To remove the deployment:

```bash
make undeploy
```

## Running Locally

For faster iteration, you can run the controller from your machine instead of deploying it to the cluster.

> **Note:** Your kubeconfig must have enough permissions to manage the addon's resources (ManifestWorks, Certificates, ManagedServiceAccounts, etc.).

Generate and install the CRDs, create the controller namespace, then build and run:

```bash
make gen-crds
kubectl apply -f chart/crds/
kubectl create namespace multicluster-mesh-system
make build
./bin/multicluster-mesh-addon controller --kubeconfig=/path/to/kubeconfig
```

<!-- Reference links -->
[kind]: https://kind.sigs.k8s.io/
