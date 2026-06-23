import type { K8sGroupVersionKind, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from './common'

export const manifestWorkGroupVersionKind: K8sGroupVersionKind = {
  group: 'work.open-cluster-management.io',
  version: 'v1',
  kind: 'ManifestWork',
}

export interface ManifestWorkStatus {
  conditions?: K8sCondition[]
}

export interface ManifestWork extends K8sResourceCommon {
  status?: ManifestWorkStatus
}
