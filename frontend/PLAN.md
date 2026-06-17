# OSSM ACM UI Plugin — Implementation Plan

## Implementation Status

As of June 2026, the following phases from this plan have been implemented:

- **Phase 1 (Project scaffolding)**: Complete
- **Phase 2 (Fleet Service Mesh perspective)**: Complete, with additional features:
  - Mesh detail page with Overview, OSSM Operator, Trust Status, and Cluster Status cards
  - List page with Namespace, Trust columns and all-column sorting
  - Cross-perspective links to ACM cluster details
  - Scale support for 5-500 clusters (filters, search, scroll)
- **Phase 3 (Additional pages)**: Partially complete — detail page done, topology view not started
- **Phase 4 (Build and deployment)**: Using ConfigMap + nginx for dev; Dockerfile approach not yet needed

The rest of this document is the original spike research by Nick Fox.

---

## Goal

Build an OpenShift `ConsolePlugin` that registers a standalone "Fleet Service Mesh" perspective in the OpenShift Console perspective switcher. The plugin will use the `@stolostron/multicluster-sdk` for multicluster service mesh visibility and have full control over its own navigation, pages, and layout. The Fleet Service Mesh perspective should surface a small number of key metrics and health indicators for each mesh in a high level view that would help an administrator identify mesh instances to investigate or troubleshoot. Initially, it should focus navigating a read-only "mesh of meshes", we will later want to consider administrative functions that help with multi-mesh administrative tasks with the under development ACM mesh addon.

---

## Background: ACM Plugin Extension Architecture

### How ACM discovers external extensions

ACM uses OpenShift Console's dynamic plugin SDK to discover extensions from **all loaded ConsolePlugins**, not just its own. The flow:

1. `useAcmExtension()` hook (`frontend/src/plugin-extensions/handler.ts`) calls `useResolvedExtensions()` with type guards for each ACM extension type.
2. Resolved extensions are stored in `PluginContext.acmExtensions` via React Context.
3. Individual page components read from `acmExtensions` and render contributed content.

An external ConsolePlugin registers extensions with `acm.*` types in its `console-extensions.ts`. ACM resolves them automatically — no code changes to ACM are required.

### Available ACM extension points

| Extension Type                   | Purpose                                                | Where it renders                                                         |
| -------------------------------- | ------------------------------------------------------ | ------------------------------------------------------------------------ |
| `acm.overview/tab`               | Add a tab to the Overview page                         | `/multicloud/home/overview` — tabs in the page header                    |
| `acm.application/action`         | Add actions (modal triggers) for Application resources | Application list rows + Application detail page action dropdown          |
| `acm.application/list/column`    | Add columns to the Applications list table             | Applications list page                                                   |
| `acm.virtualmachine/action`      | Add actions for VirtualMachine resources               | VM list rows in Search results + Search detail page                      |
| `acm.virtualmachine/list/column` | Add columns to VirtualMachine lists                    | Search results (infrastructure exists but not yet wired)                 |
| `acm.resource/route`             | Custom navigation routes for K8s resources             | `FleetResourceLink` component — controls where resource links navigate   |
| `acm.shared-context`             | Share React context across plugins                     | Plugin communication — MCE uses this to expose Recoil atoms, React Query |

### Standard OpenShift Console extension points (always available)

| Extension Type               | Purpose                                       |
| ---------------------------- | --------------------------------------------- |
| `console.navigation/section` | Add a navigation section to the left-hand nav |
| `console.navigation/href`    | Add a navigation item (link)                  |
| `console.page/route`         | Register a new page at a URL path             |
| `console.context-provider`   | Inject a React context provider               |
| `console.flag/hookProvider`  | Register feature flags                        |
| `console.perspective`        | Define a new console perspective              |

All `console.navigation/*` and `console.page/route` extensions accept a `perspective: 'acm'` property to place them within the Fleet Management perspective.

### What is NOT extensible

- Cluster detail page tabs (hardcoded)
- Policy detail page tabs (hardcoded)
- Injecting sections/cards into existing ACM pages
- Modifying existing table columns or actions
- Banners or alerts within ACM pages

A ConsolePlugin can **add alongside** (new tabs, nav items, pages) but cannot **modify within** existing ACM page internals unless ACM has built an explicit extension point.

---

## The `@stolostron/multicluster-sdk` Package

Published to npm as `@stolostron/multicluster-sdk` (v0.10.3). Designed for external consumption.

### Peer dependency

