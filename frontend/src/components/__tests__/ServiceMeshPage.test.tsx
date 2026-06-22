import { render, screen } from '@testing-library/react'
import ServiceMeshPage from '../ServiceMeshPage'
import { useMultiClusterMeshes } from '../../hooks/useMultiClusterMeshes'
import type { MultiClusterMesh } from '../../types/multiClusterMesh'

jest.mock('../../hooks/useMultiClusterMeshes')

const mockUseMultiClusterMeshes = useMultiClusterMeshes as jest.Mock

// consoleSdkMock provides useListPageFilter and useActiveColumns stubs that pass data through.
// VirtualizedTable renders rows when loaded=true and data is non-empty.

const makeMesh = (overrides: Partial<MultiClusterMesh> = {}): MultiClusterMesh => ({
  apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
  kind: 'MultiClusterMesh',
  metadata: { name: 'test-mesh', namespace: 'mesh-system' },
  spec: { clusterSet: 'global' },
  ...overrides,
})

describe('ServiceMeshPage', () => {
  afterEach(() => {
    jest.clearAllMocks()
  })

  it('renders the page header', () => {
    mockUseMultiClusterMeshes.mockReturnValue([[], true, null])
    render(<ServiceMeshPage />)
    expect(screen.getByText('Meshes')).toBeInTheDocument()
  })

  it('shows empty state when no meshes exist and data is loaded', () => {
    mockUseMultiClusterMeshes.mockReturnValue([[], true, null])
    render(<ServiceMeshPage />)
    expect(screen.getByText('No meshes have been created yet.')).toBeInTheDocument()
  })

  it('shows loading state while data is not yet loaded', () => {
    mockUseMultiClusterMeshes.mockReturnValue([[], false, null])
    render(<ServiceMeshPage />)
    expect(screen.getByTestId('loading')).toBeInTheDocument()
  })

  it('shows error state when the watch returns an error', () => {
    mockUseMultiClusterMeshes.mockReturnValue([[], true, new Error('watch failed')])
    render(<ServiceMeshPage />)
    expect(screen.getByTestId('load-error')).toBeInTheDocument()
  })

  it('renders mesh rows with name links when meshes are loaded', () => {
    const meshes = [makeMesh(), makeMesh({ metadata: { name: 'prod-mesh', namespace: 'mesh-system' } })]
    mockUseMultiClusterMeshes.mockReturnValue([meshes, true, null])
    render(<ServiceMeshPage />)
    expect(screen.getByText('test-mesh')).toBeInTheDocument()
    expect(screen.getByText('prod-mesh')).toBeInTheDocument()
  })

  it('links mesh names to their detail pages', () => {
    const mesh = makeMesh()
    mockUseMultiClusterMeshes.mockReturnValue([[mesh], true, null])
    render(<ServiceMeshPage />)
    const link = screen.getByRole('link', { name: 'test-mesh' })
    expect(link).toHaveAttribute('href', '/service-mesh/mesh-system/test-mesh')
  })

  it('links cluster set names to ACM cluster set detail pages', () => {
    const mesh = makeMesh()
    mockUseMultiClusterMeshes.mockReturnValue([[mesh], true, null])
    render(<ServiceMeshPage />)
    const link = screen.getByRole('link', { name: 'global' })
    expect(link).toHaveAttribute('href', '/multicloud/infrastructure/clusters/sets/details/global/overview')
  })

  it('shows Configured trust label when issuerRef is set', () => {
    const mesh = makeMesh({
      spec: {
        clusterSet: 'global',
        security: { trust: { certManager: { issuerRef: { name: 'my-issuer' } } } },
      },
    })
    mockUseMultiClusterMeshes.mockReturnValue([[mesh], true, null])
    render(<ServiceMeshPage />)
    expect(screen.getByText('Configured')).toBeInTheDocument()
  })

  it('shows Not configured trust label when no issuerRef', () => {
    const mesh = makeMesh()
    mockUseMultiClusterMeshes.mockReturnValue([[mesh], true, null])
    render(<ServiceMeshPage />)
    expect(screen.getByText('Not configured')).toBeInTheDocument()
  })
})
