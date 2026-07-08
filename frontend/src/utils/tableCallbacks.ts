import type { ClusterMeshStatus } from '../types/multiClusterMesh'

export const clusterMeshStatusRowKey = (cs: ClusterMeshStatus) => cs.clusterName

export const clusterMeshStatusSearchMatch = (cs: ClusterMeshStatus, query: string) =>
  cs.clusterName.toLowerCase().includes(query.toLowerCase())

export function sortWithComparator<T>(
  data: T[],
  sortDirection: string,
  compare: (a: T, b: T) => number,
): T[] {
  const dir = sortDirection === 'asc' ? 1 : -1
  return [...data].sort((a, b) => dir * compare(a, b))
}
