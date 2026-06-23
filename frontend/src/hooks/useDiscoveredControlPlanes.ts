import { useFleetSearchPoll, useIsFleetAvailable } from '@stolostron/multicluster-sdk'
import type { Istio } from '../types/istio'
import { istioGroupVersionKind } from '../types/istio'

type FleetIstio = Istio & { cluster?: string }

export function useDiscoveredControlPlanes() {
  const isFleetAvailable = useIsFleetAvailable()

  const [data, loaded, error, refetch] = useFleetSearchPoll<FleetIstio[]>({
    groupVersionKind: istioGroupVersionKind,
    isList: true,
    namespaced: false,
  })

  const results = (data ?? []).filter(
    (r): r is FleetIstio & { cluster: string } => Boolean(r.cluster && r.metadata?.name),
  )

  return { error, isFleetAvailable, loaded, refetch, results } as const
}
