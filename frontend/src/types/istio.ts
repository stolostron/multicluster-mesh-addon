import type { K8sGroupVersionKind, K8sModel, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from './common'

export const istioGroupVersionKind: K8sGroupVersionKind = {
  group: 'sailoperator.io',
  kind: 'Istio',
  version: 'v1',
}

export const istioModel: K8sModel = {
  abbr: 'ISTIO',
  apiGroup: 'sailoperator.io',
  apiVersion: 'v1',
  kind: 'Istio',
  label: 'Istio',
  labelPlural: 'Istios',
  namespaced: false,
  plural: 'istios',
}

export interface IstioSpec {
  namespace: string
  version?: string
  values?: {
    global?: {
      meshID?: string
      multiCluster?: { clusterName?: string }
      network?: string
    }
  }
}

export interface IstioStatus {
  conditions?: K8sCondition[]
}

export interface Istio extends K8sResourceCommon {
  spec: IstioSpec
  status?: IstioStatus
}

// useListPageFilter from the Console SDK accesses metadata.name for its
// built-in name filter, so EnrichedControlPlane must include metadata.
export interface EnrichedControlPlane {
  metadata: {
    name: string
    creationTimestamp?: string
    labels?: Record<string, string>
  }
  clusterName: string
  controlPlaneNamespace?: string
  managedBy?: { name: string; namespace: string }
  meshID?: string
  multiClusterName?: string
  network?: string
  status?: IstioStatus
  version?: string
}
