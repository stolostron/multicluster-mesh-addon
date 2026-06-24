# Fleet Service Mesh Console Plugin — Roadmap

## What's implemented

- **Meshes list page** — sortable table with Name, Namespace, Cluster Set, Clusters, Trust, Age, Status columns. Empty states for no data and no filter matches.
- **Mesh detail page** — Overview and OSSM Operator config cards, Trust Status card (watches cert-manager Certificates + ManifestWorks), Cluster Status card with per-cluster operator install status, Conditions table. Designed for 1 to hundreds of clusters with filters, search, and scroll.
- **Cross-perspective links** — cluster names link to ACM cluster detail pages; cluster set names link to ACM cluster set detail pages.
- **Conflict mesh UX** — friendly labels for backend condition reasons, and blocked meshes show an explanatory message instead of "No clusters."
- **Production packaging** — Dockerfile (UBI9 nodejs-24 + nginx-126), Makefile with `build`, `deploy`, `teardown`, and `test` targets.
- **Automated tests** — Rstest + Testing Library unit test framework (`make test`), with TypeScript type checking (`tsc --noEmit`) and mocks for the Console SDK, multicluster-sdk, and react-router. Tests cover `MeshStatus`, `ServiceMeshPage`, `MeshDetailPage`, `ClusterStatusSection`, `TrustStatusCard`, `ControlPlanesPage`, `ControlPlaneDetailPage`, and `useEnrichedControlPlanes`.
- **Internationalization (i18n)** — All user-facing strings externalized via react-i18next under the `plugin__ossm-acm` namespace. Locale bundle served from `dist/locales/en/plugin__ossm-acm.json`.
- **Control Planes page** — Discovers all sail-operator `Istio` CRs across managed clusters via ACM Search (`useFleetSearchPoll`), enriches with full CR data via `fleetK8sGet` (version, meshID, status), and correlates with `MultiClusterMesh` CRs for fleet management context. See [DISCOVERY-OPTIONS.md](./DISCOVERY-OPTIONS.md) for the design rationale.
- **Local dev server** — `make start` runs webpack-dev-server on localhost:9001; `make start-console` runs a local OpenShift Console (`origin-console`) on localhost:9000 via `hack/start-console.sh`, automatically port-forwarding in-cluster ACM and MCE plugins so Fleet Management links work. Additive to the existing `make build deploy` cluster workflow.
- **Overview page** — Dashboard-style landing page (`/fleet-mesh-overview`, nav label "Overview") showing fleet-wide health at a glance: count cards (meshes, control planes, clusters), health breakdowns (Ready / Not Ready / Unknown) for meshes and control planes, and a recent issues panel surfacing the latest non-True conditions. Mesh and control plane sections load independently (partial-success rendering). This is the perspective's `landingPageURL`.

## What's next (not blocked)
- **Create / delete mesh actions** — Add a "Create Mesh" button to the list page and "Delete Mesh" on the detail page.
- **Edit mesh** — Edit issuer, operator config, etc. from the detail page.
- **CI workflow** — Add a GitHub Actions workflow to the parent repo (`.github/workflows/frontend-ci.yml`) that runs `make test build` on PRs touching `frontend/**`, using two parallel jobs (test — which includes type checking — and build).

## Blocked on backend

- **Kiali deep links** — The backend doesn't produce Kiali URLs or remote cluster secrets for Kiali. When implemented, the UI can link users directly to each mesh's Kiali instance.
- **Endpoint discovery status** — The CRD has `spec.security.discovery` but the controller doesn't implement ManagedServiceAccount token exchange yet. When built, the frontend could show per-cluster discovery status (tokens issued, expiry, remote secret distribution) similar to the Trust Status card.

## Related

- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [INITIAL-SPIKE.md](./INITIAL-SPIKE.md) — Original spike research by Nick Fox
