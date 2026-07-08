# Mesh Discovery

How the Fleet Service Mesh plugin discovers and displays meshes across the fleet.

## Two kinds of meshes

| Kind | Source | How it appears |
|------|--------|----------------|
| **Managed** | `MultiClusterMesh` CR on the hub cluster | Created by a fleet admin via the backend controller. The controller installs the sail-operator and creates Istio CRs on managed clusters. |
| **Discovered** | `Istio` CRs found on managed clusters via ACM Search | Independently deployed control planes (via ACM policies, GitOps, or manually) that are not managed by a `MultiClusterMesh`. |

Both kinds appear in a unified **Meshes** list page. Managed meshes show a blue Mesh ID label; discovered meshes show a purple Mesh ID label.

## Managed meshes

Managed meshes are `MultiClusterMesh` CRs watched on the hub cluster via `useK8sWatchResource`. This is a real-time Kubernetes watch — changes appear immediately in the UI without polling.

Each `MultiClusterMesh` CR declares:
- A `ManagedClusterSet` (which clusters to target)
- A control plane namespace (where Istio runs on each cluster)
- Operator configuration (channel, source, install plan approval)
- Optional trust distribution via cert-manager

The backend controller reconciles the CR, installs the sail-operator on target clusters via `ManifestWork`, and reports per-cluster status in `status.clusterStatus[]`.

### Managed mesh ID

The mesh ID for a managed mesh is derived from the Istio CRs that the controller creates on managed clusters. The controller sets `spec.values.global.meshID` on each Istio CR using the convention `<MCM namespace>-<MCM name>`. The frontend discovers this mesh ID through the enrichment process described below and displays it in the Mesh ID column.

## Discovered meshes

Discovered meshes are Istio CRs found across the fleet via ACM Search. The discovery process has two phases.

### Phase 1: Search discovery

`useFleetSearchPoll` from `@stolostron/multicluster-sdk` queries the ACM Search index for all `Istio` CRs (`sailoperator.io/v1`) across managed clusters. The poll runs every 30 seconds — the SDK's minimum enforced interval.

Search indexes only common metadata (kind, name, namespace, cluster, labels, timestamps). It does **not** index the Istio CR's `spec` or `status` fields, so `meshID`, `version`, and health are not available from search alone.

### Phase 2: Enrichment

After search returns the list of Istio CRs, `fleetK8sGet` fetches the full CR from each managed cluster's API server. This provides:

| Field | Source in Istio CR |
|-------|-------------------|
| Mesh ID | `spec.values.global.meshID` |
| Version | `spec.version` |
| Control plane namespace | `spec.namespace` |
| Network | `spec.values.global.network` |
| Health status | `status.conditions` |

Enrichment calls are concurrency-limited (10 at a time) and cached in a module-level Map with a 150-second TTL. The cache survives page navigation within the plugin, so switching between list and detail pages does not re-fetch data. Only new CRs (not already cached) trigger `fleetK8sGet` calls on each search poll.

If enrichment fails for a cluster (unreachable, RBAC denied, timeout), the row remains in the table with `-` for enrichment fields. Failed results are not cached, so the next poll retries.

### Grouping by mesh ID

Discovered Istio CRs are grouped by `meshID` to form logical meshes:

- CRs sharing the same `meshID` are grouped into a single discovered mesh row in the Meshes list
- CRs with no `meshID` are treated as standalone single-cluster control planes (each gets its own row linking to the control plane detail page)
- If a discovered mesh's `meshID` matches a managed mesh's `meshID`, a conflict warning is shown

## Correlation: matching Istio CRs to managed meshes

An Istio CR is considered "managed by" a `MultiClusterMesh` when both conditions are true:

1. The cluster running the Istio CR appears in the MCM's `status.clusterStatus[]`
2. The Istio CR's `spec.namespace` matches the MCM's `spec.controlPlane.namespace` (default: `istio-system`)

This correlation is implemented as an O(1) lookup index (`buildMcmIndex` / `lookupMcm` in `src/utils/correlateMCM.ts`) keyed by `clusterName/controlPlaneNamespace`.

Correlated Istio CRs get a `managedBy` field pointing to the MCM's name and namespace. In the UI, their Mesh ID label is blue (managed) and links to the MCM detail page. Uncorrelated CRs with a `meshID` get a purple label (discovered) linking to the discovered mesh detail page.

**Note:** Correlation is best-effort. An independently created Istio CR that happens to be on the same cluster and namespace as an MCM will also match as "managed."

## Data flow

```text
Hub cluster                          Managed clusters
┌──────────────────────┐             ┌─────────────────────┐
│ MultiClusterMesh CRs │             │ Istio CRs           │
│ (useK8sWatchResource)│             │ (sailoperator.io/v1) │
└──────────┬───────────┘             └──────────┬──────────┘
           │                                    │
           │                         ACM Search (30s poll)
           │                                    │
           │                         ┌──────────▼──────────┐
           │                         │ Search results      │
           │                         │ (metadata only)     │
           │                         └──────────┬──────────┘
           │                                    │
           │                         fleetK8sGet (enrichment)
           │                                    │
           │                         ┌──────────▼──────────┐
           │                         │ Enriched CPs        │
           │                         │ (meshID, version,   │
           │                         │  status, namespace)  │
           │                         └──────────┬──────────┘
           │                                    │
           └────────────┬───────────────────────┘
                        │
              Correlation (cluster + namespace)
                        │
              ┌─────────▼─────────┐
              │ useFleetMeshItems  │
              │ Unified mesh list  │
              │ (managed +         │
              │  discovered)       │
              └────────────────────┘
```

## ACM Search limitations

The search-collector does not have a custom transform for `Istio.sailoperator.io`, so only common metadata is indexed. This is why the N+1 `fleetK8sGet` enrichment pattern exists. If any of the following change, enrichment can be eliminated:

- ACM Search gains spec/status indexing for CRDs
- The sail-operator adds standardized labels (e.g. `istio.io/mesh-id`) to Istio CRs — labels are always indexed by Search

## Design decisions

- **No mesh import** — "Importing" a discovered mesh into fleet management (creating a `MultiClusterMesh` CR for it) is not supported. The backend controller expects to own the Istio CRs it creates; adopting pre-existing CRs is not viable.
- **No CollectorConfig** — The search-collector supports a `CollectorConfig` CRD that can extend indexing at runtime, but it requires deployment to every managed cluster. Users with independently provisioned control planes would not have this, so it doesn't solve the general case.
- **Discovered-first design** — The plugin shows all control planes across the fleet regardless of how they were created. `MultiClusterMesh` context is a value-add layer, not a prerequisite for visibility. This aligns with [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) which requires observability for independently deployed OSSM.

## Related

- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [PERFORMANCE.md](./PERFORMANCE.md) — Scale optimizations for the enrichment pipeline
