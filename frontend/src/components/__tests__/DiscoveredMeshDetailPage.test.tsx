import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DiscoveredMeshDetailPage from '../DiscoveredMeshDetailPage'
import { useParams } from 'react-router-dom-v5-compat'
import { useMultiClusterMeshes } from '../../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../../hooks/useEnrichedControlPlanes'
import { useManagedClusters } from '../../hooks/useManagedClusters'
import type { EnrichedControlPlane } from '../../types/istio'

rstest.mock('../../hooks/useMultiClusterMeshes', { mock: true })
rstest.mock('../../hooks/useDiscoveredControlPlanes', { mock: true })
rstest.mock('../../hooks/useEnrichedControlPlanes', { mock: true })
rstest.mock('../../hooks/useManagedClusters', { mock: true })

const makeEnrichedCP = (
  cluster: string,
  name: string,
  overrides: Partial<EnrichedControlPlane> = {},
): EnrichedControlPlane => ({
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  clusterName: cluster,
  controlPlaneNamespace: 'istio-system',
  meshID: 'mesh1',
  network: 'network1',
  version: 'v1.24.0',
  status: { conditions: [{ type: 'Ready', status: 'True' }] },
  ...overrides,
})

function mockDefaults(opts: {
  enrichedPlanes?: EnrichedControlPlane[]
  enrichmentLoaded?: boolean
  enrichmentError?: unknown
  searchLoaded?: boolean
  searchError?: unknown
} = {}) {
  const {
    enrichedPlanes = [],
    enrichmentLoaded = true,
    enrichmentError = null,
    searchLoaded = true,
    searchError = null,
  } = opts

  rstest.mocked(useMultiClusterMeshes).mockReturnValue([[], true, null])
  rstest.mocked(useManagedClusters).mockReturnValue([[
    { metadata: { name: 'cluster-a' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] } },
    { metadata: { name: 'cluster-b' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] } },
  ], true, null])
  rstest.mocked(useDiscoveredControlPlanes).mockReturnValue({
    results: [],
    loaded: searchLoaded,
    error: searchError,
    isFleetAvailable: true,
    refetch: rstest.fn(),
  } as any)
  rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enrichedPlanes, true, enrichmentLoaded, enrichmentError])
}

afterEach(() => rstest.clearAllMocks())

