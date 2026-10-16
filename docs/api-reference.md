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
| `status.controlPlaneNamespace` | Auto-derived control plane namespace: `istio-system-{mesh name truncated to 39 characters}-{first 10 hex characters of SHA-256(namespace/name)}` |
| `status.conditions` | Mesh-level conditions (see below) |
| `status.clusterStatus` | Entries are created for resolved clusters, including when no issuer is configured. Existing entries are retained while their names remain selected, even if their ManagedCluster objects are missing. `TrustDistributed` is added or updated only while an issuer is configured; removing the issuer does not clear an existing trust condition. |

### Status Conditions

| Condition | Scope | Meaning |
|-----------|-------|---------|
| `Ready` | Mesh | After successful reconciliation, `True` when every resolved cluster's cacerts ManifestWork reports `Applied=True` and `Available=True`. |
| `TrustDistributed` | Per-cluster | The controller's last observation of the cacerts ManifestWork's `Applied` and `Available` conditions. Updated only while an issuer is configured. |

**Reason values** (appear in a condition's `.reason`):

| Reason | Scope | Description |
|--------|-------|-------------|
| `AllClustersReady` | Mesh | All clusters are ready |
| `ClustersNotReady` | Mesh | One or more clusters do not yet have trust distributed |
| `PlacementNotFound` | Mesh | The referenced Placement does not exist. Existing mesh plumbing is left in place. |
| `NoClustersSelected` | Mesh | The Placement exists. If it selected zero clusters, mesh plumbing is removed. If its decisions still name clusters but every corresponding ManagedCluster object is missing, namespace ManifestWorks and ManagedServiceAccounts for those names are kept. |
| `ReconcileError` | Mesh | An error occurred during reconciliation |
| `CacertsApplied` | Per-cluster | Cacerts are distributed and available on the cluster |
| `CacertsPending` | Per-cluster | Cacerts have not yet been distributed to the cluster |

### Readiness Limits

`Ready` does not verify the service mesh operator, Istio control planes, discovery-token availability, remote-secret rollout, or cross-cluster connectivity. Check those separately before using the mesh.

The current controller checks only resolved ManagedCluster objects and accepts true ManifestWork conditions without comparing their `observedGeneration` with the work's generation. A partially missing selection or an updated cacerts payload can therefore leave `Ready=True` before all desired work is current.

Clearing the issuer name deletes Certificates but retains cacerts ManifestWorks on still-selected clusters and does not clear earlier `TrustDistributed` conditions. Certificate Secret retention depends on cert-manager configuration. See [lifecycle behavior](dev/design.md#lifecycle-events) and [troubleshooting](troubleshooting.md).

<!-- Reference links -->
[Placement]: https://open-cluster-management.io/docs/concepts/content-placement/placement/