```json
{
  "@openshift-console/dynamic-plugin-sdk": ">=1.0.0 || >=4.19.0-prerelease"
}
```

### Key hooks for data fetching

| Hook                        | Purpose                                          |
| --------------------------- | ------------------------------------------------ |
| `useFleetK8sWatchResource`  | Watch a K8s resource across all managed clusters |
| `useFleetK8sWatchResources` | Watch multiple K8s resources across clusters     |
| `useFleetSearchPoll`        | Poll ACM Search for resources matching a query   |
| `useFleetClusterNames`      | Get list of all managed cluster names            |
| `useFleetClusterSetNames`   | Get list of all cluster set names                |
| `useFleetClusterSets`       | Get all ManagedClusterSet resources              |
| `useFleetAccessReview`      | Check RBAC across fleet                          |
| `useIsFleetAvailable`       | Check if RHACM fleet capabilities are available  |
| `useHubClusterName`         | Get the hub cluster name                         |
| `useFleetPrometheusPoll`    | Poll Prometheus metrics from fleet observability |

### Key functions for mutations

| Function         | Purpose                                |
| ---------------- | -------------------------------------- |
| `fleetK8sCreate` | Create a resource on a managed cluster |
| `fleetK8sDelete` | Delete a resource on a managed cluster |
| `fleetK8sPatch`  | Patch a resource on a managed cluster  |
| `fleetK8sUpdate` | Update a resource on a managed cluster |
| `fleetK8sGet`    | Get a resource from a managed cluster  |
| `fleetK8sList`   | List resources from a managed cluster  |

### Extension types provided by the SDK

- `RESOURCE_ROUTE_TYPE` (`'acm.resource/route'`) — for registering custom resource link routes
- `ResourceRoute`, `ResourceRouteHandler`, `ResourceRouteProps` — TypeScript types

---

## The `MultiClusterMesh` CRD

The `MultiClusterMesh` resource (`mesh.open-cluster-management.io/v1alpha1`) is a **hub-side** CRD from `stolostron/multicluster-mesh-addon`. It represents a fleet-wide service mesh configuration tied to a `ManagedClusterSet`.

### GVK

```typescript
{
  group: 'mesh.open-cluster-management.io',
  version: 'v1alpha1',
  kind: 'MultiClusterMesh',
}
```

### Key fields

| Field                                            | Type                | Description                                                                      |
| ------------------------------------------------ | ------------------- | -------------------------------------------------------------------------------- |
| `spec.clusterSet`                                | `string` (required) | References the ACM `ManagedClusterSet` defining cluster membership               |
| `spec.controlPlane.namespace`                    | `string`            | Namespace for Istio installation (default: `istio-system`)                       |
| `spec.operator`                                  | object              | Sail Operator subscription config (channel, source, approval)                    |
| `spec.security.trust.certManager.issuerRef.name` | `string`            | cert-manager Issuer used as Root CA                                              |
| `spec.security.discovery.tokenValidity`          | `string`            | Token validity duration (default: `1m`)                                          |
| `status.conditions`                              | `K8sCondition[]`    | Standard Kubernetes conditions                                                   |
| `status.clusterStatus[]`                         | per-cluster status  | Per-cluster booleans: `operatorReady`, `trustEstablished`, `discoveryConfigured` |

### `useMultiClusterMeshes` hook

Since `MultiClusterMesh` lives on the hub cluster only, the hook uses `useK8sWatchResource` from the Console SDK (not `useFleetK8sWatchResource` from multicluster-sdk, which fans out to managed clusters).

```typescript
import { useMultiClusterMeshes } from "../hooks/useMultiClusterMeshes";

const [meshes, loaded, error] = useMultiClusterMeshes();
```

Returns the standard `[MultiClusterMesh[], boolean, unknown]` tuple.

Types are defined in `src/types/multiClusterMesh.ts`. The hook is in `src/hooks/useMultiClusterMeshes.ts`.

---

## Implementation Plan

### Phase 1: Project scaffolding

Create a new ConsolePlugin project at `/home/nrfox/Developer/redhat/ossm-acm-ui/`.

#### 1.1 Initialize the project

```
ossm-acm-ui/
  package.json
  tsconfig.json
  webpack.config.ts
  console-extensions.ts
  console-plugin-metadata.ts
  src/
    types/
      multiClusterMesh.ts           # TypeScript types for the MultiClusterMesh CRD
    hooks/
      useMultiClusterMeshes.ts       # Hook to watch MultiClusterMesh resources
    components/
      ServiceMeshPage.tsx            # Landing page for the perspective
    perspective.ts                   # landingPageURL + importRedirectURL
    perspectiveIcon.tsx              # Icon for the perspective switcher
  Dockerfile
  deploy/                            # OpenShift deployment manifests
    consoleplugin.yaml
    deployment.yaml
    service.yaml
```

