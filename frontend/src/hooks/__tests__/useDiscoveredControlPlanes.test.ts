import { renderHook } from '@testing-library/react'
import { useDiscoveredControlPlanes } from '../useDiscoveredControlPlanes'
import { useFleetSearchPoll, useIsFleetAvailable } from '@stolostron/multicluster-sdk'

afterEach(() => rstest.clearAllMocks())

beforeEach(() => {
  rstest.mocked(useIsFleetAvailable).mockReturnValue(true)
})

describe('useDiscoveredControlPlanes', () => {
  it('returns empty results when search returns undefined', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([undefined, true, undefined, rstest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.results).toEqual([])
    expect(result.current.loaded).toBe(true)
  })

  it('returns empty results when search returns empty array', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.results).toEqual([])
  })

  it('filters out results without a cluster property', () => {
    const results = [
      { metadata: { name: 'a' }, cluster: 'cluster-1', spec: { namespace: 'istio-system' } },
      { metadata: { name: 'b' }, spec: { namespace: 'istio-system' } },
      { metadata: { name: 'c' }, cluster: undefined, spec: { namespace: 'istio-system' } },
      { metadata: { name: 'd' }, cluster: 'cluster-2', spec: { namespace: 'istio-system' } },
    ]
    rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.results).toHaveLength(2)
    expect(result.current.results[0].cluster).toBe('cluster-1')
    expect(result.current.results[1].cluster).toBe('cluster-2')
  })

  it('passes through isFleetAvailable from useIsFleetAvailable', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
    rstest.mocked(useIsFleetAvailable).mockReturnValue(false)
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.isFleetAvailable).toBe(false)
  })

  it('passes through error from useFleetSearchPoll', () => {
    const err = new Error('search failed')
    rstest.mocked(useFleetSearchPoll).mockReturnValue([undefined, true, err, rstest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.error).toBe(err)
  })

  it('passes through loaded=false when search is pending', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([undefined, false, undefined, rstest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.loaded).toBe(false)
  })

  it('provides a refetch callback', () => {
    const refetchFn = rstest.fn()
    rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, refetchFn])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.refetch).toBe(refetchFn)
  })
})
