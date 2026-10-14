# API Reference

For ready-to-use examples, see the [samples/](../samples/) directory.

## MultiClusterMesh

`MultiClusterMesh` is a namespaced resource.
The namespace provides tenant isolation on the hub.

API group: `mesh.open-cluster-management.io/v1alpha1`

### Spec Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `metadata.name` | Yes | | Mesh name. Must be a DNS label: lowercase alphanumeric characters or `-`, starting and ending with an alphanumeric character, at most 63 characters. The name is embedded in the control plane namespace (which cannot contain dots) and is used as the SPIFFE trust domain. |
| `spec.placementRef.name` | Yes | | Name of the [Placement] defining cluster membership. The Placement must be in the same namespace as the mesh. |
| `spec.security.trust.certManager.issuerRef.name` | No | | cert-manager Issuer name for Root CA |
| `spec.security.trust.certManager.issuerRef.kind` | No | `Issuer` | `Issuer` or `ClusterIssuer` |
| `spec.security.discovery.tokenValidity` | No | `360h` | ManagedServiceAccount token lifetime (minimum: `10m`) |

### Status Fields

| Field | Description |
|-------|-------------|
| `status.controlPlaneNamespace` | Auto-derived control plane namespace: `istio-system-{meshName}-{SHA-256 prefix}` |
| `status.conditions` | Mesh-level conditions (see below) |
| `status.clusterStatus` | Per-cluster conditions (see below) |

### Status Conditions

| Condition | Scope | Meaning |
|-----------|-------|---------|
| `Ready` | Mesh | All clusters have trust distributed (or no trust is configured) |
| `TrustDistributed` | Per-cluster | The cacerts ManifestWork has been applied and is available on this cluster |

**Reason values** (appear in a condition's `.reason`):

| Reason | Scope | Description |
|--------|-------|-------------|
| `AllClustersReady` | Mesh | All clusters are ready |
| `ClustersNotReady` | Mesh | One or more clusters do not yet have trust distributed |
| `PlacementNotFound` | Mesh | The referenced Placement does not exist |
| `NoClustersSelected` | Mesh | The Placement has not selected any clusters |
| `ReconcileError` | Mesh | An error occurred during reconciliation |
| `CacertsApplied` | Per-cluster | Cacerts are distributed and available on the cluster |
| `CacertsPending` | Per-cluster | Cacerts have not yet been distributed to the cluster |

<!-- Reference links -->
[Placement]: https://open-cluster-management.io/docs/concepts/cluster-inventory/placement/
