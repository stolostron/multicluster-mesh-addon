# Development Environment

## Local Environment (Recommended)

The fastest and simplest way to get a working environment is the [kind]-based setup.
It provisions one hub and two managed Kind clusters with OCM, the ManifestWorkReplicaSet feature, OLM, cert-manager, ManagedServiceAccount, and the addon controller. It also installs the governance policy framework and config-policy-controller addons, enables OperatorPolicy, and waits for the Sail operator policy to be compliant:

```bash
make dev-env
```

The command also creates the `mesh-cluster-set` ManagedClusterSet and assigns the managed clusters to it.
It does not create a MultiClusterMesh or install Istio control planes. Follow the [user guide](../user-guide.md) for mesh setup, or use the e2e suites, which create their own meshes. The developer test trust chain uses `ClusterIssuer/mesh-test-root-ca`; the user guide creates a separate namespaced issuer.

If you need to create that resource separately after joining clusters, run:

```bash
make create-clusterset
```

Initial setup time depends on image downloads, OLM installation, and operator-policy compliance. It can take approximately 5 minutes. When you're done with it, tear it down with:

```bash
make dev-clean
```

## Local Demo Environment

To practice the railroaded Sail version of the customer demo, provision one hub and three managed Kind clusters.
The demo environment intentionally does not create a ManagedClusterSet.
It prepares the add-on image and MetalLB, but leaves the controller and Gateway API uninstalled so the demo can show those steps.
`make demo-sail` installs the controller from the local chart, using the image loaded into the hub.
The demo creates the set and adds the first two clusters interactively.
The third cluster is registered and available, then added to the set during the scale-out portion of the demo.
The script enables the hub governance policy controllers and the governance/config-policy controllers on all three spokes before creating a policy.
Both the kind and OpenShift flows use `meshctl scaffold` to generate user-owned hub Policies for Istio and the east-west Gateway.
On OpenShift, the demo also applies a separate IstioCNI policy; the Istio policy waits for its readiness check.
The Policies install resources on the third spoke when it joins the Placement.
The generated Istio policy remains at `.kube/istio-installation.yaml`; on OpenShift, the CNI policy remains at `.kube/istiocni-installation.yaml`.

```bash
make demo-dev-env
make demo-sail
```

Clean up the three-spoke environment with:

```bash
make demo-dev-clean
```

For an unattended run of the local demo, keep the usual `.kube` development environment and skip the interactive pauses:

```bash
DEMO_AUTO_CONTINUE=1 DEMO_COMMAND_PAUSE=0 TYPEWRITER_DELAY=0 make demo-sail
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
- cert-manager and ManagedServiceAccount installed on the hub before starting the controller
- ManifestWorkReplicaSet enabled, and ManagedServiceAccount agents available on selected managed clusters
- A separately installed mesh operator on selected clusters before mesh setup; OperatorPolicy is the documented installation option
- Push access to the container registry

`make deploy` does not install these prerequisites. See the [user guide](../user-guide.md#prerequisites).

To remove the deployment and all meshes:

```bash
make undeploy
```

This target requests deletion of every MultiClusterMesh in every namespace, uninstalls the Helm release, and deletes the MultiClusterMesh CRD. Mesh cleanup would delete control-plane namespaces on spokes.

## Running Locally

For faster iteration, you can run the controller from your machine instead of deploying it to the cluster.

> **Note:** Your kubeconfig must have enough permissions to manage the addon's resources (ManifestWorks, Certificates, ManagedServiceAccounts, etc.).

Generate and install the CRDs, create the controller namespace, then build and run:

```bash
make gen-crds
kubectl apply -f chart/crds/
kubectl create namespace multicluster-mesh-system
make build
POD_NAMESPACE=multicluster-mesh-system ./bin/multicluster-mesh-addon controller --kubeconfig=/path/to/kubeconfig
```

<!-- Reference links -->
[ACM]: https://www.redhat.com/en/technologies/management/advanced-cluster-management
[kind]: https://kind.sigs.k8s.io/
