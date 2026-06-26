import type { K8sGroupVersionKind, K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk'
import type { K8sCondition } from './common'

export const certificateGroupVersionKind: K8sGroupVersionKind = {
  group: 'cert-manager.io',
  version: 'v1',
  kind: 'Certificate',
}

export interface CertificateStatus {
  conditions?: K8sCondition[]
  notAfter?: string
  renewalTime?: string
}

export interface Certificate extends K8sResourceCommon {
  status?: CertificateStatus
}
