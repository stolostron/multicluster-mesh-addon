---
format_version: 1
---

# Style Guide — multicluster-mesh-addon/frontend

## Enforced Conventions

### Naming

- **Component files:** PascalCase (`ServiceMeshPage.tsx`, `MeshStatus.tsx`)
- **Hook files:** camelCase with `use` prefix (`useMultiClusterMeshes.ts`)
- **Type files:** camelCase (`multiClusterMesh.ts`, `istio.ts`)
- **Utility files:** camelCase (`i18nUtils.ts`)
- **React components:** PascalCase, typed as `React.FC` or `React.FC<Props>`
- **Props interfaces:** `{ComponentName}Props` (e.g., `MeshStatusProps`)
- **Types/Interfaces:** PascalCase (`MultiClusterMesh`, `EnrichedControlPlane`)
- **Module-level constants:** UPPER_SNAKE_CASE (`CACHE_TTL_MS`, `CONCURRENCY_LIMIT`)
- **Local functions/variables:** camelCase

### Formatting

- No semicolons (ASI style)
- Single quotes for string literals
- Trailing commas on multi-line arrays, objects, and parameter lists
- No ESLint or Prettier configured — conventions are enforced by review

### AGENTS.md Rules (reference — see `AGENTS.md` for authoritative source)

- No comments unless the WHY is non-obvious
- PatternFly for all UI components and styling
- All user-facing strings via `useMeshTranslation()` with `{{variable}}` interpolation
- Import router from `react-router-dom-v5-compat`
- Sign commits with `-s`; never amend existing commits
- Page-level components use `export default` (referenced by `$codeRef` in `console-extensions.ts`)
- Reusable components, hooks, types, and utilities use named exports
- GroupVersionKind constants co-located with TypeScript interfaces in type files

### Loading/Error/Empty Pattern

Components that fetch data must handle three states:
1. **Error** — render `EmptyState` with error message
2. **Loading** — render `Spinner`
3. **No data** — render `EmptyState` with descriptive message

### Null Safety

Use optional chaining (`?.`) and nullish coalescing (`??`) for all Kubernetes object property access. K8s resources have many optional fields.

### URL Encoding

All dynamic route segments must use `encodeURIComponent()` when building links, and `decodeURIComponent()` when reading params on the target page. Example:
```tsx
<Link to={`/fleet-mesh/meshes/discovered/${encodeURIComponent(meshID)}`}>
```

### Discriminated Union Types for Unified Lists

When a list page displays items from multiple data sources (e.g., managed + discovered meshes), use a discriminated union type with:
- A `kind` field as the discriminator (e.g., `'managed' | 'discovered'`)
- Flattened fields for column sorting and display (e.g., `statusRank`, `clusterCount`) — don't reach into nested source-specific objects in render/sort code
- A pre-computed `detailLink` field with the full navigation URL — compute in the hook, not in the row component
- Source-specific optional fields (e.g., `mcm?`, `controlPlanes?`) for detail access when needed

See `FleetMeshItem` in `src/types/fleetMesh.ts` for the reference implementation.

### Module-Level State

Use module-level variables (instead of `useRef`) when state must survive component unmounts — e.g., caches that should stay warm across page navigation. Requirements:
- The module-level variable must be bounded (TTL eviction, stale-key cleanup, or similar)
- Expose a `__reset*()` function (double-underscore prefix) for test cleanup — this signals test-only use
- Add a code comment explaining why module-level scope is needed and why stale-key cleanup is safe

See `enrichmentCache` in `src/hooks/useEnrichedControlPlanes.ts` for the reference implementation.

### Two-Phase Loading

When a page depends on multiple async sources with different latencies, render the fast source immediately and update when the slow source completes. Don't block the entire UI on the slowest source. Example:
- Show mesh donut chart with MCM-only counts immediately when `mcmsLoaded` is true
- Update to include discovered meshes when `enrichmentLoaded` becomes true

Use granular loading/error states in hook returns (e.g., `mcmsLoaded`, `mcmsError`, `enrichmentLoaded`, `enrichmentError`) so consumers can render each section independently.

## Documented Conventions (not enforced, for reference)

### Import Ordering

Four groups, in order:
1. React (`import * as React from 'react'` — namespace import)
2. Third-party/framework (`react-router-dom-v5-compat`, `react-i18next`)
3. SDK/platform (`@openshift-console/dynamic-plugin-sdk`, `@patternfly/react-core`)
4. Local project imports (relative `../` paths)

Separate `import type { ... }` from value imports.

### Code Structure

- All functional components — no class components
- Helper functions as plain `function` declarations above the component
- No CSS-in-JS or CSS modules — PatternFly CSS classes + inline `style={{}}` for one-offs
- No external state management — local state via `useState`, server state via SDK hooks
- Custom hooks return `[data, loaded, error]` tuples matching Console SDK conventions

## Changelog

| Date | Change | Trigger |
|------|--------|---------|
| 2026-07-01 | Add URL encoding, discriminated unions, module-level state, two-phase loading conventions | /code-reviewer:setup refresh |
| 2026-06-23 | Initial generation | /code-reviewer:setup |
