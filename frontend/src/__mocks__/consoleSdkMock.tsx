import * as React from 'react'

// Runtime values returned by the mocked hooks. Tests can override these with
// mockReturnValue() on the exported jest.fn() references.
export const useK8sWatchResource = jest.fn(() => [null, false, null])

export const useListPageFilter = jest.fn((data: unknown[]) => [
  data ?? [],
  data ?? [],
  jest.fn(),
])

export const useActiveColumns = jest.fn(
  (opts: { columns?: { id: string }[] }) => [opts?.columns ?? [], true],
)

export const ListPageHeader: React.FC<{ title: string }> = ({ title }) => <h1>{title}</h1>

export const ListPageBody: React.FC<{ children?: React.ReactNode }> = ({ children }) => (
  <div>{children}</div>
)

export const ListPageFilter: React.FC<{
  data?: unknown[]
  loaded?: boolean
  onFilterChange?: () => void
  hideLabelFilter?: boolean
}> = () => <div data-testid="list-page-filter" />

export const VirtualizedTable: React.FC<{
  data?: unknown[]
  unfilteredData?: unknown[]
  loaded?: boolean
  loadError?: unknown
  columns?: { id: string }[]
  Row?: React.ComponentType<{ obj: unknown; activeColumnIDs: Set<string> }>
  NoDataEmptyMsg?: React.ComponentType
  EmptyMsg?: React.ComponentType
}> = ({ data = [], loaded, loadError, columns = [], Row, NoDataEmptyMsg, EmptyMsg }) => {
  if (!loaded) return <div data-testid="loading" />
  if (loadError) return <div data-testid="load-error">{String(loadError)}</div>
  if (data.length === 0) {
    const NoData = NoDataEmptyMsg
    return NoData ? <NoData /> : <div data-testid="no-data" />
  }
  if (!Row) return null
  const activeColumnIDs = new Set(columns.map((c) => c.id))
  return (
    <table data-testid="table">
      <tbody>
        {data.map((obj, i) => (
          <tr key={i}>
            <Row obj={obj} activeColumnIDs={activeColumnIDs} />
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export const TableData: React.FC<{
  id?: string
  activeColumnIDs?: Set<string>
  children?: React.ReactNode
}> = ({ children }) => <td>{children}</td>

export const Timestamp: React.FC<{ timestamp?: string }> = ({ timestamp }) => (
  <span data-testid="timestamp">{timestamp}</span>
)
