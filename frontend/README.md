# ossm-acm-ui

An OpenShift `ConsolePlugin` that registers a standalone **Fleet Service Mesh** perspective in the OpenShift Console perspective switcher, providing fleet-wide service mesh visibility across ACM-managed clusters.

## What it does

This plugin registers a new **console perspective** — a top-level entry in the perspective switcher dropdown (alongside "Administrator", "Developer", "Fleet Management", "Fleet Virtualization", etc.). Selecting it switches to a dedicated left-hand nav and landing page owned entirely by this plugin. No changes to ACM are required.

The plugin provides:
- **Meshes list page** — sortable table of all `MultiClusterMesh` resources with status, trust configuration, and cluster counts
- **Mesh detail page** — overview, operator config, per-cluster trust status (cert-manager Certificates + ManifestWorks), cluster status with filters/search, and conditions
- **Cross-perspective links** — cluster names link to ACM cluster detail pages; cluster set names link to ACM cluster set detail pages

## Prerequisites

- [CRC](https://crc.dev) or OpenShift cluster with ACM installed and the multicluster-mesh-addon backend controller deployed. See [DEV-INSTALL.md](DEV-INSTALL.md) steps 1-4 for full setup instructions.
- `oc` logged in as `kubeadmin`
- Node.js 20+
- `jq`, `make`
- For production image builds: `podman` or `docker`

## Quick Start

The **dev workflow** compiles the TypeScript/React source locally and deploys the output to the cluster via ConfigMaps, served by a stock nginx container. No container image build is needed — fast iteration for development:

```bash
make dev-build dev-deploy
```

The **production workflow** builds a self-contained container image (UBI9 nginx with assets baked in), pushes it to the OpenShift internal registry, and deploys it. This is how the plugin would be packaged for release:

```bash
make prod-build prod-deploy
```

Run `make help` to see all available targets.

## Documentation

- [DEV-INSTALL.md](DEV-INSTALL.md) — End-to-end setup guide for CRC (ACM, cert-manager, backend controller, frontend plugin)
- [AGENTS.md](AGENTS.md) — AI/dev agent context for the frontend codebase
- [ROADMAP.md](ROADMAP.md) — Current status and future plans
- [SPIKE.md](SPIKE.md) — Original spike research and architecture notes
