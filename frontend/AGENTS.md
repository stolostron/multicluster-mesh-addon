# Agent Instructions — Frontend

## Project Overview

OpenShift ConsolePlugin that adds a "Fleet Service Mesh" perspective to the OpenShift Console. Provides fleet-wide visibility into both managed (`MultiClusterMesh`) and discovered (unmanaged) service meshes across managed clusters via the [multicluster-mesh-addon](https://github.com/stolostron/multicluster-mesh-addon) backend controller.

Related links:
- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [docs/ROADMAP.md](docs/ROADMAP.md) — Future plans and backlog
- [docs/INITIAL-SPIKE.md](docs/INITIAL-SPIKE.md) — Original spike research and architecture notes
- [DEV-INSTALL.md](DEV-INSTALL.md) — End-to-end dev setup on CRC

## Project Structure

- `console-extensions.ts` — Declares perspective, nav items, page routes
- `console-plugin-metadata.ts` — Plugin name, version, exposed modules
- `webpack.config.ts` — Build config (ConsoleRemotePlugin + CopyPlugin for locales, swc-loader for TS transpilation)
- `rstest.config.ts` — Rstest test runner config (jsdom environment, module aliases, setup files, SWC JSX transform)
- `hack/start-console.sh` — Runs local OpenShift Console (`origin-console`) pointed at webpack dev server
- `deploy/` — Kubernetes manifests (ConsolePlugin CR, Deployment/Service, nginx config)
- `src/types/` — TypeScript types for K8s resources (MultiClusterMesh, Certificate, ManifestWork, Istio, ManagedCluster)
- `src/types/fleetMesh.ts` — FleetMeshItem type for the unified mesh list (managed + discovered)
- `src/types/managedCluster.ts` — ManagedCluster type, GVK, and cluster availability helpers
- `src/components/` — React page and card components (OverviewPage, ServiceMeshPage, MeshDetailPage, DiscoveredMeshDetailPage, ControlPlanesPage, ControlPlaneDetailPage, ControlPlanesCard, MeshStatus, StatusDonutChart, TrustStatusCard)
- `src/hooks/` — Data fetching hooks (useMultiClusterMeshes, useManagedClusters, useFleetMeshItems, useDiscoveredControlPlanes, useEnrichedControlPlanes)
- `src/utils/filterUtils.ts` — Case-insensitive filter utility for multi-field list filtering
- `src/utils/i18nUtils.ts` — i18n hook (`useMeshTranslation`) and namespace constant
- `src/locales/en/plugin__ossm-acm.json` — English translation strings
- `src/__mocks__/` — Rstest mocks for Console SDK, multicluster-sdk, react-router, and react-charts
- `src/setupTests.tsx` — Rstest setup (jest-dom matchers via expect.extend, i18n mock, jsdom stubs, cleanup)
- `src/rstest-globals.d.ts` — Type declarations for rstest globals (`describe`, `it`, `expect`, `rstest`, etc.)

## Build & Deploy

Requires Node.js `^20.19.0 || >=22.12.0` and `podman` or `docker` for container image builds.

Run `make help` to see all available targets.

**Local development (fast iteration):**

- `make prepare-dev-env` — Install npm deps and print local dev instructions
- `make start` — Webpack dev server on localhost:9001 (run in one terminal)
- `make start-console` — Local OpenShift Console on localhost:9000; auto port-forwards ACM/MCE plugins for Fleet Management links (requires `oc login` and `make start` in another terminal)

**Cluster deploy (production-like validation):**

- `make build` — Build the container image (npm ci + webpack inside the container)
- `make deploy` — Push image to registry and deploy to cluster (includes console restart)
- `make teardown` — Remove the plugin from the cluster
- `make test` — Run unit tests
- `make build deploy` — Full cluster deploy workflow

Override `IMG` to push to an external registry: `make build IMG=quay.io/myorg/ossm-acm-console-plugin:v1`

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

Detail routes (`/fleet-mesh/meshes/managed/:ns/:name`, `/fleet-mesh/meshes/discovered/:meshID`, `/fleet-mesh/control-planes/:type/:cluster/:name`) must be registered BEFORE their respective list routes (`/fleet-mesh/meshes`, `/fleet-mesh/control-planes`) in `console-extensions.ts`. React Router v5 matches the first route whose path prefix matches.

### Data sources

All data comes from the hub cluster Kubernetes API via `useK8sWatchResource`:

| Resource | Where it lives | What it shows |
|----------|---------------|---------------|
| `MultiClusterMesh` | Mesh namespace (e.g. `mesh-system`) | Mesh spec, status, per-cluster conditions |
| `ManagedCluster` (cluster.open-cluster-management.io/v1) | Cluster-scoped | Cluster availability (Available/Unavailable/Unreachable) |
| `Certificate` (cert-manager.io/v1) | Mesh namespace | Per-cluster cert status, expiry, renewal |
| `ManifestWork` (work.open-cluster-management.io/v1) | Per-cluster namespace (e.g. `local-cluster`) | Trust distribution status |

The Control Planes page uses `@stolostron/multicluster-sdk` for cross-cluster data:

| Resource | SDK API | What it shows |
|----------|---------|---------------|
| `Istio` (sailoperator.io/v1) | `useFleetSearchPoll` (discovery) + `fleetK8sGet` (enrichment) | Per-cluster control plane version, meshID, health |

The enrichment hook (`useEnrichedControlPlanes`) caches `fleetK8sGet` results in a module-level `Map` with a 2.5-minute TTL, fetches in batches of 10, and correlates with `MultiClusterMesh` CRs from `useMultiClusterMeshes`. See [docs/DISCOVERY-OPTIONS.md](docs/DISCOVERY-OPTIONS.md) for the full architecture.

### MeshStatus component

`MeshStatus` accepts an optional `conditionType` prop (default `"Ready"`). Use `conditionType="OperatorInstalled"` for per-cluster operator status. The component shows the condition type name (e.g. "Ready") for True status, "Unknown" for Unknown status, and the friendly reason string for False status.

### Internationalization (i18n)

All user-facing strings must be wrapped with `t()` from `useMeshTranslation()`. The plugin namespace is `plugin__ossm-acm` (matching the Console plugin name convention).

```typescript
import { useMeshTranslation } from '../utils/i18nUtils'

const MyComponent: FC = () => {
  const { t } = useMeshTranslation()
  return <span>{t('My string')}</span>
}
```

For strings with interpolated values, use `{{variable}}` syntax:
```typescript
t('Clusters ({{count}})', { count: clusterStatuses.length })
```

For navigation and perspective names in `console-extensions.ts`, use the `consoleName()` helper which produces `%plugin__ossm-acm~Title%` markers for the Console's own i18n system (separate from react-i18next).

When adding new user-facing strings, also add them to `src/locales/en/plugin__ossm-acm.json`.

The Console provides react-i18next at runtime; this plugin never initializes i18next itself.

### Build toolchain

The build uses **SWC** (`swc-loader` + `@swc/core`) for TypeScript transpilation instead of `ts-loader`/tsc. SWC is the same Rust-based transpiler that Rspack uses internally via `builtin:swc-loader`. The SWC options in `webpack.config.ts` are configured to match Rspack's API, so a future migration to Rspack (once the Console SDK supports it) is minimal. SWC does not type-check — run `tsc --noEmit` separately if needed.

### Testing

Run tests with `make test`. Tests use **Rstest** (`@rstest/core`) — an Rspack-powered test runner with Jest-compatible APIs. Tests live in `__tests__/` subdirectories alongside source, using `*.test.tsx` naming.

- `@openshift-console/dynamic-plugin-sdk` is mocked via `resolve.alias` in `rstest.config.ts`, pointing to `src/__mocks__/consoleSdkMock.tsx`. Override hook return values with `mockReturnValue()` in individual tests.
- `react-router-dom-v5-compat` is mocked via `resolve.alias` in `rstest.config.ts`, pointing to `src/__mocks__/routerMock.tsx` (`Link` renders `<a>`, `useParams` returns `{}`).
- `@patternfly/react-charts/victory` is mocked via `resolve.alias` in `rstest.config.ts`, pointing to `src/__mocks__/chartsMock.tsx`.
- `react-i18next` is mocked globally in `src/setupTests.tsx` via `rs.mock()` — `t(key)` returns the English key string with `{{variable}}` interpolations substituted. Tests can assert directly on English source strings.

When mocking a hook in a test, use `rstest.mocked()` (preferred) or `import type { Mock }`:
```typescript
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk'

// Option A — inline (preferred for one-off usage):
rstest.mocked(useK8sWatchResource).mockReturnValue([data, true, null])

// Option B — variable for repeated use:
import type { Mock } from '@rstest/core'
const mockWatch = useK8sWatchResource as unknown as Mock
mockWatch.mockReturnValue([data, true, null])
```

When mocking a local module (not aliased via `resolve.alias`), pass `{ mock: true }` to enable auto-mocking:
```typescript
rstest.mock('../../hooks/useMultiClusterMeshes', { mock: true })
```

Mock files and setup files are regular modules, not test files — rstest globals are not injected. They must `import { rs } from '@rstest/core'` and use `rs.fn()` / `rs.mock()` explicitly.

Rstest uses `>` as snapshot separator vs Jest's `:` — relevant if snapshot tests are added in the future.

## Conventions

- No comments unless the WHY is non-obvious
- Use PatternFly components for all UI — `Card`, `DescriptionList`, `Label`, `Grid`, `Flex`, `PageSection`, etc.
- Tables in scrollable containers (max 400px) use sticky `<thead>` for column visibility
- Cards showing per-cluster data should include toggle filters (All/Ready/Not Ready/Unknown), search, and scroll for scale (5-500 clusters)
- Condition status uses four colors: green (True/Ready), orange (Degraded — Ready but secondary conditions failing), red (False), grey (Unknown)
- Trust distribution status uses: green "Distributed", orange "Applied", red with reason, grey "Pending"
- A cluster can host multiple control planes (for different meshes, different namespaces, or redundancy). Never assume a 1:1 relationship between clusters and control planes. Use unique cluster names (via `Set`) when counting clusters, and include CP identity (name or namespace) when attributing data to avoid ambiguity.
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

### Adding a column to a list page

Both `ServiceMeshPage.tsx` (Meshes) and `ControlPlanesPage.tsx` (Control Planes) follow the same pattern:

1. Add entry to `buildColumns()` with `title: t('...')`, `id`, and optionally `sort`
2. `sort` can be a dot-path string or a function — if using a function, add explicit types: `(data: T[], sortDirection: string) => T[]`
3. Add the new string key to `src/locales/en/plugin__ossm-acm.json`
4. Add a `<TableData id="..." activeColumnIDs={activeColumnIDs}>` cell to the row component
5. Cell order doesn't need to match column order (matched by `id`), but keep them aligned for readability
6. `ServiceMeshPage.tsx` operates on `FleetMeshItem` (the unified type covering managed and discovered meshes); `ControlPlanesPage.tsx` operates on `EnrichedControlPlane`

## Backend CRD Reference

The `MultiClusterMesh` CRD is the primary resource. Key fields:

- `spec.clusterSet` — references a `ManagedClusterSet`
- `spec.controlPlane.namespace` — defaults to `istio-system`
- `spec.operator.*` — OLM Subscription config (channel, source, approval)
- `spec.security.trust.certManager.issuerRef.name` — cert-manager Issuer (empty = trust disabled)
- `status.conditions[]` — mesh-level conditions (primary: `Ready`)
- `status.clusterStatus[]` — per-cluster conditions (primary: `OperatorInstalled`)

Go types: `pkg/apis/mesh/v1alpha1/types.go`. The CRD uses Go value types (not pointers) so K8s always serializes the full structure with defaults.
