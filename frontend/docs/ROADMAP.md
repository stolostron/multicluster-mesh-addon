# Fleet Service Mesh Console Plugin — Roadmap

## What's next (not blocked)

- **Data plane visibility** — Meshes have control planes but also data planes — the namespaces within clusters where application workloads run with sidecar proxies. The UI needs a way to discover and visualize data planes (which clusters, which namespaces, how many workloads). The discovery mechanism and UI design are TBD.
- **Create / delete mesh actions** — Add a "Create Mesh" button to the list page and "Delete Mesh" on the detail page.
- **Edit mesh** — Edit issuer, operator config, etc. from the detail page.
- **CI workflow** — Add a GitHub Actions workflow to the parent repo (`.github/workflows/frontend-ci.yml`) that runs `make test build` on PRs touching `frontend/**`, using two parallel jobs (test — which includes type checking — and build).

## Blocked on backend

- **Kiali deep links** — The backend doesn't produce Kiali URLs or remote cluster secrets for Kiali. When implemented, the UI can link users directly to each mesh's Kiali instance.
- **Endpoint discovery status** — The CRD has `spec.security.discovery` but the controller doesn't implement ManagedServiceAccount token exchange yet. When built, the frontend could show per-cluster discovery status (tokens issued, expiry, remote secret distribution) similar to the Trust Status card.

## Related

- [OSSM-12887](https://redhat.atlassian.net/browse/OSSM-12887) — Epic: OSSM/Kiali ACM console integration developer preview
- [OCPSTRAT-2989](https://redhat.atlassian.net/browse/OCPSTRAT-2989) — Feature: Fleet-wide service mesh console integration with ACM
