export const useFleetSearchPoll = jest.fn(() => [undefined, false, undefined, jest.fn()])

export const fleetK8sGet = jest.fn(() => Promise.resolve({}))

export const useIsFleetAvailable = jest.fn(() => true)
