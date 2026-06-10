import * as React from 'react'
import { Label } from '@patternfly/react-core'
import type { K8sCondition } from '../types/multiClusterMesh'

type StatusColor = 'green' | 'red' | 'orange' | 'grey'

function deriveStatus(conditions?: K8sCondition[]): { label: string; color: StatusColor } {
  if (!conditions || conditions.length === 0) {
    return { label: 'Unknown', color: 'grey' }
  }

  const ready = conditions.find((c) => c.type === 'Ready')
  if (ready) {
    if (ready.status === 'True') {
      return { label: 'Ready', color: 'green' }
    }
    return { label: ready.reason ?? 'Not Ready', color: 'red' }
  }

  const degraded = conditions.find((c) => c.status !== 'True')
  if (degraded) {
    return { label: degraded.reason ?? degraded.type, color: 'orange' }
  }

  return { label: 'Healthy', color: 'green' }
}

export const MeshStatus: React.FC<{ conditions?: K8sCondition[] }> = ({ conditions }) => {
  const { label, color } = deriveStatus(conditions)
  return <Label color={color}>{label}</Label>
}
