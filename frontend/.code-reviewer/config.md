---
base_branch: main
languages:
  - typescript
  - tsx
key_paths:
  - src/components/
  - src/hooks/
  - src/types/
  - src/utils/
  - src/__mocks__/
  - console-extensions.ts
  - console-plugin-metadata.ts
---

OpenShift Console dynamic plugin (`plugin__ossm-acm`) for multicluster service mesh management. Built with React 18, PatternFly 6, Console Plugin SDK, and @stolostron/multicluster-sdk. Provides fleet-wide visibility into Istio control planes, MultiClusterMesh resources, trust status, and mesh health across managed clusters. Supports both MCM-managed meshes and discovered (unmanaged) Istio meshes in a unified list view.
