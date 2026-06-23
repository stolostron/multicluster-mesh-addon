# ossm-acm-ui

An OpenShift `ConsolePlugin` that registers a standalone **Fleet Service Mesh** perspective in the OpenShift Console perspective switcher, providing fleet-wide service mesh visibility across ACM-managed clusters.

## What it does

This plugin registers a new **console perspective** — a top-level entry in the perspective switcher dropdown (alongside "Administrator", "Developer", "Fleet Management", "Fleet Virtualization", etc.). Selecting it switches to a dedicated left-hand nav and landing page owned entirely by this plugin. No changes to ACM are required.

The plugin provides:
- **Fleet Meshes list page** — sortable table of all `MultiClusterMesh` resources with status, trust configuration, and cluster counts
- **Mesh detail page** — overview, operator config, per-cluster trust status (cert-manager Certificates + ManifestWorks), cluster status with filters/search, and conditions
- **Control Planes list page** — discovers all sail-operator `Istio` CRs across managed clusters via ACM Search, enriched with version, meshID, and health status. Shows which control planes are managed by a `MultiClusterMesh` CR.
- **Control Plane detail page** — per-cluster control plane details (version, namespace, meshID, network, conditions) with "Managed By" link when correlated to a fleet mesh
- **Cross-perspective links** — cluster names link to ACM cluster detail pages; cluster set names link to ACM cluster set detail pages

## Prerequisites

- [CRC](https://crc.dev) or OpenShift cluster with ACM installed and the multicluster-mesh-addon backend controller deployed. See [DEV-INSTALL.md](DEV-INSTALL.md) steps 1-4 for full setup instructions.
- `oc` logged in as `kubeadmin`
- Node.js 20+
- `jq`, `make`
- For production image builds: `podman` or `docker`

## Quick Start

Build the container image, push it to the OpenShift internal registry, and deploy:

```bash
make build deploy
```

Run `make help` to see all available targets.

## Documentation

- [DEV-INSTALL.md](DEV-INSTALL.md) — End-to-end setup guide for CRC (ACM, cert-manager, backend controller, frontend plugin)
- [AGENTS.md](AGENTS.md) — AI/dev agent context for the frontend codebase
- [docs/ROADMAP.md](docs/ROADMAP.md) — Current status and future plans
- [docs/INITIAL-SPIKE.md](docs/INITIAL-SPIKE.md) — Original spike research and architecture notes
