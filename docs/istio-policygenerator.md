# Configure Istio with PolicyGenerator

Use the PolicyGenerator Kustomize plugin to manage Istio configuration as code.

This page covers the [PolicyGenerator] + Kustomize approach for users who manage configuration in Git and want to generate ACM policies from plain Kubernetes manifests. If you want a ready-to-apply policy without build tools, see [Step 8 of the user guide](user-guide.md#step-8-configure-istio) instead.

## Prerequisites

- The [PolicyGenerator Kustomize plugin][PolicyGenerator] must be installed. For ACM, the CLI is available from the OpenShift console. See [Red Hat's download instructions][policygenerator-download]. Follow the [installation instructions][policygenerator-install] to set up the local Kustomize plugin.
- The governance-policy-framework and config-policy-controller add-ons must be enabled on the managed clusters. See [Install the Service Mesh Operator](operator-installation.md) for setup.
- The MultiClusterMesh must be created and reporting `status.controlPlaneNamespace`.
- Compatible Gateway API CRDs are available on each spoke. If they are not already present, see [Install Gateway API CRDs](operator-installation.md#install-gateway-api-crds).

## How it works

PolicyGenerator is a Kustomize plugin that reads Kubernetes manifest files and wraps them into `Policy`, `ConfigurationPolicy`, and `PlacementBinding` objects. The manifests can contain [hub templates][hub-templates] that the governance-policy-propagator resolves per cluster at propagation time.

The Istio manifests use hub templates to read the control plane namespace from the MultiClusterMesh status and to derive per-cluster values:

| Variable | Hub template |
|---|---|
| Control plane namespace | `{{hub (lookup "mesh.open-cluster-management.io/v1alpha1" "MultiClusterMesh" "<MESH_NAMESPACE>" "<MESH_NAME>").status.controlPlaneNamespace hub}}` |
| Mesh name / trust domain | `<MESH_NAME>` (literal; the trust domain is the mesh name) |
| Cluster name | `{{hub .ManagedClusterName hub}}` |
| Network | `{{hub (index .ManagedClusterLabels "topology.istio.io/network") \| default .ManagedClusterName hub}}` |

The Policy must be in the same namespace as the MultiClusterMesh, because the default hub-template resolver restricts `lookup` to the policy namespace.

## Sample directory structure

The `samples/policygenerator/` directory provides a working example:

```
samples/policygenerator/
  kustomization.yaml           # references policygenerator.yaml as a generator
  policygenerator.yaml         # PolicyGenerator CR defining policies and manifests
  istio-manifests/
    istio.yaml                 # Istio CR with hub templates
    eastwest-gateway.yaml      # Gateway with hub templates
```

> **Note:** IstioCNI (required on OpenShift and for ambient mode) is a cluster-level prerequisite, not a per-mesh resource. Manage it separately using the pre-built [policy-istiocni.yaml](../samples/policy-istiocni.yaml) sample with a Placement covering all clusters that need CNI.

## Build and apply

Set `policyDefaults.namespace` and `placement.placementName` in `policygenerator.yaml` to your mesh namespace and Placement name. Replace `<MESH_NAME>` and `<MESH_NAMESPACE>` in the `istio-manifests/` files with your mesh name and namespace. Set `MESH_NAMESPACE` to the same namespace as `policyDefaults.namespace`, then build and apply:

```bash
MESH_NAMESPACE=mesh-system
kustomize build --enable-alpha-plugins samples/policygenerator/ \
  | kubectl apply -n "$MESH_NAMESPACE" -f -
```

For additional Istio settings and the sample file to edit, see [Customize Istio](user-guide.md#customize-istio).

Check the generated policies:

```bash
kubectl get policy -n "$MESH_NAMESPACE"
```

Wait for compliance:

```bash
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-istio -n "$MESH_NAMESPACE" --timeout=15m
```

## Per-environment customization

PolicyGenerator requires all manifest paths to be within the build directory tree.
The Istio manifests also contain literal mesh-specific values inside hub template expressions.
These are not rewritten by changing `policyDefaults.namespace`, so each environment needs its own directory with the values filled in.

If multiple meshes share a hub namespace, set `policies[].name` and `placementBindingDefaults.name` to unique values for each mesh.
For example, use `policy-istio-basic-mesh` and `binding-istio-basic-mesh`.
Renaming only the policy leaves the meshes sharing the same PlacementBinding.

Copy the `samples/policygenerator/` directory for each environment and set the values:

```
my-mesh-config/
  production/
    kustomization.yaml
    policygenerator.yaml         # namespace: prod-mesh, placementName: prod-placement
    istio-manifests/
      istio.yaml                 # <MESH_NAME> and <MESH_NAMESPACE> replaced with prod values
      eastwest-gateway.yaml
  staging/
    kustomization.yaml
    policygenerator.yaml         # namespace: staging-mesh, placementName: staging-placement
    istio-manifests/
      istio.yaml                 # <MESH_NAME> and <MESH_NAMESPACE> replaced with staging values
      eastwest-gateway.yaml
```

Build and apply a specific environment:

```bash
kustomize build --enable-alpha-plugins my-mesh-config/production/ \
  | kubectl apply -n prod-mesh -f -
```

For larger setups, you can avoid duplicating manifests with a two-stage approach.
Maintain shared base manifests under plain Kustomize and render them with `kustomize build`.
Then feed the rendered output to PolicyGenerator.
See the [ACM PolicyGenerator documentation][PolicyGenerator] and the [ZTP GitOps workflow][ZTP] for more on this pattern.

## Cleanup

Delete the generated policy and its binding on the hub. Use the namespace configured in `policyDefaults.namespace`. If you changed the generated resource names, use those names instead:

```bash
kubectl delete policy policy-istio -n "$MESH_NAMESPACE"
kubectl delete placementbinding binding-istio -n "$MESH_NAMESPACE"
```

The generated policy sets `pruneObjectBehavior: DeleteIfCreated`.
Deleting it also removes the Istio CR and east-west gateway it created on the spokes.
Pruning is asynchronous and only covers objects the policy created.

If the `Istio/default` CR already existed before the policy was applied, delete it on each spoke.
Replace the example context names with those from your kubeconfig:

```bash
for CONTEXT in context-for-cluster1 context-for-cluster2; do
  kubectl --context "$CONTEXT" delete istio.sailoperator.io default --ignore-not-found
done
```

Then continue with the MultiClusterMesh deletion in [Step 9 of the user guide](user-guide.md#step-9-cleanup).

[PolicyGenerator]: https://docs.redhat.com/en/documentation/red_hat_advanced_cluster_management_for_kubernetes/2.15/html/governance/policy-deployment
[policygenerator-download]: https://access.redhat.com/articles/7049658
[policygenerator-install]: https://github.com/open-cluster-management-io/policy-generator-plugin#install-the-policy-generator
[hub-templates]: https://open-cluster-management.io/concepts/policy/#hub-cluster-templates
[ZTP]: https://docs.openshift.com/container-platform/latest/scalability_and_performance/ztp-deploying-disconnected.html
