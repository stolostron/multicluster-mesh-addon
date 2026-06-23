import { render, screen, waitFor, within } from '@testing-library/react'
import ControlPlanesPage from '../ControlPlanesPage'
import { useFleetSearchPoll, useIsFleetAvailable } from '@stolostron/multicluster-sdk'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import type { EnrichedControlPlane } from '../../types/istio'

const mockUseFleetSearchPoll = useFleetSearchPoll as jest.Mock
const mockUseIsFleetAvailable = useIsFleetAvailable as jest.Mock
const mockUseK8sWatchResource = useK8sWatchResource as jest.Mock

const makeSearchResult = (cluster: string, name: string) => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  cluster,
  spec: { namespace: 'istio-system' },
})

afterEach(() => jest.clearAllMocks())

beforeEach(() => {
  mockUseIsFleetAvailable.mockReturnValue(true)
  mockUseK8sWatchResource.mockReturnValue([[], true, null])
})

describe('ControlPlanesPage', () => {
  it('shows loading state while search is pending', () => {
    mockUseFleetSearchPoll.mockReturnValue([undefined, false, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByTestId('loading')).toBeInTheDocument()
  })

  it('shows empty state when no control planes are discovered', () => {
    mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
  })

  it('shows error state when search fails', () => {
    mockUseFleetSearchPoll.mockReturnValue([[], true, new Error('search failed'), jest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByTestId('load-error')).toBeInTheDocument()
  })

  it('renders control plane rows when search returns results', async () => {
    const results = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
    ]
    mockUseFleetSearchPoll.mockReturnValue([results, true, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      expect(screen.getByText('cluster-a')).toBeInTheDocument()
      expect(screen.getByText('cluster-b')).toBeInTheDocument()
    })
  })

  it('links cluster names to ACM cluster detail pages', async () => {
    const results = [makeSearchResult('cluster-a', 'default')]
    mockUseFleetSearchPoll.mockReturnValue([results, true, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
        'href',
        '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
      )
    })
  })

  it('links CR names to control plane detail pages', async () => {
    const results = [makeSearchResult('cluster-a', 'myistio')]
    mockUseFleetSearchPoll.mockReturnValue([results, true, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'myistio' })).toHaveAttribute(
        'href',
        '/control-planes/cluster-a/myistio',
      )
    })
  })

  it('shows dash for enrichment columns before enrichment completes', async () => {
    const results = [makeSearchResult('cluster-a', 'default')]
    mockUseFleetSearchPoll.mockReturnValue([results, true, undefined, jest.fn()])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      const nameLink = screen.getByRole('link', { name: 'default' })
      const row = nameLink.closest('tr') as HTMLTableRowElement
      expect(within(row).getAllByText('-').length).toBeGreaterThanOrEqual(4)
    })
  })

  describe('fleet availability guard', () => {
    it('shows RHACM message when loaded, no results, and fleet not available', () => {
      mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, jest.fn()])
      mockUseIsFleetAvailable.mockReturnValue(false)
      render(<ControlPlanesPage />)
      expect(screen.getByText('This page requires Red Hat Advanced Cluster Management.')).toBeInTheDocument()
    })

    it('shows NoDataEmptyMsg when loaded, no results, but fleet is available', () => {
      mockUseFleetSearchPoll.mockReturnValue([[], true, undefined, jest.fn()])
      mockUseIsFleetAvailable.mockReturnValue(true)
      render(<ControlPlanesPage />)
      expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
    })

    it('does not show guard during loading', () => {
      mockUseFleetSearchPoll.mockReturnValue([undefined, false, undefined, jest.fn()])
      mockUseIsFleetAvailable.mockReturnValue(false)
      render(<ControlPlanesPage />)
      expect(screen.queryByText('This page requires Red Hat Advanced Cluster Management.')).not.toBeInTheDocument()
    })
  })
})
