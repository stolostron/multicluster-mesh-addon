# Multi-Tenancy

## Table of Contents

- [Overview](#overview)
- [Shared Clusters, Separate Control Planes](#shared-clusters-separate-control-planes)
- [Hub Namespace Isolation](#hub-namespace-isolation)
- [Data-Plane Isolation](#data-plane-isolation)
- [Conflict Detection and Resolution](#conflict-detection-and-resolution)

## Overview

Multiple `MultiClusterMesh` resources can target the same [ManagedClusterSet], running independent meshes on the same pool of clusters. Isolation between meshes is achieved through:

1. **Separate control plane namespaces** - Each mesh installs its Istio control plane in a distinct namespace on spoke clusters
2. **Namespace-scoped CRD** - `MultiClusterMesh` is namespace-scoped on the hub, enabling RBAC-based tenant boundaries
3. **Per-mesh plumbing** - Each mesh gets its own trust domain, intermediate CA certificates, and discovery tokens

The Sail/OSSM operator is a cluster-scoped singleton shared across meshes. The add-on installs it when the first mesh needs a cluster and removes it only when no mesh targets that cluster anymore.

## Shared Clusters, Separate Control Planes

Multiple meshes on the same ClusterSet must use different `controlPlane.namespace` values. Each mesh operates independently (its certificates, discovery tokens, and trust domain are scoped to its own control plane namespace).

### Prerequisites

A [ManagedClusterSet] with clusters labeled to join it:

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: shared-cluster-set
spec:
  clusterSelector:
    selectorType: ExclusiveClusterSetLabel
```

```bash
kubectl label managedcluster cluster1 cluster.open-cluster-management.io/clusterset=shared-cluster-set
kubectl label managedcluster cluster2 cluster.open-cluster-management.io/clusterset=shared-cluster-set
```

### Hub namespaces

Each team gets a dedicated hub namespace:

```bash
kubectl create namespace mesh-team-a
kubectl create namespace mesh-team-b
```

### cert-manager trust chain

Each namespace needs its own trust chain (self-signed issuer → root CA certificate → CA-backed issuer). The add-on uses the CA-backed issuer to mint intermediate CAs per cluster.

Team A (`mesh-team-a`):

```yaml
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
  namespace: mesh-team-a
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: mesh-root-ca
  namespace: mesh-team-a
spec:
  isCA: true
  commonName: Team A Mesh Root CA
  secretName: mesh-root-ca-secret
  duration: 87600h
  renewBefore: 720h
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: mesh-root-ca
  namespace: mesh-team-a
spec:
  ca:
    secretName: mesh-root-ca-secret
```

Repeat for `mesh-team-b`.

### MultiClusterMesh resources

Team A — control plane in `istio-system-team-a`:

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

Team B — control plane in `istio-system-team-b`:

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

Both meshes share the operator installation on each cluster. Trust domains (`team-a-mesh`, `team-b-mesh`) and discovery tokens are independent.

## Hub Namespace Isolation

Since `MultiClusterMesh` is namespace-scoped, standard Kubernetes RBAC restricts which teams can create or modify mesh resources. The controller reconciles meshes across all namespaces and enforces conflict rules globally.

Example RBAC for Team A:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: mesh-admin
  namespace: mesh-team-a
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
  kind: Role
  name: mesh-admin
  apiGroup: rbac.authorization.k8s.io
```

Repeat for `mesh-team-b` with the appropriate subject.

## Data-Plane Isolation

The add-on handles control-plane plumbing. Data-plane isolation between co-located meshes requires user-side configuration on each spoke cluster.

### `discoverySelectors`

Each Istio control plane discovers services across all namespaces by default. Configure `discoverySelectors` to restrict visibility to the mesh's own application namespaces:

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

Label application namespaces to match:

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
    channel: "3.0"  # conflicts with team-a-mesh using "stable"
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
