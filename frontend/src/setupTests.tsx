import { expect, afterEach, rs } from '@rstest/core'
import { cleanup } from '@testing-library/react'
import * as jestDomMatchers from '@testing-library/jest-dom/matchers'
import type { ReactNode } from 'react'

expect.extend(jestDomMatchers)

afterEach(() => {
  cleanup()
})

// Pass translation keys through as-is, substituting any {{variable}} interpolations.
// This lets tests assert on the English source strings without a real i18next backend.
const makeTFunction = () => (key: string, opts?: Record<string, unknown>): string => {
  if (!opts) return key
  return Object.entries(opts).reduce<string>(
    (str, [k, v]) => str.replace(new RegExp(`\\{\\{${k}\\}\\}`, 'g'), String(v)),
    key,
  )
}

rs.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: makeTFunction(),
    i18n: { language: 'en', changeLanguage: () => Promise.resolve() },
  }),
  // When used with i18nKey + components (no children), render the key as plain text.
  // When used with children (inline content), render the children as-is.
  Trans: ({ children, i18nKey }: { children?: ReactNode; i18nKey?: string }) =>
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
    addListener: rs.fn(),
    removeListener: rs.fn(),
    addEventListener: rs.fn(),
    removeEventListener: rs.fn(),
    dispatchEvent: rs.fn(),
  }),
})

// scrollIntoView is not implemented in jsdom
window.HTMLElement.prototype.scrollIntoView = rs.fn()
