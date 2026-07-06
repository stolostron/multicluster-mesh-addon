import type { FC, ReactNode } from 'react'
import { Label } from '@patternfly/react-core'
import type { K8sCondition } from '../types/common'
import { useMeshTranslation } from '../utils/i18nUtils'

export type StatusColor = 'green' | 'red' | 'orange' | 'grey'

// Maps K8s condition reason codes to user-friendly English strings (also the i18n keys).
const friendlyReasons: Record<string, string> = {
  ClustersNotReady: 'Clusters Not Ready',
  ManifestWorkCreated: 'Installing',
  MissingProductClaim: 'Missing Product Claim',
  NamespaceConflict: 'Namespace Conflict',
  OperatorConfigConflict: 'Operator Config Conflict',
  ReconcileError: 'Reconcile Error',
}

export function deriveStatus(conditions?: K8sCondition[], conditionType?: string): { label: string; color: StatusColor } {
  if (!conditions || conditions.length === 0) {
    return { label: 'Unknown', color: 'grey' }
  }

  const targetType = conditionType ?? 'Ready'
  const target = conditions.find((c) => c.type === targetType)
  if (target) {
    if (target.status === 'True') {
      if (targetType === 'Ready') {
        const hasDegradedSecondary = conditions.some(
          (c) => c.type !== targetType && c.status === 'False',
        )
        if (hasDegradedSecondary) {
          return { label: 'Degraded', color: 'orange' }
        }
      }
      return { label: targetType, color: 'green' }
    }
    if (target.status === 'Unknown') {
      return { label: 'Unknown', color: 'grey' }
    }
    const reason = target.reason ?? `Not ${targetType}`
    return { label: friendlyReasons[reason] ?? reason, color: 'red' }
  }

  const degraded = conditions.find((c) => c.status !== 'True')
  if (degraded) {
    return { label: degraded.reason ?? degraded.type, color: 'orange' }
  }

  return { label: 'Healthy', color: 'green' }
}

/** Returns a numeric rank for sorting: 0 (green/healthy) through 3 (red/degraded). */
export function getStatusRank(conditions?: K8sCondition[], conditionType?: string): number {
  const { color } = deriveStatus(conditions, conditionType)
  if (color === 'green') return 0
  if (color === 'grey') return 1
  if (color === 'orange') return 2
  return 3
}

export function statusIcon(status: string): ReactNode {
  const color = status === 'True' ? 'green' : status === 'Unknown' ? 'grey' : 'red'
  return <Label color={color} isCompact>{status}</Label>
}

interface MeshStatusProps {
  conditions?: K8sCondition[]
  conditionType?: string
  isCompact?: boolean
}

/** Renders a colored PatternFly Label reflecting the status of a K8s condition (default: "Ready"). */
export const MeshStatus: FC<MeshStatusProps> = ({ conditions, conditionType, isCompact }) => {
  const { t } = useMeshTranslation()
  const { label, color } = deriveStatus(conditions, conditionType)
  return <Label color={color} isCompact={isCompact}>{t(label)}</Label>
}
