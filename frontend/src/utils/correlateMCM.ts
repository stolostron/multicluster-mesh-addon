import type { MultiClusterMesh } from '../types/multiClusterMesh'

// Determines whether an Istio control plane is managed by a MultiClusterMesh.
// A MultiClusterMesh declares intent to manage Istio on a set of clusters; the
// controller creates the actual Istio CRs. This function matches a discovered
// Istio CR back to its managing MCM by checking two things:
//   1. The cluster running this control plane appears in the MCM's status.clusterStatus[]
//   2. The control plane namespace (Istio.spec.namespace) matches the MCM's
//      spec.controlPlane.namespace (default: istio-system)
// If both match, the control plane is considered managed by that MCM.
// Note: this is a best-effort correlation — an independently created Istio CR
// that happens to be on the same cluster+namespace as an MCM will also match.
export function findManagingMCM(
  clusterName: string,
  controlPlaneNamespace: string | undefined,
  mcms: MultiClusterMesh[],
): { name: string; namespace: string } | undefined {
  const cpNs = controlPlaneNamespace ?? 'istio-system'
  for (const mcm of mcms) {
    const mcmNs = mcm.spec.controlPlane?.namespace ?? 'istio-system'
    if (mcmNs !== cpNs) continue
    const match = mcm.status?.clusterStatus?.find((cs) => cs.clusterName === clusterName)
    if (match) {
      return { name: mcm.metadata?.name ?? '', namespace: mcm.metadata?.namespace ?? '' }
    }
  }
  return undefined
}
