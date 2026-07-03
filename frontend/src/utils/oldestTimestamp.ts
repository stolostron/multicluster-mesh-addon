import type { EnrichedControlPlane } from '../types/istio'

export function oldestTimestamp(planes: EnrichedControlPlane[]): string | undefined {
  let oldest: string | undefined
  for (const cp of planes) {
    const ts = cp.metadata.creationTimestamp
    if (ts && (!oldest || ts < oldest)) oldest = ts
  }
  return oldest
}
