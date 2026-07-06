export function clusterDetailLink(name: string): string {
  return `/multicloud/infrastructure/clusters/details/${encodeURIComponent(name)}/${encodeURIComponent(name)}/overview`
}

export function clusterSetDetailLink(name: string): string {
  return `/multicloud/infrastructure/clusters/sets/details/${encodeURIComponent(name)}/overview`
}
