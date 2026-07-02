# Performance — Frontend Console Plugin

This document tracks performance considerations, analysis results, and optimizations for the frontend console plugin. The scale target is 200+ clusters, 200+ Istio control planes, and 50+ MultiClusterMesh objects.

## Architecture Overview

The frontend uses two data fetching patterns:

- **Meshes page**: `useFleetMeshItems` merges `useK8sWatchResource` for `MultiClusterMesh` CRs with ACM Search discovery and enrichment of Istio CRs. The unified list shows both managed and discovered meshes.
- **Control Planes page**: Two-phase discovery + enrichment. `useFleetSearchPoll` discovers Istio CRs across all clusters via ACM Search (30s poll). `fleetK8sGet` enriches each discovered CR with full spec/status from the individual cluster. Results are stored in a module-level cache (surviving component unmounts during page navigation) with a 2.5-minute TTL. An `initialEnrichmentDone` flag prevents spinner flash on subsequent search poll updates.

## Known Constraints

### N+1 Enrichment Pattern

The Control Planes page makes one `fleetK8sGet` API call per discovered Istio CR. With 200 control planes, this is 200 GET requests every 2.5 minutes (when the cache TTL expires). Requests are batched in chunks of 10 with `Promise.allSettled` to avoid overwhelming the API server.

This N+1 pattern exists because ACM Search only indexes common K8s metadata (kind, name, namespace, labels, created) for the `Istio` CR. Fields needed for display — `meshID`, `version`, `status` — are in `spec`/`status` which Search doesn't index. See `useEnrichedControlPlanes.ts` for the implementation and `docs/DISCOVERY-OPTIONS.md` for the design rationale.

**Exit ramps** (none are actionable today):
- ACM Search gains spec/status indexing for custom resources
- The sail-operator adds standardized labels (e.g. `istio.io/mesh-id`) to Istio CRs — labels are always indexed by Search
- Note: operators generally don't mutate `metadata` of CRs they reconcile (only `status`), so label-based solutions require upstream sail-operator changes

**Mitigations in place:**
- Module-level TTL cache (150s) survives page navigation; `initialEnrichmentDone` flag prevents spinner flash on subsequent poll updates
- Concurrent chunk limit (10 at a time) prevents API server overload
- Cancellation support prevents stale fetches from updating state
- Debounced state updates (once per second max) prevent re-render storms during enrichment
- Cache eviction sweeps remove entries for deleted control planes

## Optimizations Applied

### Indexed MCM Correlation (O(1) lookup)

**Problem:** The original `findManagingMCM` function iterated over all MCMs and their cluster statuses for every control plane — O(C × M × K) where C=control planes, M=MCMs, K=clusters per MCM. At scale: 200 × 50 × 20 = 200,000 comparisons per render.

**Solution:** `buildMcmIndex()` in `correlateMCM.ts` pre-builds a `Map<clusterName/namespace, McmInfo>` from the MCMs array once, then `lookupMcm()` does O(1) lookups. The index is memoized via `useMemo([mcms])`. Both the list page and detail page use the same shared utility.

### Debounced Enrichment Updates

**Problem:** During chunk-based enrichment, a state update after each chunk of 10 triggered a full re-render (20 re-renders for 200 control planes). Combined with the old O(C×M×K) correlation, this caused 4M comparisons during a single enrichment cycle.

**Solution:** Enrichment progress is tracked via `useRef` and state is updated at most once per second via `setTimeout` debouncing. A final state update fires when all chunks complete. With the indexed correlation, each debounced re-render costs ~200 Map lookups instead of 200K comparisons.

### Memoized TrustStatusCard Maps

**Problem:** `certsByCluster`, `mwByCluster`, `categoryByCluster`, `counts`, and `filtered` were computed inline on every render. Both `useK8sWatchResource` calls (certs, manifestWorks) produce new array references on every WebSocket update, triggering frequent re-renders.

**Solution:** All five computations are wrapped in `useMemo` with appropriate dependency arrays. Maps only rebuild when the underlying watch data actually changes.

### Single-Pass ClusterStatusSection Categorization

**Problem:** `categorizeCluster` was called twice per cluster — once for counting, once for filtering.

**Solution:** Categories are computed once into a `Map<clusterName, category>` via `useMemo`. `counts` and `filtered` both derive from the map.

### Stable Search Key via Numeric Hash

**Problem:** The `searchKey` used for effect dependencies was computed by sorting all search result strings and joining with commas — O(n log n) sort producing a ~6KB string at 200 results.

**Solution:** Replaced with a DJB2-style numeric hash that produces a stable integer in O(n) with no allocation.

### Module-Level Enrichment Cache

**Problem:** The enrichment cache was stored in a `useRef` (per-component-instance), so navigating away from the Control Planes page and back would destroy the cache. This caused a full re-enrichment cycle with a table spinner on every page visit, even when the data hadn't changed.

**Solution:** The enrichment cache was moved from `useRef` to a module-level `Map`. This allows the cache to survive component unmounts when navigating between pages. An `initialEnrichmentDone` ref flag gates the `enrichmentLoaded` reset to the first enrichment cycle only, preventing a full table spinner on subsequent search poll updates.

## Monitoring Checklist

Things to watch as scale increases:

- [ ] **Enrichment latency**: At 200 CPs with 200ms per round, enrichment takes ~4s. If latency grows, consider increasing the concurrency limit from 10 or investigating batch API alternatives.
- [ ] **TrustStatusCard DOM size**: At 200+ clusters, the card renders all rows in a plain `<table>`. If scrolling becomes janky, add pagination or virtualization within the card.
- [ ] **ClusterStatusSection DOM size**: Same concern as TrustStatusCard. The search and filter toggles reduce visible rows, but the full DOM is still rendered.
- [ ] **Cache memory**: Each cached Istio CR is ~2-5KB. At 500+ CPs with cluster churn, monitor memory usage during long sessions. Eviction is in place but only runs after enrichment cycles.
- [ ] **Search response size**: The full fleet search response is metadata-only and should be small even at 500+ CPs. If it grows, investigate the `limit` parameter on `useFleetSearchPoll`.
- [ ] **MCM index rebuild frequency**: The `mcmIndex` rebuilds whenever the MCMs WebSocket watch fires. Building a Map from 100 MCMs with 20 clusters each is ~2000 `Map.set` calls (microseconds), but if MCM count grows significantly or WebSocket updates become frequent, profile whether the index build cost is measurable.
