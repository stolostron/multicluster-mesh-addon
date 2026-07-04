import { useEffect, useMemo, useRef, useState } from 'react'
import { fleetK8sGet, useFleetSearchPoll } from '@stolostron/multicluster-sdk'
import type { FleetIstio, Istio, EnrichedControlPlane } from '../types/istio'
import { istioModel, istioGroupVersionKind } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'
import { buildMcmIndex, lookupMcm } from '../utils/correlateMCM'
import { toEnrichedControlPlane } from '../utils/enrichmentUtils'
import {
  CACHE_TTL_MS,
  getConcurrencyLimit,
  getFromEnrichmentCache,
  setInEnrichmentCache,
} from './useEnrichedControlPlanes'

/**
 * Scoped enrichment hook for detail pages. Only discovers and enriches control
 * planes on the specified clusters, avoiding fleet-wide enrichment. Populates
 * the shared enrichment cache for bidirectional warming with list pages.
 */
export function useMeshControlPlanes(
  clusterNames: string[],
  mcms: MultiClusterMesh[],
): [EnrichedControlPlane[], boolean, unknown] {
  const [enrichmentVersion, setEnrichmentVersion] = useState(0)
  const [enrichmentLoaded, setEnrichmentLoaded] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [refreshTick, setRefreshTick] = useState(0)
  const initialEnrichmentDone = useRef(false)

  useEffect(() => {
    const id = setInterval(() => setRefreshTick((v) => v + 1), CACHE_TTL_MS)
    return () => clearInterval(id)
  }, [])

  const clusterKey = useMemo(() => [...clusterNames].sort().join(','), [clusterNames])

  const [data, searchLoaded, searchError] = useFleetSearchPoll<FleetIstio[]>({
    groupVersionKind: istioGroupVersionKind,
    isList: true,
    namespaced: false,
  })

  const scopedResults = useMemo(() => {
    if (!data || clusterNames.length === 0) return []
    const clusterSet = new Set(clusterNames)
    return data.filter(
      (r): r is FleetIstio => Boolean(r.cluster && r.metadata?.name && clusterSet.has(r.cluster)),
    )
  }, [data, clusterKey]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let cancelled = false
    const now = Date.now()
    setError(null)

    if (scopedResults.length > 0 && !initialEnrichmentDone.current) {
      setEnrichmentLoaded(false)
    }

    const pending = scopedResults.filter((r) => {
      const cached = getFromEnrichmentCache(r.cluster, r.metadata?.name ?? '')
      return !cached
    }).map((r) => ({ cluster: r.cluster, name: r.metadata?.name ?? '' }))

    if (pending.length === 0) {
      setEnrichmentLoaded(true)
      if (scopedResults.length > 0) initialEnrichmentDone.current = true
      setEnrichmentVersion((v) => v + 1)
      return
    }

    ;(async () => {
      try {
        const chunkSize = getConcurrencyLimit(pending.length)
        for (let i = 0; i < pending.length; i += chunkSize) {
          if (cancelled) return
          const chunk = pending.slice(i, i + chunkSize)
          const results = await Promise.allSettled(
            chunk.map(({ cluster, name }) =>
              fleetK8sGet<Istio>({ model: istioModel, name, cluster })
                .then((r) => ({ cluster, name, data: r }))
            ),
          )
          if (cancelled) return
          for (const result of results) {
            if (result.status === 'fulfilled') {
              const { cluster, name, data } = result.value
              setInEnrichmentCache(cluster, name, data)
            }
          }
        }
        if (!cancelled) {
          setEnrichmentVersion((v) => v + 1)
          setEnrichmentLoaded(true)
          initialEnrichmentDone.current = true
        }
      } catch (e) {
        if (!cancelled) {
          setEnrichmentLoaded(true)
          initialEnrichmentDone.current = true
          setError(e)
        }
      }
    })()

    return () => { cancelled = true }
  }, [scopedResults, refreshTick])  // eslint-disable-line react-hooks/exhaustive-deps

  const mcmIndex = useMemo(() => buildMcmIndex(mcms), [mcms])

  const enrichedPlanes = useMemo(() => {
    return scopedResults.map((r) => {
      const cached = getFromEnrichmentCache(r.cluster, r.metadata?.name ?? '')
      const plane = toEnrichedControlPlane(r, cached)
      return {
        ...plane,
        managedBy: lookupMcm(mcmIndex, r.cluster, plane.controlPlaneNamespace),
      }
    })
  }, [scopedResults, mcmIndex, enrichmentVersion])  // eslint-disable-line react-hooks/exhaustive-deps

  return [enrichedPlanes, searchLoaded && enrichmentLoaded, error ?? searchError]
}
