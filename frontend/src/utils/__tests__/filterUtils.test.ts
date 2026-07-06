import { fuzzyCaseInsensitive } from '../filterUtils'

describe('fuzzyCaseInsensitive', () => {
  it('returns true when filter is undefined', () => {
    expect(fuzzyCaseInsensitive(undefined, 'anything')).toBe(true)
  })

  it('returns true when filter is empty string', () => {
    expect(fuzzyCaseInsensitive('', 'anything')).toBe(true)
  })

  it('matches case-insensitive substring', () => {
    expect(fuzzyCaseInsensitive('mesh', 'my-Mesh-name')).toBe(true)
    expect(fuzzyCaseInsensitive('MESH', 'my-mesh-name')).toBe(true)
  })

  it('returns false when no match', () => {
    expect(fuzzyCaseInsensitive('xyz', 'my-mesh-name')).toBe(false)
  })

  it('matches exact value', () => {
    expect(fuzzyCaseInsensitive('default', 'default')).toBe(true)
  })

  it('handles empty value', () => {
    expect(fuzzyCaseInsensitive('test', '')).toBe(false)
  })
})
