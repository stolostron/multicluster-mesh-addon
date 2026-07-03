export const CP_TYPES = ['managed', 'discovered', 'standalone'] as const
export type CpType = typeof CP_TYPES[number]

export function cpTypeSegment(cp: { managedBy?: unknown; meshID?: string }): CpType {
  if (cp.managedBy) return 'managed'
  if (cp.meshID) return 'discovered'
  return 'standalone'
}
