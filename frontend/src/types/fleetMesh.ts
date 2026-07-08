import type { K8sCondition } from './common'
import type { EnrichedControlPlane } from './istio'
import type { MultiClusterMesh } from './multiClusterMesh'

export interface FleetMeshItem {
  metadata: {
    name: string
    creationTimestamp?: string
  }
  clusterCount: number
  clusterSet?: string
  conditions?: K8sCondition[]
  controlPlanes?: EnrichedControlPlane[]
  detailLink: string
  kind: 'managed' | 'discovered'
  mcm?: MultiClusterMesh
  mcmNamespace?: string
  meshID?: string
  meshIDConflict?: boolean
  statusRank: number
  trustIssuer?: string
}