#### 1.2 Key dependencies

```json
{
  "dependencies": {
    "@stolostron/multicluster-sdk": "^0.10.3",
    "@patternfly/react-core": "^6.4.3",
    "@patternfly/react-styles": "^6.4.0",
    "@patternfly/react-icons": "^6.4.0",
    "react": "^18.x",
    "react-dom": "^18.x"
  },
  "devDependencies": {
    "@openshift-console/dynamic-plugin-sdk": "^4.19.1",
    "@openshift-console/dynamic-plugin-sdk-webpack": "^4.19.1",
    "typescript": "^5.8",
    "webpack": "^5.x",
    "ts-loader": "^9.x"
  }
}
```

#### 1.3 Plugin metadata (`console-plugin-metadata.ts`)

```typescript
import { ConsolePluginBuildMetadata } from "@openshift-console/dynamic-plugin-sdk-webpack";

export const pluginMetadata: ConsolePluginBuildMetadata = {
  name: "ossm-acm",
  version: "0.1.0",
  displayName: "OpenShift Service Mesh — ACM Integration",
  description:
    "Adds Service Mesh visibility to the ACM Fleet Management console",
  exposedModules: {
    serviceMeshPage: "./src/components/ServiceMeshPage.tsx",
    perspective: "./src/perspective.ts",
    perspectiveIcon: "./src/perspectiveIcon.tsx",
  },
  dependencies: {
    "@console/pluginAPI": ">=4.19.0",
  },
};
```

#### 1.4 Console extensions (`console-extensions.ts`)

```typescript
import { EncodedExtension } from "@openshift/dynamic-plugin-sdk-webpack";

const serviceMeshPerspective: EncodedExtension = {
  type: "console.perspective",
  properties: {
    id: "fleet-service-mesh",
    name: "Fleet Service Mesh",
    icon: { $codeRef: "perspectiveIcon" },
    landingPageURL: { $codeRef: "perspective.landingPageURL" },
    importRedirectURL: { $codeRef: "perspective.importRedirectURL" },
    defaultPins: [
      { group: "maistra.io", version: "v2", kind: "ServiceMeshControlPlane" },
    ],
  },
};

const meshNavSection: EncodedExtension = {
  type: "console.navigation/section",
  properties: {
    perspective: "fleet-service-mesh",
    id: "service-mesh-main",
    name: "Service Mesh",
  },
};

const meshOverviewNavItem: EncodedExtension = {
  type: "console.navigation/href",
  properties: {
    perspective: "fleet-service-mesh",
    section: "service-mesh-main",
    id: "mesh-overview",
    name: "Overview",
    href: "/service-mesh",
  },
};

const meshOverviewRoute: EncodedExtension = {
  type: "console.page/route",
  properties: {
    perspective: "fleet-service-mesh",
    path: "/service-mesh",
    component: { $codeRef: "serviceMeshPage.default" },
  },
};

export const extensions: EncodedExtension[] = [
  serviceMeshPerspective,
  meshNavSection,
  meshOverviewNavItem,
  meshOverviewRoute,
];
```

#### 1.5 Webpack config (`webpack.config.ts`)

```typescript
import { ConsoleRemotePlugin } from "@openshift-console/dynamic-plugin-sdk-webpack";
import { extensions } from "./console-extensions";
import { pluginMetadata } from "./console-plugin-metadata";

export default function (env: any, argv: any) {
  return {
    entry: {},
    resolve: {
      extensions: [".ts", ".tsx", ".js", ".jsx"],
    },
    module: {
      rules: [
        {
          test: /\.(ts|tsx)$/,
          exclude: /node_modules/,
          loader: "ts-loader",
          options: { transpileOnly: true },
        },
      ],
    },
    plugins: [
      new ConsoleRemotePlugin({
        pluginMetadata,
        extensions,
        validateSharedModules: false,
        validateExtensionIntegrity: false,
      }),
    ],
    devServer: {
      port: 9001,
      static: "./dist",
      allowedHosts: "all",
      headers: {
        "Access-Control-Allow-Origin": "*",
        "Access-Control-Allow-Methods":
          "GET, POST, PUT, DELETE, PATCH, OPTIONS",
        "Access-Control-Allow-Headers":
          "X-Requested-With, Content-Type, Authorization",
      },
    },
  };
}
```

