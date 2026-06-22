# Fleet Service Mesh Console Plugin — Roadmap

## What's implemented

- **Meshes list page** — sortable table with Name, Namespace, Cluster Set, Clusters, Trust, Age, Status columns. Empty states for no data and no filter matches.
- **Mesh detail page** — Overview and OSSM Operator config cards, Trust Status card (watches cert-manager Certificates + ManifestWorks), Cluster Status card with per-cluster operator install status, Conditions table. Designed for 1 to hundreds of clusters with filters, search, and scroll.
- **Cross-perspective links** — cluster names link to ACM cluster detail pages; cluster set names link to ACM cluster set detail pages.
- **Conflict mesh UX** — friendly labels for backend condition reasons, and blocked meshes show an explanatory message instead of "No clusters."
- **Production packaging** — Dockerfile (UBI9 nodejs-20 + nginx-124), Makefile with `dev-*`, `prod-*`, and `test` targets.
- **Automated tests** — Jest + Testing Library unit test framework (`make test`), with mocks for the Console SDK and react-router. Tests cover `MeshStatus`, `ServiceMeshPage`, `MeshDetailPage`, `ClusterStatusSection`, and `TrustStatusCard`.
- **Internationalization (i18n)** — All user-facing strings externalized via react-i18next under the `plugin__ossm-acm` namespace. Locale bundle served from `dist/locales/en/plugin__ossm-acm.json`.

## What's next (not blocked)

- **Create / delete mesh actions** — Add a "Create Mesh" button to the list page and "Delete Mesh" on the detail page.
- **Edit mesh** — Edit issuer, operator config, etc. from the detail page.
- **CI workflow** — Add a GitHub Actions workflow to the parent repo (`.github/workflows/frontend-ci.yml`) that runs `make test dev-build` on PRs touching `frontend/**`, using Node 20 and two parallel jobs (test and build).

## Blocked on backend

- **Kiali deep links** — The backend doesn't produce Kiali URLs or remote cluster secrets for Kiali. When implemented, the UI can link users directly to each mesh's Kiali instance.
- **Endpoint discovery status** — The CRD has `spec.security.discovery` but the controller doesn't implement ManagedServiceAccount token exchange yet. When built, the frontend could show per-cluster discovery status (tokens issued, expiry, remote secret distribution) similar to the Trust Status card.

## Related

- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
- [SPIKE.md](SPIKE.md) — Original spike research by Nick Fox
