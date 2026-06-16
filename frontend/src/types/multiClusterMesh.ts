import type { K8sGroupVersionKind, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'

export const multiClusterMeshGroupVersionKind: K8sGroupVersionKind = {
  group: 'mesh.open-cluster-management.io',
  version: 'v1alpha1',
  kind: 'MultiClusterMesh',
}

export interface K8sCondition {
  type: string
  status: 'True' | 'False' | 'Unknown'
  lastTransitionTime?: string
  reason?: string
  message?: string
}

export interface ClusterMeshStatus {
  clusterName: string
  conditions?: K8sCondition[]
}

export interface MultiClusterMeshSpec {
  clusterSet: string
  controlPlane?: {
    namespace?: string
  }
  operator?: {
    namespace?: string
    channel?: string
    source?: string
    sourceNamespace?: string
    startingCSV?: string
    installPlanApproval?: 'Automatic' | 'Manual'
  }
  security?: {
    trust?: {
      certManager?: {
        issuerRef: { name: string }
      }
    }
    discovery?: {
      tokenValidity?: string
    }
  }
}

export interface MultiClusterMeshStatus {
  conditions?: K8sCondition[]
  clusterStatus?: ClusterMeshStatus[]
}

export interface MultiClusterMesh extends K8sResourceCommon {
  spec: MultiClusterMeshSpec
  status?: MultiClusterMeshStatus
}
