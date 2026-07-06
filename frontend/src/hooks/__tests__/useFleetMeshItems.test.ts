import { renderHook } from '@testing-library/react'
import { useMultiClusterMeshes } from '../../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../../hooks/useEnrichedControlPlanes'
import { useFleetMeshItems } from '../../hooks/useFleetMeshItems'
import { makeMesh, makeEnrichedCP } from '../../__fixtures__/testFactories'
import type { EnrichedControlPlane } from '../../types/istio'

rstest.mock('../../hooks/useMultiClusterMeshes', { mock: true })
rstest.mock('../../hooks/useDiscoveredControlPlanes', { mock: true })
rstest.mock('../../hooks/useEnrichedControlPlanes', { mock: true })

function setupMocks({
  mcms = [] as any[],
  mcmsLoaded = true,
  mcmsError = undefined as unknown,
  searchResults = [] as any[],
  searchLoaded = true,
  searchError = undefined as unknown,
  isFleetAvailable = true,
  enrichedPlanes = [] as EnrichedControlPlane[],
  enrichmentLoaded = true,
  enrichmentError = undefined as unknown,
} = {}) {
  rstest.mocked(useMultiClusterMeshes).mockReturnValue([mcms, mcmsLoaded, mcmsError])
  rstest.mocked(useDiscoveredControlPlanes).mockReturnValue({
    results: searchResults,
    loaded: searchLoaded,
    error: searchError,
    isFleetAvailable,
    refetch: rstest.fn(),
  })
  rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enrichedPlanes, true, enrichmentLoaded, enrichmentError])
}

afterEach(() => rstest.clearAllMocks())