### Phase 2: "Fleet Service Mesh" perspective

The plugin registers its own **console perspective** — a top-level entry in the perspective switcher dropdown, like "Fleet Virtualization". This gives full control over navigation, pages, and layout.

#### How console perspectives work

OpenShift Console discovers perspectives from **all loaded ConsolePlugins** at startup. Each perspective appears in the top-left dropdown alongside built-in perspectives (Administrator, Developer) and other plugin-contributed perspectives (Fleet Management, Fleet Virtualization). Selecting a perspective switches the entire left-hand nav and content area.

#### Type definition

```typescript
// From @openshift-console/dynamic-plugin-sdk
type LazyComponent = { default: React.ComponentType };

type Perspective = ExtensionDeclaration<
  "console.perspective",
  {
    id: string; // Unique identifier
    name: string; // Display name in the dropdown
    icon: CodeRef<LazyComponent> | null; // Icon next to the name
    default?: boolean; // Only one perspective can be default
    defaultPins?: ExtensionK8sModel[]; // Pinned resources in the nav
    landingPageURL: CodeRef<
      (flags: Record<string, boolean>, isFirstVisit: boolean) => string
    >;
    importRedirectURL: CodeRef<(namespace: string) => string>;
    usePerspectiveDetection?: CodeRef<() => [boolean, boolean]>;
  }
>;
```

#### IMPORTANT: `LazyComponent` icon export pattern

The `icon` property uses `CodeRef<LazyComponent>` where `LazyComponent = { default: React.ComponentType }`. There is a subtle interaction between the SDK's CodeRef resolver and the console's NavHeader that **requires the icon module to export `{ default: Component }` as its default export**, not the component directly.

**How it works at runtime:**

1. The SDK's `parseEncodedCodeRef` parses `$codeRef: 'perspectiveIcon'` into `moduleName='perspectiveIcon'`, `exportName='default'`.
2. The SDK's `createCodeRef` returns an async function: `() => module['default']`.
3. The console's NavHeader calls `icon().then((m) => m.default)` to unwrap the component from the `LazyComponent` wrapper.
4. So the CodeRef returns `module.default`, and then the NavHeader accesses `.default` on _that_ value.

If the module does `export default MyIcon` (the component directly), the CodeRef returns the component function, and `.default` on a function is `undefined` — crashing React with error #306 ("element type is invalid: expected a string or class/function but got: undefined").

```typescript
// CORRECT — icon module must export a LazyComponent object
const MyIcon: React.FC = () => <svg>...</svg>
export default { default: MyIcon }

// WRONG — causes React error #306 crash when opening the perspective switcher
const MyIcon: React.FC = () => <svg>...</svg>
export default MyIcon
```

Also avoid importing from `@patternfly/react-icons` in the icon module. PF6 icons are compiled against React 18, but the CRC console runs React 17. Use inline SVG instead.

#### Supporting modules

```typescript
// src/perspective.ts
export const landingPageURL = (
  flags: Record<string, boolean>,
  isFirstVisit: boolean,
): string => "/service-mesh";

export const importRedirectURL = (namespace: string): string =>
  `/service-mesh/ns/${namespace}`;
```

```typescript
// src/perspectiveIcon.tsx — icon shown in the perspective dropdown
// Must export { default: Component }, not the component directly (see note above)
import * as React from 'react'

const PerspectiveIcon: React.FC = () => (
  <svg viewBox="0 0 384 512" fill="currentColor" width="1em" height="1em">
    <path d="M384 144c0-44.2-35.8-80-80-80s-80 35.8-80 80c0 36.4 24.3 67.1 57.5 76.8..." />
  </svg>
)

export default { default: PerspectiveIcon }
```

#### Page content ideas

The Service Mesh overview page could show:

- **Fleet-wide mesh topology**: Which clusters have OSSM installed, control plane status per cluster
- **Service mesh health summary**: Healthy/degraded/failed control planes across the fleet
- **Cross-cluster service discovery**: Services enrolled in each mesh
- **Istio resource inventory**: VirtualServices, DestinationRules, Gateways per cluster
- **Federation status**: If using OSSM federation, show federation links between clusters

#### Data fetching approaches

**Option A: Use `useFleetK8sWatchResource` from multicluster-sdk**

Watch OSSM CRDs (e.g., `ServiceMeshControlPlane`, `ServiceMeshMemberRoll`, Istio `VirtualService`, `DestinationRule`) across all managed clusters.

