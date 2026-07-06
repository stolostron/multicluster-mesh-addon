import { useMemo } from 'react'
import { useMultiClusterMeshes } from './useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from './useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from './useEnrichedControlPlanes'
import { getStatusRank } from '../components/MeshStatus'
import { oldestTimestamp } from '../utils/oldestTimestamp'
import { worstConditions } from '../utils/worstConditions'
import type { FleetMeshItem } from '../types/fleetMesh'
import type { EnrichedControlPlane } from '../types/istio'
import type { MultiClusterMesh } from '../types/multiClusterMesh'

export interface UseFleetMeshItemsResult {
  enrichedPlanes: EnrichedControlPlane[]
  enrichmentError: unknown
  enrichmentLoaded: boolean
  isFleetAvailable: boolean
  items: FleetMeshItem[]
  loaded: boolean
  mcms: MultiClusterMesh[]
  mcmsError: unknown
  mcmsLoaded: boolean
  searchError: unknown
  searchLoaded: boolean
}

function collectManagedMeshIDs(enrichedPlanes: EnrichedControlPlane[]): Set<string> {
  const ids = new Set<string>()
  for (const cp of enrichedPlanes) {
    if (cp.managedBy && cp.meshID) ids.add(cp.meshID)
  }
  return ids
}

function buildItems(
  mcms: MultiClusterMesh[],
  enrichedPlanes: EnrichedControlPlane[],
  enrichmentLoaded: boolean,
): FleetMeshItem[] {
  if (!enrichmentLoaded) return []

  const managedMeshIDs = collectManagedMeshIDs(enrichedPlanes)

  const managedItems: FleetMeshItem[] = mcms.map((mcm): FleetMeshItem => {
    const ns = mcm.metadata?.namespace ?? ''
    const name = mcm.metadata?.name ?? ''

    const correlatedPlanes = enrichedPlanes.filter(
      (cp) => cp.managedBy?.name === name && cp.managedBy?.namespace === ns
    )

    const { conditions, rank } = correlatedPlanes.length > 0 && correlatedPlanes.some(cp => cp.status?.conditions)
      ? worstConditions(correlatedPlanes)
      : { conditions: mcm.status?.conditions, rank: getStatusRank(mcm.status?.conditions) }

    const meshID = correlatedPlanes.find((cp) => cp.meshID)?.meshID

    return {
      metadata: {
        name,
        creationTimestamp: mcm.metadata?.creationTimestamp,
      },
      clusterCount: mcm.status?.clusterStatus?.length ?? 0,
      clusterSet: mcm.spec.clusterSet,
      conditions,
      detailLink: `/fleet-mesh/meshes/managed/${encodeURIComponent(ns)}/${encodeURIComponent(name)}`,
      kind: 'managed',
      mcm,
      mcmNamespace: ns,
      meshID,
      meshIDConflict: false,
      statusRank: rank,
      trustIssuer: mcm.spec.security?.trust?.certManager?.issuerRef?.name,
    }
  })

  const unmanaged = enrichedPlanes.filter((cp) => !cp.managedBy)

  const meshIDGroups = new Map<string, EnrichedControlPlane[]>()
  const standalones: EnrichedControlPlane[] = []
  for (const cp of unmanaged) {
    if (cp.meshID) {
      const group = meshIDGroups.get(cp.meshID)
      if (group) group.push(cp)
      else meshIDGroups.set(cp.meshID, [cp])
    } else {
      standalones.push(cp)
    }
  }

  const discoveredItems: FleetMeshItem[] = []

  for (const [meshID, planes] of meshIDGroups) {
    const conflict = managedMeshIDs.has(meshID)
    const { conditions, rank } = worstConditions(planes)
    discoveredItems.push({
      metadata: {
        name: meshID,
        creationTimestamp: oldestTimestamp(planes),
      },
      clusterCount: new Set(planes.map((cp) => cp.clusterName)).size,
      conditions,
      controlPlanes: planes,
      detailLink: `/fleet-mesh/meshes/discovered/${encodeURIComponent(meshID)}`,
      kind: 'discovered',
      meshID,
      meshIDConflict: conflict,
      statusRank: rank,
    })
  }

  for (const cp of standalones) {
    discoveredItems.push({
      metadata: {
        name: `${cp.clusterName}/${cp.metadata.name}`,
        creationTimestamp: cp.metadata.creationTimestamp,
      },
      clusterCount: 1,
      conditions: cp.status?.conditions,
      controlPlanes: [cp],
      detailLink: `/fleet-mesh/control-planes/standalone/${encodeURIComponent(cp.clusterName)}/${encodeURIComponent(cp.metadata.name)}`,
      kind: 'discovered',
      statusRank: getStatusRank(cp.status?.conditions),
    })
  }

  for (const item of managedItems) {
    if (item.meshID && meshIDGroups.has(item.meshID)) {
      item.meshIDConflict = true
    }
  }

  return [...managedItems, ...discoveredItems]
}

export function useFleetMeshItems(): UseFleetMeshItemsResult {
  const [mcms, mcmsLoaded, mcmsError] = useMultiClusterMeshes()
  const {
    results: searchResults,
    loaded: searchLoaded,
    error: searchError,
    isFleetAvailable,
  } = useDiscoveredControlPlanes()
  const [enrichedPlanes, , enrichmentLoaded, enrichmentError] = useEnrichedControlPlanes(
    searchResults,
    mcms ?? [],
  )

  const effectiveEnrichmentLoaded = !isFleetAvailable ? true : enrichmentLoaded

  const items = useMemo(
    () => buildItems(mcms ?? [], enrichedPlanes, effectiveEnrichmentLoaded),
    [mcms, enrichedPlanes, effectiveEnrichmentLoaded],
  )

  return {
    enrichedPlanes,
    enrichmentError,
    enrichmentLoaded: effectiveEnrichmentLoaded,
    isFleetAvailable,
    items,
    loaded: (mcmsLoaded ?? false) && effectiveEnrichmentLoaded,
    mcms: mcms ?? [],
    mcmsError,
    mcmsLoaded: mcmsLoaded ?? false,
    searchError,
    searchLoaded,
  }
}
