import * as React from 'react'
import { Label } from '@patternfly/react-core'
import type { K8sCondition } from '../types/multiClusterMesh'

type StatusColor = 'green' | 'red' | 'orange' | 'grey'

function deriveStatus(conditions?: K8sCondition[], conditionType?: string): { label: string; color: StatusColor } {
  if (!conditions || conditions.length === 0) {
    return { label: 'Unknown', color: 'grey' }
  }

  const targetType = conditionType ?? 'Ready'
  const target = conditions.find((c) => c.type === targetType)
  if (target) {
    if (target.status === 'True') {
      return { label: targetType, color: 'green' }
    }
    return { label: target.reason ?? `Not ${targetType}`, color: 'red' }
  }

  const degraded = conditions.find((c) => c.status !== 'True')
  if (degraded) {
    return { label: degraded.reason ?? degraded.type, color: 'orange' }
  }

  return { label: 'Healthy', color: 'green' }
}

export function getStatusRank(conditions?: K8sCondition[], conditionType?: string): number {
  const { color } = deriveStatus(conditions, conditionType)
  if (color === 'green') return 0
  if (color === 'grey') return 1
  if (color === 'orange') return 2
  return 3
}

interface MeshStatusProps {
  conditions?: K8sCondition[]
  conditionType?: string
}

export const MeshStatus: React.FC<MeshStatusProps> = ({ conditions, conditionType }) => {
  const { label, color } = deriveStatus(conditions, conditionType)
  return <Label color={color}>{label}</Label>
}
