import type { K8sGroupVersionKind, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from './common'

export const multiClusterMeshGroupVersionKind: K8sGroupVersionKind = {
  group: 'mesh.open-cluster-management.io',
  version: 'v1alpha1',
  kind: 'MultiClusterMesh',
}

export interface ClusterMeshStatus {
  clusterName: string
  conditions?: K8sCondition[]
}

export interface TemplateSourceConfig {
  basic?: Record<string, never>
  configMapRef?: { name: string; key?: string }
  git?: { url: string; path: string; ref?: { branch?: string; tag?: string; commit?: string }; secretRef?: { name: string } }
  none?: Record<string, never>
}

export interface MultiClusterMeshSpec {
  clusterSet: string
  controlPlane?: {
    namespace?: string
    templateSource?: TemplateSourceConfig
    version?: string
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
        issuerRef: { name: string; kind?: 'Issuer' | 'ClusterIssuer' }
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
