import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  Flex,
  FlexItem,
  SearchInput,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import { useVirtualRows } from '../hooks/useVirtualRows'
import { useMeshTranslation } from '../utils/i18nUtils'

export interface CategoryLabel {
  key: string
  label: string
}

export interface VirtualFilterColumn<T> {
  key: string
  label: string
  render: (item: T) => ReactNode
  width: string
}

interface VirtualFilterTableProps<T> {
  categorize: (item: T) => string
  categoryLabels: CategoryLabel[]
  columns: VirtualFilterColumn<T>[]
  emptyMessage: string
  items: T[]
  rowKey: (item: T) => string
  searchMatch: (item: T, query: string) => boolean
  searchPlaceholder: string
}

export function VirtualFilterTable<T>({
  categorize,
  categoryLabels,
  columns,
  emptyMessage,
  items,
  rowKey,
  searchMatch,
  searchPlaceholder,
}: VirtualFilterTableProps<T>,) {
  const { t } = useMeshTranslation()
  const allKey = categoryLabels[0].key
  const [filter, setFilter] = useState(allKey)
  const [searchInput, setSearchInput] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(searchInput), 200)
    return () => clearTimeout(timer)
  }, [searchInput])

  const categoryMap = useMemo(() => {
    const map = new Map<string, string>()
    items.forEach((item) => map.set(rowKey(item), categorize(item)))
    return map
  }, [items, categorize, rowKey])

  const counts = useMemo(() => {
    const result: Record<string, number> = {}
    for (const cat of categoryLabels) {
      if (cat.key !== allKey) result[cat.key] = 0
    }
    categoryMap.forEach((cat) => {
      if (result[cat] !== undefined) result[cat]++
    })
    return result
  }, [categoryMap, categoryLabels, allKey])

  const filtered = useMemo(() => {
    return items.filter((item) => {
      if (filter !== allKey && categoryMap.get(rowKey(item)) !== filter) return false
      if (debouncedSearch && !searchMatch(item, debouncedSearch)) return false
      return true
    })
  }, [items, categoryMap, filter, debouncedSearch, allKey, rowKey, searchMatch])

  const { visibleItems, topSpacer, bottomSpacer, containerRef } = useVirtualRows(filtered)

  return (
    <>
      <Flex style={{ marginBottom: '1rem' }}>
        <FlexItem>
          <ToggleGroup>
            {categoryLabels.map((cat) => (
              <ToggleGroupItem
                key={cat.key}
                text={t(cat.label, { count: cat.key === allKey ? items.length : counts[cat.key] })}
                isSelected={filter === cat.key}
                onChange={() => setFilter(cat.key)}
              />
            ))}
          </ToggleGroup>
        </FlexItem>
        <FlexItem grow={{ default: 'grow' }}>
          <SearchInput
            placeholder={t(searchPlaceholder)}
            value={searchInput}
            onChange={(_event, value) => setSearchInput(value)}
            onClear={() => { setSearchInput(''); setDebouncedSearch('') }}
          />
        </FlexItem>
      </Flex>

      <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
        <thead className="pf-v6-c-table__thead">
          <tr className="pf-v6-c-table__tr">
            {columns.map((col) => (
              <th className="pf-v6-c-table__th" scope="col" style={{ width: col.width }} key={col.key}>{t(col.label)}</th>
            ))}
          </tr>
        </thead>
      </table>
      <div ref={containerRef} style={{ maxHeight: '368px', overflowY: 'auto' }}>
        {filtered.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '1rem' }}>
            {t(emptyMessage)}
          </div>
        ) : (
          <table className="pf-v6-c-table pf-m-grid-md pf-m-compact" role="grid" style={{ tableLayout: 'fixed' }}>
            <colgroup>
              {columns.map((col, i) => <col key={i} style={{ width: col.width }} />)}
            </colgroup>
            <tbody className="pf-v6-c-table__tbody">
              {topSpacer > 0 && <tr><td colSpan={columns.length} style={{ height: topSpacer, padding: 0, border: 'none' }} /></tr>}
              {visibleItems.map((item) => (
                <tr className="pf-v6-c-table__tr" key={rowKey(item)}>
                  {columns.map((col) => (
                    <td
                      className="pf-v6-c-table__td"
                      key={col.key}
                      style={{ width: col.width, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    >
                      {col.render(item)}
                    </td>
                  ))}
                </tr>
              ))}
              {bottomSpacer > 0 && <tr><td colSpan={columns.length} style={{ height: bottomSpacer, padding: 0, border: 'none' }} /></tr>}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}
