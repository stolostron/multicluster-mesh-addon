import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TrustStatusCard } from '../TrustStatusCard'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import type { Certificate } from '../../types/certManager'
import type { ManifestWork } from '../../types/manifestWork'
import type { ClusterMeshStatus, K8sCondition } from '../../types/multiClusterMesh'

const mockUseK8sWatchResource = useK8sWatchResource as jest.Mock

const CLUSTER_NAME_LABEL = 'mesh.open-cluster-management.io/cluster-name'
const MESH_NAME_LABEL = 'mesh.open-cluster-management.io/mesh-name'
const MESH_NAMESPACE_LABEL = 'mesh.open-cluster-management.io/mesh-namespace'

// ---------------------------------------------------------------------------
// Test data factories
// ---------------------------------------------------------------------------

const makeCondition = (
  type: string,
  status: 'True' | 'False' | 'Unknown',
  reason?: string,
): K8sCondition => ({ type, status, reason })

const makeCluster = (name: string): ClusterMeshStatus => ({
  clusterName: name,
  conditions: [makeCondition('OperatorInstalled', 'True')],
})

const makeCert = (
  clusterName: string,
  readyStatus: 'True' | 'False' | 'Unknown',
  reason?: string,
  timestamps?: { notAfter?: string; renewalTime?: string },
): Certificate => ({
  apiVersion: 'cert-manager.io/v1',
  kind: 'Certificate',
  metadata: {
    name: `cert-${clusterName}`,
    namespace: 'mesh-system',
    labels: {
      [CLUSTER_NAME_LABEL]: clusterName,
      [MESH_NAME_LABEL]: 'test-mesh',
    },
  },
  status: {
    conditions: [makeCondition('Ready', readyStatus, reason)],
    ...timestamps,
  },
})

const makeMW = (clusterName: string, applied: boolean, available: boolean): ManifestWork => ({
  apiVersion: 'work.open-cluster-management.io/v1',
  kind: 'ManifestWork',
  metadata: {
    name: `mw-${clusterName}`,
    namespace: clusterName,
    labels: {
      [CLUSTER_NAME_LABEL]: clusterName,
      [MESH_NAME_LABEL]: 'test-mesh',
      [MESH_NAMESPACE_LABEL]: 'mesh-system',
    },
  },
  status: {
    conditions: [
      makeCondition('Applied', applied ? 'True' : 'False'),
      makeCondition('Available', available ? 'True' : 'False'),
    ],
  },
})

// Convenience: set up the mock implementation so cert and MW calls are
// distinguished by GVK kind. Re-renders (from filter/search interactions)
// keep getting the right data without exhausting a once-queue.
const setupWatches = (certs: Certificate[], mws: ManifestWork[], opts?: {
  certsLoaded?: boolean
  certsError?: unknown
  mwLoaded?: boolean
  mwError?: unknown
}) => {
  const { certsLoaded = true, certsError = null, mwLoaded = true, mwError = null } = opts ?? {}
  mockUseK8sWatchResource.mockImplementation((params: { groupVersionKind?: { kind?: string } } | null) => {
    if (params?.groupVersionKind?.kind === 'Certificate') return [certs, certsLoaded, certsError]
    if (params?.groupVersionKind?.kind === 'ManifestWork') return [mws, mwLoaded, mwError]
    return [null, false, null]
  })
}

const defaultProps = {
  meshName: 'test-mesh',
  meshNamespace: 'mesh-system',
  issuerName: 'root-ca',
  clusterStatuses: [makeCluster('cluster-a')],
}

afterEach(() => jest.clearAllMocks())

// ---------------------------------------------------------------------------
// No issuer configured
// ---------------------------------------------------------------------------

