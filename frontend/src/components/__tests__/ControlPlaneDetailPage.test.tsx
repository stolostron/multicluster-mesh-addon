import { render, screen, waitFor } from '@testing-library/react'
import ControlPlaneDetailPage from '../ControlPlaneDetailPage'
import { useParams } from 'react-router-dom-v5-compat'
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { fleetK8sGet } from '@stolostron/multicluster-sdk'
import type { Istio } from '../../types/istio'

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

afterEach(() => rstest.clearAllMocks())

beforeEach(() => {
  rstest.mocked(useK8sWatchResource).mockReturnValue([[], true, null])
})

describe('ControlPlaneDetailPage', () => {
  describe('invalid URL (missing params)', () => {
    it('shows Not Found when cluster and name are absent', () => {
      rstest.mocked(useParams).mockReturnValue({})
      render(<ControlPlaneDetailPage />)
      expect(screen.getByText('Not Found')).toBeInTheDocument()
      expect(screen.getByText('Invalid URL. Expected /fleet-mesh/control-planes/:type/:cluster/:name.')).toBeInTheDocument()
    })

    it('shows Not Found when type param is invalid', () => {
      rstest.mocked(useParams).mockReturnValue({ type: 'bogus', cluster: 'cluster-a', name: 'default' })
      render(<ControlPlaneDetailPage />)
      expect(screen.getByText('Not Found')).toBeInTheDocument()
      expect(screen.getByText('Invalid URL. Expected /fleet-mesh/control-planes/:type/:cluster/:name.')).toBeInTheDocument()
    })
  })

  describe('loading state', () => {
    it('shows spinner while fleetK8sGet is pending', () => {
      rstest.mocked(useParams).mockReturnValue({ type: 'discovered', cluster: 'cluster-a', name: 'default' })
      rstest.mocked(fleetK8sGet).mockReturnValue(new Promise(() => {}))
      render(<ControlPlaneDetailPage />)
      expect(screen.getByLabelText('Loading control plane')).toBeInTheDocument()
    })
  })

  describe('error states', () => {
    it('shows generic error when fleetK8sGet rejects', async () => {
      rstest.mocked(useParams).mockReturnValue({ type: 'discovered', cluster: 'cluster-a', name: 'default' })
      rstest.mocked(fleetK8sGet).mockRejectedValue(new Error('network timeout'))
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Error loading control plane')).toBeInTheDocument()
        expect(screen.getByText('An unexpected error occurred. Check the browser console for details.')).toBeInTheDocument()
      })
    })

    it('shows not-found message for 404 errors', async () => {
      rstest.mocked(useParams).mockReturnValue({ type: 'discovered', cluster: 'cluster-a', name: 'default' })
      const error404 = new Error('Not Found')
      ;(error404 as any).code = 404
      rstest.mocked(fleetK8sGet).mockRejectedValue(error404)
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Control plane not found')).toBeInTheDocument()
        expect(screen.getByText('Istio "default" was not found on cluster "cluster-a".')).toBeInTheDocument()
      })
    })
  })

  describe('loaded state', () => {
    beforeEach(() => {
      rstest.mocked(useParams).mockReturnValue({ type: 'discovered', cluster: 'cluster-a', name: 'default' })
    })

    it('renders the breadcrumb and name heading', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'Control Planes' })).toBeInTheDocument()
        expect(screen.getByRole('heading', { name: 'default' })).toBeInTheDocument()
      })
    })

    it('shows version in the overview card', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('v1.24.0')).toBeInTheDocument()
      })
    })

    it('shows meshID in the overview card', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getAllByText('mesh1')).toHaveLength(1)
      })
    })

    it('shows network in the overview card', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('network1')).toBeInTheDocument()
      })
    })

    it('links cluster name to ACM cluster detail page', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'cluster-a' })).toHaveAttribute(
          'href',
          '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
        )
      })
    })

    it('shows conditions table when conditions are present', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Conditions')).toBeInTheDocument()
      })
    })

    it('links mesh ID to managed mesh detail page when correlated to a MultiClusterMesh', async () => {
      const mcm = {
        metadata: { name: 'my-mesh', namespace: 'mesh-system' },
        spec: { clusterSet: 'global', controlPlane: { namespace: 'istio-system' } },
        status: { clusterStatus: [{ clusterName: 'cluster-a' }] },
      }
      rstest.mocked(useK8sWatchResource).mockReturnValue([[mcm], true, null])
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'mesh1' })).toHaveAttribute(
          'href',
          '/fleet-mesh/meshes/managed/mesh-system/my-mesh',
        )
      })
    })

    it('shows discovered mesh link when not correlated to a managed mesh', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio())
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Mesh ID')).toBeInTheDocument()
      })
      expect(screen.queryByRole('link', { name: 'mesh1' })).toHaveAttribute(
        'href',
        '/fleet-mesh/meshes/discovered/mesh1',
      )
    })

    it('shows dash for mesh ID when CP has no meshID and no MCM', async () => {
      rstest.mocked(fleetK8sGet).mockResolvedValue(makeIstio({
        spec: { namespace: 'istio-system' },
      }))
      render(<ControlPlaneDetailPage />)
      await waitFor(() => {
        expect(screen.getByText('Mesh ID')).toBeInTheDocument()
      })
      expect(screen.queryByRole('link', { name: 'mesh1' })).not.toBeInTheDocument()
    })
  })

  describe('useEffect cancellation', () => {
    it('does not update state after unmount', async () => {
      rstest.mocked(useParams).mockReturnValue({ type: 'discovered', cluster: 'cluster-a', name: 'default' })
      let resolvePromise: (value: any) => void
      rstest.mocked(fleetK8sGet).mockReturnValue(new Promise((resolve) => { resolvePromise = resolve }))
      const { unmount } = render(<ControlPlaneDetailPage />)
      unmount()
      resolvePromise!(makeIstio())
      await new Promise((r) => setTimeout(r, 0))
      expect(screen.queryByText('default')).not.toBeInTheDocument()
    })
  })
})