describe('DiscoveredMeshDetailPage', () => {
  describe('invalid URL', () => {
    it('shows Not Found when meshID param is missing', () => {
      rstest.mocked(useParams).mockReturnValue({})
      mockDefaults()
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Not Found')).toBeInTheDocument()
      expect(screen.getByText('Invalid mesh URL. Expected /fleet-mesh/meshes/discovered/:meshID.')).toBeInTheDocument()
    })
  })

  describe('loading and error states', () => {
    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ meshID: 'mesh1' })
    })

    it('shows spinner while data is loading', () => {
      mockDefaults({ enrichmentLoaded: false })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByLabelText('Loading mesh details')).toBeInTheDocument()
    })

    it('shows spinner when search is not loaded', () => {
      mockDefaults({ searchLoaded: false })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByLabelText('Loading mesh details')).toBeInTheDocument()
    })

    it('shows error state on fetch failure', () => {
      mockDefaults({ searchError: new Error('search failed') })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Error loading mesh')).toBeInTheDocument()
      expect(screen.getByText('An unexpected error occurred. Check the browser console for details.')).toBeInTheDocument()
    })

    it('shows warning banner on enrichment failure instead of blocking error', () => {
      const planes = [makeEnrichedCP('cluster-a', 'default')]
      mockDefaults({ enrichedPlanes: planes, enrichmentError: new Error('enrichment failed') })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Unable to load control plane data. Some information may be incomplete.')).toBeInTheDocument()
      expect(screen.queryByText('Error loading mesh')).not.toBeInTheDocument()
    })

    it('shows "Mesh not found" when no enriched CRs match the meshID', () => {
      mockDefaults({ enrichedPlanes: [] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Mesh not found')).toBeInTheDocument()
      expect(screen.getByText('Discovered mesh "mesh1" was not found.')).toBeInTheDocument()
    })

    it('shows "Mesh not found" when only managed CRs match the meshID', () => {
      const managed = makeEnrichedCP('cluster-a', 'default', {
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
      })
      mockDefaults({ enrichedPlanes: [managed] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Mesh not found')).toBeInTheDocument()
    })
  })

  describe('loaded state', () => {
    const cp1 = makeEnrichedCP('cluster-a', 'default')
    const cp2 = makeEnrichedCP('cluster-b', 'secondary', { network: 'network2' })

    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ meshID: 'mesh1' })
    })

    it('renders breadcrumb with link to Meshes', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      const link = screen.getByRole('link', { name: 'Meshes' })
      expect(link).toHaveAttribute('href', '/fleet-mesh/meshes')
    })

    it('renders meshID as heading', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByRole('heading', { name: 'mesh1' })).toBeInTheDocument()
    })

    it('shows Discovered in breadcrumb', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Discovered')).toBeInTheDocument()
    })

    it('shows Overview card with Mesh ID', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Mesh ID')).toBeInTheDocument()
    })

    it('shows Networks in the Overview card', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Networks')).toBeInTheDocument()
      expect(screen.getByText('network1')).toBeInTheDocument()
    })

    it('shows Clusters count in the Overview card', () => {
      mockDefaults({ enrichedPlanes: [cp1, cp2] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Clusters')).toBeInTheDocument()
      expect(screen.getByText('2')).toBeInTheDocument()
    })

    it('shows Created timestamp in the Overview card', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Created')).toBeInTheDocument()
    })

    it('shows dash for networks when no CPs have a network', () => {
      const noNetwork = makeEnrichedCP('cluster-a', 'default', { network: undefined })
      mockDefaults({ enrichedPlanes: [noNetwork] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('-')).toBeInTheDocument()
    })

    it('shows Control Planes table with constituent CRs', () => {
      mockDefaults({ enrichedPlanes: [cp1, cp2] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Control Planes (2)')).toBeInTheDocument()
      expect(screen.getAllByText('cluster-a').length).toBeGreaterThanOrEqual(1)
      expect(screen.getAllByText('cluster-b').length).toBeGreaterThanOrEqual(1)
    })

    it('links CR names to control plane detail pages', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByRole('link', { name: 'default' })).toHaveAttribute(
        'href',
        '/fleet-mesh/control-planes/cluster-a/default',
      )
    })

    it('shows version and namespace in table rows', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('v1.24.0')).toBeInTheDocument()
      expect(screen.getByText('istio-system')).toBeInTheDocument()
    })
  })

  describe('conditions card', () => {
    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ meshID: 'mesh1' })
    })

    it('shows non-True conditions by default', () => {
      const cp = makeEnrichedCP('cluster-a', 'default', {
        status: {
          conditions: [
            { type: 'Ready', status: 'True' },
            { type: 'Reconciled', status: 'False', reason: 'ReconcileError', message: 'something broke' },
          ],
        },
      })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Conditions')).toBeInTheDocument()
      expect(screen.getByText('Reconciled')).toBeInTheDocument()
      expect(screen.getByText('something broke')).toBeInTheDocument()
    })

    it('hides True conditions by default', () => {
      const cp = makeEnrichedCP('cluster-a', 'default', {
        status: {
          conditions: [
            { type: 'Ready', status: 'True' },
            { type: 'Reconciled', status: 'False' },
          ],
        },
      })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Reconciled')).toBeInTheDocument()
      expect(screen.queryByText('No issues detected.')).not.toBeInTheDocument()
    })

    it('shows all conditions when toggle is clicked', async () => {
      const user = userEvent.setup()
      const cp = makeEnrichedCP('cluster-a', 'default', {
        status: {
          conditions: [
            { type: 'Ready', status: 'True' },
            { type: 'Reconciled', status: 'False' },
          ],
        },
      })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)

      expect(screen.getByText('Reconciled')).toBeInTheDocument()
      expect(screen.queryByText('Ready')).not.toBeInTheDocument()

      await user.click(screen.getByText('Show all conditions'))

      expect(screen.getByText('Reconciled')).toBeInTheDocument()
      expect(screen.getByText('Ready')).toBeInTheDocument()
    })

    it('toggles back to issues only', async () => {
      const user = userEvent.setup()
      const cp = makeEnrichedCP('cluster-a', 'default', {
        status: {
          conditions: [
            { type: 'Ready', status: 'True' },
            { type: 'Reconciled', status: 'False' },
          ],
        },
      })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)

      await user.click(screen.getByText('Show all conditions'))
      expect(screen.getByText('Ready')).toBeInTheDocument()

      await user.click(screen.getByText('Show issues only'))
      expect(screen.queryByText('Ready')).not.toBeInTheDocument()
      expect(screen.getByText('Reconciled')).toBeInTheDocument()
    })

    it('shows "No issues detected" when all conditions are True and showing issues only', () => {
      const cp = makeEnrichedCP('cluster-a', 'default', {
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('No issues detected.')).toBeInTheDocument()
    })

    it('hides conditions card when no CPs have conditions', () => {
      const cp = makeEnrichedCP('cluster-a', 'default', { status: undefined })
      mockDefaults({ enrichedPlanes: [cp] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.queryByText('Conditions')).not.toBeInTheDocument()
    })
  })

  describe('mesh ID conflict', () => {
    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ meshID: 'mesh1' })
    })

    it('shows conflict warning banner when managed CPs share the meshID', () => {
      const discovered = makeEnrichedCP('cluster-a', 'default')
      const managed = makeEnrichedCP('cluster-b', 'managed-cp', {
        managedBy: { name: 'my-mesh', namespace: 'mesh-system' },
      })
      mockDefaults({ enrichedPlanes: [discovered, managed] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Mesh ID Conflict')).toBeInTheDocument()
      expect(screen.getByText(/also used by a managed mesh/)).toBeInTheDocument()
    })

    it('does not show conflict banner when no managed CPs share the meshID', () => {
      const discovered = makeEnrichedCP('cluster-a', 'default')
      mockDefaults({ enrichedPlanes: [discovered] })
      render(<DiscoveredMeshDetailPage />)
      expect(screen.queryByText('Mesh ID Conflict')).not.toBeInTheDocument()
    })
  })

  describe('Clusters card', () => {
    const cp1 = makeEnrichedCP('cluster-a', 'default')
    const cp2 = makeEnrichedCP('cluster-b', 'cp2')

    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ meshID: 'mesh1' })
    })

    it('shows Clusters card with count and cluster availability', () => {
      mockDefaults({ enrichedPlanes: [cp1, cp2] })
      rstest.mocked(useManagedClusters).mockReturnValue([[
        { metadata: { name: 'cluster-a' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] } },
        { metadata: { name: 'cluster-b' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'False' }] } },
      ], true, null])
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getByText('Clusters (2)')).toBeInTheDocument()
      expect(screen.getAllByText('Available').length).toBeGreaterThanOrEqual(1)
      expect(screen.getAllByText('Unavailable').length).toBeGreaterThanOrEqual(1)
    })

    it('shows Unreachable when cluster is not in ManagedCluster data', () => {
      mockDefaults({ enrichedPlanes: [cp1] })
      rstest.mocked(useManagedClusters).mockReturnValue([[], true, null])
      render(<DiscoveredMeshDetailPage />)
      expect(screen.getAllByText('Unreachable').length).toBeGreaterThanOrEqual(1)
    })

    it('filters clusters by availability toggle', async () => {
      const user = userEvent.setup()
      mockDefaults({ enrichedPlanes: [cp1, cp2] })
      rstest.mocked(useManagedClusters).mockReturnValue([[
        { metadata: { name: 'cluster-a' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] } },
        { metadata: { name: 'cluster-b' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'False' }] } },
      ], true, null])
      render(<DiscoveredMeshDetailPage />)
      await user.click(screen.getByText('Available (1)'))
      const clusterLinks = screen.getAllByRole('link', { name: 'cluster-a' })
      expect(clusterLinks.length).toBeGreaterThanOrEqual(1)
    })

    it('filters clusters by search', async () => {
      const user = userEvent.setup()
      mockDefaults({ enrichedPlanes: [cp1, cp2] })
      render(<DiscoveredMeshDetailPage />)
      const searchInputs = screen.getAllByPlaceholderText('Filter by cluster name')
      await user.type(searchInputs[0], 'cluster-a')
      const clusterLinks = screen.getAllByRole('link', { name: 'cluster-a' })
      expect(clusterLinks.length).toBeGreaterThanOrEqual(1)
    })

    it('shows empty state when filter matches nothing', async () => {
      const user = userEvent.setup()
      mockDefaults({ enrichedPlanes: [cp1] })
      render(<DiscoveredMeshDetailPage />)
      const searchInputs = screen.getAllByPlaceholderText('Filter by cluster name')
      await user.type(searchInputs[0], 'zzznomatch')
      expect(screen.getAllByText('No clusters match the current filter.').length).toBeGreaterThanOrEqual(1)
    })
  })
})
