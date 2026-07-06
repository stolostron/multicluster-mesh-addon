import { clusterDetailLink, clusterSetDetailLink } from '../linkUtils'

describe('clusterDetailLink', () => {
  it('produces correct URL for a simple cluster name', () => {
    expect(clusterDetailLink('cluster-a')).toBe(
      '/multicloud/infrastructure/clusters/details/cluster-a/cluster-a/overview',
    )
  })

  it('encodes special characters with encodeURIComponent', () => {
    const name = 'cluster/with spaces&special'
    const encoded = encodeURIComponent(name)
    expect(clusterDetailLink(name)).toBe(
      `/multicloud/infrastructure/clusters/details/${encoded}/${encoded}/overview`,
    )
  })
})

describe('clusterSetDetailLink', () => {
  it('produces correct URL for a simple cluster set name', () => {
    expect(clusterSetDetailLink('global')).toBe(
      '/multicloud/infrastructure/clusters/sets/details/global/overview',
    )
  })

  it('encodes special characters with encodeURIComponent', () => {
    const name = 'set/with spaces&special'
    const encoded = encodeURIComponent(name)
    expect(clusterSetDetailLink(name)).toBe(
      `/multicloud/infrastructure/clusters/sets/details/${encoded}/overview`,
    )
  })
})
