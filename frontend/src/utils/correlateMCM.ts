import type { MultiClusterMesh } from '../types/multiClusterMesh'

export interface McmInfo {
  name: string
  namespace: string
}

// Correlation determines whether an Istio control plane is managed by a
// MultiClusterMesh. A MultiClusterMesh declares intent to manage Istio on a
// set of clusters; the controller creates the actual Istio CRs. We match a
// discovered Istio CR back to its managing MCM by checking two things:
//   1. The cluster running this control plane appears in the MCM's status.clusterStatus[]
//   2. The control plane namespace (Istio.spec.namespace) matches the MCM's
//      spec.controlPlane.namespace (default: istio-system)
// If both match, the control plane is considered managed by that MCM.
// Note: this is a best-effort correlation — an independently created Istio CR
// that happens to be on the same cluster+namespace as an MCM will also match.

/**
 * Builds a lookup index from MultiClusterMesh objects for O(1) correlation.
 * Key: "clusterName/controlPlaneNamespace" -> MCM identity.
 * Callers should memoize the result (e.g. via useMemo keyed on the MCMs array)
 * to avoid rebuilding the index on every render.
 */
export function buildMcmIndex(mcms: MultiClusterMesh[]): Map<string, McmInfo> {
  const map = new Map<string, McmInfo>()
  for (const mcm of mcms) {
    const cpNs = mcm.spec.controlPlane?.namespace ?? 'istio-system'
    for (const cs of mcm.status?.clusterStatus ?? []) {
      const key = `${cs.clusterName}/${cpNs}`
      if (map.has(key)) {
        console.warn(`MCM index collision: [${key}] claimed by both [${map.get(key)!.name}] and [${mcm.metadata?.name}]`)
      }
      map.set(key, {
        name: mcm.metadata?.name ?? '',
        namespace: mcm.metadata?.namespace ?? '',
      })
    }
  }
  return map
}

/** Looks up a control plane's managing MCM from a pre-built index. */
export function lookupMcm(
  index: Map<string, McmInfo>,
  clusterName: string,
  controlPlaneNamespace: string | undefined,
): McmInfo | undefined {
  return index.get(`${clusterName}/${controlPlaneNamespace ?? 'istio-system'}`)
}
