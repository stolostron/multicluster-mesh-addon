import { useEffect, useMemo, useRef, useState } from 'react'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { FleetIstio, Istio, EnrichedControlPlane } from '../types/istio'
import { istioModel } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { buildMcmIndex, lookupMcm } from '../utils/correlateMCM'
import { toEnrichedControlPlane } from '../utils/enrichmentUtils'

interface CacheEntry {
  data: Istio
  fetchedAt: number
}

// N+1 enrichment pattern: ACM Search only indexes common K8s metadata for the
// Istio CR (kind, name, namespace, labels, created). Fields needed for display
// (meshID, version, status) are in spec/status which Search doesn't index.
// We fetch the full CR per cluster via fleetK8sGet. This is a known architectural
// tradeoff — see docs/DISCOVERY-OPTIONS.md for the design rationale.
// Exit ramp: if ACM Search gains spec/status indexing for custom resources, or
// if Istio CRs gain standardized labels for key fields, enrichment can be eliminated.
export const CACHE_TTL_MS = 150_000
const CONCURRENCY_MIN = 10
const CONCURRENCY_MAX = 25
const MAX_CACHE_SIZE = 2000

export function getConcurrencyLimit(pendingCount: number): number {
  return Math.max(CONCURRENCY_MIN, Math.min(CONCURRENCY_MAX, Math.ceil(pendingCount / 20)))
}

// Module-level cache so enrichment data survives component unmounts. When a user
// navigates from the list page to a detail page, the new component instance reads
// from the same warm cache instead of triggering a full fleet-wide re-fetch.
// The cache is read-only from useMemo's perspective (it reads cache.get(key)?.data).
// The per-instance enrichmentVersion state forces memo re-evaluation after fetches
// update the cache. Stale-key cleanup is safe because only one page is mounted at
// a time in the Console plugin (route-based rendering).
const enrichmentCache = new Map<string, CacheEntry>()

/** Clears the module-level enrichment cache. Only for use in tests. */
export function __resetEnrichmentCache() {
  enrichmentCache.clear()
}

/** Reads a cached Istio CR if it exists and is within TTL. */
export function getFromEnrichmentCache(cluster: string, name: string): Istio | undefined {
  const entry = enrichmentCache.get(`${cluster}/${name}`)
  if (entry && Date.now() - entry.fetchedAt <= CACHE_TTL_MS) return entry.data
  return undefined
}

/** Writes an Istio CR to the shared cache for bidirectional cache warming. */
export function setInEnrichmentCache(cluster: string, name: string, data: Istio): void {
  const cacheKey = `${cluster}/${name}`
  enrichmentCache.delete(cacheKey)
  enrichmentCache.set(cacheKey, { data, fetchedAt: Date.now() })
  if (enrichmentCache.size > MAX_CACHE_SIZE) {
    const oldest = enrichmentCache.keys().next().value
    if (oldest !== undefined) enrichmentCache.delete(oldest)
  }
}

async function fetchInChunks(
  pending: { cluster: string; name: string }[],
  onChunkProcessed: (n: number) => void,
  isCancelled: () => boolean,
) {
  const chunkSize = getConcurrencyLimit(pending.length)
  for (let i = 0; i < pending.length; i += chunkSize) {
    if (isCancelled()) return
    const chunk = pending.slice(i, i + chunkSize)
    const results = await Promise.allSettled(
      chunk.map(({ cluster, name }) =>
        fleetK8sGet<Istio>({ model: istioModel, name, cluster })
          .then((r) => ({ cluster, name, data: r }))
      ),
    )
    if (isCancelled()) return
    for (const result of results) {
      if (result.status === 'fulfilled') {
        const { cluster, name, data } = result.value
        setInEnrichmentCache(cluster, name, data)
      }
    }
    onChunkProcessed(chunk.length)
  }
}

/** Enriches discovered Istio CRs with full spec/status via per-cluster GET and correlates each with its managing MultiClusterMesh. */
export function useEnrichedControlPlanes(
  searchResults: FleetIstio[],
  mcms: MultiClusterMesh[],
): [EnrichedControlPlane[], boolean, boolean, unknown] {
  const [enrichmentVersion, setEnrichmentVersion] = useState(0)
  const [enrichmentLoaded, setEnrichmentLoaded] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Only reset enrichmentLoaded on the first enrichment cycle. After initial
  // enrichment completes, subsequent search poll updates skip the reset so the
  // table never flashes a spinner — new CRs briefly appear as standalone (~1s)
  // before enrichment reveals their meshID and the next rebuild regroups them.
  const initialEnrichmentDone = useRef(false)

  const searchKey = useMemo(() => {
    if (searchResults.length === 0) return 0
    let hash = searchResults.length
    for (const r of searchResults) {
      const s = `${r.cluster}/${r.metadata?.name}`
      for (let i = 0; i < s.length; i++) hash = (hash * 31 + s.charCodeAt(i)) | 0
    }
    return hash
  }, [searchResults])

  // Stabilize the search results reference so downstream memos don't fire on
  // every 30s poll when the actual data hasn't changed.
  const stableResults = useMemo(() => searchResults, [searchKey]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let cancelled = false
    const now = Date.now()
    setError(null)

    if (stableResults.length > 0 && !initialEnrichmentDone.current) {
      setEnrichmentLoaded(false)
    }

    const pending = stableResults
      .filter((r) => {
        const key = `${r.cluster}/${r.metadata?.name}`
        const entry = enrichmentCache.get(key)
        return !entry || (now - entry.fetchedAt > CACHE_TTL_MS)
      })
      .map((r) => ({ cluster: r.cluster, name: r.metadata?.name ?? '' }))

    if (pending.length === 0) {
      setEnrichmentLoaded(true)
      if (stableResults.length > 0) initialEnrichmentDone.current = true
      return
    }

    fetchInChunks(
      pending,
      () => {
        if (!cancelled) {
          if (debounceRef.current) clearTimeout(debounceRef.current)
          debounceRef.current = setTimeout(() => {
            if (!cancelled) setEnrichmentVersion((v) => v + 1)
          }, 1000)
        }
      },
      () => cancelled,
    )
      .then(() => {
        if (!cancelled) {
          if (debounceRef.current) clearTimeout(debounceRef.current)
          const currentKeys = new Set(stableResults.map((r) => `${r.cluster}/${r.metadata?.name}`))
          for (const key of enrichmentCache.keys()) {
            if (!currentKeys.has(key)) enrichmentCache.delete(key)
          }
          setEnrichmentVersion((v) => v + 1)
          setEnrichmentLoaded(true)
          initialEnrichmentDone.current = true
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setEnrichmentLoaded(true)
          initialEnrichmentDone.current = true
          setError(e)
        }
      })

    return () => {
      cancelled = true
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchKey])

  const enrichedBeforeCorrelation = useMemo(() => {
    return stableResults.map((r) => {
      const cached = enrichmentCache.get(`${r.cluster}/${r.metadata?.name}`)?.data
      return toEnrichedControlPlane(r, cached)
    })
  }, [stableResults, enrichmentVersion])

  const mcmIndex = useMemo(() => buildMcmIndex(mcms), [mcms])

  const enrichedPlanes = useMemo(
    () => enrichedBeforeCorrelation.map((plane) => ({
      ...plane,
      managedBy: lookupMcm(mcmIndex, plane.clusterName, plane.controlPlaneNamespace),
    })),
    [enrichedBeforeCorrelation, mcmIndex],
  )

  return [enrichedPlanes, searchResults.length > 0 || enrichmentLoaded, enrichmentLoaded, error]
}
