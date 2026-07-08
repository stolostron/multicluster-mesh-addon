import { categorizeCp } from '../istio'
import type { EnrichedControlPlane } from '../istio'

function makeCp(conditions?: { type: string; status: string }[]): EnrichedControlPlane {
  return {
    apiVersion: 'sailoperator.io/v1',
    kind: 'Istio',
    metadata: { name: 'default', creationTimestamp: '2026-01-01T00:00:00Z' },
    clusterName: 'cluster-a',
    controlPlaneNamespace: 'istio-system',
    status: conditions ? { conditions } : undefined,
  } as EnrichedControlPlane
}

describe('categorizeCp', () => {
  it('returns ready when Ready condition is True', () => {
    expect(categorizeCp(makeCp([{ type: 'Ready', status: 'True' }]))).toBe('ready')
  })

  it('returns notReady when Ready condition is False', () => {
    expect(categorizeCp(makeCp([{ type: 'Ready', status: 'False' }]))).toBe('notReady')
  })

  it('returns unknown when Ready condition is Unknown', () => {
    expect(categorizeCp(makeCp([{ type: 'Ready', status: 'Unknown' }]))).toBe('unknown')
  })

  it('returns unknown when no Ready condition exists', () => {
    expect(categorizeCp(makeCp([{ type: 'Reconciled', status: 'True' }]))).toBe('unknown')
  })

  it('returns unknown when conditions array is empty', () => {
    expect(categorizeCp(makeCp([]))).toBe('unknown')
  })

  it('returns unknown when status is undefined', () => {
    expect(categorizeCp(makeCp())).toBe('unknown')
  })
})
