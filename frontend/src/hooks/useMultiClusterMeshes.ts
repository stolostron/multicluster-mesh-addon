import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { MultiClusterMesh, multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'

/** Watches all MultiClusterMesh CRs across namespaces on the hub cluster. */
export function useMultiClusterMeshes() {
  return useK8sWatchResource<MultiClusterMesh[]>({
    groupVersionKind: multiClusterMeshGroupVersionKind,
    isList: true,
  })
}
