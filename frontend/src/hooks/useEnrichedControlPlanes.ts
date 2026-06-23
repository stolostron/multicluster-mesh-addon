import { useEffect, useMemo, useRef, useState } from 'react'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio, EnrichedControlPlane } from '../types/istio'
import { istioModel } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { buildMcmIndex, lookupMcm } from '../utils/correlateMCM'

type FleetIstio = Istio & { cluster: string }

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
const CACHE_TTL_MS = 150_000
const CONCURRENCY_LIMIT = 10

async function fetchInChunks(
  pending: { cluster: string; name: string }[],
  cache: Map<string, CacheEntry>,
  onChunkProcessed: (n: number) => void,
  isCancelled: () => boolean,
) {
  for (let i = 0; i < pending.length; i += CONCURRENCY_LIMIT) {
    if (isCancelled()) return
    const chunk = pending.slice(i, i + CONCURRENCY_LIMIT)
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
        cache.set(`${cluster}/${name}`, { data, fetchedAt: Date.now() })
      }
    }
    onChunkProcessed(chunk.length)
  }
}

export function useEnrichedControlPlanes(
  searchResults: FleetIstio[],
  mcms: MultiClusterMesh[],
): [EnrichedControlPlane[], boolean, boolean, unknown] {
  const cacheRef = useRef<Map<string, CacheEntry>>(new Map())
  const [enrichmentVersion, setEnrichmentVersion] = useState(0)
  const [enrichmentLoaded, setEnrichmentLoaded] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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
    const cache = cacheRef.current
    const now = Date.now()
    setError(null)

    const pending = stableResults
      .filter((r) => {
        const key = `${r.cluster}/${r.metadata?.name}`
        const entry = cache.get(key)
        return !entry || (now - entry.fetchedAt > CACHE_TTL_MS)
      })
      .map((r) => ({ cluster: r.cluster, name: r.metadata?.name ?? '' }))

    if (pending.length === 0) {
      setEnrichmentLoaded(true)
      return
    }

    fetchInChunks(
      pending,
      cache,
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
          for (const key of cache.keys()) {
            if (!currentKeys.has(key)) cache.delete(key)
          }
          setEnrichmentVersion((v) => v + 1)
          setEnrichmentLoaded(true)
        }
      })
      .catch((e) => {
        if (!cancelled) { setEnrichmentLoaded(true); setError(e) }
      })

    return () => {
      cancelled = true
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchKey])

  const enrichedBeforeCorrelation = useMemo(() => {
    const cache = cacheRef.current
    return stableResults.map((r): EnrichedControlPlane => {
      const key = `${r.cluster}/${r.metadata?.name}`
      const cached = cache.get(key)?.data
      const spec = cached?.spec
      return {
        metadata: {
          name: r.metadata?.name ?? '',
          creationTimestamp: r.metadata?.creationTimestamp,
          labels: r.metadata?.labels as Record<string, string> | undefined,
        },
        clusterName: r.cluster,
        controlPlaneNamespace: spec?.namespace,
        meshID: spec?.values?.global?.meshID,
        multiClusterName: spec?.values?.global?.multiCluster?.clusterName,
        network: spec?.values?.global?.network,
        status: cached?.status,
        version: spec?.version,
      }
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
