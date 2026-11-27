# Multiple meshes on shared clusters

Team A and Team B can each run a Service mesh in sidecar mode across the same two managed clusters, `cluster1` and `cluster2`. The add-on prepares a separate control plane namespace, trust material, and discovery credentials for each mesh. Use ACM PolicyGenerator to install both Istio control planes on each spoke. [How the add-on selects clusters and distributes resources](architecture.md#cluster-selection)

## Set up each mesh on the hub

| Hub namespace | MultiClusterMesh name | Placement in that namespace | Generated policy name |
|---|---|---|---|
| `team-a-ns` | `mcm-team-a` | `mesh-placement` | `policy-istio-team-a` |
| `team-b-ns` | `mcm-team-b` | `mesh-placement` | `policy-istio-team-b` |

1. Create both hub namespaces. Bind the same ManagedClusterSet to each and create a Placement in each that selects `cluster1` and `cluster2`. Use namespace-scoped RoleBindings to delegate mesh administration to each team. [Placement setup](user-guide.md#step-4-create-a-placement)
2. Install Sail or OSSM once per spoke and share the `IstioCNI/default` resource on spokes that require it. [Operator installation](operator-installation.md) · [PolicyGenerator prerequisites](istio-policygenerator.md#prerequisites)
3. Apply the issuer sample in each hub namespace to give the teams separate root CAs. Create one MultiClusterMesh per row with `spec.placementRef.name: mesh-placement` and the local `mesh-root-ca` Issuer. Use distinct mesh names because the name determines the trust domain. Confirm that both spokes are selected and each mesh reports `Ready=True` before installing Istio. Its derived namespace is in `status.controlPlaneNamespace`. [Trust chain](user-guide.md#step-3-create-a-trust-chain) · [MultiClusterMesh example](user-guide.md#step-6-create-a-multiclustermesh) · [Verify the setup](user-guide.md#step-7-verify-the-setup)

## Configure the two PolicyGenerator builds

Copy `samples/policygenerator/` once per team. In each copy's `policygenerator.yaml`, use its table row to set `policyDefaults.namespace`, the mesh-name label under `policyDefaults.policyLabels`, `policyDefaults.placement.placementName`, and `policies[].name`. The distinct policy names give the generated ConfigurationPolicies unique names on the shared spokes. [Per-environment customization](istio-policygenerator.md#per-environment-customization)

In each copy's `istio-manifests/istio.yaml`, merge the following fields into the sample. The values shown are for Team A. For Team B, use `mcm-team-b` and `team-b-ns`. Preserve the sample's hub templates for the control plane namespace, trust domain, mesh ID, cluster name, and network.

```yaml
metadata:
  name: mcm-team-a
spec:
  updateStrategy:
    type: InPlace
  values:
    meshConfig:
      discoverySelectors:
        - matchLabels:
            mesh.open-cluster-management.io/mesh-name: mcm-team-a
            mesh.open-cluster-management.io/mesh-namespace: team-a-ns
```

The Istio CR is cluster-scoped, so each team needs a distinct name. With `InPlace`, that name also identifies the revision. The add-on already labels each control plane namespace with both discovery labels. In `istio-manifests/eastwest-gateway.yaml`, add `istio.io/rev: mcm-team-a` or `istio.io/rev: mcm-team-b` alongside the existing network label under `metadata.labels`. Both Gateways can use the name `eastwestgateway` because they live in separate control plane namespaces.

Build each team's directory locally and apply its output to that team's namespace on the hub. Use the generated policy name from the table for the compliance check. [Build and apply](istio-policygenerator.md#build-and-apply)

## Enroll workloads and check both control planes

On **each spoke**, label each team's application namespaces for discovery and sidecar injection. For example, set `SPOKE_CONTEXT` to the spoke's kubeconfig context and run this for Team A's existing `app-team-a` namespace. Repeat for Team B with its namespace, mesh name, and revision. Remove any legacy `istio-injection` label first and recreate existing pods to pick up the selected revision.

```bash
kubectl --context "$SPOKE_CONTEXT" label namespace app-team-a \
  mesh.open-cluster-management.io/mesh-name=mcm-team-a \
  mesh.open-cluster-management.io/mesh-namespace=team-a-ns \
  istio.io/rev=mcm-team-a
```

Discovery selectors scope the services visible to each control plane. Use an Istio `AuthorizationPolicy` to control which workloads can communicate.

```bash
kubectl --context "$SPOKE_CONTEXT" wait --for=condition=Ready --timeout=15m \
  istio.sailoperator.io/mcm-team-a istio.sailoperator.io/mcm-team-b
kubectl --context "$SPOKE_CONTEXT" get istio.sailoperator.io mcm-team-a mcm-team-b \
  -o custom-columns='MESH:.metadata.name,NAMESPACE:.spec.namespace,REVISION:.status.activeRevisionName'
```

Each row should show the corresponding mesh's `status.controlPlaneNamespace` from the hub and a revision matching its mesh name. For gateway and deployment checks, use the named Istio CR and `istiod-<revision>` in that namespace. [Runtime readiness](user-guide.md#verify-runtime-readiness) · [Troubleshooting discovery and Istio](troubleshooting.md#mesh-is-ready-but-discovery-or-istio-is-not-working)

To remove one mesh, delete the Policy and PlacementBinding generated from that team's build directory. Wait for pruning of the Istio CR and Gateway that its policy created before deleting its MultiClusterMesh. Keep the shared operator and `IstioCNI/default` resource for the remaining mesh. [PolicyGenerator cleanup](istio-policygenerator.md#cleanup)
