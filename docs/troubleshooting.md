# Troubleshooting

All commands run on the hub cluster unless noted otherwise.

## Checking Status

Mesh status and per-cluster conditions (see [API reference](api-reference.md#status-conditions) for condition and reason definitions):

```bash
kubectl get multiclustermesh -n <mesh-namespace> -o yaml
```

OCM cluster and ClusterSet status:

```bash
clusteradm get clusters
clusteradm get clustersets
```

## Inspecting Addon Resources

ManifestWorks created by the addon (operator, cacerts, control plane namespace, istio-reader RBAC):

```bash
kubectl get manifestwork -A -l app.kubernetes.io/managed-by=multicluster-mesh-addon
```

Resources owned by a specific mesh:

```bash
kubectl get manifestwork -A -l mesh.open-cluster-management.io/mesh-name=<mesh-name>
```

Certificates and secrets:

```bash
kubectl get certificate -n <mesh-namespace>
kubectl get secret -n <mesh-namespace> -l mesh.open-cluster-management.io/mesh-name=<mesh-name>
```

ManagedServiceAccounts (endpoint discovery credentials):

```bash
kubectl get managedserviceaccount -A -l mesh.open-cluster-management.io/mesh-name=<mesh-name>
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

### Nothing happens after creating a mesh

The addon only works with ManagedClusterSets using the `ExclusiveClusterSetLabel` selector (the default type).
Clusters are assigned to a set by the `cluster.open-cluster-management.io/clusterset` label.

Check that the ClusterSet exists and has clusters assigned:

```bash
clusteradm get clustersets
```

If no clusters have the label, the addon has nothing to reconcile.

### Operator not installing on a spoke cluster

Check the ManifestWork status (on the hub):

```bash
kubectl get manifestwork -n <cluster-name> multicluster-mesh-operator -o yaml
```

Look at `.status.conditions` for errors.
Common causes:
- OLM not installed on the managed cluster.
- Subscription misconfiguration: wrong package name, channel, CatalogSource, or source namespace
  (e.g., `redhat-operators` only exists on OpenShift; for [kind]/vanilla K8s, override `spec.operator` fields).
- OCM issues: work agent not running, cluster not accepted, or cluster unreachable.

### No cacerts secret on spoke clusters (TrustDistributed=False)

When `spec.security.trust` is configured, the addon creates cert-manager `Certificate` resources on the hub,
which produce `Secret` resources that get distributed to spokes via `ManifestWork` resources.

The `TrustDistributed` condition tracks whether this chain completed successfully, per spoke cluster.

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

### Mesh shows OperatorConfigConflict

Two meshes targeting the same ClusterSet have different `spec.operator` settings.
The oldest mesh (by creation timestamp) takes precedence.

Options:
1. Update `spec.operator` on the newer mesh to match.
2. Delete one of the conflicting meshes.

### Mesh shows NamespaceConflict

Either:
- Two meshes targeting the same ClusterSet use the same `spec.controlPlane.namespace` (the oldest mesh takes precedence).
- The mesh's `spec.controlPlane.namespace` equals its `spec.operator.namespace`.

Use a different control plane or operator namespace to resolve.

### No remote secrets on spoke clusters

The addon creates ManagedServiceAccounts to obtain tokens, grants istio-reader RBAC via ManifestWork, and distributes remote secrets to peer clusters via ManifestWorkReplicaSet.

Check each step:

```bash
# ManagedServiceAccount created?
kubectl get managedserviceaccount -A -l mesh.open-cluster-management.io/mesh-name=<mesh-name>

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
kubectl get namespace istio-system -o jsonpath='{.metadata.labels.topology\.istio\.io/network}'
```

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