describe('useFleetMeshItems', () => {
  it('returns empty items when enrichmentLoaded is false', () => {
    setupMocks({ enrichmentLoaded: false })

    const { result } = renderHook(() => useFleetMeshItems())

    expect(result.current.items).toEqual([])
  })

  it('converts MCM CRs to managed FleetMeshItems with correct flattened fields', () => {
    const mcm = makeMesh({
      metadata: { name: 'my-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
      spec: { clusterSet: 'global', security: { trust: { certManager: { issuerRef: { name: 'mesh-ca' } } } } },
      status: { conditions: [{ type: 'Ready', status: 'True' }], clusterStatus: [{ clusterName: 'cluster-a' }] },
    })
    setupMocks({ mcms: [mcm] })

    const { result } = renderHook(() => useFleetMeshItems())
    const item = result.current.items[0]

    expect(item.kind).toBe('managed')
    expect(item.metadata.name).toBe('my-mesh')
    expect(item.metadata.creationTimestamp).toBe('2026-06-22T12:00:00Z')
    expect(item.clusterSet).toBe('global')
    expect(item.mcmNamespace).toBe('mesh-system')
    expect(item.clusterCount).toBe(1)
    expect(item.trustIssuer).toBe('mesh-ca')
    expect(item.detailLink).toBe('/fleet-mesh/meshes/managed/mesh-system/my-mesh')
    expect(item.statusRank).toBe(0)
    expect(item.conditions).toEqual([{ type: 'Ready', status: 'True' }])
    expect(item.mcm).toBe(mcm)
  })

  it('groups unmanaged enriched CRs by meshID into discovered items', () => {
    const planes = [
      makeEnrichedCP({ meshID: 'shared-mesh' }),
      makeEnrichedCP({ clusterName: 'cluster-b', meshID: 'shared-mesh' }),
    ]
    setupMocks({ enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const discovered = result.current.items.filter((i) => i.kind === 'discovered')

    expect(discovered).toHaveLength(1)
    expect(discovered[0].metadata.name).toBe('shared-mesh')
    expect(discovered[0].meshID).toBe('shared-mesh')
    expect(discovered[0].clusterCount).toBe(2)
    expect(discovered[0].controlPlanes).toHaveLength(2)
    expect(discovered[0].detailLink).toBe('/fleet-mesh/meshes/discovered/shared-mesh')
  })

  it('standalone CRs (no meshID) become individual discovered items', () => {
    const planes = [
      makeEnrichedCP(),
      makeEnrichedCP({ clusterName: 'cluster-b', metadata: { name: 'istio' } }),
    ]
    setupMocks({ enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const discovered = result.current.items.filter((i) => i.kind === 'discovered')

    expect(discovered).toHaveLength(2)
    expect(discovered[0].metadata.name).toBe('cluster-a/default')
    expect(discovered[0].clusterCount).toBe(1)
    expect(discovered[1].metadata.name).toBe('cluster-b/istio')
    expect(discovered[1].detailLink).toBe('/fleet-mesh/control-planes/standalone/cluster-b/istio')
  })

  it('excludes managed CRs from discovered items', () => {
    const planes = [
      makeEnrichedCP({ managedBy: { name: 'my-mesh', namespace: 'mesh-system' } }),
      makeEnrichedCP({ clusterName: 'cluster-b' }),
    ]
    setupMocks({ enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const discovered = result.current.items.filter((i) => i.kind === 'discovered')

    expect(discovered).toHaveLength(1)
    expect(discovered[0].metadata.name).toBe('cluster-b/default')
  })

  it('sets meshIDConflict on both managed and discovered items when meshIDs collide', () => {
    const mcm = makeMesh({
      metadata: { name: 'my-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
      status: { conditions: [{ type: 'Ready', status: 'True' }], clusterStatus: [{ clusterName: 'cluster-a' }] },
    })
    const planes = [
      makeEnrichedCP({
        meshID: 'conflict-id',
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
      }),
      makeEnrichedCP({ clusterName: 'cluster-x', meshID: 'conflict-id' }),
    ]
    setupMocks({ mcms: [mcm], enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const managed = result.current.items.find((i) => i.kind === 'managed')
    const discovered = result.current.items.find((i) => i.kind === 'discovered')

    expect(managed!.meshIDConflict).toBe(true)
    expect(discovered!.meshIDConflict).toBe(true)
  })

  it('reflects worst CR statusRank and conditions in meshID group', () => {
    const planes = [
      makeEnrichedCP({
        meshID: 'mesh1',
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
      makeEnrichedCP({
        clusterName: 'cluster-b',
        meshID: 'mesh1',
        status: { conditions: [{ type: 'Ready', status: 'False', reason: 'ReconcileError' }] },
      }),
    ]
    setupMocks({ enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const discovered = result.current.items.find((i) => i.kind === 'discovered')

    expect(discovered!.statusRank).toBe(3)
    expect(discovered!.conditions).toEqual([{ type: 'Ready', status: 'False', reason: 'ReconcileError' }])
  })

  it('standalone items with colliding names on different clusters have unique metadata.name', () => {
    const planes = [
      makeEnrichedCP(),
      makeEnrichedCP({ clusterName: 'cluster-b' }),
    ]
    setupMocks({ enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const discovered = result.current.items.filter((i) => i.kind === 'discovered')
    const names = discovered.map((i) => i.metadata.name)

    expect(names).toContain('cluster-a/default')
    expect(names).toContain('cluster-b/default')
    expect(new Set(names).size).toBe(2)
  })

  it('exposes MCM items and enrichmentError when enrichment fails', () => {
    const mcm = makeMesh({
      metadata: { name: 'my-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
      status: { conditions: [{ type: 'Ready', status: 'True' }], clusterStatus: [{ clusterName: 'cluster-a' }] },
    })
    const err = new Error('enrichment failed')
    setupMocks({ mcms: [mcm], enrichmentError: err })

    const { result } = renderHook(() => useFleetMeshItems())
    const managed = result.current.items.filter((i) => i.kind === 'managed')

    expect(managed).toHaveLength(1)
    expect(result.current.enrichmentError).toBe(err)
  })

  it('when ACM unavailable, isFleetAvailable=false and only managed items shown', () => {
    const mcm = makeMesh({
      metadata: { name: 'my-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
      status: { conditions: [{ type: 'Ready', status: 'True' }], clusterStatus: [{ clusterName: 'cluster-a' }] },
    })
    rstest.mocked(useMultiClusterMeshes).mockReturnValue([[mcm], true, undefined])
    rstest.mocked(useDiscoveredControlPlanes).mockReturnValue({
      results: [],
      loaded: false,
      error: undefined,
      isFleetAvailable: false,
      refetch: rstest.fn(),
    })
    rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[], true, false, undefined])

    const { result } = renderHook(() => useFleetMeshItems())

    expect(result.current.isFleetAvailable).toBe(false)
    expect(result.current.enrichmentLoaded).toBe(true)
    expect(result.current.items).toHaveLength(1)
    expect(result.current.items[0].kind).toBe('managed')
  })

  it('falls back to MCM conditions when correlated CPs have no status object', () => {
    const mcm = makeMesh({
      metadata: { name: 'my-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
      status: { conditions: [{ type: 'Ready', status: 'True' }], clusterStatus: [{ clusterName: 'cluster-a' }] },
    })
    const planes = [
      makeEnrichedCP({
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
        status: undefined,
      }),
    ]
    setupMocks({ mcms: [mcm], enrichedPlanes: planes })

    const { result } = renderHook(() => useFleetMeshItems())
    const managed = result.current.items.find((i) => i.kind === 'managed')

    expect(managed!.conditions).toEqual([{ type: 'Ready', status: 'True' }])
    expect(managed!.statusRank).toBe(0)
  })

  it('loaded equals mcmsLoaded AND enrichmentLoaded', () => {
    setupMocks({ mcmsLoaded: true, enrichmentLoaded: false })
    const { result: r1 } = renderHook(() => useFleetMeshItems())
    expect(r1.current.loaded).toBe(false)

    setupMocks({ mcmsLoaded: false, enrichmentLoaded: true })
    const { result: r2 } = renderHook(() => useFleetMeshItems())
    expect(r2.current.loaded).toBe(false)

    setupMocks({ mcmsLoaded: true, enrichmentLoaded: true })
    const { result: r3 } = renderHook(() => useFleetMeshItems())
    expect(r3.current.loaded).toBe(true)
  })
})
