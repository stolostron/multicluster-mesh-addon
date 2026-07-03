import { useCallback, useRef, useState } from 'react'

interface VirtualRowsResult<T> {
  bottomSpacer: number
  containerRef: (node: HTMLDivElement | null) => void
  topSpacer: number
  visibleItems: T[]
}

export function useVirtualRows<T>(items: T[], rowHeight = 40, overscan = 5): VirtualRowsResult<T> {
  const [scrollTop, setScrollTop] = useState(0)
  const [containerHeight, setContainerHeight] = useState(368)
  const nodeRef = useRef<HTMLDivElement | null>(null)

  const handleScroll = useCallback(() => {
    if (nodeRef.current) setScrollTop(nodeRef.current.scrollTop)
  }, [])

  const containerRef = useCallback((node: HTMLDivElement | null) => {
    if (nodeRef.current) {
      nodeRef.current.removeEventListener('scroll', handleScroll)
    }
    nodeRef.current = node
    if (node) {
      setContainerHeight(node.clientHeight || 368)
      node.addEventListener('scroll', handleScroll, { passive: true })
    }
  }, [handleScroll])

  const startIndex = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan)
  const endIndex = Math.min(items.length, Math.ceil((scrollTop + containerHeight) / rowHeight) + overscan)

  return {
    bottomSpacer: (items.length - endIndex) * rowHeight,
    containerRef,
    topSpacer: startIndex * rowHeight,
    visibleItems: items.slice(startIndex, endIndex),
  }
}
