# Troubleshooting

All commands run on the hub cluster unless noted otherwise.

## Checking Status

Mesh status and per-cluster conditions (see [API reference](api-reference.md#status-conditions) for condition and reason definitions):

```bash
kubectl get multiclustermesh -n <mesh-namespace> -o yaml
```

OCM cluster status and Placement decisions:

```bash
clusteradm get clusters
kubectl get placementdecision -n <mesh-namespace> -l cluster.open-cluster-management.io/placement=<placement-name>
```

## Inspecting Addon Resources

ManifestWorks created by the addon (cacerts, control plane namespace, istio-reader RBAC):

```bash
kubectl get manifestwork -A -l app.kubernetes.io/managed-by=multicluster-mesh-addon
```

Resources owned by a specific mesh:

```bash
kubectl get manifestwork -A -l 'mesh.open-cluster-management.io/mesh-name=<mesh-name>,mesh.open-cluster-management.io/mesh-namespace=<mesh-namespace>'
```

Certificates and secrets:

```bash
kubectl get certificate -n <mesh-namespace>
kubectl get secret -n <mesh-namespace> -l mesh.open-cluster-management.io/mesh-name=<mesh-name>
```

ManagedServiceAccounts (endpoint discovery credentials):

```bash
kubectl get managedserviceaccount -A -l 'mesh.open-cluster-management.io/mesh-name=<mesh-name>,mesh.open-cluster-management.io/mesh-namespace=<mesh-namespace>'
```

ManifestWorkReplicaSet (remote secret distribution):

```bash
kubectl get manifestworkreplicaset -n <mesh-namespace> <mesh-name> -o yaml
```

Controller logs:

```bash
kubectl logs -n multicluster-mesh-system deploy/multicluster-mesh-controller -f
```

## Common Issues

### Controller does not start

The controller requires the Certificate and ManagedServiceAccount CRDs on the hub even when no mesh uses cert-manager trust. Missing ones produce `prerequisite CRDs not installed on the hub` in the controller logs. Install the prerequisite controllers and CRDs before deploying the addon:

```bash
kubectl get crd certificates.cert-manager.io managedserviceaccounts.authentication.open-cluster-management.io
```

OCM's Placement, PlacementDecision, ManifestWork, and ManifestWorkReplicaSet APIs must also be available. Enable the ManifestWorkReplicaSet feature on the ClusterManager and ensure the ManagedServiceAccount addon is available on each selected spoke.

### Nothing happens after creating a mesh

The addon reads cluster membership from PlacementDecision resources.
Check that the referenced Placement exists, has selected clusters, and that PlacementDecisions have been created:

```bash
kubectl get placement -n <mesh-namespace> <placement-name>
kubectl get placementdecision -n <mesh-namespace> -l cluster.open-cluster-management.io/placement=<placement-name>
```

If the Placement doesn't exist, the mesh reports `PlacementNotFound` and leaves existing plumbing in place.
If the Placement exists and has selected zero clusters, the mesh reports `NoClustersSelected` and removes that plumbing after the removal grace period.
The same reason is set when `PlacementDecision` objects still name clusters but every corresponding `ManagedCluster` object is missing; namespace ManifestWorks and ManagedServiceAccounts for those names stay until the names leave the decisions and the removal grace period expires.
See [Mesh shows NoClustersSelected](#mesh-shows-noclustersselected).

### Mesh shows ClustersNotReady

When trust is configured (cert-manager issuer is set), mesh waits for cacerts ManifestWork to report `Applied=True` for its current generation and `Available=True` on every managed cluster before becoming `Ready`.
After certificate rotation, mesh can temporarily report `ClustersNotReady` until updated trust material is applied.
Existing traffic can continue using valid older material during this interval.

Check the per-cluster `TrustDistributed` condition in the mesh status:

```bash
kubectl get multiclustermesh -n <mesh-namespace> <mesh-name> -o jsonpath='{.status.clusterStatus}' | jq .
```

If a cluster shows `CertificatePending`, the CA certificate has not been issued yet. Start with steps 1 and 2.
If a cluster shows `DistributionPending`, distribution has started. Start with steps 3 and 4.

Check each step:

1. **Certificate created and ready?**
   ```bash
   kubectl get certificate -n <mesh-namespace> -l 'mesh.open-cluster-management.io/mesh-name=<mesh-name>,mesh.open-cluster-management.io/cluster-name=<cluster-name>'
   ```
2. **Cacerts ManifestWork exists?**
   ```bash
   kubectl get manifestwork -n <cluster-name> -l 'mesh.open-cluster-management.io/mesh-name=<mesh-name>,mesh.open-cluster-management.io/mesh-namespace=<mesh-namespace>'
   ```
3. **ManifestWork applied and Secret available on spoke?** Check `.status.conditions` for `Applied=True` and `Available=True`. Applied condition's `observedGeneration` must equal ManifestWork's `metadata.generation`.
4. **Work agent running on the managed cluster?** The OCM work agent must be running to apply ManifestWorks.

### Mesh is Ready but discovery or Istio is not working

Ready does not check operator installation, Istio health, discovery-token availability, remote-secret rollout, or cluster reachability. Without an issuer, it does not wait for any spoke work to be applied. Selected names whose ManagedCluster objects are missing are excluded from readiness checks when other clusters still resolve.

### No cacerts secret on spoke clusters (TrustDistributed=False)

When `spec.security.trust` is configured, addon creates cert-manager `Certificate` resources on hub.
These produce `Secret` resources distributed to spokes via `ManifestWork` resources.

`TrustDistributed` tracks whether this chain completed for each spoke cluster.

Check each step:

```bash
# Certificate created?
kubectl get certificate -n <mesh-namespace>

# Certificate ready?
kubectl get certificate -n <mesh-namespace> -o jsonpath='{.items[*].status.conditions}'

# Issuer exists and is ready?
kubectl get issuer -n <mesh-namespace>
# Or if using a ClusterIssuer:
kubectl get clusterissuer

# Cacerts ManifestWork applied to the spoke?
kubectl get manifestwork -n <cluster-name> multicluster-mesh-cacerts-<mesh-namespace>.<mesh-name> -o yaml
```

Common causes:
- cert-manager not installed or not running on the hub.
- The `Issuer` (or `ClusterIssuer`) referenced in `spec.security.trust.certManager.issuerRef` doesn't exist or isn't ready.
- cert-manager failed to issue the certificate (check cert-manager controller logs for details).
- The cacerts ManifestWork exists but hasn't been applied on the spoke (e.g., the control plane namespace doesn't exist on the spoke cluster).

### Mesh shows PlacementNotFound

The Placement referenced by `spec.placementRef.name` does not exist in the mesh's namespace.
Existing control plane namespaces, Certificates, cacerts, ManagedServiceAccounts, and remote secrets are left in place.
`status.clusterStatus` is cleared until the Placement exists again.
Create the Placement or fix the reference.

### Mesh shows NoClustersSelected

The Placement exists but the mesh has no members.
This has two underlying situations, and the controller reports both the same way.

The Placement selected zero clusters: `status.numberOfSelectedClusters` is 0 and the `PlacementDecision` objects agree.
Reconciliation removes all mesh plumbing after the removal grace period, including namespace ManifestWorks, istio-reader RBAC, intermediate CA Certificates, cacerts works, and ManagedServiceAccounts.
The ReplicaSet remains with an empty manifest list; OCM removes its remote-secret resources asynchronously.
The MultiClusterMesh remains.
The same cleanup can occur before a new or recreated Placement has produced its first scheduling result, and without the grace period, because the mesh status no longer tracks the clusters.

`PlacementDecision` objects still name clusters, but every corresponding `ManagedCluster` object is gone.
Those names stay in desired membership, preserving their ManifestWorks and ManagedServiceAccounts, plus their Certificates while an issuer remains configured.
The remote-secret template omits unresolved clusters.
Cleanup runs after the Placement controller removes their names from decisions and the removal grace period expires.

Common causes of an empty selection:

1. **Missing ManagedClusterSetBinding** - Placements only select from ClusterSets bound to the namespace.
   Check that a `ManagedClusterSetBinding` exists in the mesh namespace:
   ```bash
   kubectl get managedclustersetbinding -n <mesh-namespace>
   ```
2. **Clusters not in a ClusterSet** - each cluster needs the `cluster.open-cluster-management.io/clusterset` label
   to be a member of the bound ClusterSet.
3. **Predicate mismatch** - the Placement's label selector doesn't match any clusters.

### Cluster shows PlacementSelected=False with RemovalPending

The cluster left the `Placement` selection.
Its resources are preserved for the removal grace period (30 seconds by default) and then deleted: namespace ManifestWork, istio-reader ManifestWork, intermediate CA Certificate, cacerts work, and ManagedServiceAccount, plus the cluster's remote-secret manifest in the ReplicaSet.
The spoke-side remote secret is not preserved: the ReplicaSet selects clusters through the mesh's Placement, and OCM deletes the spoke work as soon as the Placement stops selecting the cluster.
Only the hub-side ReplicaSet template survives the grace period.
The `clusterStatus` entry is pruned after the deletion.

This state is expected for about 30 seconds after removing a cluster from the `Placement`.
It also appears briefly when OCM redistributes `PlacementDecision` objects, for example when the selection grows past the 100-cluster page boundary and a cluster moves between `PlacementDecision` objects; the cluster then reappears in the decisions, the condition clears, and the removal is cancelled.

Check the selection:

```bash
kubectl get placementdecision -n <mesh-namespace> -l cluster.open-cluster-management.io/placement=<placement-name>
```

If the cluster is still listed in a `PlacementDecision` but keeps reporting `RemovalPending`, the deduplicated decision set does not match `status.numberOfSelectedClusters`, which means the Placement controller has not finished writing the `PlacementDecision` objects.
The mesh reconciles once the counts agree.

Deleting the `MultiClusterMesh` does not wait for the grace period; the finalizer removes labeled resources directly.

### No remote secrets on spoke clusters

The addon creates ManagedServiceAccounts to obtain tokens, grants istio-reader RBAC via ManifestWork, and distributes remote secrets to peer clusters via ManifestWorkReplicaSet.

Check each step:

```bash
# ManagedServiceAccount created?
kubectl get managedserviceaccount -A -l 'mesh.open-cluster-management.io/mesh-name=<mesh-name>,mesh.open-cluster-management.io/mesh-namespace=<mesh-namespace>'

# Token secret synced back?
kubectl get managedserviceaccount -n <cluster-name> -o jsonpath='{.items[*].status.tokenSecretRef}'

# ManifestWorkReplicaSet created for remote secrets?
kubectl get manifestworkreplicaset -n <mesh-namespace> <mesh-name> -o yaml
```

Common causes:
- [ManagedServiceAccount][msa-addon] addon not installed on the hub.
  The token secret won't appear until the MSA controller processes the request.
- The `ManifestWorkReplicaSet` feature gate is not enabled on the hub's `ClusterManager`.

### Cross-cluster traffic not working

This can be an addon issue or an Istio configuration issue.
For general Istio multicluster troubleshooting, see the [Istio multicluster troubleshooting guide][istio-mc-troubleshoot].

#### Network label mismatch

The addon labels the control plane namespace with `topology.istio.io/network` (defaults to the cluster name).
The Istio CR's `global.network` and the east-west gateway's `topology.istio.io/network` label must match.

Check what the addon set on the control plane namespace (run on the spoke cluster):

```bash
kubectl get namespace <cp-namespace> -o jsonpath='{.metadata.labels.topology\.istio\.io/network}'
```

The control plane namespace name is reported in `status.controlPlaneNamespace` on the mesh CR.

Check what the ManagedCluster has on the hub (if set, the addon uses this instead of the cluster name):

```bash
kubectl get managedcluster <cluster-name> -o jsonpath='{.metadata.labels.topology\.istio\.io/network}'
```

To override the default network identity, label the ManagedCluster on the hub:

```bash
kubectl label managedcluster <cluster-name> topology.istio.io/network=<network>
```

After changing the label, the addon updates the namespace label on the next reconciliation.
Reapply the Istio CR and east-west gateway with the matching `NETWORK` value.

<!-- Reference links -->
[istio-mc-troubleshoot]: https://istio.io/latest/docs/ops/diagnostic-tools/multicluster/
[kind]: https://kind.sigs.k8s.io/
[msa-addon]: https://open-cluster-management.io/docs/getting-started/integration/managed-serviceaccount/
