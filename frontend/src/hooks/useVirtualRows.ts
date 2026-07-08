import { useCallback, useRef, useState } from 'react'

interface VirtualRowsResult<T> {
  bottomSpacer: number
  containerRef: (node: HTMLDivElement | null) => void
  topSpacer: number
  visibleItems: T[]
}

function computeIndices(
  scrollTop: number, containerHeight: number, itemsLength: number, rowHeight: number, overscan: number,
): [number, number] {
  const maxScroll = Math.max(0, itemsLength * rowHeight - containerHeight)
  const clamped = Math.min(scrollTop, maxScroll)
  const start = Math.max(0, Math.floor(clamped / rowHeight) - overscan)
  const end = Math.min(itemsLength, Math.ceil((clamped + containerHeight) / rowHeight) + overscan)
  return [start, end]
}

export function useVirtualRows<T>(items: T[], rowHeight = 40, overscan = 5): VirtualRowsResult<T> {
  const [scrollTop, setScrollTop] = useState(0)
  const [containerHeight, setContainerHeight] = useState(368)
  const nodeRef = useRef<HTMLDivElement | null>(null)

  const scrollTopRef = useRef(scrollTop)
  scrollTopRef.current = scrollTop

  const itemsLengthRef = useRef(items.length)
  itemsLengthRef.current = items.length

  const rowHeightRef = useRef(rowHeight)
  rowHeightRef.current = rowHeight

  const overscanRef = useRef(overscan)
  overscanRef.current = overscan

  const containerHeightRef = useRef(containerHeight)
  containerHeightRef.current = containerHeight

  const rafRef = useRef(0)

  const handleScroll = useCallback(() => {
    cancelAnimationFrame(rafRef.current)
    rafRef.current = requestAnimationFrame(() => {
      if (!nodeRef.current) return
      const newScrollTop = nodeRef.current.scrollTop
      const h = containerHeightRef.current
      const len = itemsLengthRef.current
      const rh = rowHeightRef.current
      const os = overscanRef.current

      const [curStart, curEnd] = computeIndices(scrollTopRef.current, h, len, rh, os)
      const [newStart, newEnd] = computeIndices(newScrollTop, h, len, rh, os)

      if (newStart !== curStart || newEnd !== curEnd) {
        setScrollTop(newScrollTop)
      }
    })
  }, [])

  const containerRef = useCallback((node: HTMLDivElement | null) => {
    if (nodeRef.current) {
      nodeRef.current.removeEventListener('scroll', handleScroll)
      cancelAnimationFrame(rafRef.current)
    }
    nodeRef.current = node
    if (node) {
      setContainerHeight(node.clientHeight || 368)
      node.addEventListener('scroll', handleScroll, { passive: true })
    }
  }, [handleScroll])

  const [startIndex, endIndex] = computeIndices(scrollTop, containerHeight, items.length, rowHeight, overscan)

  return {
    bottomSpacer: (items.length - endIndex) * rowHeight,
    containerRef,
    topSpacer: startIndex * rowHeight,
    visibleItems: items.slice(startIndex, endIndex),
  }
}
