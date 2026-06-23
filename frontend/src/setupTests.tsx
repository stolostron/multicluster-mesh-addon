import '@testing-library/jest-dom'

// Pass translation keys through as-is, substituting any {{variable}} interpolations.
// This lets tests assert on the English source strings without a real i18next backend.
const makeTFunction = () => (key: string, opts?: Record<string, unknown>): string => {
  if (!opts) return key
  return Object.entries(opts).reduce<string>(
    (str, [k, v]) => str.replace(new RegExp(`\\{\\{${k}\\}\\}`, 'g'), String(v)),
    key,
  )
}

jest.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: makeTFunction(),
    i18n: { language: 'en', changeLanguage: () => Promise.resolve() },
  }),
  // When used with i18nKey + components (no children), render the key as plain text.
  // When used with children (inline content), render the children as-is.
  Trans: ({ children, i18nKey }: { children?: React.ReactNode; i18nKey?: string }) =>
    children ? <>{children}</> : <>{i18nKey}</>,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// PatternFly and some Console SDK components use ResizeObserver
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
global.ResizeObserver = ResizeObserverStub

// PatternFly uses matchMedia for responsive breakpoints
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: jest.fn(),
    removeListener: jest.fn(),
    addEventListener: jest.fn(),
    removeEventListener: jest.fn(),
    dispatchEvent: jest.fn(),
  }),
})

// scrollIntoView is not implemented in jsdom
window.HTMLElement.prototype.scrollIntoView = jest.fn()
