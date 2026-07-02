import { render, screen } from '@testing-library/react'
import OverviewPage from '../OverviewPage'
import { useFleetMeshItems } from '../../hooks/useFleetMeshItems'
import type { UseFleetMeshItemsResult } from '../../hooks/useFleetMeshItems'
import type { MultiClusterMesh } from '../../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../../types/istio'
import type { FleetMeshItem } from '../../types/fleetMesh'

rstest.mock('../../hooks/useFleetMeshItems', { mock: true })

const makeMesh = (overrides: Partial<MultiClusterMesh> = {}): MultiClusterMesh => ({
  apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
  kind: 'MultiClusterMesh',
  metadata: { name: 'test-mesh', namespace: 'mesh-system' },
  spec: { clusterSet: 'global' },
  ...overrides,
})

const makeEnrichedCP = (overrides: Partial<EnrichedControlPlane> = {}): EnrichedControlPlane => ({
  metadata: { name: 'default' },
  clusterName: 'cluster-a',
  ...overrides,
})

const makeItem = (overrides: Partial<FleetMeshItem> = {}): FleetMeshItem => ({
  metadata: { name: 'test-mesh' },
  kind: 'managed',
  detailLink: '/fleet-mesh/meshes/mesh-system/test-mesh',
  clusterCount: 1,
  statusRank: 0,
  conditions: [{ type: 'Ready', status: 'True' }],
  ...overrides,
})

function mockDefaults(overrides: Partial<UseFleetMeshItemsResult> = {}) {
  rstest.mocked(useFleetMeshItems).mockReturnValue({
    items: [],
    loaded: true,
    isFleetAvailable: true,
    mcms: [],
    mcmsLoaded: true,
    mcmsError: null,
    enrichedPlanes: [],
    enrichmentLoaded: true,
    enrichmentError: null,
    searchLoaded: true,
    searchError: null,
    ...overrides,
  })
}

afterEach(() => rstest.clearAllMocks())

