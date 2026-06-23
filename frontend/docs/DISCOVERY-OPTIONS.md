# Discovered Meshes — Design

## Context

[OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) explicitly requires:

> "While this should work well with the service mesh ACM addon, if a user already has OSSM applied across clusters with ACM (without the addon) it should also work for observability at least."

And:

> "In the future, it should work with ACM addon (OCPSTRAT-2988) to accomplish common multi-cluster mesh administrative tasks, though observability should be the initial focus."

Today the Fleet Service Mesh plugin only shows `MultiClusterMesh` CRs — meshes that were created through the backend controller. Users who have OSSM installed across clusters by other means (ACM policies, GitOps, or manually) see nothing in the plugin, even though those control planes are running on managed clusters.

This document explores how to discover and display all OSSM control planes across the fleet, regardless of how they were created, and enrich them with fleet management context from `MultiClusterMesh` CRs when available.

Related:
- [HPSTRAT-215](https://redhat.atlassian.net/browse/HPSTRAT-215) — Strategic outcome: Fleet-wide service mesh management with ACM
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview

## Discovery mechanism

ACM Search (`useFleetSearchPoll` from `@stolostron/multicluster-sdk`) is the recommended approach for fleet-scale discovery. It queries a single server-side search index rather than opening per-cluster WebSocket connections, making it viable at 500-1000 clusters.

The search query uses the GVK `{ group: 'sailoperator.io', version: 'v1', kind: 'Istio' }` to discover all sail-operator `Istio` CRs across managed clusters. OSSM 2.x `ServiceMeshControlPlane` (maistra.io) is out of support and is not targeted.

**Polling interval:** 30 seconds. This is the SDK's minimum enforced interval (values 0-30 are clamped to 30s). For a fleet observability dashboard, 30-second freshness is sufficient — control plane status changes are infrequent and the search-collector's own sync interval is comparable.

## Two data sources

| Source | Resource | Location | Granularity | What it represents |
|--------|----------|----------|-------------|-------------------|
| Controller | `MultiClusterMesh` | Hub cluster | One CR = one mesh spanning N clusters | Fleet-level mesh intent (desired state) |
| Search | `Istio` CR | Each managed cluster | One CR = one control plane on one cluster | Per-cluster actual state |

The fundamental tension is that these operate at different granularities. A `MultiClusterMesh` is a fleet concept (cluster set, trust policy, operator config). An `Istio` CR is a cluster concept (one control plane, one namespace, one version).

## Grouping discovered Istio CRs

A discovered `Istio` CR is not necessarily standalone. Istio supports multi-cluster topologies (multi-primary, primary-remote) where multiple `Istio` CRs across clusters form a single logical mesh. The sail-operator CRD provides fields to identify this grouping under `spec.values.global`:

| Field | Purpose |
|-------|---------|
| `meshID` | Shared identifier across all clusters in the same logical mesh. All Istio CRs with the same `meshID` belong to the same mesh. |
| `multiCluster.clusterName` | Unique name for this specific cluster within the mesh. |
| `network` | Network identifier — clusters on the same network communicate directly; different networks require gateways. |

Example: two clusters in the same mesh (from [OSSM 3.1 multi-cluster docs](https://docs.redhat.com/en/documentation/red_hat_openshift_service_mesh/3.1/html/installing/ossm-multi-cluster-topologies)):

```yaml
# East cluster
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  namespace: istio-system
  values:
    global:
      meshID: mesh1
      multiCluster:
        clusterName: cluster1
      network: network1

# West cluster — same meshID, different cluster/network
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  namespace: istio-system
  values:
    global:
      meshID: mesh1
      multiCluster:
        clusterName: cluster2
      network: network2
```

Both share `meshID: mesh1`, so they belong to the same logical mesh.

## What ACM Search indexes for the Istio CR

The search-collector's [transform config](https://github.com/stolostron/search-collector/blob/main/pkg/transforms/genericResourceConfig.go) defines which properties are extracted for each resource kind. **There is no entry for `Istio.sailoperator.io`**, which means search only indexes the common properties:

| Indexed | Not indexed |
|---------|-------------|
| `kind`, `name`, `namespace` | `spec.values.global.meshID` |
| `created`, `apigroup`, `apiversion` | `spec.values.global.multiCluster.clusterName` |
| `label` (all labels) | `spec.values.global.network` |
| cluster name (implicit from search) | `spec.version`, `spec.namespace` |
| | `status` fields |

**Implication**: Search can discover *that* Istio CRs exist and *where* (which cluster, namespace, name), but cannot provide the `meshID` needed for grouping or the `version`/`status` needed for display.

**Mitigation options:**

1. **Follow-up `fleetK8sGet`** — After search discovers Istio CRs, make targeted `fleetK8sGet` calls to fetch the full CR from each cluster. This is one call per cluster, not a fan-out watch, and can be done lazily (e.g. when the user clicks into a detail view, or batched on page load).

2. **Labels convention** — If the sail-operator or addon set labels like `istio.io/mesh-id` on the Istio CR, search would index them automatically (labels are always indexed). This would require upstream changes to the sail-operator.

## Options considered

During brainstorming we identified four approaches. After discussion, we decided to focus on **Option 4**.

### Option 1: Two separate pages (rejected)

Separate "Managed Meshes" and "Discovered Instances" nav items. Clean separation but too disjointed — users have to look in two places, and controller-managed Istio CRs appear in both views.

### Option 2: One unified list with mixed sources (rejected)

Single table merging `MultiClusterMesh` and `Istio` CRs. True single pane of glass but the different granularities (fleet vs. per-cluster) make a unified table awkward and the correlation logic is complex.

### Option 3: One page, two tabs (rejected)

Tabs for "Fleet Meshes" and "Cluster Instances" on the same page. Less disjointed than Option 1 but still two mental models the user has to understand.

### Option 4: Discovered-first, addon enriches (selected)

The primary view is always the discovered `Istio` CRs — these are the real control planes running on clusters. Every user sees all their control planes regardless of how they were created.

When `MultiClusterMesh` CRs exist, their context is layered on top: which cluster set the control plane belongs to, trust status, operator config, fleet-level conditions. Control planes not managed by a `MultiClusterMesh` still appear with basic info (cluster, namespace, version, health).

**Why this option:**
- Shows all control planes across the fleet — whether created by the controller or independently
- `MultiClusterMesh` context is a value-add layer, not a prerequisite for visibility
- Directly aligns with the OCPSTRAT-2989 requirement
- "Single pane of glass" regardless of how control planes were provisioned
- The existing `MultiClusterMesh` list/detail pages remain — they are the fleet management layer. The Control Planes page provides per-cluster visibility that users can also navigate to directly

**List page design:**
The Control Planes list is a flat table — one row per Istio CR. `meshID` is a sortable column; sorting by it naturally groups control planes belonging to the same mesh by placing them next to each other. No expandable/grouped rows — keep it simple. A future "Same Mesh" section on the detail page could show other control planes sharing the same `meshID`, but this is deferred until a shared enrichment cache is available.

**Challenges:**
- Need to correlate `Istio` CRs with `MultiClusterMesh` cluster membership (see Correlation below)
- Two data sources with different refresh characteristics (search poll at 30s vs. real-time hub watch on the Fleet Meshes page)
- `meshID` is not in the search index — requires `fleetK8sGet` enrichment (see Enrichment Strategy below)

## Enrichment strategy

Since ACM Search does not index `meshID`, `version`, or `status` for the `Istio` CR, the Control Planes page uses a two-phase approach:

**Phase 1 (immediate):** Search results arrive and the table renders with the fields search provides — cluster name, CR name, created timestamp. The table is usable immediately for basic discovery. Enrichment-dependent columns (`meshID`, `version`, `namespace`, `status`) show `-` until enrichment completes.

**Phase 2 (background enrichment):** `fleetK8sGet` calls fetch the full `Istio` CR from each cluster to populate `meshID`, `version`, `spec.namespace`, and `status`. Calls are concurrency-limited (e.g., 10 at a time) to avoid overwhelming the API. The Console SDK's `VirtualizedTable` does not expose which rows are currently visible, so viewport-aware batching is not possible — all discovered CRs are enriched with a concurrency cap.

**Caching and invalidation:** Results are cached by `${clusterName}/${crName}`. When a search poll returns new Istio CRs, enrichment is fetched for the new CRs only. Cache entries have a TTL of 5 poll cycles (~2.5 minutes) — after expiry, the next search poll triggers a re-fetch. This ensures version upgrades, health changes, and `meshID` modifications are eventually reflected without re-fetching the full fleet on every poll.

**Error handling:** If a `fleetK8sGet` call fails (cluster unreachable, RBAC denied, network timeout), the row remains in the table with `-` for all enrichment fields. Failed enrichment does not block other rows. The cache does not store failed results, so the next poll cycle will retry.

**RBAC prerequisite:** `fleetK8sGet` fetches resources from managed clusters via the ACM hub proxy. The Console user must have read access to the `Istio` CR on each target cluster. Users who deploy OSSM independently should verify that ManagedClusterView or equivalent RBAC is in place for the sailoperator.io API group. RBAC failures surface as enrichment errors (see error handling above).

This approach scales to hundreds of clusters because:
- The initial render is not blocked on enrichment — the table is interactive immediately
- Enrichment is concurrency-limited, cached, and incrementally updated
- Failed enrichment degrades gracefully without breaking the page

**Exit ramp from N+1 enrichment:** The per-cluster `fleetK8sGet` pattern exists because ACM Search doesn't index `spec`/`status` for custom resources. If any of the following change, enrichment can be eliminated:
- ACM Search gains spec/status indexing for CRDs (no search-collector change needed on our side)
- The sail-operator adds standardized labels (e.g. `istio.io/mesh-id`, `istio.io/version`) to `Istio` CRs — labels are always indexed by Search
Note: operators generally don't mutate the `metadata` of CRs they reconcile (they only update `status`), so label-based solutions would require upstream sail-operator changes.

## Correlation

Matching `Istio` CRs to `MultiClusterMesh` CRs uses the composite key: **cluster name + control plane namespace**.

- **Cluster name:** The managed cluster name from the search result (implicit in search data) matched against `MultiClusterMesh.status.clusterStatus[].clusterName`.
- **Control plane namespace:** `Istio.spec.namespace` (where the control plane pods run, fetched via `fleetK8sGet` enrichment) matched against `MultiClusterMesh.spec.controlPlane.namespace` (defaults to `istio-system` if not set).

Notes:
- The `Istio` CR is a cluster-scoped resource, so `metadata.namespace` is empty. The control plane namespace is always in `spec.namespace`, not `metadata.namespace`. ACM Search returns `metadata.namespace`, which will be empty for cluster-scoped resources — this is expected and correlation relies on the enriched `spec.namespace` from `fleetK8sGet`.
- `Istio.spec.namespace` is a required, immutable field per the sail-operator CRD. All official examples set it explicitly (typically to `istio-system`). As a defensive fallback, if `spec.namespace` is somehow absent after enrichment, correlation defaults to `istio-system`.

## Data flow for Option 4

```
┌─────────────────────────────────────────────────────────────┐
│              Control Planes Page (new)                       │
│                                                             │
│  useFleetSearchPoll ──► Discover Istio CRs across clusters  │
│    GVK: sailoperator.io/v1 Istio, poll every 30s            │
│                                                             │
│  useK8sWatchResource ──► Watch MultiClusterMesh CRs on hub  │
│    (reuses existing useMultiClusterMeshes hook)              │
│    Hub-side CRDs may not be in the ACM Search index, so     │
│    we use a direct hub watch rather than search.             │
│                                                             │
│  fleetK8sGet (per cluster, concurrency-limited) ──►         │
│    Fetch full Istio CR for meshID, version, status           │
│    Cached by cluster+name; re-fetch only for new CRs        │
│    Cache entries expire after 5 poll cycles (~2.5 min)       │
│    Failed fetches show '-' for enrichment fields             │
│                                                             │
│  Correlate: match Istio CRs to MultiClusterMesh by          │
│             cluster name + control plane namespace          │
│                                                             │
│  Display: flat list of Istio CRs (one row per control plane)│
│    - Controller-managed CRs show "Managed by: <MCM>" badge  │
│    - meshID column for sorting/grouping                     │
│    - All CRs show cluster, namespace, version, health       │
│    - Enrichment columns show '-' while loading or on error  │
└─────────────────────────────────────────────────────────────┘
         │                          │
         ▼                          ▼
┌─────────────────┐     ┌───────────────────────┐
│ MultiClusterMesh│     │ Istio CR Detail       │
│ Detail Page     │     │ (per-cluster view)    │
│ (fleet mgmt)    │     │ version, health,      │
│ trust, operator,│     │ config, meshID        │
│ cluster status  │     │                       │
│                 │     │ Reachable from:       │
│    (deferred)   │     │ - Direct navigation   │
│                 │     │ - MCM drilldown (future)
└─────────────────┘     └───────────────────────┘
```

**Note:** The existing Fleet Meshes list page and MultiClusterMesh detail page are unchanged. They continue to use `useK8sWatchResource` for real-time hub-side watches. The Control Planes page is an additional view that provides visibility into all control planes across the fleet. The Istio CR detail page design is covered in the implementation plan.

## Navigation

The Fleet Service Mesh perspective will have two nav items:

- **Fleet Meshes** — the existing `MultiClusterMesh` list page (fleet-level mesh configuration)
- **Control Planes** — the new unified page showing discovered Istio CRs across all clusters

The landing page (when switching to the Fleet Service Mesh perspective) remains **Fleet Meshes**. This means users who have no `MultiClusterMesh` CRs will land on an empty page and need to navigate to Control Planes — this is acceptable because the Fleet Meshes page is the primary management interface, and users who have independently deployed OSSM will quickly learn to navigate to Control Planes. If user feedback shows this is confusing, the landing page can be changed later. Names are tentative and may change after user feedback.

## Decisions

- **No mesh import** — "Importing" a discovered mesh into fleet management (creating a `MultiClusterMesh` CR for it) is not viable. The addon controller expects to own and manage the Istio CRs it creates — adopting pre-existing Istio CRs that were created independently is not supported and would be extremely difficult to implement safely.
- **No CollectorConfig** — The search-collector supports a `CollectorConfig` CRD (`collectorconfigs.search.open-cluster-management.io`) that can extend the default transform config at runtime to index additional fields like `meshID`, `version`, and status for `Istio.sailoperator.io`. However, this requires deploying a `CollectorConfig` to every managed cluster. Users who provisioned their control planes independently would not have this deployed, so it doesn't solve the general case. Rejected in favor of `fleetK8sGet` enrichment which works regardless of how control planes were created.

## Open questions

- How should the UI handle Istio CRs with no `meshID` set? Treat each as a standalone single-cluster mesh? Or attempt to infer grouping from shared trust domains?
- How do we handle Kiali deep links for discovered instances? (Deferred — not blocking initial implementation.)
