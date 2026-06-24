import { render, screen } from '@testing-library/react'
import OverviewPage from '../OverviewPage'
import { useMultiClusterMeshes } from '../../hooks/useMultiClusterMeshes'
import { useDiscoveredControlPlanes } from '../../hooks/useDiscoveredControlPlanes'
import { useEnrichedControlPlanes } from '../../hooks/useEnrichedControlPlanes'
import type { MultiClusterMesh } from '../../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../../types/istio'

rstest.mock('../../hooks/useMultiClusterMeshes', { mock: true })
rstest.mock('../../hooks/useDiscoveredControlPlanes', { mock: true })
rstest.mock('../../hooks/useEnrichedControlPlanes', { mock: true })

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

function mockDefaults(opts: {
  meshes?: MultiClusterMesh[]
  meshesLoaded?: boolean
  meshesError?: unknown
  enrichedPlanes?: EnrichedControlPlane[]
  cpLoaded?: boolean
  cpError?: unknown
  isFleetAvailable?: boolean
} = {}) {
  const {
    meshes = [],
    meshesLoaded = true,
    meshesError = null,
    enrichedPlanes = [],
    cpLoaded = true,
    cpError = null,
    isFleetAvailable = true,
  } = opts

  rstest.mocked(useMultiClusterMeshes).mockReturnValue([meshes, meshesLoaded, meshesError])
  rstest.mocked(useDiscoveredControlPlanes).mockReturnValue({
    results: [],
    loaded: cpLoaded,
    error: cpError,
    isFleetAvailable,
    refetch: rstest.fn(),
  } as any)
  rstest.mocked(useEnrichedControlPlanes).mockReturnValue([enrichedPlanes, true, true, null])
}

afterEach(() => rstest.clearAllMocks())

