# Architecture

The addon runs on an [OCM] hub cluster and automates [multi-cluster Istio service mesh setup][multi-cluster] across managed clusters.

The service mesh operator (Sail or OSSM) is a prerequisite that the user installs separately, for example via [OperatorPolicy] from the governance-policy-framework.

## What the Addon Does

The addon manages the "plumbing" (trust and connectivity) while you configure the mesh control plane using the Istio CRDs.

- **Control Plane Namespace**: Creates an auto-derived, unique namespace per mesh on each managed cluster
- **Trust Distribution**: Mints per-cluster intermediate CAs from a shared root using [cert-manager], implementing Istio's [Plug-in CA] pattern for mTLS trust
- **Endpoint Discovery**: Exchanges discovery credentials via [ManagedServiceAccount] with appropriate RBAC to each service account, and distributes remote secrets to all peer clusters

```mermaid
flowchart TD
    subgraph Istio Multi-Cluster Mesh
        c1[Cluster1]
        c2[Cluster2]
    end 

    user([User]) -->|Creates| mesh([MultiClusterMesh])
    mesh --> addon[Mesh Add-on<br/>on Hub]
    addon -->|Sets up plumbing| c1
    addon -->|Sets up plumbing| c2
    user -->|Creates| crd1([Istio CRDs])
    user -->|Creates| crd2([Istio CRDs])
    crd1 --> c1
    crd2 --> c2
```

For a detailed resource-level diagram, see the [design doc](dev/design.md#architecture).

## Cluster Selection

The addon uses OCM [Placement] for cluster selection.
The user creates a `Placement` resource in the same namespace as the `MultiClusterMesh`, and the mesh references it via `spec.placementRef.name`.
The addon reads the resulting `PlacementDecision` resources to determine which clusters to target.

This gives the user full control over cluster selection using Placement's rich predicate and scheduling capabilities (label selectors, cluster sets, tolerations, priority-based scheduling, etc.).

The addon watches both Placements and PlacementDecisions. It creates resources for selected clusters whose ManagedCluster objects can be read, while cleanup preserves names still present in decisions even if their ManagedCluster objects have disappeared. A mismatch between the decision-entry count and Placement status delays reconciliation.

Each mesh has its own Certificates, ManagedServiceAccounts, namespace ManifestWorks, istio-reader RBAC, and ManifestWorkReplicaSet. The ReplicaSet contains the available remote secrets and references the user's Placement; OCM distributes its template to the selected clusters, including each source cluster itself. Existing remote secrets remain in the template while their source clusters are still selected and replacements cannot yet be built because their ManagedCluster objects, API endpoints, or discovery tokens are unavailable. Updates for clusters with available prerequisites continue during this gap.

## Readiness and Cleanup

Mesh `Ready` requires `DiscoveryDistributed=True` for every resolved cluster and `TrustDistributed=True` when trust is configured.
Operator health, Istio readiness, token validity, and connectivity require separate checks.
See [API reference](api-reference.md#readiness-limits) for current status limitations.

Removing a cluster from the decisions requests cleanup of its mesh resources and removes its credentials from the remote-secret template. An empty selection requests cleanup for every cluster. A missing Placement preserves existing resources and clears per-cluster status. Deleting the mesh requests deletion of its ManifestWorks and ManagedServiceAccounts; owner references allow garbage collection of Certificates and the ReplicaSet.

## Supported Topologies

The addon supports the [Multi-Primary Multi-Network] topology, in which each cluster runs its own control plane.

<!-- Reference links -->
[cert-manager]: https://cert-manager.io/
[ManagedServiceAccount]: https://open-cluster-management.io/docs/getting-started/integration/managed-serviceaccount/
[multi-cluster]: https://istio.io/latest/docs/setup/install/multicluster/
[Multi-Primary Multi-Network]: https://istio.io/latest/docs/setup/install/multicluster/multi-primary_multi-network/
[OCM]: https://open-cluster-management.io/
[OperatorPolicy]: https://open-cluster-management.io/docs/getting-started/integration/policy-controllers/operator-policy/
[Placement]: https://open-cluster-management.io/docs/concepts/content-placement/placement/
[Plug-in CA]: https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/
