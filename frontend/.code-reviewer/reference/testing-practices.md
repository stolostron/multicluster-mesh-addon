---
format_version: 1
---

# Testing Practices — multicluster-mesh-addon/frontend

## Framework & Configuration

- **Rstest** (`@rstest/core`) with SWC transpilation, `jsdom` environment
- **@testing-library/react** for component rendering and queries
- **@testing-library/user-event** for user interaction simulation
- **@testing-library/jest-dom** for DOM assertions (matchers extended via `expect.extend` in setup)
- Config: `rstest.config.ts` with `tools.swc` for automatic JSX runtime
- Global setup: `src/setupTests.tsx` (jest-dom matchers, cleanup, react-i18next mock, ResizeObserver polyfill, matchMedia polyfill)

## Enforced Conventions

### File Naming & Location

- Test files named `<SourceName>.test.tsx` (or `.test.ts` for non-JSX such as hooks)
- Located in `__tests__/` directories co-located with the source they test:
  - `src/components/__tests__/` for component tests
  - `src/hooks/__tests__/` for hook tests

### Test Structure

- Use `describe/it` nesting pattern
- Use `test.each` for parameterized/table-driven tests
- Every test file must include `afterEach(() => rstest.clearAllMocks())`
- When using fake timers, include `rstest.useRealTimers()` in `afterEach`

### Assertions

- Prefer accessible queries over test IDs: `getByText`, `getByRole`, `getByLabelText` before `getByTestId`
- Use `@testing-library/jest-dom` matchers: `toBeInTheDocument()`, `toHaveAttribute()`
- Use `queryByText` (not `getByText`) for absence checks: `expect(screen.queryByText('...')).not.toBeInTheDocument()`
- No snapshot testing

### Mocking

- **Global module mocks** via `resolve.alias` in `rstest.config.ts`, implemented in `src/__mocks__/`:
  - `consoleSdkMock.tsx` — `@openshift-console/dynamic-plugin-sdk`
  - `multiclusterSdkMock.tsx` — `@stolostron/multicluster-sdk`
  - `routerMock.tsx` — `react-router-dom-v5-compat`
- Mock files are regular modules (not test files) — they must `import { rs } from '@rstest/core'` and use `rs.fn()`
- **Per-test mocks** via `rstest.mock('../../hooks/...', { mock: true })` + `rstest.mocked(hook).mockReturnValue(...)`
- **Factory mocks** via `rstest.mock('...', () => ({ ... }))` for replacing a module with a custom implementation
- Override return values with `mockReturnValue()`, `mockResolvedValue()`, `mockRejectedValue()`, or `mockImplementation()` for conditional returns

### Test Data

- Use `make*()` factory functions to build Kubernetes resource fixtures (e.g., `makeMesh()`, `makeCondition()`, `makeSearchResult()`)
- Factory functions are defined at the top of each test file

### Hook Testing

- Use `renderHook` from `@testing-library/react`
- Use `waitFor` for async state updates
- Use `rstest.useFakeTimers()` + `act(async () => { await rstest.runAllTimersAsync() })` for timer-based behavior
- Use `rerender()` from `renderHook` result to test re-render scenarios

### User Interaction

- Use `userEvent.setup()` — not `fireEvent`
- All interaction calls must be awaited: `await user.click(...)`, `await user.type(...)`

## Changelog

| Date | Change | Trigger |
|------|--------|---------|
| 2026-06-23 | Migrate from Jest to Rstest | Rstack toolchain alignment |
| 2026-06-23 | Initial generation | /code-reviewer:setup |
