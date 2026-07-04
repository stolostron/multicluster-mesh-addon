import { renderHook, waitFor, act } from '@testing-library/react'
import { useMeshControlPlanes } from '../useMeshControlPlanes'
import { __resetEnrichmentCache, getFromEnrichmentCache, setInEnrichmentCache } from '../useEnrichedControlPlanes'
import { fleetK8sGet, useFleetSearchPoll } from '@stolostron/multicluster-sdk'
import { makeSearchResult } from '../../__fixtures__/testFactories'
import type { Istio } from '../../types/istio'
import type { MultiClusterMesh } from '../../types/multiClusterMesh'

const makeIstio = (namespace = 'istio-system', meshID?: string): Istio => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name: 'default' },
  spec: {
    namespace,
    version: 'v1.24.0',
    values: meshID ? { global: { meshID } } : undefined,
  },
  status: { conditions: [{ type: 'Ready', status: 'True' as const }] },
})

const makeMcm = (clusterNames: string[]): MultiClusterMesh => ({
  apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
  kind: 'MultiClusterMesh',
  metadata: { name: 'my-mesh', namespace: 'mesh-system' },
  spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
  status: { clusterStatus: clusterNames.map((c) => ({ clusterName: c })) },
})

afterEach(() => {
  rstest.clearAllMocks()
  rstest.useRealTimers()
  __resetEnrichmentCache()
})

function setupSearchPoll(results: any[]) {
  rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
}

describe('useMeshControlPlanes', () => {
  it('returns empty planes when clusterNames is empty', () => {
    setupSearchPoll([makeSearchResult('cluster-a', 'default')])
    const { result } = renderHook(() => useMeshControlPlanes([], []))
    expect(result.current[0]).toHaveLength(0)
  })

  it('only enriches CPs on specified clusters', async () => {
    const allResults = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
      makeSearchResult('cluster-c', 'default'),
    ]
    setupSearchPoll(allResults)
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system', 'mesh1'))

    const { result } = renderHook(() => useMeshControlPlanes(['cluster-a', 'cluster-b'], []))

    await waitFor(() => expect(result.current[1]).toBe(true))

    expect(result.current[0]).toHaveLength(2)
    expect(result.current[0].map((p) => p.clusterName).sort()).toEqual(['cluster-a', 'cluster-b'])
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(2)
  })

  it('populates the shared enrichment cache for bidirectional warming', async () => {
    setupSearchPoll([makeSearchResult('cluster-a', 'default')])
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())

    const { result } = renderHook(() => useMeshControlPlanes(['cluster-a'], []))
    await waitFor(() => expect(result.current[1]).toBe(true))

    expect(getFromEnrichmentCache('cluster-a', 'default')).toBeDefined()
  })

  it('reads from warm cache without network calls', async () => {
    setInEnrichmentCache('cluster-a', 'default', makeIstio('istio-system', 'mesh1'))
    setupSearchPoll([makeSearchResult('cluster-a', 'default')])

    const { result } = renderHook(() => useMeshControlPlanes(['cluster-a'], []))
    await waitFor(() => expect(result.current[1]).toBe(true))

    expect(rstest.mocked(fleetK8sGet)).not.toHaveBeenCalled()
    expect(result.current[0][0].meshID).toBe('mesh1')
  })

  it('correlates control planes with MCMs', async () => {
    setupSearchPoll([makeSearchResult('cluster-a', 'default')])
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system'))
    const mcms = [makeMcm(['cluster-a'])]

    const { result } = renderHook(() => useMeshControlPlanes(['cluster-a'], mcms))
    await waitFor(() => expect(result.current[1]).toBe(true))

    expect(result.current[0][0].managedBy).toEqual({ name: 'my-mesh', namespace: 'mesh-system' })
  })

  it('exposes error state from search poll failures', async () => {
    const searchErr = new Error('network error')
    rstest.mocked(useFleetSearchPoll).mockReturnValue([
      [makeSearchResult('cluster-a', 'default')], true, searchErr, rstest.fn(),
    ])
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())

    const { result } = renderHook(() => useMeshControlPlanes(['cluster-a'], []))
    await waitFor(() => expect(result.current[1]).toBe(true))

    expect(result.current[2]).toBe(searchErr)
  })
})
