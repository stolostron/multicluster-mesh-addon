# Agent Instructions — Frontend

## Project Overview

OpenShift ConsolePlugin that adds a "Fleet Service Mesh" perspective to the OpenShift Console. Provides fleet-wide visibility into `MultiClusterMesh` resources managed by the [multicluster-mesh-addon](https://github.com/stolostron/multicluster-mesh-addon) backend controller.

Related links:
- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [ROADMAP.md](ROADMAP.md) — Current status and future plans
- [SPIKE.md](SPIKE.md) — Original spike research and architecture notes
- [DEV-INSTALL.md](DEV-INSTALL.md) — End-to-end dev setup on CRC

## Project Structure

- `console-extensions.ts` — Declares perspective, nav items, page routes
- `console-plugin-metadata.ts` — Plugin name, version, exposed modules
- `webpack.config.ts` — Build config (ConsoleRemotePlugin + CopyPlugin for locales)
- `jest.config.cjs` / `tsconfig.jest.json` — Test runner config
- `deploy/` — Kubernetes manifests (ConsolePlugin CR, Deployment/Service, nginx config)
- `src/types/` — TypeScript types for K8s resources (MultiClusterMesh, Certificate, ManifestWork)
- `src/components/` — React page and card components
- `src/hooks/` — Data fetching hooks
- `src/utils/i18nUtils.ts` — i18n hook (`useMeshTranslation`) and namespace constant
- `src/locales/en/plugin__ossm-acm.json` — English translation strings
- `src/__mocks__/` — Jest mocks for Console SDK, react-router, and static assets
- `src/setupTests.tsx` — Jest global setup (jest-dom, i18n mock, jsdom stubs)

## Build & Deploy

Requires Node.js 20+.

Run `make help` to see all available targets.

**Dev workflow** (ConfigMap + stock nginx, no image build needed):
- `make dev-build` — `npm install && npm run build` (compiles to `dist/`)
- `make dev-deploy` — Idempotent deploy to OpenShift (configmaps, deployment, consoleplugin, console restart)
- `make dev-teardown` — Remove dev plugin from cluster
- `make dev-build dev-deploy` — The standard dev workflow for iterating on changes

**Production workflow** (baked container image):
- `make prod-build` — Build container image via Dockerfile (npm ci + webpack inside the image)
- `make prod-push` — Push image to registry (default: auto-detected OpenShift internal registry)
- `make prod-deploy` — Push + deploy using the baked image (no ConfigMaps)
- `make prod-teardown` — Remove prod plugin from cluster

Override `IMG` to push to an external registry: `make prod-build IMG=quay.io/myorg/ossm-acm-console-plugin:v1`

## Key Architecture Decisions

### OpenShift Console Plugin SDK

This is a dynamic plugin loaded by the OpenShift Console at runtime via webpack module federation. Shared modules (React, PatternFly, react-router) are provided by the Console at runtime, not bundled.

- Import router hooks from `react-router-dom-v5-compat` (not `react-router` or `react-router-dom`). This is the package the Console shares at runtime.
- `useK8sWatchResource` from `@openshift-console/dynamic-plugin-sdk` is the primary data fetching mechanism. Pass `null` to skip a watch.
- `validateSharedModules: false` and `validateExtensionIntegrity: false` in webpack config are intentional — version mismatches between build-time and runtime Console are resolved at runtime.

### Perspective icon export pattern

The `console.perspective` icon CodeRef expects `LazyComponent = { default: React.ComponentType }`. The icon module must export `{ default: Component }` as its default export, NOT the component directly. Getting this wrong causes a React crash (error #306).

```typescript
// CORRECT
export default { default: PerspectiveIcon }

// WRONG — causes crash
export default PerspectiveIcon
```

### Route registration order

The detail route (`/service-mesh/:ns/:name`) must be registered BEFORE the list route (`/service-mesh`) in `console-extensions.ts`. React Router v5 matches the first route whose path prefix matches.

### Data sources

All data comes from the hub cluster Kubernetes API via `useK8sWatchResource`:

| Resource | Where it lives | What it shows |
|----------|---------------|---------------|
| `MultiClusterMesh` | Mesh namespace (e.g. `mesh-system`) | Mesh spec, status, per-cluster conditions |
| `Certificate` (cert-manager.io/v1) | Mesh namespace | Per-cluster cert status, expiry, renewal |
| `ManifestWork` (work.open-cluster-management.io/v1) | Per-cluster namespace (e.g. `local-cluster`) | Trust distribution status |

No multicluster-sdk hooks (`useFleetK8sWatchResource`) are used yet — all current data is hub-side only.

### MeshStatus component

`MeshStatus` accepts an optional `conditionType` prop (default `"Ready"`). Use `conditionType="OperatorInstalled"` for per-cluster operator status. The component shows the condition type name (e.g. "Ready") for True status, "Unknown" for Unknown status, and the friendly reason string for False status.

### Internationalization (i18n)

All user-facing strings must be wrapped with `t()` from `useMeshTranslation()`. The plugin namespace is `plugin__ossm-acm` (matching the Console plugin name convention).

```typescript
import { useMeshTranslation } from '../utils/i18nUtils'

const MyComponent: React.FC = () => {
  const { t } = useMeshTranslation()
  return <span>{t('My string')}</span>
}
```

For strings with interpolated values, use `{{variable}}` syntax:
```typescript
t('Cluster Status ({{count}})', { count: clusterStatuses.length })
```

For navigation and perspective names in `console-extensions.ts`, use the `consoleName()` helper which produces `%plugin__ossm-acm~Title%` markers for the Console's own i18n system (separate from react-i18next).

When adding new user-facing strings, also add them to `src/locales/en/plugin__ossm-acm.json`.

The Console provides react-i18next at runtime; this plugin never initializes i18next itself.

### Testing

Run tests with `make test`. Tests live in `__tests__/` subdirectories alongside source, using `*.test.tsx` naming.

- `@openshift-console/dynamic-plugin-sdk` is mocked in `src/__mocks__/consoleSdkMock.tsx`. Override hook return values with `mockReturnValue()` in individual tests.
- `react-router-dom-v5-compat` is mocked in `src/__mocks__/routerMock.tsx` (`Link` renders `<a>`, `useParams` returns `{}`).
- `react-i18next` is mocked globally in `src/setupTests.tsx` — `t(key)` returns the English key string with `{{variable}}` interpolations substituted. Tests can assert directly on English source strings.

When mocking `useK8sWatchResource` in a test:
```typescript
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'
const mockWatch = useK8sWatchResource as jest.Mock
mockWatch.mockReturnValue([data, true, null])  // [data, loaded, error]
```

## Conventions

- No comments unless the WHY is non-obvious
- Use PatternFly components for all UI — `Card`, `DescriptionList`, `Label`, `Grid`, `Flex`, `PageSection`, etc.
- Tables in scrollable containers (max 400px) use sticky `<thead>` for column visibility
- Cards showing per-cluster data should include summary labels, toggle filters, search, and scroll for scale (5-500 clusters)
- Condition status uses three colors: green (True), red (False), grey (Unknown)
- Trust distribution status uses: green "Distributed", orange "Applied", red with reason, grey "Pending"
- Empty states should distinguish "no data exists" from "filter matched nothing"
- Sign commits with `-s` flag
- Never amend or rewrite existing commits unless explicitly asked

## Common Operations

### Adding a new page

1. Create the component in `src/components/`
2. Add a `console.page/route` in `console-extensions.ts` (more specific paths before less specific)
3. Add the module to `exposedModules` in `console-plugin-metadata.ts`
4. Add navigation if needed (`console.navigation/href`)

### Adding a new data watch

1. Define types in `src/types/` with a `K8sGroupVersionKind` constant
2. Use `useK8sWatchResource` with `groupVersionKind`, `isList`, `namespace`, and `selector`
3. Pass `null` to skip the watch conditionally
4. Handle three states: loading (spinner), error (message), loaded (render)
5. For cross-namespace watches, omit the `namespace` field (requires cluster-level RBAC)

### Adding a column to the list page

1. Add entry to `buildColumns()` in `ServiceMeshPage.tsx` with `title: t('...')`, `id`, and optionally `sort`
2. `sort` can be a dot-path string or a function — if using a function, add explicit types: `(data: MultiClusterMesh[], sortDirection: string) => MultiClusterMesh[]`
3. Add the new string key to `src/locales/en/plugin__ossm-acm.json`
4. Add a `<TableData id="..." activeColumnIDs={activeColumnIDs}>` cell to `MeshRow`
5. Cell order doesn't need to match column order (matched by `id`), but keep them aligned for readability

## Backend CRD Reference

The `MultiClusterMesh` CRD is the primary resource. Key fields:

- `spec.clusterSet` — references a `ManagedClusterSet`
- `spec.controlPlane.namespace` — defaults to `istio-system`
- `spec.operator.*` — OLM Subscription config (channel, source, approval)
- `spec.security.trust.certManager.issuerRef.name` — cert-manager Issuer (empty = trust disabled)
- `status.conditions[]` — mesh-level conditions (primary: `Ready`)
- `status.clusterStatus[]` — per-cluster conditions (primary: `OperatorInstalled`)

Go types: `pkg/apis/mesh/v1alpha1/types.go`. The CRD uses Go value types (not pointers) so K8s always serializes the full structure with defaults.