```typescript
import { useFleetK8sWatchResource } from "@stolostron/multicluster-sdk";

const [smcps, loaded, error] = useFleetK8sWatchResource({
  groupVersionKind: {
    group: "maistra.io",
    version: "v2",
    kind: "ServiceMeshControlPlane",
  },
  isList: true,
});
```

**Option B: Use `useFleetSearchPoll` from multicluster-sdk**

Query ACM Search for OSSM resources across all clusters. More efficient for broad queries but returns search result objects rather than full K8s resources.

```typescript
import { useFleetSearchPoll } from "@stolostron/multicluster-sdk";

const [results, error, loading] = useFleetSearchPoll({
  query: {
    filters: [{ property: "kind", values: ["ServiceMeshControlPlane"] }],
  },
});
```

---

### Phase 3: Additional pages and extension points (future)

#### 3.1 Add additional nav sections and pages

Add more pages within the Fleet Service Mesh perspective:

```typescript
const meshTopologyNavItem: EncodedExtension = {
  type: "console.navigation/href",
  properties: {
    perspective: "fleet-service-mesh",
    section: "service-mesh-main",
    id: "mesh-topology",
    name: "Topology",
    href: "/service-mesh/topology",
  },
};

const meshTopologyRoute: EncodedExtension = {
  type: "console.page/route",
  properties: {
    perspective: "fleet-service-mesh",
    path: "/service-mesh/topology",
    component: { $codeRef: "meshTopology.default" },
  },
};
```

#### 3.2 Add resource routes for OSSM resources (optional ACM integration)

Optionally use `acm.resource/route` so that `FleetResourceLink` in the Fleet Management perspective navigates to OSSM resources within this plugin's perspective:

```typescript
const smcpResourceRoute: EncodedExtension = {
  type: "acm.resource/route",
  properties: {
    model: {
      group: "maistra.io",
      kind: "ServiceMeshControlPlane",
      version: "v2",
    },
    handler: { $codeRef: "resourceRoutes.ossmResourceRouteHandler" },
  },
};
```

### Phase 4: Build and deployment

#### 4.1 Container image

```dockerfile
FROM registry.access.redhat.com/ubi9/nodejs-20:latest AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nginx-124:latest
COPY --from=build /app/dist /opt/app-root/src
```

#### 4.2 ConsolePlugin CR

```yaml
apiVersion: console.openshift.io/v1
kind: ConsolePlugin
metadata:
  name: ossm-acm
spec:
  displayName: OpenShift Service Mesh — ACM Integration
  backend:
    type: Service
    service:
      name: ossm-acm-plugin
      namespace: openshift-service-mesh
      port: 9443
      basePath: /
```

#### 4.3 Enable the plugin

```bash
oc patch console.operator.openshift.io cluster \
  --type=merge \
  --patch='{"spec":{"plugins":["ossm-acm"]}}'
```

---

## Dependency version alignment

Critical: the plugin's shared dependencies must match the versions shipped by OpenShift Console at runtime. Key versions to align (based on ACM console's current dependencies):

| Package                                 | Version | Notes                              |
| --------------------------------------- | ------- | ---------------------------------- |
| `react`                                 | 18.x    | Shared by Console at runtime       |
| `react-dom`                             | 18.x    | Shared by Console at runtime       |
| `@patternfly/react-core`                | ^6.4.3  | PF6 — must match Console's version |
| `@patternfly/react-styles`              | ^6.4.0  | Must match Console's version       |
| `@openshift-console/dynamic-plugin-sdk` | ^4.19.1 | Peer dep for multicluster-sdk      |
| `@stolostron/multicluster-sdk`          | ^0.10.3 | Only peer dep is the SDK above     |

The `ConsoleRemotePlugin` webpack plugin handles module federation and shared module configuration. Setting `validateSharedModules: false` avoids build-time validation failures for version mismatches that are resolved at runtime.

---

## Summary of plugin capabilities

| Capability                                                            | Extension type                                           |
| --------------------------------------------------------------------- | -------------------------------------------------------- |
| Register "Fleet Service Mesh" perspective                             | `console.perspective`                                    |
| Add nav sections and items within the perspective                     | `console.navigation/section` + `console.navigation/href` |
| Add dedicated Service Mesh pages                                      | `console.page/route`                                     |
| Feature flags for conditional rendering                               | `console.flag/hookProvider`                              |
| Custom resource link routing for OSSM CRDs (optional ACM integration) | `acm.resource/route`                                     |
