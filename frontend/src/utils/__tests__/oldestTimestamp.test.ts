import { oldestTimestamp } from '../oldestTimestamp'
import { makeEnrichedCP } from '../../__fixtures__/testFactories'

describe('oldestTimestamp', () => {
  it('finds the oldest timestamp among multiple planes', () => {
    const planes = [
      makeEnrichedCP({ metadata: { name: 'cp1', creationTimestamp: '2026-06-25T12:00:00Z' } }),
      makeEnrichedCP({ metadata: { name: 'cp2', creationTimestamp: '2026-06-20T08:00:00Z' } }),
      makeEnrichedCP({ metadata: { name: 'cp3', creationTimestamp: '2026-06-22T10:00:00Z' } }),
    ]
    expect(oldestTimestamp(planes)).toBe('2026-06-20T08:00:00Z')
  })

  it('returns undefined for empty array', () => {
    expect(oldestTimestamp([])).toBeUndefined()
  })

  it('handles planes with undefined timestamps', () => {
    const planes = [
      makeEnrichedCP({ metadata: { name: 'cp1' } }),
      makeEnrichedCP({ metadata: { name: 'cp2', creationTimestamp: '2026-06-22T12:00:00Z' } }),
      makeEnrichedCP({ metadata: { name: 'cp3' } }),
    ]
    expect(oldestTimestamp(planes)).toBe('2026-06-22T12:00:00Z')
  })

  it('returns undefined when all timestamps are undefined', () => {
    const planes = [
      makeEnrichedCP({ metadata: { name: 'cp1' } }),
      makeEnrichedCP({ metadata: { name: 'cp2' } }),
    ]
    expect(oldestTimestamp(planes)).toBeUndefined()
  })

  it('returns the single timestamp when only one plane exists', () => {
    const planes = [
      makeEnrichedCP({ metadata: { name: 'cp1', creationTimestamp: '2026-06-22T12:00:00Z' } }),
    ]
    expect(oldestTimestamp(planes)).toBe('2026-06-22T12:00:00Z')
  })
})
