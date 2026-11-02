# Configure Istio with PolicyGenerator

Use the PolicyGenerator Kustomize plugin to manage Istio configuration as code.

This page covers the [PolicyGenerator] + Kustomize approach for users who manage configuration in Git and want to generate ACM policies from plain Kubernetes manifests. If you want a ready-to-apply policy without build tools, see [Step 8 of the user guide](user-guide.md#step-8-configure-istio) instead.

## Prerequisites

- The [PolicyGenerator Kustomize plugin][PolicyGenerator] must be installed. Follow the installation instructions in the upstream documentation.
- The governance-policy-framework and config-policy-controller add-ons must be enabled on the managed clusters. See [Install the Service Mesh Operator](operator-installation.md) for setup.
- The MultiClusterMesh must be created and reporting `status.controlPlaneNamespace`.

> **Warning:** The sample manifests create the cluster-scoped `Istio/default` resource. This means one mesh per spoke cluster. Running two meshes on the same cluster requires distinct Istio resource names plus revision, injection, discovery-selector, and gateway ownership configuration.

## How it works

PolicyGenerator is a Kustomize plugin that reads Kubernetes manifest files and wraps them into `Policy`, `ConfigurationPolicy`, and `PlacementBinding` objects. The manifests can contain [hub templates][hub-templates] that the governance-policy-propagator resolves per cluster at propagation time.

The addon creates a ConfigMap (`mesh-config-<meshName>`) in the mesh namespace with mesh-level values. The Istio manifests reference this ConfigMap via hub templates:

| Variable | Hub template |
|---|---|
| Mesh name | `{{hub fromConfigMap "<ns>" "mesh-config-<mesh>" "meshName" hub}}` |
| Control plane namespace | `{{hub fromConfigMap "<ns>" "mesh-config-<mesh>" "controlPlaneNamespace" hub}}` |
| Trust domain | `{{hub fromConfigMap "<ns>" "mesh-config-<mesh>" "trustDomain" hub}}` |
| Cluster name | `{{hub .ManagedClusterName hub}}` |
| Network | `{{hub (index .ManagedClusterLabels "topology.istio.io/network") \| default .ManagedClusterName hub}}` |

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

Set `policyDefaults.namespace` and `placement.placementName` in `policygenerator.yaml` to your mesh namespace and Placement name. Replace `<MESH_NAME>` and `<MESH_NAMESPACE>` in the `istio-manifests/` files with your mesh name and namespace. Then build and apply:

```bash
kustomize build --enable-alpha-plugins samples/policygenerator/ \
  | kubectl apply -n mesh-system -f -
```

> **Tip:** The manifest files under `istio-manifests/` are minimal templates. To configure additional Istio fields (discovery selectors, pilot settings, proxy resource limits, etc.), edit `spec.values` in `istio-manifests/istio.yaml` before building. See the [Sail operator API reference][sail-api] for all available fields.

Check the generated policies:

```bash
kubectl get policy -n mesh-system
```

Wait for compliance:

```bash
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-istio -n mesh-system --timeout=15m
```

## Per-environment customization

PolicyGenerator requires all manifest paths to be within the build directory tree. The Istio manifests also contain literal mesh-specific values inside hub template expressions (`<MESH_NAME>`, `<MESH_NAMESPACE>`), which are not rewritten by changing `policyDefaults.namespace`. Each environment therefore needs its own self-contained directory with the correct values filled in.

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

For larger setups where you want to avoid duplicating manifests, use a two-stage approach: maintain shared base manifests under plain Kustomize, render them with `kustomize build` first, then feed the rendered output to PolicyGenerator. See the [ACM PolicyGenerator documentation][PolicyGenerator] and the [ZTP GitOps workflow][ZTP] for more on this pattern.

[PolicyGenerator]: https://open-cluster-management.io/getting-started/integration/policy-framework/
[hub-templates]: https://open-cluster-management.io/concepts/policy/#hub-cluster-templates
[sail-api]: https://docs.sailoperator.io/docs/api-reference/sailoperator.io/
[ZTP]: https://docs.openshift.com/container-platform/latest/scalability_and_performance/ztp-deploying-disconnected.html
