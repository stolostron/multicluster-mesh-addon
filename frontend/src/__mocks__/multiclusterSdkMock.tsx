import { rs } from '@rstest/core'

export const useFleetSearchPoll = rs.fn(() => [undefined, false, undefined, rs.fn()])

export const fleetK8sGet = rs.fn(() => Promise.resolve({}))

export const useIsFleetAvailable = rs.fn(() => true)
