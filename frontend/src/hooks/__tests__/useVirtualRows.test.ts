import { renderHook } from '@testing-library/react'
import { useVirtualRows } from '../useVirtualRows'

describe('useVirtualRows', () => {
  const items = Array.from({ length: 100 }, (_, i) => `item-${i}`)

  it('returns all items when count fits within container', () => {
    const smallList = ['a', 'b', 'c']
    const { result } = renderHook(() => useVirtualRows(smallList))
    expect(result.current.visibleItems).toEqual(smallList)
    expect(result.current.topSpacer).toBe(0)
    expect(result.current.bottomSpacer).toBe(0)
  })

  it('returns a windowed subset for large lists', () => {
    const { result } = renderHook(() => useVirtualRows(items))
    expect(result.current.visibleItems.length).toBeLessThan(items.length)
    expect(result.current.visibleItems.length).toBeGreaterThan(0)
  })

  it('starts at the beginning of the list (scrollTop=0)', () => {
    const { result } = renderHook(() => useVirtualRows(items))
    expect(result.current.visibleItems[0]).toBe('item-0')
    expect(result.current.topSpacer).toBe(0)
  })

  it('bottomSpacer accounts for items below the visible window', () => {
    const { result } = renderHook(() => useVirtualRows(items, 40, 5))
    const visibleCount = result.current.visibleItems.length
    const expectedBottom = (items.length - visibleCount) * 40
    expect(result.current.bottomSpacer).toBe(expectedBottom)
  })

  it('returns empty result for empty items', () => {
    const { result } = renderHook(() => useVirtualRows([]))
    expect(result.current.visibleItems).toEqual([])
    expect(result.current.topSpacer).toBe(0)
    expect(result.current.bottomSpacer).toBe(0)
  })

  it('respects custom rowHeight', () => {
    const { result } = renderHook(() => useVirtualRows(items, 20, 5))
    expect(result.current.visibleItems.length).toBeGreaterThan(
      renderHook(() => useVirtualRows(items, 80, 5)).result.current.visibleItems.length,
    )
  })

  it('provides a containerRef callback', () => {
    const { result } = renderHook(() => useVirtualRows(items))
    expect(typeof result.current.containerRef).toBe('function')
  })
})
