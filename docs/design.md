# Design

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Scope](#scope)
- [Supported Topologies](#supported-topologies)
- [Custom Resource](#custom-resource)
- [Cluster Selection and Multi-Tenancy](#cluster-selection-and-multi-tenancy)
- [Operator Lifecycle](#operator-lifecycle)
- [Trust Distribution](#trust-distribution)
- [Endpoint Discovery](#endpoint-discovery)
- [Control Plane Management](#control-plane-management)
- [Topology Support](#topology-support)
- [Lifecycle Events](#lifecycle-events)
- [Known Limitations](#known-limitations)
- [Phased Approach](#phased-approach)

## Overview

The OCM Service Mesh Add-on automates multi-cluster Istio service mesh setup via [OCM]. It manages the `MultiClusterMesh` custom resource on the hub cluster to orchestrate three concerns across managed clusters:

1. **Operator Lifecycle** - Installing and managing the service mesh operator ([OSSM]/[Sail])
2. **Trust Distribution** - Establishing mTLS trust via [cert-manager]
3. **Endpoint Discovery** - Exchanging discovery credentials via [ManagedServiceAccount]

Without this add-on, multi-cluster mesh setup is a manual process involving certificate management, O(N^2) secret exchanges, and per-cluster operator configuration.

## Architecture

The add-on follows OCM's hub-and-spoke model:

- **Hub**: The Mesh Add-on controller watches `MultiClusterMesh` resources and creates [ManifestWorks][ManifestWork], orchestrates cert-manager and ManagedServiceAccount
- **Spoke** (managed clusters): Receives ManifestWorks from the hub, runs the service mesh operator and Istio control plane

A [ClusterManagementAddOn] resource is deployed to register this addon with OCM's addon manager, but the addon uses manual installation strategy and does not leverage the framework's lifecycle management features (auto-deployment, per-cluster enable/disable via `ManagedClusterAddOn`).

```mermaid
flowchart TD
    subgraph Hub Cluster
        mesh([MultiClusterMesh]) --> addon[Mesh Add-on]
        addon --> cert(["Certificate
        (per cluster)"])
        cert --> certmanager[cert-manager]
        addon --> msa(["ManagedServiceAccount
        (per cluster, per mesh)"])
        certmanager --> casecret(["Secret
        (intermediate CA, per cluster)"])
        msa -.->|"token synced
        back by OCM"| tokensecret(["Secret
        (MSA token, per cluster)"])
        addon --> mw_operator(["ManifestWork
        (operator)"])
        addon --> mw_cni(["ManifestWork
        (IstioCNI)"])
        addon --> mw_cp(["ManifestWork
        (Istio CR, per mesh)"])
        addon --> mw_gw(["ManifestWork
        (gateway, per mesh)"])
        casecret --> mw_cacerts(["ManifestWork
        (cacerts)"])
        tokensecret --> mw_remote(["ManifestWork
        (remote secret, per peer)"])
    end

    subgraph Managed Cluster
        agent[Work Agent - OCM] --> subscription(["Subscription
        (sail / OSSM operator)"])
        agent --> istiocni(["IstioCNI"])
        agent --> istiocr(["Istio CR"])
        agent --> gw(["East-West Gateway
        (Deployment + Service + Gateway)"])
        agent --> cacerts(["Secret
        (cacerts)"])
        agent --> remotesecret(["Secret
        (remote secret, per peer)"])
    end

    mw_operator --> agent
    mw_cni --> agent
    mw_cp --> agent
    mw_gw --> agent
    mw_cacerts --> agent
    mw_remote --> agent

    subgraph Legend[Legend #40;colors show managing component#41;]
        L1([Resource]) ~~~ L2[Component]
        C1[Mesh Add-on]:::addon ~~~ C2[cert-manager]:::certmgr ~~~ C3[OCM]:::ocm
    end

    %% fake link to make legend centered
    cacerts ~~~ Legend

    style Legend fill:#d0d0d0,stroke:#999,color:#333
    style C1 stroke:none
    style C2 stroke:none
    style C3 stroke:none

    %% Mesh Add-on managed (blue)
    classDef addon fill:#dbeafe,stroke:#3b82f6,color:#1e3a5f
    class addon,mesh addon

    %% cert-manager managed (green)
    classDef certmgr fill:#d1fae5,stroke:#10b981,color:#064e3b
    class certmanager,cert certmgr

    %% OCM managed (orange)
    classDef ocm fill:#ffedd5,stroke:#f97316,color:#7c2d12
    class agent,msa,mw_operator,mw_cni,mw_cp,mw_gw,mw_cacerts,mw_remote ocm

```


## Scope

### What the add-on does (Plumbing + Configuration)

- Installs the service mesh operator (OSSM by default) on managed clusters via OLM
- Distributes intermediate CA certificates for mTLS trust
- Exchanges discovery tokens between peer clusters
- Creates IstioCNI, Istio CRs, east-west gateways, and remote secrets on managed clusters via ManifestWork
- Handles lifecycle events (cluster add/remove, mesh creation/deletion)

### What the add-on does not do

- Does not patch existing Istio CRs on spoke clusters (this would conflict with ArgoCD/GitOps reconciliation)
- Does not enforce control plane version consistency across clusters
- Does not deploy monitoring, observability, or application workloads
- Does not create AuthorizationPolicies or other application-level security config
- Does not integrate with ACM addon lifecycle (enable/disable via `ManagedClusterAddOn` and such)
- Does not adopt pre-existing mesh deployments (brownfield). Note: the add-on *does* adopt pre-existing operator installations (see [Collision Handling](#collision-handling)). This non-goal refers specifically to adopting an existing mesh configuration and trust root.

## Supported Topologies

The add-on supports two mesh topologies: [Multi-Primary Multi-Network] and Primary-Remote Multi-Network. See [Topology Support](#topology-support) for details on each topology and how they are configured.

## Custom Resource

`MultiClusterMesh` is a namespaced resource. The namespace provides tenant isolation on the hub.
The resource name (`metadata.name`) is limited to 63 characters because it is used in X.509 certificate subject fields and Kubernetes label values.

### Key Fields

| Field | Required | Description |
|-------|----------|-------------|
| `spec.clusterSet` | Yes | Name of the [ManagedClusterSet] defining cluster membership (immutable after creation) |
| `spec.controlPlane.namespace` | No | Namespace where Istio is installed on each cluster (default: `istio-system`) |
| `spec.controlPlane.version` | No | Pins the Istio control plane version (e.g. `v1.28.8`). If empty, the operator selects its default version |
| `spec.operator.name` | No | OLM package name (default: `servicemeshoperator3`) |
| `spec.operator.namespace` | No | Namespace where the operator is installed (default: `openshift-operators`) |
| `spec.operator.channel` | No | OLM subscription channel (default: `stable`) |
| `spec.operator.source` | No | CatalogSource name (default: `redhat-operators`) |
| `spec.operator.sourceNamespace` | No | CatalogSource namespace (default: `openshift-marketplace`) |
| `spec.operator.startingCSV` | No | Pin to a specific operator version |
| `spec.operator.installPlanApproval` | No | `Automatic` or `Manual` (default: `Automatic`) |
| `spec.security.trust.certManager.issuerRef.name` | No | cert-manager Issuer name for Root CA |
| `spec.security.trust.certManager.issuerRef.kind` | No | Kind of the cert-manager issuer (`Issuer` or `ClusterIssuer`, default: `Issuer`) |
| `spec.security.discovery.tokenValidity` | No | ManagedServiceAccount token lifetime (default: `360h`, minimum value: `10m`) |
| `spec.topology.type` | No | Mesh topology: `MultiPrimary` (default) or `PrimaryRemote` |
| `spec.topology.primaryCluster` | No | Name of the primary cluster for `PrimaryRemote` topology. Defaults to first cluster alphabetically. Must be empty for `MultiPrimary` |
| `status.primaryGatewayAddress` | - | LB address (IP or hostname) of the primary cluster's east-west gateway. Populated by the controller for `PrimaryRemote` topology; empty for `MultiPrimary` |

### Example

```yaml
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: prod-mesh
  namespace: mesh-team-a
spec:
  clusterSet: finance-prod
  controlPlane:
    namespace: istio-system
    version: v1.28.8
  operator:
    name: servicemeshoperator3
    channel: "stable"
    source: redhat-operators
    sourceNamespace: openshift-marketplace
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-issuer
          kind: Issuer
    discovery:
      tokenValidity: "168h"
  topology:
    type: MultiPrimary
```

## Cluster Selection and Multi-Tenancy

The add-on uses OCM [ManagedClusterSet] with `ExclusiveClusterSetLabel` as the unit of mesh membership. A cluster can only belong to one ClusterSet at a time.

The `spec.clusterSet` field is immutable after creation. With exclusive ClusterSets, changing the reference means an entirely different set of clusters. All plumbing is cluster-specific, so nothing carries over, making migration equivalent to deleting and recreating the mesh. Users who need a different ClusterSet should delete the mesh CR and create a new one.

`MultiClusterMesh` is namespace-scoped, enabling tenant isolation on the hub. Each mesh operates independently - its certificates, discovery tokens, and operator configuration are scoped to its namespace. Multiple meshes can target the same ClusterSet, provided they use different control plane namespaces. For example, Mesh A targets ClusterSet X with namespace `istio-system-a`, while Mesh B targets the same ClusterSet X with namespace `istio-system-b`. Each mesh gets its own trust domain, certificates, and discovery tokens. If two meshes target the same control plane namespace on the same ClusterSet, the older resource (by creation timestamp) wins and the newer one is rejected.

The add-on defaults to OSSM (OpenShift Service Mesh) operator configuration. All `spec.operator` fields can be overridden to use a different operator (e.g., upstream Sail on non-OCP clusters).

Plumbing resources (ManifestWorks, ManagedServiceAccounts) must use a deterministic naming strategy scoped to the owning mesh, so that multiple meshes on the same cluster don't collide. The operator ManifestWork is an exception - it is shared across meshes since the operator is a cluster-wide singleton. See [#72] for the naming convention discussion.

## Operator Lifecycle

The service mesh operator is a cluster-scoped singleton - only one instance can run per cluster. The operator is therefore a **shared resource** across meshes, not owned by any individual mesh. Multiple meshes targeting the same cluster share the operator installation. Cleanup is scoped to the ClusterSet: when a cluster is no longer needed by any mesh in its ClusterSet, the operator ManifestWork is removed. If the cluster moves to a different ClusterSet with a mesh, the new mesh bootstraps a fresh operator installation with its own configuration.

The add-on follows a **Do No Harm** strategy: it never forcibly uninstalls or downgrades an existing operator. If the operator is already present with a compatible configuration, the add-on adopts it. If there's a conflict (e.g., different channel), the add-on reports an error and halts reconciliation for that cluster.

### Installation Workflow

1. **Pre-existing operator detection**: The controller creates a [ManagedClusterView] to check if a Sail/OSSM Subscription already exists on the managed cluster. This is necessary because ManifestWork claims ownership of any resource it applies, and deleting the ManifestWork would remove a pre-existing Subscription, potentially disrupting other components that depend on it (e.g., OpenShift Gateway API).
2. **Adoption (operator already present)**: If a compatible Subscription is found, the add-on skips ManifestWork creation. If the configuration is incompatible, the add-on reports a conflict.
3. **Installation (operator missing)**: If no Subscription is found, the controller creates a [ManifestWork] containing the OLM objects (Namespace, OperatorGroup, Subscription) using the operator configuration from `spec.operator`.

### Collision Handling

The controller handles two types of collisions:

1. **Hub-side (between meshes)**: If two `MultiClusterMesh` resources target the same cluster but request different operator configurations (e.g., different channels or catalog sources), the oldest mesh (by creation timestamp) takes precedence. Newer meshes with conflicting configs are halted with a `ConfigurationConflict` status.
2. **Spoke-side (pre-existing operator)**: If the ManagedClusterView detects an existing Subscription not created by the add-on, the controller compares the installed configuration against the mesh's `spec.operator`. If compatible, the operator is adopted. If incompatible, the controller halts and reports a `ConfigurationConflict`.

In both cases, the add-on will never forcibly uninstall, downgrade, or overwrite an existing operator. The user must resolve conflicts manually.

The add-on does not validate OpenShift version compatibility with the requested operator channel. It delegates this to OLM - if a cluster's OCP version is incompatible with the requested operator version, the OLM installation will stall, preventing the cluster from joining the mesh with an unsupported control plane.

## Trust Distribution

Trust distribution requires [cert-manager] to be installed on the hub cluster. The user is responsible for setting up cert-manager and creating the `Issuer` or `ClusterIssuer` resource that acts as the Root CA.

The add-on implements Istio's [Plug-in CA] pattern:

1. A cert-manager `Issuer` or `ClusterIssuer` acts as the Root CA (user-provisioned)
2. The add-on creates per-cluster `Certificate` resources, yielding intermediate CAs
3. Intermediate CAs are distributed to managed clusters as `cacerts` secrets in the control plane namespace
4. The root CA private key never leaves the hub

The trust domain is derived from the mesh name (one trust domain per mesh, not per cluster). The controller sets the certificate CN and configures the matching `trustDomain` in the Istio CR automatically. This simplifies multi-cluster mTLS - all clusters in a mesh share the same trust domain, so workloads can authenticate across clusters without additional configuration.

Certificate rotation is handled automatically by cert-manager. Updated certificates are propagated to clusters when they change.

## Endpoint Discovery

For multi-primary mesh topologies, each control plane needs API access to its peers. The add-on automates this using [ManagedServiceAccount]:

1. Creates a `ManagedServiceAccount` per cluster per mesh, yielding short-lived tokens. See [#72] for the naming convention discussion.
2. Constructs kubeconfig-style remote secrets from these tokens
3. Distributes remote secrets to all peer clusters in the mesh
4. Token rotation is handled automatically by the OCM platform
5. When a cluster is removed from the mesh, its MSA is deleted and its remote secrets are removed from all peers

## Control Plane Management

The controller centrally manages the Istio control plane on managed clusters through a phased deployment pipeline. Each phase gates on the previous phase's readiness, reported back to the hub via ManifestWork [FeedbackRules] and [ConditionRules].

### Phase Ordering

1. **Operator** - OLM Subscription applied via ManifestWork; gated on `installedCSV` feedback
2. **IstioCNI** - `IstioCNI` CR applied on all clusters (see below); no readiness gate (uses [CreateOnly] update strategy)
3. **Istio CR** - `Istio` CR applied via ManifestWork; gated on CEL ConditionRule checking `Ready=True` on the Istio status
4. **Gateway** - East-west gateway Deployment, Service, and Istio Gateway resources; gated on LoadBalancer IP/hostname feedback
5. **Remote Secrets** - Cross-cluster discovery secrets; gated on MSA token availability

If any phase is not yet complete, the controller requeues with a 5-minute backoff and skips subsequent phases.

### IstioCNI

The controller creates an `IstioCNI` CR on every managed cluster. CNI is required on OpenShift (where pods cannot run with elevated privileges without it) and recommended for hardened Kubernetes clusters. The IstioCNI ManifestWork is a **shared resource** (like the operator), not scoped to an individual mesh. It uses the [CreateOnly] update strategy so that the first mesh to create it owns the version; subsequent meshes reuse the existing IstioCNI without overwriting it.

### Istio CR

The controller builds an `Istio` CR ([Sail Operator API][Sail]) per mesh per cluster, containing:

- `meshID` derived from the mesh namespace and name
- `clusterName` set to the managed cluster name
- `network` set to `network-<clusterName>` (one network per cluster, multi-network model)
- `trustDomain` set to the mesh name
- `version` from `spec.controlPlane.version` (if specified)
- DNS proxy metadata (`ISTIO_META_DNS_CAPTURE`, `ISTIO_META_DNS_AUTO_ALLOCATE`)
- Topology-specific values: `remotePilotAddress` for remote clusters, `externalIstiod` for primary clusters, and `profile: remote` for remote clusters (see [Topology Support](#topology-support))

The ManifestWork also includes a `ClusterRole` and `ClusterRoleBinding` granting the MSA service account read access to Kubernetes and Istio resources for cross-cluster discovery.

### East-West Gateway

Each cluster gets an east-west gateway consisting of:

- A `ServiceAccount`, `Deployment`, and `Service` (type `LoadBalancer`) for the gateway proxy
- A `cross-network-gateway` Istio Gateway resource with `AUTO_PASSTHROUGH` TLS on port 15443 for cross-network service traffic
- For PrimaryRemote primary clusters: additional `istiod-gateway` and `istiod-vs` VirtualService resources that route remote cluster traffic to the primary's istiod

The gateway Deployment uses sidecar injection (`inject.istio.io/templates: gateway`) with the appropriate revision label and network identity. The Service's LoadBalancer IP/hostname is reported back to the hub via ManifestWork FeedbackRules and used as the readiness signal.

### Readiness Gating

The controller uses two ManifestWork mechanisms for readiness gating:

- **FeedbackRules** (JSONPaths): Extract status values from spoke-side resources and report them to the hub. Used for operator `installedCSV` and gateway `loadBalancer.ingress` address.
- **ConditionRules** (CEL expressions): Evaluate conditions on spoke-side resources and surface them as manifest-level conditions on the ManifestWork status. Used for Istio CR `Ready` condition.

## Topology Support

The add-on supports two mesh topologies, configured via `spec.topology.type`.

### MultiPrimary (default)

Every cluster runs its own independent Istio control plane. All clusters are processed in parallel through the phase pipeline. Remote secrets are exchanged bidirectionally - every cluster gets a remote secret for every other cluster, enabling each control plane to discover services across the mesh.

This aligns with OCM's model where each cluster is autonomous. It provides the highest resilience (no single point of failure for the control plane) at the cost of higher resource usage.

### PrimaryRemote

One cluster is designated as the **primary** and runs the only full control plane. All other clusters are **remotes** that connect to the primary's istiod via `remotePilotAddress` (the primary's east-west gateway LoadBalancer address). Remote clusters use `profile: remote` in their Istio CR.

The controller enforces ordering: the primary cluster must complete all phases (operator → IstioCNI → Istio CR → gateway) and its gateway LoadBalancer must have an address before any remote cluster's Istio CR is created. This ensures remotes can be configured with the correct `remotePilotAddress`.

Remote secrets are still exchanged bidirectionally - the primary needs to discover services on remotes, and remotes need their remote secrets installed for the primary to reach them.

### Configuration Fields

| Field | Default | Description |
|-------|---------|-------------|
| `spec.topology.type` | `MultiPrimary` | `MultiPrimary` or `PrimaryRemote` |
| `spec.topology.primaryCluster` | First alphabetically | Name of the primary cluster for `PrimaryRemote`. Validated to be empty for `MultiPrimary` (CEL validation rule) |

The `status.primaryGatewayAddress` field is populated by the controller for `PrimaryRemote` topology with the primary's east-west gateway LoadBalancer address (IP or hostname). This is for user observability; the controller reads the address live from ManifestWork feedback internally.

## Lifecycle Events

- **Scale Up**: When a new cluster joins the ClusterSet, the controller automatically provisions the mesh plumbing for it: installs the operator, mints an intermediate CA, and distributes discovery tokens to all peers. This is the same process as the initial mesh bootstrap, applied incrementally to the new cluster.
- **Scale Down**: When a cluster is removed from a set, the controller immediately revokes its access by removing the remote secrets from all peer clusters and cleaning up the local CA bundles.

## Known Limitations

- **LoadBalancer prerequisite**: Managed clusters must support `Service` type `LoadBalancer` for the east-west gateway. Clusters without LoadBalancer support (e.g., bare-metal without MetalLB) cannot join a mesh.
- **Cross-ClusterSet namespace collision**: Multiple meshes targeting different ClusterSets can configure the same `controlPlane.namespace` on the same cluster (if the cluster moves between sets). The controller only detects namespace conflicts within a single ClusterSet.
- **Version compatibility**: The controller does not validate compatibility between the Sail operator version and the requested `spec.controlPlane.version`. If the operator does not support the requested Istio version, the Istio CR will fail to reconcile on the spoke.
- **IstioCNI version lock**: The IstioCNI ManifestWork uses the [CreateOnly] update strategy, so the first mesh to create it determines the version. Subsequent meshes with a different `spec.controlPlane.version` will not update the existing IstioCNI. If a version change is needed, the IstioCNI ManifestWork must be manually deleted and recreated.

## Phased Approach

**Phase 1 (Implemented)**: "Lean" approach - the add-on handles plumbing (operator, certificates, discovery).

The user is responsible for:

- Creating and managing Istio custom resources on each spoke cluster (directly or via GitOps)
- Enabling Istio CNI on OpenShift clusters
- Configuring `discoverySelectors` in multi-tenant environments to prevent cross-mesh service visibility
- Labeling application namespaces to match discovery selector configuration

ArgoCD with ApplicationSets is the recommended approach for managing Istio configuration across clusters.

**Phase 2 (Implemented)**: "Full" approach - the add-on also manages Istio custom resources centrally, automating topology configuration and enforcing consistency. The controller creates IstioCNI, Istio CRs, east-west gateways, and cross-network Gateway resources on managed clusters via ManifestWork, with phased ordering and readiness gating. Support for both MultiPrimary and PrimaryRemote topologies is included.

**Phase 3 (Future)**: Potential additions include observability stack management, `discoverySelectors` configuration for multi-tenant environments, and full addon framework integration (leveraging `ManagedClusterAddOn` for per-cluster enable/disable).

<!-- Reference links -->
[OCM]: https://open-cluster-management.io/
[OSSM]: https://docs.openshift.com/service-mesh/
[Sail]: https://github.com/istio-ecosystem/sail-operator
[cert-manager]: https://cert-manager.io/
[ManagedServiceAccount]: https://open-cluster-management.io/docs/getting-started/integration/managed-serviceaccount/
[ManifestWork]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestwork/
[ManagedClusterSet]: https://open-cluster-management.io/docs/concepts/cluster-inventory/managedclusterset/
[ManagedClusterView]: https://github.com/stolostron/cluster-lifecycle-api
[ClusterManagementAddOn]: https://open-cluster-management.io/docs/concepts/addon/#clustermanagementaddon
[Plug-in CA]: https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/
[Multi-Primary Multi-Network]: https://istio.io/latest/docs/setup/install/multicluster/multi-primary_multi-network/
[FeedbackRules]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestwork/#scalability
[ConditionRules]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestwork/#scalability
[CreateOnly]: https://open-cluster-management.io/docs/concepts/work-distribution/manifestwork/#update-strategy
[#72]: https://github.com/stolostron/multicluster-mesh-addon/issues/72
