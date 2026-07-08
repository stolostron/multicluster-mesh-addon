import type { K8sCondition } from '../types/common'
import type { EnrichedControlPlane } from '../types/istio'
import { getStatusRank } from '../components/MeshStatus'

export function worstConditions(planes: EnrichedControlPlane[]): {
  conditions: K8sCondition[] | undefined
  rank: number
} {
  let worstRank = -1
  let worst: K8sCondition[] | undefined
  for (const cp of planes) {
    const rank = getStatusRank(cp.status?.conditions)
    if (rank > worstRank) {
      worstRank = rank
      worst = cp.status?.conditions
    }
  }
  return { conditions: worst, rank: worstRank === -1 ? 1 : worstRank }
}
