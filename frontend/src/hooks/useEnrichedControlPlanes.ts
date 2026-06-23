import * as React from 'react'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio, EnrichedControlPlane } from '../types/istio'
import { istioModel } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { findManagingMCM } from '../utils/correlateMCM'

type FleetIstio = Istio & { cluster: string }

interface CacheEntry {
  data: Istio
  fetchedAt: number
}

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
  const cacheRef = React.useRef<Map<string, CacheEntry>>(new Map())
  const [enrichmentCount, setEnrichmentCount] = React.useState(0)
  const [enrichmentLoaded, setEnrichmentLoaded] = React.useState(false)
  const [error, setError] = React.useState<unknown>(null)

  const searchKey = React.useMemo(
    () => searchResults.map((r) => `${r.cluster}/${r.metadata?.name}`).sort().join(','),
    [searchResults],
  )

  // Stabilize the search results reference so downstream memos don't fire on
  // every 30s poll when the actual data hasn't changed.
  const stableResults = React.useMemo(() => searchResults, [searchKey]) // eslint-disable-line react-hooks/exhaustive-deps

  React.useEffect(() => {
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
      (n) => { if (!cancelled) setEnrichmentCount((c) => c + n) },
      () => cancelled,
    )
      .then(() => {
        if (!cancelled) setEnrichmentLoaded(true)
      })
      .catch((e) => {
        if (!cancelled) { setEnrichmentLoaded(true); setError(e) }
      })

    return () => { cancelled = true }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchKey])

  const enrichedBeforeCorrelation = React.useMemo(() => {
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
  }, [stableResults, enrichmentCount])

  const enrichedPlanes = React.useMemo(
    () => enrichedBeforeCorrelation.map((plane) => ({
      ...plane,
      managedBy: findManagingMCM(plane.clusterName, plane.controlPlaneNamespace, mcms),
    })),
    [enrichedBeforeCorrelation, mcms],
  )

  return [enrichedPlanes, searchResults.length > 0 || enrichmentLoaded, enrichmentLoaded, error]
}
