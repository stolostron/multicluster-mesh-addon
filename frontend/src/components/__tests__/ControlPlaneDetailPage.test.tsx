import { render, screen, waitFor } from '@testing-library/react'
import ControlPlaneDetailPage from '../ControlPlaneDetailPage'
import { useParams } from 'react-router-dom-v5-compat'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio } from '../../types/istio'

const mockUseParams = useParams as jest.Mock
const mockUseK8sWatchResource = useK8sWatchResource as jest.Mock
const mockFleetK8sGet = fleetK8sGet as jest.Mock

const makeIstio = (overrides: Partial<Istio> = {}): Istio => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name: 'default', creationTimestamp: '2026-06-22T12:00:00Z' },
  spec: {
    namespace: 'istio-system',
    version: 'v1.24.0',
    values: {
      global: {
        meshID: 'mesh1',
        multiCluster: { clusterName: 'cluster-a' },
        network: 'network1',
      },
    },
  },
  status: {
    conditions: [{ type: 'Ready', status: 'True' }],
  },
  ...overrides,
})

afterEach(() => jest.clearAllMocks())

beforeEach(() => {
  mockUseK8sWatchResource.mockReturnValue([[], true, null])
})

describe('ControlPlaneDetailPage', () => {
  describe('invalid URL (missing params)', () => {
    it('shows Not Found when cluster and name are absent', () => {
      mockUseParams.mockReturnValue({})
      render(<ControlPlaneDetailPage />)
      expect(screen.getByText('Not Found')).toBeInTheDocument()
      expect(screen.getByText('Invalid control plane URL. Expected /control-planes/:cluster/:name.')).toBeInTheDocument()
    })
  })

  describe('loading state', () => {
    it('shows spinner while fleetK8sGet is pending', () => {
      mockUseParams.mockReturnValue({ cluster: 'cluster-a', name: 'default' })
      mockFleetK8sGet.mockReturnValue(new Promise(() => {}))
      render(<ControlPlaneDetailPage />)
      expect(screen.getByLabelText('Loading control plane')).toBeInTheDocument()
    })
  })

  describe('error states', () => {
    it('shows generic error when fleetK8sGet rejects', async () => {
      mockUseParams.mockReturnValue({ cluster: 'cluster-a', name: 'default' })
      mockFleetK8sGet.mockRejectedValue(new Error('network timeout'))
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Error loading control plane')).toBeInTheDocument()
        expect(screen.getByText('network timeout')).toBeInTheDocument()
      })
    })

    it('shows not-found message for 404 errors', async () => {
      mockUseParams.mockReturnValue({ cluster: 'cluster-a', name: 'default' })
      const error404 = new Error('Not Found')
      ;(error404 as any).code = 404
      mockFleetK8sGet.mockRejectedValue(error404)
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Control plane not found')).toBeInTheDocument()
        expect(screen.getByText('Istio "default" was not found on cluster "cluster-a".')).toBeInTheDocument()
      })
    })
  })

  describe('loaded state', () => {
    beforeEach(() => {
      mockUseParams.mockReturnValue({ cluster: 'cluster-a', name: 'default' })
    })

    it('renders the breadcrumb and name heading', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'Control Planes' })).toBeInTheDocument()
        expect(screen.getByRole('heading', { name: 'default' })).toBeInTheDocument()
      })
    })

    it('shows version in the overview card', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('v1.24.0')).toBeInTheDocument()
      })
    })

    it('shows meshID in the overview card', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('mesh1')).toBeInTheDocument()
      })
    })

    it('shows network in the overview card', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('network1')).toBeInTheDocument()
      })
    })

    it('links cluster name to ACM cluster detail page', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
          'href',
          '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
        )
      })
    })

    it('shows conditions table when conditions are present', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Conditions')).toBeInTheDocument()
      })
    })

    it('shows Managed By card when correlated to a MultiClusterMesh', async () => {
      const mcm = {
        metadata: { name: 'my-mesh', namespace: 'mesh-system' },
        spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
        status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
      }
      mockUseK8sWatchResource.mockReturnValue([[mcm], true, null])
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Managed By')).toBeInTheDocument()
        expect(screen.getByRole('link', { name: 'my-mesh' })).toHaveAttribute(
          'href',
          '/service-mesh/mesh-system/my-mesh',
        )
      })
    })

    it('does not show Managed By card when not correlated', async () => {
      mockFleetK8sGet.mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Overview')).toBeInTheDocument()
      })
      expect(screen.queryByText('Managed By')).not.toBeInTheDocument()
    })
  })

  describe('useEffect cleanup', () => {
    it('does not update state after unmount', async () => {
      mockUseParams.mockReturnValue({ cluster: 'cluster-a', name: 'default' })
      let resolvePromise: (value: any) => void
      mockFleetK8sGet.mockReturnValue(new Promise((resolve) => { resolvePromise = resolve }))
      const { unmount } = render(<ControlPlaneDetailPage />)
      unmount()
      resolvePromise!(makeIstio())
      // No assertion needed — the test passes if no "state update on unmounted component" warning occurs
    })
  })
})
