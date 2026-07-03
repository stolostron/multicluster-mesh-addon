import { renderHook, waitFor, act } from '@testing-library/react'
import { useEnrichedControlPlanes, __resetEnrichmentCache } from '../useEnrichedControlPlanes'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
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

afterEach(() => {
  rstest.clearAllMocks()
  rstest.useRealTimers()
  __resetEnrichmentCache()
})

describe('useEnrichedControlPlanes', () => {
  it('returns search-derived fields immediately before enrichment', () => {
    rstest.mocked(fleetK8sGet).mockReturnValue(new Promise(() => {}))
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))
    const [planes] = result.current
    expect(planes).toHaveLength(1)
    expect(planes[0].clusterName).toBe('cluster-a')
    expect(planes[0].metadata.name).toBe('default')
    expect(planes[0].version).toBeUndefined()
    expect(planes[0].meshID).toBeUndefined()
  })

  it('enrichmentLoaded starts false and becomes true after fleetK8sGet resolves', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    expect(result.current[2]).toBe(false)

    await waitFor(() => {
      expect(result.current[2]).toBe(true)
    })
  })

  it('populates enrichment fields after fleetK8sGet resolves', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system', 'mesh1'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    await waitFor(() => {
      expect(result.current[0][0].version).toBe('v1.24.0')
      expect(result.current[0][0].meshID).toBe('mesh1')
      expect(result.current[0][0].controlPlaneNamespace).toBe('istio-system')
    })
  })

  it('caches enrichment and does not re-fetch on identical search results', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results } },
    )

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(1)

    // Re-render with a new array reference but same content
    const results2 = [makeSearchResult('cluster-a', 'default')]
    rerender({ r: results2 })
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(1)
  })

  it('fetches enrichment for new CRs when search results change', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results1 = [makeSearchResult('cluster-a', 'default')]
    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results1 } },
    )

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(1)

    const results2 = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
    ]
    rerender({ r: results2 })

    await waitFor(() => {
      expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(2)
    })
  })

  it('re-fetches after cache TTL expires', async () => {
    rstest.useFakeTimers()
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]

    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results } },
    )

    await act(async () => { await rstest.runAllTimersAsync() })
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(1)

    // Advance past the 150s TTL
    rstest.setSystemTime(Date.now() + 160_000)

    // Re-render with a new reference to trigger effect re-evaluation
    // The searchKey is the same, so the effect won't re-run from the key alone.
    // But the TTL check happens inside the effect, so we need a new key.
    const results2 = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-c', 'default'),
    ]
    rerender({ r: results2 })

    await act(async () => { await rstest.runAllTimersAsync() })
    // cluster-a should be re-fetched (TTL expired) + cluster-c is new
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(3)
  })

  it('leaves enrichment undefined when fleetK8sGet fails', async () => {
    rstest.mocked(fleetK8sGet).mockRejectedValue(new Error('cluster unreachable'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(result.current[0][0].version).toBeUndefined()
    expect(result.current[0][0].meshID).toBeUndefined()
  })

  it('correlates with MultiClusterMesh when enrichment namespace matches', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const mcms: MultiClusterMesh[] = [{
      apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
      kind: 'MultiClusterMesh',
      metadata: { name: 'my-mesh', namespace: 'mesh-system' },
      spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
      status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
    }]

    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, mcms))

    await waitFor(() => {
      expect(result.current[0][0].managedBy).toEqual({ name: 'my-mesh', namespace: 'mesh-system' })
    })
  })

  it('correlates using default istio-system when MCM has no explicit controlPlane.namespace', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const mcms: MultiClusterMesh[] = [{
      apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
      kind: 'MultiClusterMesh',
      metadata: { name: 'my-mesh', namespace: 'mesh-system' },
      spec: { clusterSet: 'global' },
      status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
    }]

    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, mcms))

    await waitFor(() => {
      expect(result.current[0][0].managedBy).toEqual({ name: 'my-mesh', namespace: 'mesh-system' })
    })
  })

  it('correlates using default istio-system when Istio CR controlPlaneNamespace is undefined', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio(undefined as any))
    const results = [makeSearchResult('cluster-a', 'default')]
    const mcms: MultiClusterMesh[] = [{
      apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
      kind: 'MultiClusterMesh',
      metadata: { name: 'my-mesh', namespace: 'mesh-system' },
      spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
      status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
    }]

    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, mcms))

    await waitFor(() => {
      expect(result.current[0][0].managedBy).toEqual({ name: 'my-mesh', namespace: 'mesh-system' })
    })
  })

  it('does not correlate when MCM has empty clusterStatus', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const mcms: MultiClusterMesh[] = [{
      apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
      kind: 'MultiClusterMesh',
      metadata: { name: 'my-mesh', namespace: 'mesh-system' },
      spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
      status: { clusterStatus: [] },
    }]

    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, mcms))

    await waitFor(() => {
      expect(result.current[0][0].managedBy).toBeUndefined()
    })
  })

  it('does not correlate when namespace does not match', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-other'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const mcms: MultiClusterMesh[] = [{
      apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
      kind: 'MultiClusterMesh',
      metadata: { name: 'my-mesh', namespace: 'mesh-system' },
      spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
      status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
    }]

    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, mcms))

    await waitFor(() => {
      expect(result.current[0][0].managedBy).toBeUndefined()
    })
  })

  it('reuses module-level cache across component remounts without new network calls', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]

    const { result, unmount } = renderHook(() => useEnrichedControlPlanes(results as any, []))
    await waitFor(() => expect(result.current[2]).toBe(true))
    const callsAfterFirstMount = rstest.mocked(fleetK8sGet).mock.calls.length

    unmount()

    const { result: result2 } = renderHook(() => useEnrichedControlPlanes(results as any, []))
    await waitFor(() => expect(result2.current[2]).toBe(true))
    expect(rstest.mocked(fleetK8sGet).mock.calls.length).toBe(callsAfterFirstMount)
    expect(result2.current[0][0].version).toBe('v1.24.0')
  })

  it('handles partial enrichment failure — successful CPs are enriched, failed CPs remain undefined', async () => {
    rstest.mocked(fleetK8sGet).mockImplementation(({ cluster }: any) => {
      if (cluster === 'cluster-a') return Promise.resolve(makeIstio('istio-system', 'mesh1'))
      return Promise.reject(new Error('cluster unreachable'))
    })
    const results = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'secondary'),
      makeSearchResult('cluster-c', 'tertiary'),
    ]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    await waitFor(() => expect(result.current[2]).toBe(true))

    const [planes, , enrichmentLoaded] = result.current
    expect(enrichmentLoaded).toBe(true)

    const cpA = planes.find((p) => p.clusterName === 'cluster-a')!
    expect(cpA.version).toBe('v1.24.0')
    expect(cpA.meshID).toBe('mesh1')
    expect(cpA.controlPlaneNamespace).toBe('istio-system')

    const cpB = planes.find((p) => p.clusterName === 'cluster-b')!
    expect(cpB.version).toBeUndefined()
    expect(cpB.meshID).toBeUndefined()
    expect(cpB.controlPlaneNamespace).toBeUndefined()

    const cpC = planes.find((p) => p.clusterName === 'cluster-c')!
    expect(cpC.version).toBeUndefined()
    expect(cpC.meshID).toBeUndefined()
    expect(cpC.controlPlaneNamespace).toBeUndefined()
  })

  it('does not reset enrichmentLoaded on subsequent search poll updates', async () => {
    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
    const results1 = [makeSearchResult('cluster-a', 'default')]

    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results1 } },
    )

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(rstest.mocked(fleetK8sGet)).toHaveBeenCalledTimes(1)

    rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio('istio-system', 'mesh2'))
    const results2 = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
    ]
    rerender({ r: results2 })

    // After initial enrichment, enrichmentLoaded stays true during subsequent cycles
    expect(result.current[2]).toBe(true)

    await waitFor(() => {
      expect(rstest.mocked(fleetK8sGet).mock.calls.length).toBeGreaterThanOrEqual(2)
    })
    expect(result.current[2]).toBe(true)
  })
})
