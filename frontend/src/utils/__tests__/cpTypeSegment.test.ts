import { cpTypeSegment, CP_TYPES } from '../cpTypeSegment'

describe('cpTypeSegment', () => {
  it('returns managed when managedBy is truthy', () => {
    expect(cpTypeSegment({ managedBy: { name: 'mesh-a', namespace: 'ns' }, meshID: 'mesh1' })).toBe('managed')
  })

  it('returns discovered when meshID is truthy but no managedBy', () => {
    expect(cpTypeSegment({ meshID: 'mesh1' })).toBe('discovered')
  })

  it('returns standalone when neither managedBy nor meshID', () => {
    expect(cpTypeSegment({})).toBe('standalone')
  })

  it('returns standalone when meshID is undefined and managedBy is undefined', () => {
    expect(cpTypeSegment({ managedBy: undefined, meshID: undefined })).toBe('standalone')
  })
})

describe('CP_TYPES', () => {
  it('contains all three segment values', () => {
    expect(CP_TYPES).toContain('managed')
    expect(CP_TYPES).toContain('discovered')
    expect(CP_TYPES).toContain('standalone')
    expect(CP_TYPES).toHaveLength(3)
  })
})
