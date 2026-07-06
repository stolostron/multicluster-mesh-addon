import { useMemo } from 'react'
import { useManagedClusters } from './useManagedClusters'
import type { ManagedCluster } from '../types/managedCluster'

export function useManagedClusterMap(): [Map<string, ManagedCluster>, boolean, unknown] {
  const [managedClusters, loaded, error] = useManagedClusters()

  const map = useMemo(() => {
    const m = new Map<string, ManagedCluster>()
    for (const mc of managedClusters ?? []) {
      if (mc.metadata?.name) m.set(mc.metadata.name, mc)
    }
    return m
  }, [managedClusters])

  return [map, loaded ?? false, error]
}
