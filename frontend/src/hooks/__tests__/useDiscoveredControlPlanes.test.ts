import { renderHook } from '@testing-library/react'
import { useDiscoveredControlPlanes } from '../useDiscoveredControlPlanes'
import { useFleetSearchPoll, useIsFleetAvailable } from '@stolostron/multicluster-sdk'

const mockUseFleetSearchPoll = useFleetSearchPoll as jest.Mock
const mockUseIsFleetAvailable = useIsFleetAvailable as jest.Mock

afterEach(() => jest.clearAllMocks())

beforeEach(() => {
  mockUseIsFleetAvailable.mockReturnValue(true)
})

describe('useDiscoveredControlPlanes', () => {
  it('returns empty results when search returns undefined', () => {
    mockUseFleetSearchPoll.mockReturnValue([undefined, true, undefined, jest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.results).toEqual([])
    expect(result.current.loaded).toBe(true)
  })

  it('returns empty results when search returns empty array', () => {
    mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, jest.fn()])
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
    mockUseFleetSearchPoll.mockReturnValue([results, true, undefined, jest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.results).toHaveLength(2)
    expect(result.current.results[0].cluster).toBe('cluster-1')
    expect(result.current.results[1].cluster).toBe('cluster-2')
  })

  it('passes through isFleetAvailable from useIsFleetAvailable', () => {
    mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, jest.fn()])
    mockUseIsFleetAvailable.mockReturnValue(false)
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.isFleetAvailable).toBe(false)
  })

  it('passes through error from useFleetSearchPoll', () => {
    const err = new Error('search failed')
    mockUseFleetSearchPoll.mockReturnValue([undefined, true, err, jest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.error).toBe(err)
  })

  it('passes through loaded=false when search is pending', () => {
    mockUseFleetSearchPoll.mockReturnValue([undefined, false, undefined, jest.fn()])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.loaded).toBe(false)
  })

  it('provides a refetch callback', () => {
    const refetchFn = jest.fn()
    mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, refetchFn])
    const { result } = renderHook(() => useDiscoveredControlPlanes())
    expect(result.current.refetch).toBe(refetchFn)
  })
})
