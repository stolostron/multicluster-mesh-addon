export function cpTypeSegment(cp: { managedBy?: unknown; meshID?: string }): string {
  if (cp.managedBy) return 'managed'
  if (cp.meshID) return 'discovered'
  return 'standalone'
}
