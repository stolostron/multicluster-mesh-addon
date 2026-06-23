import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
import { MultiClusterMesh, multiClusterMeshGroupVersionKind } from '../types/multiClusterMesh'

export function useMultiClusterMeshes() {
  return useK8sWatchResource<MultiClusterMesh[]>({
    groupVersionKind: multiClusterMeshGroupVersionKind,
    isList: true,
  })
}