describe('OverviewPage', () => {
  it('renders the page title', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('Overview')).toBeInTheDocument()
  })

  it('shows spinners while mesh data is loading', () => {
    mockDefaults({ meshesLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading fleet meshes count')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading managed clusters count')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading fleet meshes health')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading recent issues')).toBeInTheDocument()
  })

  it('shows spinners while control plane data is loading', () => {
    mockDefaults({ cpLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading control planes count')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading control planes health')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading recent issues')).toBeInTheDocument()
  })

  it('shows mesh count when meshes are loaded', () => {
    const meshes = [makeMesh(), makeMesh({ metadata: { name: 'mesh-2', namespace: 'mesh-system' } })]
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('shows cluster count from mesh cluster statuses', () => {
    const meshes = [
      makeMesh({
        status: {
          clusterStatus: [
            { clusterName: 'cluster-a', conditions: [{ type: 'OperatorInstalled', status: 'True' }] },
            { clusterName: 'cluster-b', conditions: [{ type: 'OperatorInstalled', status: 'True' }] },
          ],
        },
      }),
    ]
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('shows control plane count when enriched planes are loaded', () => {
    const enrichedPlanes = [makeEnrichedCP(), makeEnrichedCP({ clusterName: 'cluster-b' })]
    mockDefaults({ enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('shows health status labels for Ready meshes', () => {
    const meshes = [
      makeMesh({
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
    ]
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('1 Ready')).toBeInTheDocument()
    expect(screen.getByText('0 Not Ready')).toBeInTheDocument()
    expect(screen.getByText('0 Unknown')).toBeInTheDocument()
  })

  it('shows health status labels for degraded meshes', () => {
    const meshes = [
      makeMesh({
        status: { conditions: [{ type: 'Ready', status: 'False', reason: 'ClustersNotReady' }] },
      }),
    ]
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('0 Ready')).toBeInTheDocument()
    expect(screen.getByText('1 Not Ready')).toBeInTheDocument()
  })

  it('shows "No issues detected" when everything is healthy', () => {
    const meshes = [
      makeMesh({
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
    ]
    const enrichedPlanes = [
      makeEnrichedCP({ status: { conditions: [{ type: 'Ready', status: 'True' }] } }),
    ]
    mockDefaults({ meshes, enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('No issues detected.')).toBeInTheDocument()
  })

  it('shows recent issues when meshes have non-True conditions', () => {
    const meshes = [
      makeMesh({
        status: {
          conditions: [
            { type: 'Ready', status: 'False', reason: 'ClustersNotReady', lastTransitionTime: '2026-06-24T10:00:00Z' },
          ],
        },
      }),
    ]
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('Clusters Not Ready')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'test-mesh' })
    expect(link).toHaveAttribute('href', '/service-mesh/mesh-system/test-mesh')
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
    expect(link).toHaveAttribute('href', '/mesh-control-planes/cluster-a/default')
  })

  it('shows per-cluster mesh issues in recent issues card', () => {
    const meshes = [
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
    mockDefaults({ meshes })
    render(<OverviewPage />)
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'test-mesh / cluster-a' })
    expect(link).toHaveAttribute('href', '/service-mesh/mesh-system/test-mesh')
  })

  it('shows both mesh and control plane issues together sorted by time', () => {
    const meshes = [
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
    mockDefaults({ meshes, enrichedPlanes })
    render(<OverviewPage />)

    expect(screen.getByText('Clusters Not Ready')).toBeInTheDocument()
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()

    const meshLink = screen.getByRole('link', { name: 'test-mesh' })
    expect(meshLink).toHaveAttribute('href', '/service-mesh/mesh-system/test-mesh')
    const cpLink = screen.getByRole('link', { name: 'cluster-a / default' })
    expect(cpLink).toHaveAttribute('href', '/mesh-control-planes/cluster-a/default')

    const rows = screen.getAllByRole('row')
    const dataRows = rows.filter((r) => r.querySelector('td'))
    expect(dataRows).toHaveLength(2)
    expect(dataRows[0]).toHaveTextContent('cluster-a / default')
    expect(dataRows[1]).toHaveTextContent('test-mesh')
  })

  it('shows empty mesh state in health card when no meshes exist', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('No meshes have been created yet.')).toBeInTheDocument()
  })

  it('shows empty CP state in health card when no control planes exist', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByText('No control planes discovered across the fleet.')).toBeInTheDocument()
  })

  it('shows Requires ACM note when fleet is unavailable and data is loaded', () => {
    mockDefaults({ isFleetAvailable: false, cpLoaded: true })
    render(<OverviewPage />)
    expect(screen.getByText('Requires ACM')).toBeInTheDocument()
    expect(screen.getByText('This page requires Red Hat Advanced Cluster Management.')).toBeInTheDocument()
  })

  it('shows spinners instead of Requires ACM while CP data is still loading', () => {
    mockDefaults({ isFleetAvailable: false, cpLoaded: false })
    render(<OverviewPage />)
    expect(screen.getByLabelText('Loading control planes count')).toBeInTheDocument()
    expect(screen.getByLabelText('Loading control planes health')).toBeInTheDocument()
    expect(screen.queryByText('Requires ACM')).not.toBeInTheDocument()
  })

  it('shows mesh error alert when mesh watch fails', () => {
    mockDefaults({ meshesError: new Error('watch failed') })
    render(<OverviewPage />)
    expect(screen.getAllByText('Unable to load mesh data').length).toBeGreaterThanOrEqual(1)
  })

  it('renders view-all links to list pages', () => {
    mockDefaults()
    render(<OverviewPage />)
    expect(screen.getByRole('link', { name: 'View all fleet meshes' })).toHaveAttribute('href', '/service-mesh')
    expect(screen.getByRole('link', { name: 'View all control planes' })).toHaveAttribute('href', '/mesh-control-planes')
  })

  it('renders mesh section even when CP section errors', () => {
    const meshes = [
      makeMesh({
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
    ]
    mockDefaults({ meshes, cpError: new Error('search failed') })
    render(<OverviewPage />)
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('1 Ready')).toBeInTheDocument()
    expect(screen.getAllByText('Unable to load control plane data').length).toBeGreaterThanOrEqual(1)
  })

  it('shows partial-data warning in issues card when only mesh data fails', () => {
    mockDefaults({ meshesError: new Error('watch failed') })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load mesh data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.queryByText('No issues detected.')).not.toBeInTheDocument()
  })

  it('shows partial-data warning in issues card when only CP data fails', () => {
    mockDefaults({ cpError: new Error('search failed') })
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
    mockDefaults({ meshesError: new Error('watch failed'), enrichedPlanes })
    render(<OverviewPage />)
    expect(screen.getByText('Unable to load mesh data. Some issues may not be shown.')).toBeInTheDocument()
    expect(screen.getByText('Reconcile Error')).toBeInTheDocument()
  })
})
