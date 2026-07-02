import type { K8sGroupVersionKind, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from './common'

export const managedClusterGroupVersionKind: K8sGroupVersionKind = {
  group: 'cluster.open-cluster-management.io',
  kind: 'ManagedCluster',
  version: 'v1',
}

export interface ManagedClusterStatus {
  conditions?: K8sCondition[]
}

export interface ManagedCluster extends K8sResourceCommon {
  status?: ManagedClusterStatus
}

export type ClusterAvailability = 'available' | 'unavailable' | 'unreachable'

export function getClusterAvailability(cluster: ManagedCluster | undefined): ClusterAvailability {
  const condition = cluster?.status?.conditions?.find(
    (c) => c.type === 'ManagedClusterConditionAvailable',
  )
  if (!condition) return 'unreachable'
  if (condition.status === 'True') return 'available'
  if (condition.status === 'False') return 'unavailable'
  return 'unreachable'
}

export function availabilityColor(availability: ClusterAvailability): 'green' | 'red' | 'grey' {
  if (availability === 'available') return 'green'
  if (availability === 'unavailable') return 'red'
  return 'grey'
}

export function availabilityLabelKey(availability: ClusterAvailability): string {
  if (availability === 'available') return 'Available'
  if (availability === 'unavailable') return 'Unavailable'
  return 'Unreachable'
}
