import { worstConditions } from '../worstConditions'
import { makeEnrichedCP } from '../../__fixtures__/testFactories'

describe('worstConditions', () => {
  it('returns the worst rank among multiple planes', () => {
    const planes = [
      makeEnrichedCP({
        clusterName: 'cluster-a',
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
      makeEnrichedCP({
        clusterName: 'cluster-b',
        status: { conditions: [{ type: 'Ready', status: 'False', reason: 'ReconcileError' }] },
      }),
    ]

    const result = worstConditions(planes)
    expect(result.rank).toBe(3)
    expect(result.conditions).toEqual([{ type: 'Ready', status: 'False', reason: 'ReconcileError' }])
  })

  it('returns rank 1 for empty array', () => {
    const result = worstConditions([])
    expect(result.rank).toBe(1)
    expect(result.conditions).toBeUndefined()
  })

  it('handles all-undefined-status planes', () => {
    const planes = [
      makeEnrichedCP({ clusterName: 'cluster-a', status: undefined }),
      makeEnrichedCP({ clusterName: 'cluster-b', status: undefined }),
    ]

    const result = worstConditions(planes)
    expect(result.rank).toBe(1)
    expect(result.conditions).toBeUndefined()
  })

  it('selects worst from mixed statuses', () => {
    const planes = [
      makeEnrichedCP({
        clusterName: 'cluster-a',
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
      makeEnrichedCP({
        clusterName: 'cluster-b',
        status: { conditions: [{ type: 'Ready', status: 'Unknown' }] },
      }),
      makeEnrichedCP({
        clusterName: 'cluster-c',
        status: undefined,
      }),
    ]

    const result = worstConditions(planes)
    expect(result.rank).toBe(1)
  })

  it('returns green rank when all planes are healthy', () => {
    const planes = [
      makeEnrichedCP({
        clusterName: 'cluster-a',
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
      makeEnrichedCP({
        clusterName: 'cluster-b',
        status: { conditions: [{ type: 'Ready', status: 'True' }] },
      }),
    ]

    const result = worstConditions(planes)
    expect(result.rank).toBe(0)
    expect(result.conditions).toEqual([{ type: 'Ready', status: 'True' }])
  })
})
