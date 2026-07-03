import { render, screen, waitFor, within } from '@testing-library/react'
import ControlPlanesPage from '../ControlPlanesPage'
import { useFleetSearchPoll, useIsFleetAvailable } from '@stolostron/multicluster-sdk'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { useEnrichedControlPlanes } from '../../hooks/useEnrichedControlPlanes'
import type { EnrichedControlPlane } from '../../types/istio'

rstest.mock('../../hooks/useEnrichedControlPlanes', { mock: true })

const makeSearchResult = (cluster: string, name: string) => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  cluster,
  spec: { namespace: 'istio-system' },
})

const makeEnrichedFromSearch = (cluster: string, name: string, overrides: Partial<EnrichedControlPlane> = {}): EnrichedControlPlane => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  clusterName: cluster,
  controlPlaneNamespace: 'istio-system',
  ...overrides,
} as EnrichedControlPlane)

afterEach(() => rstest.clearAllMocks())

beforeEach(() => {
  rstest.mocked(useIsFleetAvailable).mockReturnValue(true)
  rstest.mocked(useK8sWatchResource).mockReturnValue([[], true, null])
  rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[], false, false, null])
})

describe('ControlPlanesPage', () => {
  it('shows loading state while search is pending', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([undefined, false, undefined, rstest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByTestId('loading')).toBeInTheDocument()
  })

  it('shows empty state when no control planes are discovered', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
  })

  it('shows error state when search fails', () => {
    rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, new Error('search failed'), rstest.fn()])
    render(<ControlPlanesPage />)
    expect(screen.getByTestId('load-error')).toBeInTheDocument()
  })

  it('renders control plane rows when search returns results', async () => {
    const results = [
      makeSearchResult('cluster-a', 'default'),
      makeSearchResult('cluster-b', 'default'),
    ]
    const enriched = [
      makeEnrichedFromSearch('cluster-a', 'default'),
      makeEnrichedFromSearch('cluster-b', 'default'),
    ]
    rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
    rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enriched, true, true, null])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      expect(screen.getByText('cluster-a')).toBeInTheDocument()
      expect(screen.getByText('cluster-b')).toBeInTheDocument()
    })
  })

  it('links cluster names to ACM cluster detail pages', async () => {
    const results = [makeSearchResult('cluster-a', 'default')]
    const enriched = [makeEnrichedFromSearch('cluster-a', 'default')]
    rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
    rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enriched, true, true, null])
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
    const enriched = [makeEnrichedFromSearch('cluster-a', 'myistio')]
    rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
    rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enriched, true, true, null])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'myistio' })).toHaveAttribute(
        'href',
        '/fleet-mesh/control-planes/cluster-a/myistio',
      )
    })
  })

  it('shows dash for enrichment columns before enrichment completes', async () => {
    const results = [makeSearchResult('cluster-a', 'default')]
    const enriched = [makeEnrichedFromSearch('cluster-a', 'default')]
    rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
    rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enriched, true, false, null])
    render(<ControlPlanesPage />)
    await waitFor(() => {
      const nameLink = screen.getByRole('link', { name: 'default' })
      const row = nameLink.closest('tr') as HTMLTableRowElement
      expect(within(row).getAllByText('-').length).toBeGreaterThanOrEqual(2)
    })
  })

  describe('Mesh ID column', () => {
    it('shows dash with no link for standalone CPs', async () => {
      const results = [makeSearchResult('cluster-a', 'default')]
      const enriched = [makeEnrichedFromSearch('cluster-a', 'default')]
      rstest.mocked(useFleetSearchPoll).mockReturnValue([results, true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enriched, true, true, null])
      render(<ControlPlanesPage />)
      await waitFor(() => {
        const nameLink = screen.getByRole('link', { name: 'default' })
        const row = nameLink.closest('tr') as HTMLTableRowElement
        const meshIdCell = within(row).getAllByRole('cell')[0]
        expect(meshIdCell).toHaveTextContent('-')
        expect(within(meshIdCell).queryByRole('link')).not.toBeInTheDocument()
      })
    })
  })

  describe('fleet availability guard', () => {
    it('shows RHACM message when loaded, no results, and fleet not available', () => {
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useIsFleetAvailable).mockReturnValue(false)
      render(<ControlPlanesPage />)
      expect(screen.getByText('This page requires Red Hat Advanced Cluster Management.')).toBeInTheDocument()
    })

    it('shows NoDataEmptyMsg when loaded, no results, but fleet is available', () => {
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useIsFleetAvailable).mockReturnValue(true)
      render(<ControlPlanesPage />)
      expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
    })

    it('does not show guard during loading', () => {
      rstest.mocked(useFleetSearchPoll).mockReturnValue([undefined, false, undefined, rstest.fn()])
      rstest.mocked(useIsFleetAvailable).mockReturnValue(false)
      render(<ControlPlanesPage />)
      expect(screen.queryByText('This page requires Red Hat Advanced Cluster Management.')).not.toBeInTheDocument()
    })
  })

  describe('Type column', () => {
    it('shows Managed type for CPs with managedBy', () => {
      const cp = makeEnrichedFromSearch('cluster-a', 'default', {
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
        meshID: 'mesh-system-my-mesh',
      })
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[cp], true, true, null])
      render(<ControlPlanesPage />)
      expect(screen.getByText('Managed')).toBeInTheDocument()
    })

    it('shows Discovered type for CPs with meshID but no managedBy', () => {
      const cp = makeEnrichedFromSearch('cluster-a', 'default', {
        meshID: 'discovered-id',
      })
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[cp], true, true, null])
      render(<ControlPlanesPage />)
      expect(screen.getByText('Discovered')).toBeInTheDocument()
    })

    it('shows Standalone type for CPs with no meshID and no managedBy', () => {
      const cp = makeEnrichedFromSearch('cluster-a', 'default')
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[cp], true, true, null])
      render(<ControlPlanesPage />)
      expect(screen.getByText('Standalone')).toBeInTheDocument()
    })

    it('links managed mesh ID to the managed mesh detail page', () => {
      const cp = makeEnrichedFromSearch('cluster-a', 'default', {
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
        meshID: 'mesh-system-my-mesh',
      })
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[cp], true, true, null])
      render(<ControlPlanesPage />)
      expect(screen.getByRole('link', { name: 'mesh-system-my-mesh' })).toHaveAttribute(
        'href',
        '/fleet-mesh/meshes/managed/mesh-system/my-mesh',
      )
    })

    it('links discovered mesh ID to the discovered mesh detail page', () => {
      const cp = makeEnrichedFromSearch('cluster-a', 'default', {
        meshID: 'discovered-id',
      })
      rstest.mocked(useFleetSearchPoll).mockReturnValue([[], true, undefined, rstest.fn()])
      rstest.mocked(useEnrichedControlPlanes).mockReturnValue([[cp], true, true, null])
      render(<ControlPlanesPage />)
      expect(screen.getByRole('link', { name: 'discovered-id' })).toHaveAttribute(
        'href',
        '/fleet-mesh/meshes/discovered/discovered-id',
      )
    })
  })
})
