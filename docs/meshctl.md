# meshctl

`meshctl` creates an editable ACM policy bundle for installing Istio and an east-west Gateway on clusters selected for a `MultiClusterMesh`.
The PoC uses the `sailoperator.io/v1` resources shared by compatible Sail and OSSM 3.x installations. It does not change the add-on controller or the MCM API.

## Build and generate

Build the CLI, then provide mesh identity, namespace, Placement, and a component version supported by the installed operator:

```bash
make build-meshctl
bin/meshctl scaffold istio basic-mesh \
  --namespace mesh-system \
  --placement mesh-placement \
  --istio-version v1.30.4 \
  > mesh-installation.yaml
```

The CLI is entirely offline. It does not read a kubeconfig or contact the hub.
`--placement` is required for both commands, and `--istio-version` is required for `scaffold istio`.
The namespace defaults to `default`; `-n` is an alias for `--namespace`.
The Istio resource name defaults to `default`, and the Gateway name defaults to `eastwestgateway`.
Use `--policy-name`, `--istio-name`, and `--gateway-name` to change generated names.

`scaffold istio` prints a root OCM `Policy` and a `PlacementBinding`. Its spoke `ConfigurationPolicy` creates the Sail `Istio` and Gateway API `Gateway` resources.
The generated `object-templates` keeps both resources as structured YAML. YAML anchors reuse the control-plane namespace and network expressions in the Istio and Gateway objects, and share the mesh name between Istio values.
The hub expressions still resolve separately for each target cluster, so its ManagedCluster name and network label remain cluster-specific.

For clusters that require Istio CNI, generate a separate policy first:

```bash
bin/meshctl scaffold istiocni \
  --namespace mesh-system \
  --placement mesh-placement \
  --policy-name istiocni-installation-basic-mesh \
  > istiocni-installation.yaml

bin/meshctl scaffold istio basic-mesh \
  --namespace mesh-system \
  --placement mesh-placement \
  --istio-version v1.30.4 \
  --depends-on-istiocni istiocni-installation-basic-mesh \
  > mesh-installation.yaml
```

`scaffold istiocni` creates a separate `IstioCNI` policy, including its target Namespace. Its readiness ConfigurationPolicy waits for `IstioCNI.status.conditions[type=Ready].status=True`.
The optional `--depends-on-istiocni` flag names the CNI root Policy; the Istio policy waits for that readiness ConfigurationPolicy to become Compliant.
Use this flag when the target clusters require CNI, such as OpenShift. The kind demo does not set it.
The default remediation action is `enforce`, and the default pruning behavior is `DeleteIfCreated`.
Use `--remediation-action inform` to inspect compliance without applying resources, or `--prune-object-behavior None` to retain resources when the ConfigurationPolicy is removed.

Review and apply the bundles on the hub, or keep them in GitOps. Apply CNI before Istio; the generated dependency also preserves that order when both policies target a new spoke:

```bash
kubectl --context hub diff -f istiocni-installation.yaml
kubectl --context hub apply -f istiocni-installation.yaml
kubectl --context hub diff -f mesh-installation.yaml
kubectl --context hub apply -f mesh-installation.yaml
```

The `Policy`, `PlacementBinding`, referenced Placement, and MCM must share the supplied namespace.
The Placement should select only members of the mesh because the add-on prepares trust, namespace labels, and endpoint discovery for the MCM's selected clusters.
The MCM must exist before the policy is enforced so the namespace lookup resolves.

The Configuration Policy Controller recommends structured `object-templates` for normal Kubernetes objects and reserves `object-templates-raw` for advanced templating. A `ConfigurationPolicy` can use only one of those fields. This generator uses structured objects with YAML anchors to keep the manifest readable while reducing repeated template expressions. See the [ConfigurationPolicy controller reference](https://github.com/open-cluster-management-io/config-policy-controller#configuration-policy).

## How values stay aligned

ACM resolves the hub templates for every cluster selected by the binding:

- `spec.namespace` on `Istio` and `metadata.namespace` on `Gateway` look up the MCM's current `status.controlPlaneNamespace`.
- `values.global.meshID` and `values.meshConfig.trustDomain` use the supplied MCM name.
- `values.global.multiCluster.clusterName` uses `.ManagedClusterName`.
- `values.global.network` and the Gateway network label use the `topology.istio.io/network` label on the ManagedCluster, falling back to its name.

When `--istio-name` differs from `default`, the generated Gateway also receives `istio.io/rev` with that name so Istio's revisioned Gateway API controller can select it.

The default policy-template account can read namespaced resources in the Policy namespace and the target ManagedCluster context.
Keep the Policy and MCM in the same namespace.
The generator does not create a separate ServiceAccount or additional lookup RBAC.
The network fallback mirrors the add-on's current logic, so changing the label can briefly converge in the add-on and policy controllers at different times.

No network, cluster, trust-domain or control-plane-namespace values are frozen into the generated file.
After editing the root Policy, apply or sync the change on the hub.
ACM propagates it to current members, and a new member is targeted automatically by the existing PlacementBinding.
The CLI does not need to run again.

## Per-spoke changes

The generated bundle provides the same baseline to each member.
For a per-cluster difference, edit the embedded policy to use a hub ConfigMap and the managed-cluster template context, or keep a separate policy that owns disjoint resources.
Keep one policy as the owner of each Istio or Gateway field.
With `musthave`, removing a field from desired state does not reliably reset an existing value; write the desired value explicitly.

The kind and OpenShift demos show the policy and automatic scale-out. OpenShift also applies the separate CNI policy; the Istio policy waits for its readiness check.
The sample does not yet include a ConfigMap-based override.

## Prerequisites and limits

Before enforcing the bundle, install and verify:

- Sail or OSSM 3.x, with the required `sailoperator.io/v1` APIs available.
- The OCM governance-policy-framework hub add-on and managed-cluster `governance-policy-framework` and `config-policy-controller` add-ons.
- Gateway API CRDs supported by the installed Istio version.
- LoadBalancer support on each spoke for the multi-network east-west gateway.
- The MCM, its Placement, and trust/discovery bootstrap.

The scaffold does not install the operator, Gateway API, ztunnel, workloads, or the MCM.
It is a sidecar dataplane sample for a multi-primary, multi-network setup. Ambient installations need additional platform-specific resources.

`DeleteIfCreated` only removes resources tracked as created by that ConfigurationPolicy.
When a cluster leaves the Placement, ACM withdraws its managed policy and the ConfigurationPolicy prunes resources it created. This is asynchronous and does not coordinate with the add-on's own cleanup.
For a controlled uninstall, remove the Istio policy and the CNI policy when it is used, wait for the spoke `Istio`, `Gateway`, `IstioCNI`, and CNI Namespace resources to be removed, then remove the mesh membership or MCM.
The add-on removes its own spoke resources asynchronously and can race with policy pruning.

The local kind workflow is documented in [development environment](dev/environment.md#local-demo-environment).
