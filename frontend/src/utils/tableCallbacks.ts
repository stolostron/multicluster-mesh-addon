import type { ClusterMeshStatus } from '../types/multiClusterMesh'

export const clusterMeshStatusRowKey = (cs: ClusterMeshStatus) => cs.clusterName

export const clusterMeshStatusSearchMatch = (cs: ClusterMeshStatus, query: string) =>
  cs.clusterName.toLowerCase().includes(query.toLowerCase())
