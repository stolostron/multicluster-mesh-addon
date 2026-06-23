---
format_version: 1
---

# Testing Practices — multicluster-mesh-addon/frontend

## Framework & Configuration

- **Jest 30** with `ts-jest` transformer, `jsdom` environment
- **@testing-library/react** for component rendering and queries
- **@testing-library/user-event** for user interaction simulation
- **@testing-library/jest-dom** for DOM assertions
- Config: `jest.config.cjs` with `tsconfig.jest.json`
- Global setup: `src/setupTests.tsx` (react-i18next mock, ResizeObserver polyfill, matchMedia polyfill)

## Enforced Conventions

### File Naming & Location

- Test files named `<SourceName>.test.tsx` (or `.test.ts` for non-JSX such as hooks)
- Located in `__tests__/` directories co-located with the source they test:
  - `src/components/__tests__/` for component tests
  - `src/hooks/__tests__/` for hook tests

### Test Structure

- Use `describe/it` nesting pattern
- Use `test.each` for parameterized/table-driven tests
- Every test file must include `afterEach(() => jest.clearAllMocks())`
- When using fake timers, include `jest.useRealTimers()` in `afterEach`

### Assertions

- Prefer accessible queries over test IDs: `getByText`, `getByRole`, `getByLabelText` before `getByTestId`
- Use `@testing-library/jest-dom` matchers: `toBeInTheDocument()`, `toHaveAttribute()`
- Use `queryByText` (not `getByText`) for absence checks: `expect(screen.queryByText('...')).not.toBeInTheDocument()`
- No snapshot testing

### Mocking

- **Global module mocks** via `moduleNameMapper` in `jest.config.cjs`, implemented in `src/__mocks__/`:
  - `consoleSdkMock.tsx` — `@openshift-console/dynamic-plugin-sdk`
  - `multiclusterSdkMock.tsx` — `@stolostron/multicluster-sdk`
  - `routerMock.tsx` — `react-router-dom-v5-compat`
  - `fileMock.ts` — static assets (svg, png, jpg, gif)
  - `styleMock.ts` — CSS files
- **Per-test mocks** via `jest.mock('../../hooks/...')` + type-cast: `const mockHook = hook as jest.Mock`
- Override return values with `mockReturnValue()`, `mockResolvedValue()`, `mockRejectedValue()`, or `mockImplementation()` for conditional returns

### Test Data

- Use `make*()` factory functions to build Kubernetes resource fixtures (e.g., `makeMesh()`, `makeCondition()`, `makeSearchResult()`)
- Factory functions are defined at the top of each test file

### Hook Testing

- Use `renderHook` from `@testing-library/react`
- Use `waitFor` for async state updates
- Use `jest.useFakeTimers()` + `act(async () => { await jest.runAllTimersAsync() })` for timer-based behavior
- Use `rerender()` from `renderHook` result to test re-render scenarios

### User Interaction

- Use `userEvent.setup()` — not `fireEvent`
- All interaction calls must be awaited: `await user.click(...)`, `await user.type(...)`

## Changelog

| Date | Change | Trigger |
|------|--------|---------|
| 2026-06-23 | Initial generation | /code-reviewer:setup |
