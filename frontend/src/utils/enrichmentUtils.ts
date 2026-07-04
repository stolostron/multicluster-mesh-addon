import type { FleetIstio, Istio, EnrichedControlPlane } from '../types/istio'

/** Maps a discovered Istio CR + its cached enrichment data into an EnrichedControlPlane projection. */
export function toEnrichedControlPlane(r: FleetIstio, cached: Istio | undefined): EnrichedControlPlane {
  const spec = cached?.spec
  return {
    metadata: {
      name: r.metadata?.name ?? '',
      creationTimestamp: r.metadata?.creationTimestamp,
      labels: r.metadata?.labels as Record<string, string> | undefined,
    },
    clusterName: r.cluster,
    controlPlaneNamespace: spec?.namespace,
    meshID: spec?.values?.global?.meshID,
    network: spec?.values?.global?.network,
    status: cached?.status,
    version: spec?.version,
  }
}