describe('OverviewPage', () => {
  it('renders the page title', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('Overview')).toBeInTheDocument()
  })

  it('shows spinners while mesh data is loading', () => {
    mockDefaults({ mcmsLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading fleet meshes')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading recent issues')).toBeInTheDocument()
  })

  it('shows spinners while control plane data is loading', () => {
    mockDefaults({ searchLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading control planes')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading recent issues')).toBeInTheDocument()
  })

  it('shows Meshes card title with count from items when enrichmentLoaded', () => {
    const items = [makeItem(), makeItem({ metadata: { name: 'mesh-2' } })]
    mockDefaults({ items, enrichmentLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
  })

  it('shows Meshes card title with count from mcms when enrichmentLoaded is false', () => {
    const mcms = [makeMesh(), makeMesh({ metadata: { name: 'mesh-2', namespace: 'mesh-system' } })]
    mockDefaults({ mcms, enrichmentLoaded: false, items: [] })
    render(<OverviewPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
  })

  it('shows Meshes title without count while loading', () => {
    mockDefaults({ mcmsLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
  })

  it('shows Control Planes card title with count when loaded', () => {
    const enrichedPlanes = [
      makeEnrichedCP({ clusterName: 'cluster-a' }),
      makeEnrichedCP({ clusterName: 'cluster-b', metadata: { name: 'cp-2' } }),
    ]
    mockDefaults({ enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('Control Planes')).toBeInTheDocument()
  })

  it('shows donut chart for Ready meshes in card body', () => {
    const items = [makeItem({ conditions: [{ type: 'Ready', status: 'True' }] })]
    mockDefaults({ items, enrichmentLoaded: true })
    render(<OverviewPage />)
    const donuts = screen.getAllByTestId('chart-donut')
    expect(donuts.length).toBeGreaterThanOrEqual(1)
    expect(donuts[0]).toHaveAttribute('data-subtitle', 'total')
  })

  it('shows donut chart for degraded meshes in card body', () => {
    const items = [makeItem({ conditions: [{ type: 'Ready', status: 'False', reason: 'ClustersNotReady' }] })]
    mockDefaults({ items, enrichmentLoaded: true })
    render(<OverviewPage />)
    const donuts = screen.getAllByTestId('chart-donut')
    expect(donuts.length).toBeGreaterThanOrEqual(1)
    expect(screen.getByTestId('donut-segment-Not Ready')).toBeInTheDocument()
  })

  it('classifies mesh as Degraded when Ready=True but secondary condition is False', () => {
    const items = [makeItem({
      conditions: [
        { type: 'Ready', status: 'True' },
        { type: 'Reconciled', status: 'False', reason: 'ReconcileError' },
      ],
    })]
    mockDefaults({ items, enrichmentLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByTestId('donut-segment-Degraded')).toHaveTextContent('Degraded: 1')
  })

  it('does not classify as Degraded when secondary condition is Unknown', () => {
    const items = [makeItem({
      conditions: [
        { type: 'Ready', status: 'True' },
        { type: 'Progressing', status: 'Unknown' },
      ],
    })]
    mockDefaults({ items, enrichmentLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByTestId('donut-segment-Degraded')).toHaveTextContent('Degraded: 0')
    expect(screen.getByTestId('donut-segment-Ready')).toHaveTextContent('Ready: 1')
  })

  it('shows donut chart for control plane health in card body', () => {
    const enrichedPlanes = [
      makeEnrichedCP({ status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
      makeEnrichedCP({
        clusterName: 'cluster-b',
        metadata: { name: 'cp-2' },
        status: { conditions: [{ type: 'Ready', status: 'False', reason: 'ReconcileError' }] },
      }),
    ]
    mockDefaults({ enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('Control Planes')).toBeInTheDocument()
    const donuts = screen.getAllByTestId('chart-donut')
    const cpDonut = donuts.find((d) => d.getAttribute('data-subtitle') === 'total')
    expect(cpDonut).toBeTruthy()
  })

  it('shows empty state in Meshes card when no meshes exist', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('No managed or discovered meshes found.')).toBeInTheDocument()
  })

  it('shows empty state in Control Planes card when no control planes exist', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
  })

  it('shows "No issues detected" when everything is healthy', () => {
    const items = [makeItem({ conditions: [{ type: 'Ready', status: 'True' }] })]
    const mcms = [makeMesh({ status: { conditions: [{ type: 'Ready', status: 'True' }] } })]
    const enrichedPlanes = [
      makeEnrichedCP({ status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
    ]
    mockDefaults({ items, mcms, enrichedPlanes, enrichmentLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByText('No issues detected.')).toBeInTheDocument()
  })

  it('shows recent issues when meshes have non-True conditions', () => {
    const mcms = [
      makeMesh({
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ClustersNotReady', lastTransitionTime: '2026-06-24T10:00:00Z' },
          ],
        },
      }),
    ]
    mockDefaults({ mcms })
    render(<OverviewPage />)
    expect(screen.getByText('Clusters Not Ready')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'test-mesh' })
    expect(link).toHaveAttribute('href', '/fleet-mesh/meshes/mesh-system/test-mesh')
  })

  it('shows control plane issues in recent issues card', () => {
    const enrichedPlanes = [
      makeEnrichedCP({
        clusterName: 'cluster-a',
        metadata: { name: 'default' },
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T11:00:00Z' },
          ],
        },
      }),
    ]
    mockDefaults({ enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'cluster-a / default' })
    expect(link).toHaveAttribute('href', '/fleet-mesh/control-planes/cluster-a/default')
  })

  it('shows per-cluster mesh issues in recent issues card', () => {
    const mcms = [
      makeMesh({
        status: {
          conditions: [{ type: 'Ready', status: 'True' }],
          clusterStatus: [
            {
              clusterName: 'cluster-a',
              conditions: [
                { type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T09:00:00Z' },
              ],
            },
          ],
        },
      }),
    ]
    mockDefaults({ mcms })
    render(<OverviewPage />)
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'test-mesh / cluster-a' })
    expect(link).toHaveAttribute('href', '/fleet-mesh/meshes/mesh-system/test-mesh')
  })

  it('shows both mesh and control plane issues together sorted by time', () => {
    const mcms = [
      makeMesh({
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ClustersNotReady', lastTransitionTime: '2026-06-24T08:00:00Z' },
          ],
        },
      }),
    ]
    const enrichedPlanes = [
      makeEnrichedCP({
        clusterName: 'cluster-a',
        metadata: { name: 'default' },
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T10:00:00Z' },
          ],
        },
      }),
    ]
    mockDefaults({ mcms, enrichedPlanes })
    render(<OverviewPage />)

    expect(screen.getByText('Clusters Not Ready')).toBeInTheDocument()
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()

    const meshLink = screen.getByRole('link', { name: 'test-mesh' })
    expect(meshLink).toHaveAttribute('href', '/fleet-mesh/meshes/mesh-system/test-mesh')
    const cpLink = screen.getByRole('link', { name: 'cluster-a / default' })
    expect(cpLink).toHaveAttribute('href', '/fleet-mesh/control-planes/cluster-a/default')

    const rows = screen.getAllByRole('row')
    const dataRows = rows.filter((r) => r.querySelector('td'))
    expect(dataRows).toHaveLength(2)
    expect(dataRows[0]).toHaveTextContent('cluster-a / default')
    expect(dataRows[1]).toHaveTextContent('test-mesh')
  })

  it('shows ACM required message when fleet is unavailable and data is loaded', () => {
    mockDefaults({ isFleetAvailable: false, searchLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByText('This page requires Red Hat Advanced Cluster Management.')).toBeInTheDocument()
  })

  it('shows spinner instead of ACM message while CP data is still loading', () => {
    mockDefaults({ isFleetAvailable: false, searchLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading control planes')).toBeInTheDocument()
    expect(screen.queryByText('This page requires Red Hat Advanced Cluster Management.')).not.toBeInTheDocument()
  })

  it('shows mesh error alert when mesh watch fails', () => {
    mockDefaults({ mcmsError: new Error('watch failed') })
    render(<OverviewPage />)
    expect(screen.getAllByText('Unable to load mesh data').length).toBeGreaterThanOrEqual(1)
  })

  it('renders view-all links to list pages', () => {
    mockDefaults()
    render(<OverviewPage />)
    const viewAllLinks = screen.getAllByRole('link', { name: 'View all' })
    expect(viewAllLinks[0]).toHaveAttribute('href', '/fleet-mesh/meshes')
    expect(viewAllLinks[1]).toHaveAttribute('href', '/fleet-mesh/control-planes')
  })

  it('renders mesh section even when CP section errors', () => {
    const items = [makeItem({ conditions: [{ type: 'Ready', status: 'True' }] })]
    mockDefaults({ items, enrichmentLoaded: true, searchError: new Error('search failed') })
    render(<OverviewPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
    const donuts = screen.getAllByTestId('chart-donut')
    expect(donuts.find((d) => d.getAttribute('data-subtitle') === 'total')).toBeTruthy()
    expect(screen.getAllByText('Unable to load control plane data').length).toBeGreaterThanOrEqual(1)
  })

  it('shows partial-data warning in issues card when only mesh data fails', () => {
    mockDefaults({ mcmsError: new Error('watch failed') })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load mesh data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.queryByText('No issues detected.')).not.toBeInTheDocument()
  })

  it('shows partial-data warning in issues card when only CP data fails', () => {
    mockDefaults({ searchError: new Error('search failed') })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load control plane data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.queryByText('No issues detected.')).not.toBeInTheDocument()
  })

  it('shows CP error in issues card when enrichmentError is set but searchError is null', () => {
    mockDefaults({ enrichmentError: new Error('enrichment failed') })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load control plane data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.queryByText('No issues detected.')).not.toBeInTheDocument()
  })

  it('shows partial-data warning alongside issues from the working source', () => {
    const enrichedPlanes = [
      makeEnrichedCP({
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T10:00:00Z' },
          ],
        },
      }),
    ]
    mockDefaults({ mcmsError: new Error('watch failed'), enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load mesh data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
  })

  it('limits recent issues to 5 most recent entries', () => {
    const mcms = [
      makeMesh({
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ClustersNotReady', lastTransitionTime: '2026-06-24T01:00:00Z' },
          ],
          clusterStatus: [
            {
              clusterName: 'c1',
              conditions: [{ type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T02:00:00Z' }],
            },
            {
              clusterName: 'c2',
              conditions: [{ type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T03:00:00Z' }],
            },
            {
              clusterName: 'c3',
              conditions: [{ type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T04:00:00Z' }],
            },
            {
              clusterName: 'c4',
              conditions: [{ type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T05:00:00Z' }],
            },
            {
              clusterName: 'c5',
              conditions: [{ type: 'OperatorInstalled', status: 'False', reason: 'ReconcileError', lastTransitionTime: '2026-06-24T06:00:00Z' }],
            },
          ],
        },
      }),
    ]
    mockDefaults({ mcms })
    render(<OverviewPage />)
    const rows = screen.getAllByRole('row')
    const dataRows = rows.filter((r) => r.querySelector('td'))
    expect(dataRows).toHaveLength(5)
    expect(dataRows[0]).toHaveTextContent('c5')
    expect(dataRows[4]).toHaveTextContent('c1')
  })

  it('shows mesh loading independently from CP loading', () => {
    const items = [makeItem()]
    mockDefaults({ items, mcmsLoaded: true, enrichmentLoaded: true, searchLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading control planes')).toBeInTheDocument()
  })

  it('uses mesh health from mcms when enrichmentLoaded is false', () => {
    const mcms = [
      makeMesh({ status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
      makeMesh({
        metadata: { name: 'mesh-2', namespace: 'mesh-system' },
        status: { conditions: [{ type: 'Ready', status: 'False', reason: 'ClustersNotReady' }] },
      }),
    ]
    mockDefaults({ mcms, enrichmentLoaded: false, items: [] })
    render(<OverviewPage />)
    const donuts = screen.getAllByTestId('chart-donut')
    const meshDonut = donuts.find((d) => d.getAttribute('data-subtitle') === 'total')
    expect(meshDonut).toBeTruthy()
    expect(meshDonut).toHaveAttribute('data-title', '2')
  })
})
