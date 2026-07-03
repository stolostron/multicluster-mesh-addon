import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import MeshDetailPage, { ClusterStatusSection } from '../MeshDetailPage'
import { useParams } from 'react-router-dom-v5-compat'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { makeMesh, makeCluster } from '../../__fixtures__/testFactories'
import type { K8sCondition } from '../../types/common'

// TrustStatusCard has its own test file; stub it here to avoid consuming
// useK8sWatchResource mock slots meant for the mesh watch.
rstest.mock('../TrustStatusCard', () => ({
  TrustStatusCard: () => <div data-testid="trust-status-card" />,
}))

rstest.mock('../../hooks/useMultiClusterMeshes', () => ({
  useMultiClusterMeshes: () => [[], true, null],
}))
rstest.mock('../../hooks/useDiscoveredControlPlanes', () => ({
  useDiscoveredControlPlanes: () => ({ results: [], loaded: true, error: null, isFleetAvailable: true }),
}))
rstest.mock('../../hooks/useEnrichedControlPlanes', () => ({
  useEnrichedControlPlanes: () => [[], undefined, true, null],
}))
rstest.mock('../../hooks/useManagedClusters', () => ({
  useManagedClusters: () => [[
    {
      metadata: { name: 'cluster-a' },
      status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] },
    },
    {
      metadata: { name: 'cluster-b' },
      status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'False' }] },
    },
    {
      metadata: { name: 'cluster-c' },
      status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'Unknown' }] },
    },
  ], true, null],
}))

const makeCondition = (
  type: string,
  status: 'True' | 'False' | 'Unknown',
  reason?: string,
  message?: string,
): K8sCondition => ({ type, status, reason, message })

// ---------------------------------------------------------------------------
// MeshDetailPage — router shell
// ---------------------------------------------------------------------------

describe('MeshDetailPage', () => {
  afterEach(() => rstest.clearAllMocks())

  describe('invalid URL (missing params)', () => {
    it('shows Not Found when ns and name are absent', () => {
      rstest.mocked(useParams).mockReturnValue({})
      render(<MeshDetailPage />)
      expect(screen.getByText('Not Found')).toBeInTheDocument()
      expect(screen.getByText('Invalid mesh URL. Expected /fleet-mesh/meshes/managed/:namespace/:name.')).toBeInTheDocument()
    })
  })

  describe('MeshDetailContent states', () => {
    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ ns: 'mesh-system', name: 'test-mesh' })
    })

    it('shows a spinner while loading', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([null, false, null])
      render(<MeshDetailPage />)
      expect(screen.getByLabelText('Loading mesh details')).toBeInTheDocument()
    })

    it('shows the error message when the watch fails', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([null, true, new Error('watch exploded')])
      render(<MeshDetailPage />)
      expect(screen.getByText('Error loading mesh')).toBeInTheDocument()
      expect(screen.getByText('An unexpected error occurred. Check the browser console for details.')).toBeInTheDocument()
    })

    it('shows mesh not found when loaded but mesh is null', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([null, true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('Mesh not found')).toBeInTheDocument()
      expect(
        screen.getByText('MultiClusterMesh "test-mesh" was not found in namespace "mesh-system".')
      ).toBeInTheDocument()
    })

    it('renders the breadcrumb and mesh name heading when loaded', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByRole('link', { name: 'Meshes' })).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: 'test-mesh' })).toBeInTheDocument()
    })

    it('shows Managed in breadcrumb', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('Managed')).toBeInTheDocument()
    })

    it('links spec.clusterSet to the ACM cluster set detail page', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh({ spec: { clusterSet: 'my-clusterset' } }), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByRole('link', { name: 'my-clusterset' })).toHaveAttribute(
        'href',
        '/multicloud/infrastructure/clusters/sets/details/my-clusterset/overview',
      )
    })

    it('shows the istio-system default when controlPlane.namespace is absent', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('istio-system')).toBeInTheDocument()
    })

    it('shows the actual controlPlane.namespace when set', () => {
      const mesh = makeMesh({ spec: { clusterSet: 'global', controlPlane: { namespace: 'custom-ns' } } })
      rstest.mocked(useK8sWatchResource).mockReturnValue([mesh, true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('custom-ns')).toBeInTheDocument()
    })

    it('shows Not configured for issuer when none is set', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('Not configured')).toBeInTheDocument()
    })

    it('shows the issuer name and kind when set', () => {
      const mesh = makeMesh({
        spec: {
          clusterSet: 'global',
          security: { trust: { certManager: { issuerRef: { name: 'root-ca', kind: 'ClusterIssuer' } } } },
        },
      })
      rstest.mocked(useK8sWatchResource).mockReturnValue([mesh, true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('root-ca (ClusterIssuer)')).toBeInTheDocument()
    })

    it('defaults Issuer kind when kind is not specified', () => {
      const mesh = makeMesh({
        spec: {
          clusterSet: 'global',
          security: { trust: { certManager: { issuerRef: { name: 'my-issuer' } } } },
        },
      })
      rstest.mocked(useK8sWatchResource).mockReturnValue([mesh, true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('my-issuer (Issuer)')).toBeInTheDocument()
    })

    it('shows the stable channel default when operator.channel is absent', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('stable')).toBeInTheDocument()
    })

    it('shows the Automatic install plan approval default', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('Automatic')).toBeInTheDocument()
    })

    it('hides the Conditions table when there are no conditions', () => {
      rstest.mocked(useK8sWatchResource).mockReturnValue([makeMesh(), true, null])
      render(<MeshDetailPage />)
      expect(screen.queryByText('Conditions')).not.toBeInTheDocument()
    })

    it('shows the Conditions table when conditions are present', () => {
      const mesh = makeMesh({
        status: { conditions: [makeCondition('Ready', 'True', 'AllClustersReady', 'All good')] },
      })
      rstest.mocked(useK8sWatchResource).mockReturnValue([mesh, true, null])
      render(<MeshDetailPage />)
      expect(screen.getByText('Conditions')).toBeInTheDocument()
      // "Ready" also appears in the MeshStatus header label — use the unique reason/message to pin the table row
      expect(screen.getByText('AllClustersReady')).toBeInTheDocument()
      expect(screen.getByText('All good')).toBeInTheDocument()
    })

  })
})

