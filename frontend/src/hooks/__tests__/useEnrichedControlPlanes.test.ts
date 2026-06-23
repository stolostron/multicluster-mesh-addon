import { renderHook, waitFor, act } from '@testing-library/react'
import { useEnrichedControlPlanes } from '../useEnrichedControlPlanes'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio } from '../../types/istio'
import type { MultiClusterMesh } from '../../types/multiClusterMesh'

const mockFleetK8sGet = fleetK8sGet as jest.Mock

const makeSearchResult = (cluster: string, name: string) => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  cluster,
  spec: { namespace: 'istio-system' },
})

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
  jest.clearAllMocks()
  jest.useRealTimers()
})

describe('useEnrichedControlPlanes', () => {
  it('returns search-derived fields immediately before enrichment', () => {
    mockFleetK8sGet.mockReturnValue(new Promise(() => {}))
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
    mockFleetK8sGet.mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    expect(result.current[2]).toBe(false)

    await waitFor(() => {
      expect(result.current[2]).toBe(true)
    })
  })

  it('populates enrichment fields after fleetK8sGet resolves', async () => {
    mockFleetK8sGet.mockResolvedValue(makeIstio('istio-system', 'mesh1'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    await waitFor(() => {
      expect(result.current[0][0].version).toBe('v1.24.0')
      expect(result.current[0][0].meshID).toBe('mesh1')
      expect(result.current[0][0].controlPlaneNamespace).toBe('istio-system')
    })
  })

  it('caches enrichment and does not re-fetch on identical search results', async () => {
    mockFleetK8sGet.mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results } },
    )

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(mockFleetK8sGet).toHaveBeenCalledTimes(1)

    // Re-render with a new array reference but same content
    const results2 = [makeSearchResult('cluster-a', 'default')]
    rerender({ r: results2 })
    expect(mockFleetK8sGet).toHaveBeenCalledTimes(1)
  })

  it('fetches enrichment for new CRs when search results change', async () => {
    mockFleetK8sGet.mockResolvedValue(makeIstio())
    const results1 = [makeSearchResult('cluster-a', 'default')]
    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results1 } },
    )

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(mockFleetK8sGet).toHaveBeenCalledTimes(1)

    const results2 = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
    ]
    rerender({ r: results2 })

    await waitFor(() => {
      expect(mockFleetK8sGet).toHaveBeenCalledTimes(2)
    })
  })

  it('re-fetches after cache TTL expires', async () => {
    jest.useFakeTimers()
    mockFleetK8sGet.mockResolvedValue(makeIstio())
    const results = [makeSearchResult('cluster-a', 'default')]

    const { result, rerender } = renderHook(
      ({ r }) => useEnrichedControlPlanes(r as any, []),
      { initialProps: { r: results } },
    )

    await act(async () => { await jest.runAllTimersAsync() })
    expect(mockFleetK8sGet).toHaveBeenCalledTimes(1)

    // Advance past the 150s TTL
    jest.setSystemTime(Date.now() + 160_000)

    // Re-render with a new reference to trigger effect re-evaluation
    // The searchKey is the same, so the effect won't re-run from the key alone.
    // But the TTL check happens inside the effect, so we need a new key.
    const results2 = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-c', 'default'),
    ]
    rerender({ r: results2 })

    await act(async () => { await jest.runAllTimersAsync() })
    // cluster-a should be re-fetched (TTL expired) + cluster-c is new
    expect(mockFleetK8sGet).toHaveBeenCalledTimes(3)
  })

  it('leaves enrichment undefined when fleetK8sGet fails', async () => {
    mockFleetK8sGet.mockRejectedValue(new Error('cluster unreachable'))
    const results = [makeSearchResult('cluster-a', 'default')]
    const { result } = renderHook(() => useEnrichedControlPlanes(results as any, []))

    await waitFor(() => expect(result.current[2]).toBe(true))
    expect(result.current[0][0].version).toBeUndefined()
    expect(result.current[0][0].meshID).toBeUndefined()
  })

  it('correlates with MultiClusterMesh when enrichment namespace matches', async () => {
    mockFleetK8sGet.mockResolvedValue(makeIstio('istio-system'))
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
    mockFleetK8sGet.mockResolvedValue(makeIstio('istio-system'))
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
    mockFleetK8sGet.mockResolvedValue(makeIstio(undefined as any))
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
    mockFleetK8sGet.mockResolvedValue(makeIstio('istio-system'))
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
    mockFleetK8sGet.mockResolvedValue(makeIstio('istio-other'))
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
})
