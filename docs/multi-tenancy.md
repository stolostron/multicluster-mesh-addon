# Multi-Tenancy

## Table of Contents

- [Overview](#overview)
- [Shared Clusters, Separate Control Planes](#shared-clusters-separate-control-planes)
- [Hub Namespace Isolation](#hub-namespace-isolation)
- [Data-Plane Isolation](#data-plane-isolation)
- [Conflict Detection and Resolution](#conflict-detection-and-resolution)

## Overview

Multiple `MultiClusterMesh` resources can target the same [ManagedClusterSet], running independent meshes on the same pool of clusters. Isolation between meshes is achieved through:

1. **Separate control plane namespaces** - Each mesh uses a separate control plane namespace on spoke clusters (users create the `Istio` CRs themselves; the add-on manages operator installation and mesh plumbing)
2. **Namespace-scoped CRD** - `MultiClusterMesh` is namespace-scoped on the hub, enabling RBAC-based tenant boundaries
3. **Per-mesh plumbing** - Each mesh gets its own trust domain, intermediate CA certificates, and discovery tokens

The Sail/OSSM operator is a cluster-scoped singleton shared across meshes. The add-on installs it when the first mesh needs a cluster and removes it only when no mesh targets that cluster anymore.

## Shared Clusters, Separate Control Planes

Multiple meshes on the same ClusterSet must use different `controlPlane.namespace` values. Each mesh operates independently (its certificates, discovery tokens, and trust domain are scoped to its own control plane namespace).

### Prerequisites

A [ManagedClusterSet] with clusters labeled to join it:

```bash
clusteradm create clusterset shared-cluster-set
clusteradm clusterset set shared-cluster-set --clusters cluster1,cluster2
```

### Hub namespaces

Each team gets a dedicated hub namespace.

> **Note:** The add-on controller lists clusters by their ClusterSet label directly. A `ManagedClusterSetBinding` is not required for the add-on to function, though you may want one if using Placements alongside the mesh.

```bash
kubectl create namespace mesh-team-a
kubectl create namespace mesh-team-b
```

### cert-manager trust chain

Each namespace needs its own trust chain (self-signed issuer -> root CA certificate -> CA-backed issuer). The add-on uses the CA-backed issuer to mint intermediate CAs per cluster.

Apply the trust chain from [`samples/cert-manager-issuer.yaml`](../samples/cert-manager-issuer.yaml) in each team namespace.  Optionally adjust the `commonName` to distinguish each team's root CA (e.g., `Mesh Root CA - Team A`):

```bash
kubectl apply -f samples/cert-manager-issuer.yaml -n mesh-team-a
kubectl apply -f samples/cert-manager-issuer.yaml -n mesh-team-b
```

### MultiClusterMesh resources

Team A, control plane in `istio-system-team-a`:

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: team-a-mesh
  namespace: mesh-team-a
spec:
  clusterSet: shared-cluster-set
  controlPlane:
    namespace: istio-system-team-a
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
```

Team B, control plane in `istio-system-team-b`:

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: team-b-mesh
  namespace: mesh-team-b
spec:
  clusterSet: shared-cluster-set
  controlPlane:
    namespace: istio-system-team-b
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
```

Both meshes share the operator installation on each cluster. Trust domains (`team-a-mesh`, `team-b-mesh`) and discovery tokens are independent. Although both meshes reference `issuerRef.name: mesh-root-ca`, the issuerRef resolves in each mesh's hub namespace, so each team's `mesh-root-ca` is a different issuer backed by a different root CA.

## Hub Namespace Isolation

Since `MultiClusterMesh` is namespace-scoped, standard Kubernetes RBAC restricts which teams can create or modify mesh resources. The controller reconciles meshes across all namespaces and enforces conflict rules globally.

Define a `ClusterRole` once and bind it per namespace via `RoleBinding`:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mesh-admin
rules:
- apiGroups: ["mesh.open-cluster-management.io"]
  resources: ["multiclustermeshes"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: team-a-mesh-admin
  namespace: mesh-team-a
subjects:
- kind: Group
  name: team-a
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: mesh-admin
  apiGroup: rbac.authorization.k8s.io
```

Repeat the `RoleBinding` for `mesh-team-b` with the appropriate subject.

> In practice, scope full CRUD access to platform engineers and grant the rest of the team read-only (`get`, `list`, `watch`) access.

## Data-Plane Isolation

The add-on handles control-plane plumbing. Data-plane isolation between co-located meshes requires user-side configuration on each spoke cluster.

> The following configuration is applied directly on each spoke cluster (not on the hub).

### `discoverySelectors`

Each Istio control plane discovers services across all namespaces by default. Configure `discoverySelectors` on each spoke cluster to restrict visibility to the mesh's own application namespaces:

```yaml
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: team-a-istio
  namespace: istio-system-team-a
spec:
  values:
    meshConfig:
      discoverySelectors:
      - matchLabels:
          mesh: team-a
```

Repeat for Team B with `mesh: team-b`.

Label application namespaces on each spoke cluster to match:

```bash
kubectl label namespace app-frontend mesh=team-a
kubectl label namespace app-backend mesh=team-a
```

## Conflict Detection and Resolution

The controller validates all meshes targeting the same ClusterSet on every reconciliation. The older mesh (by creation timestamp) takes precedence; newer conflicting meshes are blocked.

### Namespace conflict

Two meshes on the same ClusterSet cannot use the same `controlPlane.namespace`:

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: conflicting-mesh
  namespace: mesh-team-b
spec:
  clusterSet: shared-cluster-set
  controlPlane:
    namespace: istio-system-team-a  # already used by team-a-mesh
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
```

Resulting status on the newer mesh:

```yaml
status:
  conditions:
  - type: Ready
    status: "False"
    reason: NamespaceConflict
    message: 'controlPlane.namespace "istio-system-team-a" conflicts with older mesh
      mesh-team-a/team-a-mesh targeting the same ClusterSet shared-cluster-set'
```

### Operator configuration conflict

Meshes on the same ClusterSet must have compatible operator configurations (channel, source, namespace, etc.) since they share a single operator installation:

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: conflicting-operator-mesh
  namespace: mesh-team-b
spec:
  clusterSet: shared-cluster-set
  controlPlane:
    namespace: istio-system-team-b
  operator:
    channel: "3.0"  # conflicts with team-a-mesh (defaults to "stable")
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
```

Resulting status on the newer mesh:

```yaml
status:
  conditions:
  - type: Ready
    status: "False"
    reason: OperatorConfigConflict
    message: 'operator config conflicts with older mesh mesh-team-a/team-a-mesh
      targeting the same ClusterSet shared-cluster-set'
```

### Resolution

| Conflict type | Resolution |
|---------------|------------|
| Namespace | Change the newer mesh's `controlPlane.namespace` to a unique value, or delete the older mesh |
| Operator config | Align operator configuration across all meshes on the ClusterSet, or delete the older mesh |

When the blocking mesh is deleted, the controller automatically unblocks and reconciles the previously-blocked mesh.

<!-- Reference links -->
[ManagedClusterSet]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/
