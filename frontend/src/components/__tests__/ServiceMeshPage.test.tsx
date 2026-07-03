import { render, screen } from '@testing-library/react'
import ServiceMeshPage from '../ServiceMeshPage'
import { useFleetMeshItems } from '../../hooks/useFleetMeshItems'
import type { FleetMeshItem } from '../../types/fleetMesh'
import type { UseFleetMeshItemsResult } from '../../hooks/useFleetMeshItems'

rstest.mock('../../hooks/useFleetMeshItems', { mock: true })

const makeItem = (overrides: Partial<FleetMeshItem> = {}): FleetMeshItem => ({
  metadata: { name: 'test-mesh' },
  kind: 'managed',
  detailLink: '/fleet-mesh/meshes/managed/mesh-system/test-mesh',
  clusterCount: 1,
  clusterSet: 'global',
  mcmNamespace: 'mesh-system',
  meshID: 'mesh-system-test-mesh',
  statusRank: 0,
  trustIssuer: undefined,
  conditions: [{ type: 'Ready', status: 'True' }],
  ...overrides,
})

const defaultHookResult: UseFleetMeshItemsResult = {
  items: [],
  loaded: true,
  enrichmentError: null,
  isFleetAvailable: true,
  mcms: [],
  mcmsLoaded: true,
  mcmsError: null,
  enrichedPlanes: [],
  enrichmentLoaded: true,
  searchLoaded: true,
  searchError: null,
}

function mockHook(overrides: Partial<UseFleetMeshItemsResult> = {}) {
  rstest.mocked(useFleetMeshItems).mockReturnValue({ ...defaultHookResult, ...overrides })
}

describe('ServiceMeshPage', () => {
  afterEach(() => {
    rstest.clearAllMocks()
  })

  it('shows empty state when no meshes exist and data is loaded', () => {
    mockHook()
    render(<ServiceMeshPage />)
    expect(screen.getByText('No managed or discovered meshes found.')).toBeInTheDocument()
  })

  it('shows loading state while data is not yet loaded', () => {
    mockHook({ loaded: false })
    render(<ServiceMeshPage />)
    expect(screen.getByTestId('loading')).toBeInTheDocument()
  })

  it('renders managed mesh rows with Mesh ID links to detail page', () => {
    const items = [
      makeItem(),
      makeItem({ metadata: { name: 'prod-mesh' }, meshID: 'mesh-system-prod-mesh', detailLink: '/fleet-mesh/meshes/managed/mesh-system/prod-mesh' }),
    ]
    mockHook({ items })
    render(<ServiceMeshPage />)
    expect(screen.getByText('test-mesh')).toBeInTheDocument()
    expect(screen.getByText('prod-mesh')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'mesh-system-test-mesh' })).toHaveAttribute('href', '/fleet-mesh/meshes/managed/mesh-system/test-mesh')
    expect(screen.getByRole('link', { name: 'mesh-system-prod-mesh' })).toHaveAttribute('href', '/fleet-mesh/meshes/managed/mesh-system/prod-mesh')
  })

  it('renders discovered mesh rows with Mesh ID links to their detailLink', () => {
    const items = [
      makeItem({
        metadata: { name: 'discovered-mesh' },
        kind: 'discovered',
        meshID: 'discovered-id',
        detailLink: '/fleet-mesh/meshes/discovered/discovered-id',
        mcmNamespace: undefined,
        clusterSet: undefined,
      }),
    ]
    mockHook({ items })
    render(<ServiceMeshPage />)
    const link = screen.getByRole('link', { name: 'discovered-id' })
    expect(link).toHaveAttribute('href', '/fleet-mesh/meshes/discovered/discovered-id')
  })

  it('shows Mesh ID column values for managed and discovered items', () => {
    const items = [
      makeItem({ metadata: { name: 'managed-mesh' }, meshID: 'managed-id' }),
      makeItem({
        metadata: { name: 'discovered-mesh' },
        kind: 'discovered',
        meshID: 'discovered-id',
        detailLink: '/fleet-mesh/control-planes/c1/cp',
      }),
    ]
    mockHook({ items })
    render(<ServiceMeshPage />)
    expect(screen.getByText('managed-id')).toBeInTheDocument()
    expect(screen.getByText('discovered-id')).toBeInTheDocument()
  })

  it('renders Type column with Managed for managed items and Discovered for discovered items', () => {
    const items = [
      makeItem({ metadata: { name: 'managed-mesh' }, meshID: 'managed-id' }),
      makeItem({
        metadata: { name: 'discovered-mesh' },
        kind: 'discovered',
        meshID: 'discovered-id',
        detailLink: '/fleet-mesh/meshes/discovered/discovered-id',
      }),
    ]
    mockHook({ items })
    render(<ServiceMeshPage />)
    expect(screen.getByText('Managed')).toBeInTheDocument()
    expect(screen.getByText('Discovered')).toBeInTheDocument()
  })

  it('shows ACM-unavailable banner when isFleetAvailable is false', () => {
    mockHook({ isFleetAvailable: false })
    render(<ServiceMeshPage />)
    expect(
      screen.getByText('Install Red Hat Advanced Cluster Management to discover unmanaged meshes across the fleet.'),
    ).toBeInTheDocument()
  })

  it('shows enrichment error banner when enrichmentError is set', () => {
    mockHook({ enrichmentError: new Error('search failed') })
    render(<ServiceMeshPage />)
    expect(
      screen.getByText('Unable to load control plane data. Some meshes may not be shown.'),
    ).toBeInTheDocument()
  })

  it('shows warning icon for managed item with meshIDConflict', () => {
    const items = [makeItem({ meshIDConflict: true })]
    mockHook({ items })
    render(<ServiceMeshPage />)
    expect(screen.getByText('Mesh ID Conflict')).toBeInTheDocument()
  })
})
