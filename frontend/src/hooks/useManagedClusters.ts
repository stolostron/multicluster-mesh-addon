import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import type { ManagedCluster } from '../types/managedCluster'
import { managedClusterGroupVersionKind } from '../types/managedCluster'

export function useManagedClusters() {
  return useK8sWatchResource<ManagedCluster[]>({
    groupVersionKind: managedClusterGroupVersionKind,
    isList: true,
  })
}
