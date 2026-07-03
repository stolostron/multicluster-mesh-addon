# ossm-acm-ui

An OpenShift `ConsolePlugin` that registers a standalone **Fleet Service Mesh** perspective in the OpenShift Console perspective switcher, providing fleet-wide service mesh visibility across ACM-managed clusters.

## What it does

This plugin registers a new **console perspective** — a top-level entry in the perspective switcher dropdown (alongside "Administrator", "Developer", "Fleet Management", "Fleet Virtualization", etc.). Selecting it switches to a dedicated left-hand nav and landing page owned entirely by this plugin. No changes to ACM are required.

The plugin provides:
- **Overview page** (`/fleet-mesh/overview`) — landing page with donut charts for mesh and control plane health, plus a Recent Issues panel showing the latest non-healthy conditions across the fleet
- **Meshes list page** (`/fleet-mesh/meshes`) — unified table of managed (`MultiClusterMesh`) and discovered (Istio CR) meshes with Mesh ID, Type, Name, Cluster Set, Clusters, Trust, and Status columns.
- **Managed mesh detail page** (`/fleet-mesh/meshes/managed/:ns/:name`) — mesh configuration and OSSM Operator settings in a single card, per-cluster operator status (Clusters card), control planes with filter/search, trust distribution status (cert-manager Certificates + ManifestWorks), and conditions
- **Discovered mesh detail page** (`/fleet-mesh/meshes/discovered/:meshID`) — overview, control planes with filter/search, and aggregated conditions for meshID-grouped Istio CRs not managed by a `MultiClusterMesh`
- **Control Planes list page** (`/fleet-mesh/control-planes`) — discovers all sail-operator `Istio` CRs across managed clusters via ACM Search, enriched with version, meshID, and health status. Mesh ID column links to the managing mesh or discovered mesh detail page.
- **Control Plane detail page** (`/fleet-mesh/control-planes/:cluster/:name`) — per-cluster control plane details (meshID, network, namespace, version, conditions) with inline links to the managing or discovered mesh
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
