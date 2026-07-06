import type { MultiClusterMesh, ClusterMeshStatus } from '../types/multiClusterMesh'
import type { EnrichedControlPlane } from '../types/istio'
import type { K8sCondition } from '../types/common'

export const makeMesh = (overrides: Partial<MultiClusterMesh> = {}): MultiClusterMesh => ({
  apiVersion: 'mesh.open-cluster-management.io/v1alpha1',
  kind: 'MultiClusterMesh',
  metadata: { name: 'test-mesh', namespace: 'mesh-system', creationTimestamp: '2026-06-22T12:00:00Z' },
  spec: { clusterSet: 'global' },
  ...overrides,
})

export const makeEnrichedCP = (overrides: Partial<EnrichedControlPlane> = {}): EnrichedControlPlane => ({
  metadata: { name: 'default', creationTimestamp: '2026-06-22T12:00:00Z' },
  clusterName: 'cluster-a',
  controlPlaneNamespace: 'istio-system',
  ...overrides,
})

export const makeCluster = (
  name: string,
  status: 'True' | 'False' | 'Unknown' = 'True',
  reason?: string,
): ClusterMeshStatus => ({
  clusterName: name,
  conditions: [{ type: 'OperatorInstalled', status, reason } as K8sCondition],
})

export const makeSearchResult = (cluster: string, name: string) => ({
  apiVersion: 'sailoperator.io/v1',
  kind: 'Istio',
  metadata: { name, creationTimestamp: '2026-06-22T12:00:00Z' },
  cluster,
  spec: { namespace: 'istio-system' },
})
