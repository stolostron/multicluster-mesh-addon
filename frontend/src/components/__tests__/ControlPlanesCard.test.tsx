import { act, render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ControlPlanesCard } from '../ControlPlanesCard'
import { makeEnrichedCP } from '../../__fixtures__/testFactories'
import type { EnrichedControlPlane } from '../../types/istio'

function makeCp(cluster: string, name: string, overrides: Partial<EnrichedControlPlane> = {}): EnrichedControlPlane {
  return makeEnrichedCP({
    clusterName: cluster,
    metadata: { name, creationTimestamp: '2026-01-01T00:00:00Z' },
    version: 'v1.24.0',
    status: { conditions: [{ type: 'Ready', status: 'True' }] },
    ...overrides,
  })
}

describe('ControlPlanesCard', () => {
  it('returns null when planes array is empty', () => {
    const { container } = render(<ControlPlanesCard planes={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders card title with plane count', () => {
    render(<ControlPlanesCard planes={[makeCp('cluster-a', 'default')]} />)
    expect(screen.getByText('Control Planes (1)')).toBeInTheDocument()
  })

  it('renders cluster links to ACM detail page', () => {
    render(<ControlPlanesCard planes={[makeCp('cluster-a', 'default')]} />)
    expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
      'href',
      '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
    )
  })

  it('renders CP name links to control plane detail page', () => {
    render(<ControlPlanesCard planes={[makeCp('cluster-a', 'myistio')]} />)
    expect(screen.getByRole('link', { name: 'myistio' })).toHaveAttribute(
      'href',
      '/fleet-mesh/control-planes/standalone/cluster-a/myistio',
    )
  })

  it('shows version and namespace in table rows', () => {
    render(<ControlPlanesCard planes={[makeCp('cluster-a', 'default')]} />)
    expect(screen.getByText('v1.24.0')).toBeInTheDocument()
    expect(screen.getByText('istio-system')).toBeInTheDocument()
  })

  it('shows toggle group with status counts', () => {
    const planes = [
      makeCp('cluster-a', 'cp1', { status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
      makeCp('cluster-b', 'cp2', { status: { conditions: [{ type: 'Ready', status: 'False' }] } }),
    ]
    render(<ControlPlanesCard planes={planes} />)
    expect(screen.getByText('All (2)')).toBeInTheDocument()
    expect(screen.getByText('Ready (1)')).toBeInTheDocument()
    expect(screen.getByText('Not Ready (1)')).toBeInTheDocument()
  })

  it('filters by status when toggle is clicked', async () => {
    const user = userEvent.setup()
    const planes = [
      makeCp('cluster-a', 'cp1', { status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
      makeCp('cluster-b', 'cp2', { status: { conditions: [{ type: 'Ready', status: 'False' }] } }),
    ]
    render(<ControlPlanesCard planes={planes} />)

    await user.click(screen.getByText('Ready (1)'))
    expect(screen.getByText('cluster-a')).toBeInTheDocument()
    expect(screen.queryByText('cluster-b')).not.toBeInTheDocument()
  })

  describe('search with debounce', () => {
    beforeEach(() => { rstest.useFakeTimers() })
    afterEach(() => { rstest.useRealTimers() })

    it('filters by search term matching cluster or name', () => {
      const planes = [
        makeCp('cluster-a', 'default'),
        makeCp('cluster-b', 'default'),
      ]
      render(<ControlPlanesCard planes={planes} />)

      fireEvent.change(screen.getByPlaceholderText('Filter by cluster name'), { target: { value: 'cluster-a' } })
      act(() => { rstest.advanceTimersByTime(200) })
      expect(screen.getByText('cluster-a')).toBeInTheDocument()
      expect(screen.queryByText('cluster-b')).not.toBeInTheDocument()
    })

    it('shows empty state when filter matches nothing', () => {
      render(<ControlPlanesCard planes={[makeCp('cluster-a', 'default')]} />)

      fireEvent.change(screen.getByPlaceholderText('Filter by cluster name'), { target: { value: 'zzznomatch' } })
      act(() => { rstest.advanceTimersByTime(200) })
      expect(screen.getByText('No control planes match the current filter.')).toBeInTheDocument()
    })
  })

  it('shows Unknown label when CP has no conditions', () => {
    render(<ControlPlanesCard planes={[makeCp('cluster-a', 'default', { status: undefined })]} />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
  })
})
