import * as React from 'react'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio, EnrichedControlPlane } from '../types/istio'
import { istioModel } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'

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
  setEnrichmentCount: React.Dispatch<React.SetStateAction<number>>,
) {
  for (let i = 0; i < pending.length; i += CONCURRENCY_LIMIT) {
    const chunk = pending.slice(i, i + CONCURRENCY_LIMIT)
    const results = await Promise.allSettled(
      chunk.map(({ cluster, name }) =>
        fleetK8sGet<Istio>({ model: istioModel, name, cluster })
          .then((r) => ({ cluster, name, data: r }))
      ),
    )
    for (const result of results) {
      if (result.status === 'fulfilled') {
        const { cluster, name, data } = result.value
        cache.set(`${cluster}/${name}`, { data, fetchedAt: Date.now() })
      }
    }
    setEnrichmentCount((c) => c + chunk.length)
  }
}

// Determines whether an Istio control plane is managed by a MultiClusterMesh.
// A MultiClusterMesh declares intent to manage Istio on a set of clusters; the
// controller creates the actual Istio CRs. This function matches a discovered
// Istio CR back to its managing MCM by checking two things:
//   1. The cluster running this control plane appears in the MCM's status.clusterStatus[]
//   2. The control plane namespace (Istio.spec.namespace) matches the MCM's
//      spec.controlPlane.namespace (default: istio-system)
// If both match, the control plane is considered managed by that MCM.
// Note: this is a best-effort correlation — an independently created Istio CR
// that happens to be on the same cluster+namespace as an MCM will also match.
function correlate(
  plane: EnrichedControlPlane,
  mcms: MultiClusterMesh[],
): { name: string; namespace: string } | undefined {
  if (!plane.controlPlaneNamespace) return undefined
  for (const mcm of mcms) {
    const mcmNs = mcm.spec.controlPlane?.namespace ?? 'istio-system'
    if (mcmNs !== plane.controlPlaneNamespace) continue
    const match = mcm.status?.clusterStatus?.find((cs) => cs.clusterName === plane.clusterName)
    if (match) {
      return { name: mcm.metadata?.name ?? '', namespace: mcm.metadata?.namespace ?? '' }
    }
  }
  return undefined
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

    fetchInChunks(pending, cache, setEnrichmentCount)
      .then(() => {
        if (!cancelled) setEnrichmentLoaded(true)
      })
      .catch((e) => {
        if (!cancelled) setError(e)
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
      managedBy: correlate(plane, mcms),
    })),
    [enrichedBeforeCorrelation, mcms],
  )

  return [enrichedPlanes, searchResults.length > 0 || enrichmentLoaded, enrichmentLoaded, error]
}