// ---------------------------------------------------------------------------
// ClusterStatusSection — imported directly, no hooks
// ---------------------------------------------------------------------------

describe('ClusterStatusSection', () => {
  it('shows empty state when there are no clusters and no conflict', () => {
    render(<ClusterStatusSection clusterStatuses={[]} />)
    expect(screen.getByText('No clusters are part of this mesh yet.')).toBeInTheDocument()
  })

  it('shows blocked message for OperatorConfigConflict', () => {
    const conditions = [makeCondition('Ready', 'False', 'OperatorConfigConflict', 'Operator conflict detected')]
    render(<ClusterStatusSection clusterStatuses={[]} meshConditions={conditions} />)
    expect(
      screen.getByText('This mesh is blocked: Operator conflict detected. Resolve the conflict to allow reconciliation.')
    ).toBeInTheDocument()
  })

  it('shows blocked message for NamespaceConflict', () => {
    const conditions = [makeCondition('Ready', 'False', 'NamespaceConflict', 'Namespace already in use')]
    render(<ClusterStatusSection clusterStatuses={[]} meshConditions={conditions} />)
    expect(
      screen.getByText('This mesh is blocked: Namespace already in use. Resolve the conflict to allow reconciliation.')
    ).toBeInTheDocument()
  })

  it('falls back to reason text when message is absent in a conflict', () => {
    const conditions = [makeCondition('Ready', 'False', 'OperatorConfigConflict')]
    render(<ClusterStatusSection clusterStatuses={[]} meshConditions={conditions} />)
    expect(screen.getByText(/OperatorConfigConflict/)).toBeInTheDocument()
  })

  it('does not show the conflict message when clusterStatuses is non-empty', () => {
    const conditions = [makeCondition('Ready', 'False', 'OperatorConfigConflict', 'Operator conflict detected')]
    render(<ClusterStatusSection clusterStatuses={[makeCluster('cluster-a', 'True')]} meshConditions={conditions} />)
    expect(screen.queryByText(/blocked/)).not.toBeInTheDocument()
    expect(screen.getByText('cluster-a')).toBeInTheDocument()
  })

  it('renders all cluster rows and the count in the card title', () => {
    const clusters = [
      makeCluster('cluster-a', 'True'),
      makeCluster('cluster-b', 'False', 'ReconcileError'),
    ]
    render(<ClusterStatusSection clusterStatuses={clusters} />)
    expect(screen.getByText('Clusters (2)')).toBeInTheDocument()
    expect(screen.getByText('cluster-a')).toBeInTheDocument()
    expect(screen.getByText('cluster-b')).toBeInTheDocument()
  })

  it('shows correct summary counts', () => {
    const clusters = [
      makeCluster('cluster-a', 'True'),
      makeCluster('cluster-b', 'False'),
      makeCluster('cluster-c', 'Unknown'),
    ]
    render(<ClusterStatusSection clusterStatuses={clusters} />)
    expect(screen.getByText('Ready (1)')).toBeInTheDocument()
    expect(screen.getByText('Not Ready (1)')).toBeInTheDocument()
    expect(screen.getByText('Unknown (1)')).toBeInTheDocument()
  })

  it('links cluster names to ACM cluster detail pages', () => {
    render(<ClusterStatusSection clusterStatuses={[makeCluster('cluster-a', 'True')]} />)
    expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
      'href',
      '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
    )
  })

  describe('filter toggles', () => {
    const clusters = [
      makeCluster('ready-cluster', 'True'),
      makeCluster('notready-cluster', 'False'),
      makeCluster('unknown-cluster', 'Unknown'),
    ]

    it('filters to only Ready clusters', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.click(screen.getByText('Ready (1)'))
      expect(screen.getByText('ready-cluster')).toBeInTheDocument()
      expect(screen.queryByText('notready-cluster')).not.toBeInTheDocument()
      expect(screen.queryByText('unknown-cluster')).not.toBeInTheDocument()
    })

    it('filters to only Not Ready clusters', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.click(screen.getByText('Not Ready (1)'))
      expect(screen.getByText('notready-cluster')).toBeInTheDocument()
      expect(screen.queryByText('ready-cluster')).not.toBeInTheDocument()
    })

    it('filters to only Unknown clusters', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.click(screen.getByText('Unknown (1)'))
      expect(screen.getByText('unknown-cluster')).toBeInTheDocument()
      expect(screen.queryByText('ready-cluster')).not.toBeInTheDocument()
      expect(screen.queryByText('notready-cluster')).not.toBeInTheDocument()
    })

    it('returns to all clusters when All is clicked after a filter', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.click(screen.getByText('Ready (1)'))
      await user.click(screen.getByText('All (3)'))
      expect(screen.getByText('ready-cluster')).toBeInTheDocument()
      expect(screen.getByText('notready-cluster')).toBeInTheDocument()
      expect(screen.getByText('unknown-cluster')).toBeInTheDocument()
    })
  })

  describe('search', () => {
    const clusters = [
      makeCluster('alpha-cluster', 'True'),
      makeCluster('beta-cluster', 'False'),
    ]

    it('shows only matching clusters when searching', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.type(screen.getByPlaceholderText('Filter by cluster name'), 'alpha')
      expect(screen.getByText('alpha-cluster')).toBeInTheDocument()
      expect(screen.queryByText('beta-cluster')).not.toBeInTheDocument()
    })

    it('shows no-match row when search has no results', async () => {
      const user = userEvent.setup()
      render(<ClusterStatusSection clusterStatuses={clusters} />)
      await user.type(screen.getByPlaceholderText('Filter by cluster name'), 'zzznomatch')
      expect(screen.getByText('No clusters match the current filter.')).toBeInTheDocument()
    })
  })

  describe('cluster availability status', () => {
    it('shows Available, Unavailable, and Unreachable labels from ManagedCluster data', () => {
      const clusters = [
        makeCluster('cluster-a', 'True'),
        makeCluster('cluster-b', 'False'),
        makeCluster('cluster-c', 'Unknown'),
      ]
      const managedClusterMap = new Map([
        ['cluster-a', { metadata: { name: 'cluster-a' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'True' }] } }],
        ['cluster-b', { metadata: { name: 'cluster-b' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'False' }] } }],
        ['cluster-c', { metadata: { name: 'cluster-c' }, status: { conditions: [{ type: 'ManagedClusterConditionAvailable', status: 'Unknown' }] } }],
      ]) as any
      render(<ClusterStatusSection clusterStatuses={clusters} managedClusterMap={managedClusterMap} />)
      expect(screen.getByText('Available')).toBeInTheDocument()
      expect(screen.getByText('Unavailable')).toBeInTheDocument()
      expect(screen.getByText('Unreachable')).toBeInTheDocument()
    })

    it('shows Unreachable when cluster is not in ManagedCluster map', () => {
      const clusters = [makeCluster('missing-cluster', 'True')]
      const managedClusterMap = new Map() as any
      render(<ClusterStatusSection clusterStatuses={clusters} managedClusterMap={managedClusterMap} />)
      expect(screen.getByText('Unreachable')).toBeInTheDocument()
    })
  })
})
