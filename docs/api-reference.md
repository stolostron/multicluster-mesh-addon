# API Reference

For ready-to-use examples, see the [samples/](../samples/) directory.

### Status Conditions

| Condition | Scope | Meaning |
|-----------|-------|---------|
| `Ready` | Mesh | All clusters have confirmed operator installation |
| `OperatorInstalled` | Per-cluster | The service mesh operator CSV is installed on this cluster |

**Reason values** (appear in a condition's `.reason`):

| Reason | Scope | Description |
|--------|-------|-------------|
| `AllClustersReady` | Mesh | All clusters are ready |
| `ClustersNotReady` | Mesh | One or more clusters have not confirmed operator installation |
| `ReconcileError` | Mesh | An error occurred during reconciliation |
| `OperatorConfigConflict` | Mesh | Operator config conflicts with an older mesh on the same ClusterSet |
| `NamespaceConflict` | Mesh | Control plane namespace conflicts with an older mesh or equals the operator namespace |
| `InstallationPending` | Per-cluster | Operator installation has been requested but not yet confirmed |
| `Installed` | Per-cluster | Operator is installed |

## MultiClusterMesh

`MultiClusterMesh` is a namespaced resource.
The namespace provides tenant isolation on the hub.

API group: `mesh.open-cluster-management.io/v1alpha1`

### Spec Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `spec.clusterSet` | Yes | | Name of the ManagedClusterSet defining cluster membership. Must use `ExclusiveClusterSetLabel` selector. Immutable after creation. |
| `spec.controlPlane.namespace` | No | `istio-system` | Namespace where Istio is installed on each cluster. Immutable after creation. Must differ from `spec.operator.namespace`. |
| `spec.operator.name` | No | `servicemeshoperator3` | OLM package name |
| `spec.operator.namespace` | No | `multicluster-mesh-operator` | Namespace where the operator is installed. Must not use `openshift-`, `kube-`, or `default`. |
| `spec.operator.channel` | No | `stable` | OLM subscription channel |
| `spec.operator.source` | No | `redhat-operators` | CatalogSource name |
| `spec.operator.sourceNamespace` | No | `openshift-marketplace` | CatalogSource namespace |
| `spec.operator.startingCSV` | No | | Pin to a specific operator version |
| `spec.operator.installPlanApproval` | No | `Automatic` | `Automatic` or `Manual` |
| `spec.security.trust.certManager.issuerRef.name` | No | | cert-manager Issuer name for Root CA |
| `spec.security.trust.certManager.issuerRef.kind` | No | `Issuer` | `Issuer` or `ClusterIssuer` |
| `spec.security.discovery.tokenValidity` | No | `360h` | ManagedServiceAccount token lifetime (minimum: `10m`) |
