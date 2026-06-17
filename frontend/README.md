# ossm-acm-ui

An OpenShift `ConsolePlugin` that registers a standalone **Fleet Service Mesh** perspective in the OpenShift Console perspective switcher, providing fleet-wide service mesh visibility across ACM-managed clusters.

## What it does

This plugin registers a new **console perspective** — a top-level entry in the perspective switcher dropdown (alongside "Administrator", "Developer", "Fleet Management", "Fleet Virtualization", etc.). Selecting it switches to a dedicated left-hand nav and landing page owned entirely by this plugin. No changes to ACM are required.

The plugin provides:
- **Meshes list page** — sortable table of all `MultiClusterMesh` resources with status, trust configuration, and cluster counts
- **Mesh detail page** — overview, operator config, per-cluster trust status (cert-manager Certificates + ManifestWorks), cluster status with filters/search, and conditions
- **Cross-perspective links** — cluster names link to ACM's cluster detail page

## Prerequisites

- [CRC](https://crc.dev) or OpenShift cluster with ACM installed
- `oc` logged in as `kubeadmin`
- Node.js 20 (Node 22+ may fail due to stricter ESM module resolution in ts-node)
- For production image builds: `podman` or `docker`

## Quick Start

```bash
# Dev workflow (ConfigMap + stock nginx, no image build)
make dev-build dev-deploy

# Production workflow (baked container image)
make prod-build prod-deploy
```

Run `make help` to see all available targets.

## Documentation

- [DEV-INSTALL.md](DEV-INSTALL.md) — End-to-end setup guide for CRC (ACM, cert-manager, backend controller, frontend plugin)
- [AGENTS.md](AGENTS.md) — AI/dev agent context for the frontend codebase
- [PLAN.md](PLAN.md) — Original spike research and architecture notes
