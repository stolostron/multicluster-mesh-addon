import { render, screen, fireEvent } from '@testing-library/react'
import { VirtualFilterTable } from '../VirtualFilterTable'
import type { CategoryLabel, VirtualFilterColumn } from '../VirtualFilterTable'

interface TestItem {
  id: string
  category: string
  label: string
}

const columns: VirtualFilterColumn<TestItem>[] = [
  { key: 'id', label: 'ID', render: (item) => item.id, width: '30%' },
  { key: 'label', label: 'Label', render: (item) => item.label, width: '70%' },
]

const categoryLabels: CategoryLabel[] = [
  { key: 'all', label: 'All ({{count}})' },
  { key: 'catA', label: 'Cat A ({{count}})' },
  { key: 'catB', label: 'Cat B ({{count}})' },
]

const items: TestItem[] = [
  { id: '1', category: 'catA', label: 'Alpha' },
  { id: '2', category: 'catA', label: 'Bravo' },
  { id: '3', category: 'catB', label: 'Charlie' },
]

function renderTable(overrides: Partial<Parameters<typeof VirtualFilterTable<TestItem>>[0]> = {}) {
  return render(
    <VirtualFilterTable<TestItem>
      categorize={(item) => item.category}
      categoryLabels={categoryLabels}
      columns={columns}
      emptyMessage="No items found"
      items={items}
      rowKey={(item) => item.id}
      searchMatch={(item, q) => item.label.toLowerCase().includes(q.toLowerCase())}
      searchPlaceholder="Search..."
      {...overrides}
    />,
  )
}

describe('VirtualFilterTable', () => {
  it('renders header and body rows', () => {
    renderTable()

    expect(screen.getByText('ID')).toBeInTheDocument()
    expect(screen.getByText('Label')).toBeInTheDocument()

    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Bravo')).toBeInTheDocument()
    expect(screen.getByText('Charlie')).toBeInTheDocument()
  })

  it('toggle filter shows correct counts and filters items', () => {
    renderTable()

    expect(screen.getByText('All (3)')).toBeInTheDocument()
    expect(screen.getByText('Cat A (2)')).toBeInTheDocument()
    expect(screen.getByText('Cat B (1)')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Cat A (2)'))

    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Bravo')).toBeInTheDocument()
    expect(screen.queryByText('Charlie')).not.toBeInTheDocument()
  })

  it('search filters items by the searchMatch predicate', () => {
    renderTable()

    const searchInput = screen.getByPlaceholderText('Search...')
    fireEvent.change(searchInput, { target: { value: 'alpha' } })

    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.queryByText('Bravo')).not.toBeInTheDocument()
    expect(screen.queryByText('Charlie')).not.toBeInTheDocument()
  })

  it('shows empty message when filter matches nothing', () => {
    renderTable()

    const searchInput = screen.getByPlaceholderText('Search...')
    fireEvent.change(searchInput, { target: { value: 'zzz-no-match' } })

    expect(screen.getByText('No items found')).toBeInTheDocument()
  })

  it('shows all items when "All" toggle is selected', () => {
    renderTable()

    fireEvent.click(screen.getByText('Cat A (2)'))
    expect(screen.queryByText('Charlie')).not.toBeInTheDocument()

    fireEvent.click(screen.getByText('All (3)'))
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Bravo')).toBeInTheDocument()
    expect(screen.getByText('Charlie')).toBeInTheDocument()
  })

  it('returns items via useVirtualRows (spacer rows present for large datasets)', () => {
    const largeItems: TestItem[] = Array.from({ length: 200 }, (_, i) => ({
      id: `item-${i}`,
      category: i % 2 === 0 ? 'catA' : 'catB',
      label: `Item ${i}`,
    }))

    const { container } = render(
      <VirtualFilterTable<TestItem>
        categorize={(item) => item.category}
        categoryLabels={categoryLabels}
        columns={columns}
        emptyMessage="No items found"
        items={largeItems}
        rowKey={(item) => item.id}
        searchMatch={(item, q) => item.label.toLowerCase().includes(q.toLowerCase())}
        searchPlaceholder="Search..."
      />,
    )

    const tbodyRows = container.querySelectorAll('tbody tr')
    expect(tbodyRows.length).toBeLessThan(largeItems.length + 2)
    expect(tbodyRows.length).toBeGreaterThan(0)
  })
})