describe('TrustStatusCard — no issuer', () => {
  it('renders the not-configured message and does not start any watches', () => {
    render(<TrustStatusCard {...defaultProps} issuerName="" clusterStatuses={[]} />)
    expect(screen.getByText('Trust Status')).toBeInTheDocument()
    expect(screen.getByText('trustNotConfiguredMessage')).toBeInTheDocument()
    // Hooks must always be called (rules of hooks), but both are passed null — no real watch started.
    expect(mockUseK8sWatchResource).not.toHaveBeenCalledWith(
      expect.objectContaining({ groupVersionKind: expect.anything() }),
    )
  })
})

// ---------------------------------------------------------------------------
// Loading states
// ---------------------------------------------------------------------------

describe('TrustStatusCard — loading', () => {
  it('shows spinner when certs are not yet loaded', () => {
    setupWatches([], [], { certsLoaded: false })
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByLabelText('Loading trust status')).toBeInTheDocument()
  })

  it('shows spinner when mw watch is still loading and no mwError', () => {
    setupWatches([], [], { certsLoaded: true, mwLoaded: false, mwError: null })
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByLabelText('Loading trust status')).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Error states
// ---------------------------------------------------------------------------

describe('TrustStatusCard — errors', () => {
  it('shows certs error card when cert watch fails', () => {
    setupWatches([], [], { certsError: new Error('cert watch failed') })
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('Unable to load certificate data')).toBeInTheDocument()
    expect(screen.getByText('cert watch failed')).toBeInTheDocument()
  })

  it('renders the full table when mwError is set but certs are loaded', () => {
    // mwError is row-level — the component does not early-return;
    // it renders the table with "Unavailable" in the distribution column.
    const certs = [makeCert('cluster-a', 'True')]
    setupWatches(certs, [], { mwError: new Error('mw watch failed') })
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('cluster-a')).toBeInTheDocument()
    expect(screen.getByText('Unavailable')).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Empty states (after loading)
// ---------------------------------------------------------------------------

describe('TrustStatusCard — empty states', () => {
  it('shows no-clusters message when clusterStatuses is empty', () => {
    setupWatches([], [])
    render(<TrustStatusCard {...defaultProps} clusterStatuses={[]} />)
    expect(screen.getByText('No clusters are part of this mesh yet.')).toBeInTheDocument()
  })

  it('shows not-reconciling message when certs and mws are both empty', () => {
    setupWatches([], [])
    render(<TrustStatusCard {...defaultProps} />)
    expect(
      screen.getByText('No certificates have been created yet — the controller may still be reconciling.')
    ).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Row rendering — cert and distribution status labels
// ---------------------------------------------------------------------------

describe('TrustStatusCard — row status labels', () => {
  it('shows Distributed when cert is Ready and MW is Applied + Available', () => {
    const certs = [makeCert('cluster-a', 'True', undefined, {
      notAfter: '2026-12-31T00:00:00Z',
      renewalTime: '2026-11-30T00:00:00Z',
    })]
    const mws = [makeMW('cluster-a', true, true)]
    setupWatches(certs, mws)
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('Ready')).toBeInTheDocument()
    expect(screen.getByText('Distributed')).toBeInTheDocument()
    expect(screen.getByText('2026-12-31T00:00:00Z')).toBeInTheDocument()
    expect(screen.getByText('2026-11-30T00:00:00Z')).toBeInTheDocument()
  })

  it('shows dash in Expires and Renews columns when cert has no timestamp fields', () => {
    const certs = [makeCert('cluster-a', 'True')]
    const mws = [makeMW('cluster-a', true, true)]
    setupWatches(certs, mws)
    render(<TrustStatusCard {...defaultProps} />)
    // Two dashes: one for Expires, one for Renews
    expect(screen.getAllByText('-').length).toBeGreaterThanOrEqual(2)
  })

  it('shows Applied when cert is Ready but MW is only Applied (not yet Available)', () => {
    const certs = [makeCert('cluster-a', 'True')]
    const mws = [makeMW('cluster-a', true, false)]
    setupWatches(certs, mws)
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('Applied')).toBeInTheDocument()
  })

  it('shows cert reason when cert is not Ready', () => {
    const certs = [makeCert('cluster-a', 'False', 'IssuerNotReady')]
    const mws = [makeMW('cluster-a', false, false)]
    setupWatches(certs, mws)
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('IssuerNotReady')).toBeInTheDocument()
  })

  it('shows Pending cert label when no cert exists for a cluster', () => {
    // Use Applied=true so distribution shows "Applied" — keeps "Pending" unambiguous in the cert column.
    setupWatches([], [makeMW('cluster-a', true, false)])
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('Applied')).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Filter toggles
// ---------------------------------------------------------------------------

describe('TrustStatusCard — filters', () => {
  const clusters = [
    makeCluster('dist-cluster'),
    makeCluster('pending-cluster'),
    makeCluster('failed-cluster'),
  ]
  const certs = [
    makeCert('dist-cluster', 'True'),
    makeCert('failed-cluster', 'False'),
    // no cert for pending-cluster → pending
  ]
  const mws = [
    makeMW('dist-cluster', true, true),
  ]

  beforeEach(() => setupWatches(certs, mws))

  it('filters to only Distributed clusters', async () => {
    const user = userEvent.setup()
    render(<TrustStatusCard {...defaultProps} clusterStatuses={clusters} />)
    await user.click(screen.getByText('Distributed (1)'))
    expect(screen.getByText('dist-cluster')).toBeInTheDocument()
    expect(screen.queryByText('pending-cluster')).not.toBeInTheDocument()
    expect(screen.queryByText('failed-cluster')).not.toBeInTheDocument()
  })

  it('filters to only Pending clusters', async () => {
    const user = userEvent.setup()
    render(<TrustStatusCard {...defaultProps} clusterStatuses={clusters} />)
    await user.click(screen.getByText('Pending (1)'))
    expect(screen.getByText('pending-cluster')).toBeInTheDocument()
    expect(screen.queryByText('dist-cluster')).not.toBeInTheDocument()
  })

  it('filters to only Failed clusters', async () => {
    const user = userEvent.setup()
    render(<TrustStatusCard {...defaultProps} clusterStatuses={clusters} />)
    await user.click(screen.getByText('Failed (1)'))
    expect(screen.getByText('failed-cluster')).toBeInTheDocument()
    expect(screen.queryByText('dist-cluster')).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

describe('TrustStatusCard — search', () => {
  const clusters = [makeCluster('alpha-cluster'), makeCluster('beta-cluster')]
  const certs = [makeCert('alpha-cluster', 'True'), makeCert('beta-cluster', 'True')]
  const mws = [makeMW('alpha-cluster', true, true), makeMW('beta-cluster', true, true)]

  beforeEach(() => setupWatches(certs, mws))

  it('narrows results to the matching cluster', async () => {
    const user = userEvent.setup()
    render(<TrustStatusCard {...defaultProps} clusterStatuses={clusters} />)
    await user.type(screen.getByPlaceholderText('Filter by cluster name'), 'alpha')
    expect(screen.getByText('alpha-cluster')).toBeInTheDocument()
    expect(screen.queryByText('beta-cluster')).not.toBeInTheDocument()
  })

  it('shows no-match row when search has no results', async () => {
    const user = userEvent.setup()
    render(<TrustStatusCard {...defaultProps} clusterStatuses={clusters} />)
    await user.type(screen.getByPlaceholderText('Filter by cluster name'), 'zzznomatch')
    expect(screen.getByText('No clusters match the current filter.')).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// Cluster links
// ---------------------------------------------------------------------------

describe('TrustStatusCard — cluster links', () => {
  it('links cluster names to ACM cluster detail pages', () => {
    const certs = [makeCert('cluster-a', 'True')]
    const mws = [makeMW('cluster-a', true, true)]
    setupWatches(certs, mws)
    render(<TrustStatusCard {...defaultProps} />)
    expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
      'href',
      '/multicloud/infrastructure/clusters/details/cluster-a',
    )
  })
})
