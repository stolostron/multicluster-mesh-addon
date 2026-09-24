# Development Environment

## Local Environment (Recommended)

The fastest and simplest way to get a working environment is the [kind]-based setup.
It provisions a complete multi-cluster topology (1 hub + 2 managed clusters) with OCM, OLM, cert-manager, and the addon deployed:

```bash
make dev-env
```

The command also creates the `mesh-cluster-set` ManagedClusterSet and assigns the managed clusters to it.
If you need to create that resource separately after joining clusters, run:

```bash
make create-clusterset
```

This takes ~5 minutes.
When you're done with it, tear it down with:

```bash
make dev-clean
```

## Local Demo Environment

To practice the railroaded Sail version of the customer demo, provision one hub and three managed Kind clusters.
The demo environment intentionally does not create a ManagedClusterSet.
It prepares the add-on image and MetalLB, but leaves the controller and Gateway API uninstalled so the demo can show those steps.
The demo creates the set and adds the first two clusters interactively.
The third cluster is registered and available, then added to the set during the scale-out portion of the demo.

```bash
make demo-dev-env
make demo-sail
```

Clean up the three-spoke environment with:

```bash
make demo-dev-clean
```

Run `make help` to see all available targets, including individual steps if you need to re-run only part of the setup.

## Bring Your Own Clusters (OCP)

If you have an existing OCP hub with OCM and managed clusters, you can build and deploy the addon directly:

```bash
make deploy
```

This builds the container image, pushes it to the registry, and installs the controller via Helm.

To run the interactive OpenShift demo, both `DEMO_KUBECONFIG` and `SPOKE_CLUSTERS` are required.
`DEMO_KUBECONFIG` is a colon-separated list of four kubeconfig file paths ordered as hub, first spoke, second spoke, and third spoke.
`SPOKE_CLUSTERS` is a space-separated list of exactly three ACM managed-cluster names matching the spoke kubeconfigs:

```bash
make demo \
  DEMO_KUBECONFIG=/path/to/hub.config:/path/to/cluster1.config:/path/to/cluster2.config:/path/to/cluster3.config \
  SPOKE_CLUSTERS="cluster1 cluster2 cluster3"
```

The demo checks that all four kubeconfig files exist and are readable before it starts.
The three managed clusters must not already belong to another ManagedClusterSet (the demo creates one interactively).
Use `DEMO_DRY_RUN=1` to print the demo flow without changing the clusters.

To use a different registry or tag:

```bash
make deploy HUB=quay.io/myorg TAG=dev-latest
```

**Prerequisites:**
- Valid kubeconfig pointing to your OCP hub cluster
- OCM installed on the cluster (or [Red Hat Advanced Cluster Management][ACM] which includes OCM out of the box)
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
[ACM]: https://www.redhat.com/en/technologies/management/advanced-cluster-management
[kind]: https://kind.sigs.k8s.io/
