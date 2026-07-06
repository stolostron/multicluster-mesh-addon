# Multi-Cluster Demo Environment Setup

Instructions for setting up a 6-control-plane demo environment across two OpenShift clusters.
Refer to [DEV-INSTALL.md](DEV-INSTALL.md) for general guidance on managing a dev install.
If you only have a single CRC cluster, see [DEV-INSTALL.md](DEV-INSTALL.md) instead.

This guide targets a real two-cluster ACM environment with a hub (`my-hub` context) and
a spoke (`my-spoke` context). The hub auto-registers itself as `local-cluster`, giving
two managed clusters in total. All MCM meshes span both clusters; standalone "discovered"
Istio CRs are split across clusters to show the cross-cluster discovery story.

The controller automatically installs the Sail operator, IstioCNI, Istio CRs, east-west
gateways, and remote secrets on both clusters when MCM CRs are created. No manual Istio
CR creation or namespace setup is required for managed meshes.

## Resource Layout

| MCM CR | MCM Namespace | Istio CR (auto-created) | CP Namespace | Mesh ID | Trust | Clusters |
|--------|---------------|-------------------------|--------------|---------|-------|----------|
| `unsecure-mcm` | `unsecure-mcm-ns` | `unsecure-mcm-ns-unsecure-mcm-cp` (on each cluster) | `unsecure-ns` | `unsecure-mcm-ns-unsecure-mcm` | No | local-cluster, my-spoke |
| `secure-mcm` | `secure-mcm-ns` | `secure-mcm-ns-secure-mcm-cp` (on each cluster) | `secure-ns` | `secure-mcm-ns-secure-mcm` | Yes | local-cluster, my-spoke |
| — | — | `discovered-hub-istio` | `discovered-hub-ns` | `discovered-hub-id` | — | local-cluster only |
| — | — | `discovered-spoke-istio` | `discovered-spoke-ns` | `discovered-spoke-id` | — | my-spoke only |

The managed Istio CRs are created automatically by the controller when it reconciles
each MCM CR. The controller creates a control plane ManifestWork per cluster with the
same `meshID` but different `clusterName` and `network` values (multi-primary
multi-network topology). The last two rows are standalone "discovered" control planes
with no MCM association, each on a different cluster, created manually.

### ManifestWorks created per cluster

| ManifestWork | Created by | Per-mesh? |
|--------------|------------|-----------|
| `multicluster-mesh-operator` | First MCM reconcile | No (shared) |
| `multicluster-mesh-istiocni` | First MCM reconcile | No (shared) |
| `multicluster-mesh-cacerts` | MCM with trust config | No (shared) |
| `multicluster-mesh-cp-{ns}-{name}` | Each MCM | Yes |
| `multicluster-mesh-gw-{ns}-{name}` | Each MCM | Yes |
| `multicluster-mesh-rs-{ns}-{name}` | Each MCM | Yes |

## Prerequisites

### Required tools

- `oc` CLI installed and available in PATH
- `podman` installed
- `jq` installed
- `make` installed
- `helm` installed
- Go toolchain
- Node.js `^20.19.0 || >=22.12.0`

### Required cluster state

The following must already be in place:

- Two OpenShift clusters accessible via kubeconfig contexts `my-hub` (hub) and `my-spoke` (spoke)
- ACM 2.16+ installed on the hub with `my-spoke` imported as a managed cluster
- Both `local-cluster` and `my-spoke` showing as joined and available

Verify ACM readiness:

```bash
oc --context=my-hub get mch multiclusterhub -n open-cluster-management \
  -o jsonpath='{.status.phase}'
# Should output: Running

oc --context=my-hub get managedclusters
# Should show local-cluster and my-spoke as JOINED=True, AVAILABLE=True
```

### Image registry

The hub cluster's OpenShift image registry must be exposed for backend and frontend
image builds:

```bash
oc --context=my-hub get image.config.openshift.io/cluster \
  -o jsonpath='{.status.externalRegistryHostnames[0]}'
```

If the output is empty, the registry is not exposed. **STOP: Ask the user if you should
expose it.** If the user declines, abort the entire setup. If yes:

```bash
oc --context=my-hub patch configs.imageregistry.operator.openshift.io/cluster \
  --type merge -p '{"spec":{"defaultRoute":true}}'
```

### cert-manager

Required on the hub for trust distribution. Check if deployed:

```bash
oc --context=my-hub get deployment cert-manager -n cert-manager
```

**STOP: If the command fails (cert-manager is not deployed), ask the user if you should
install it.** If the user declines, abort the entire setup. If the installation fails,
abort and report the error. If yes:

```bash
oc --context=my-hub apply \
  -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
oc --context=my-hub rollout status deployment/cert-manager \
  -n cert-manager --timeout=120s
oc --context=my-hub rollout status deployment/cert-manager-webhook \
  -n cert-manager --timeout=120s
```

### Backend controller

The multicluster-mesh-addon controller runs on the hub. Check if deployed:

```bash
oc --context=my-hub get deployment multicluster-mesh-controller \
  -n multicluster-mesh-system
```

**STOP: If the command fails (controller is not deployed), ask the user if you should
build and deploy it.** If the user declines, abort the entire setup. If the build or
deployment fails, abort and report the error. If yes:

```bash
cd <multicluster-mesh-addon-repo>

REGISTRY=$(oc --context=my-hub get image.config.openshift.io/cluster \
  -o jsonpath='{.status.externalRegistryHostnames[0]}')
INTERNAL_REGISTRY=image-registry.openshift-image-registry.svc:5000
BACKEND_NAMESPACE=multicluster-mesh-system
BACKEND_IMAGE_NAME=multicluster-mesh-addon
BACKEND_IMAGE_TAG=dev

# Login to the OpenShift image registry
podman login --tls-verify=false \
  -u $(oc --context=my-hub whoami | tr -d ':') \
  -p $(oc --context=my-hub whoami -t) \
  ${REGISTRY}

# Create the controller namespace
oc --context=my-hub create namespace ${BACKEND_NAMESPACE} \
  --dry-run=client -o yaml | oc --context=my-hub apply -f -

# Build and push the controller image
make images IMG=${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}
podman push --tls-verify=false \
  ${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}

# Deploy via Helm
helm upgrade --install ${BACKEND_IMAGE_NAME} chart/ \
  --kube-context=my-hub \
  --create-namespace \
  --namespace ${BACKEND_NAMESPACE} \
  --set image.repository=${INTERNAL_REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME} \
  --set image.tag=${BACKEND_IMAGE_TAG} \
  --wait --timeout 180s

# Verify
oc --context=my-hub rollout status deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE} --timeout=120s
```

### Frontend ConsolePlugin

The `ossm-acm` plugin runs on the hub. Check if deployed:

```bash
oc --context=my-hub get consoleplugin ossm-acm
```

**STOP: If the command fails (plugin is not deployed), ask the user if you should
build and deploy it.** If the user declines, abort the entire setup. If the build or
deployment fails, abort and report the error. If yes:

```bash
cd <multicluster-mesh-addon-repo>/frontend
make build deploy
```

### Clean state

The following must NOT be present on either cluster:

- No Sail/OSSM operator (no CSV, no subscription, no `sailoperator.io` or `istio.io` CRDs)
- No existing MultiClusterMesh CRs
- No existing Istio CRs
- No ManagedClusterSet bound for mesh use

Verify the clean state on both clusters:

```bash
# Hub
oc --context=my-hub get csv --all-namespaces | grep -i servicemesh
# Should return nothing

oc --context=my-hub get crd | grep -E 'sailoperator|istio'
# Should return nothing

oc --context=my-hub get multiclustermesh --all-namespaces
# Should return "No resources found"

# Spoke
oc --context=my-spoke get csv --all-namespaces | grep -i servicemesh
# Should return nothing

oc --context=my-spoke get crd | grep -E 'sailoperator|istio'
# Should return nothing
```

---

## 1. Create a ManagedClusterSet

```bash
oc --context=my-hub apply -f - <<'EOF'
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: demo-cluster-set
EOF

oc --context=my-hub label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset=demo-cluster-set --overwrite

oc --context=my-hub label managedcluster my-spoke \
  cluster.open-cluster-management.io/clusterset=demo-cluster-set --overwrite
```

## 2. Create MCM namespaces

Only MCM namespaces need to be created manually on the hub. Control plane namespaces
are created automatically by the controller via ManifestWork on each cluster.

```bash
oc --context=my-hub create namespace unsecure-mcm-ns
oc --context=my-hub create namespace secure-mcm-ns
```

## 3. Deploy cert-manager Issuer chain

The `secure-mcm` MCM uses cert-manager for trust distribution. The controller creates
Certificates in the MCM's namespace, so the Issuer must live there too.

```bash
oc --context=my-hub apply -n secure-mcm-ns -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: mesh-selfsigned-issuer
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: mesh-root-ca
spec:
  isCA: true
  commonName: Mesh Root CA
  secretName: mesh-root-ca-secret
  duration: 87600h
  renewBefore: 720h
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: mesh-selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: mesh-root-ca
spec:
  ca:
    secretName: mesh-root-ca-secret
EOF
```

Wait for the root CA certificate to become Ready:

```bash
oc --context=my-hub wait certificate mesh-root-ca -n secure-mcm-ns \
  --for=condition=Ready --timeout=60s
```

## 4. Create MCM CRs

Create both MultiClusterMesh CRs. The controller will reconcile them and automatically
on both `local-cluster` and `my-spoke`:

1. Install the Sail operator via ManifestWork
2. Deploy IstioCNI
3. Create control plane namespaces, Istio CRs, and RBAC
4. Deploy east-west gateways with networking
5. Configure remote secrets for cross-cluster endpoint discovery
6. Distribute trust certificates (for `secure-mcm`)

```bash
oc --context=my-hub apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: unsecure-mcm
  namespace: unsecure-mcm-ns
spec:
  clusterSet: demo-cluster-set
  controlPlane:
    namespace: unsecure-ns
  gateway:
    serviceType: LoadBalancer
  topology:
    type: MultiPrimary
EOF

oc --context=my-hub apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: secure-mcm
  namespace: secure-mcm-ns
spec:
  clusterSet: demo-cluster-set
  controlPlane:
    namespace: secure-ns
  gateway:
    serviceType: LoadBalancer
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
  topology:
    type: MultiPrimary
EOF
```

## 5. Monitor controller reconciliation

Watch the controller progress through its phases on both clusters. Each MCM progresses
through: `OperatorInstalled` → `ControlPlaneReady` → `GatewayReady` → `DiscoveryReady`.

```bash
# Watch ManifestWorks being created on both clusters
oc --context=my-hub get manifestwork -n local-cluster -w &
oc --context=my-hub get manifestwork -n my-spoke -w &

# Wait for the operator ManifestWork on both clusters
oc --context=my-hub wait manifestwork multicluster-mesh-operator -n local-cluster \
  --for=condition=Applied --timeout=180s
oc --context=my-hub wait manifestwork multicluster-mesh-operator -n my-spoke \
  --for=condition=Applied --timeout=180s

# Wait for the Sail operator CSV on the hub
until oc --context=my-hub get csv -n openshift-operators 2>/dev/null \
  | grep -q servicemeshoperator3; do
  echo "Waiting for Sail operator CSV on hub..."
  sleep 10
done

CSV_HUB=$(oc --context=my-hub get csv -n openshift-operators -o name \
  | grep servicemeshoperator3)
oc --context=my-hub wait ${CSV_HUB} -n openshift-operators \
  --for=jsonpath='{.status.phase}'=Succeeded --timeout=300s

# Wait for the Sail operator CSV on the spoke
until oc --context=my-spoke get csv -n openshift-operators 2>/dev/null \
  | grep -q servicemeshoperator3; do
  echo "Waiting for Sail operator CSV on spoke..."
  sleep 10
done

CSV_SPOKE=$(oc --context=my-spoke get csv -n openshift-operators -o name \
  | grep servicemeshoperator3)
oc --context=my-spoke wait ${CSV_SPOKE} -n openshift-operators \
  --for=jsonpath='{.status.phase}'=Succeeded --timeout=300s

# Watch per-cluster conditions progressing
oc --context=my-hub get multiclustermesh unsecure-mcm -n unsecure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
oc --context=my-hub get multiclustermesh secure-mcm -n secure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
```

## 6. Create standalone Istio CRs

These are independent control planes not associated with any MCM. The frontend
discovers them via ACM Search but does not consider them "managed". Each lives on a
different cluster to demonstrate cross-cluster discovery. The Sail operator must already
be installed (the controller does this when reconciling the MCM CRs).

**On the hub:**

```bash
oc --context=my-hub create namespace discovered-hub-ns \
  --dry-run=client -o yaml | oc --context=my-hub apply -f -

oc --context=my-hub apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-hub-istio
spec:
  namespace: discovered-hub-ns
  values:
    global:
      meshID: discovered-hub-id
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF
```

**On the spoke:**

```bash
oc --context=my-spoke create namespace discovered-spoke-ns \
  --dry-run=client -o yaml | oc --context=my-spoke apply -f -

oc --context=my-spoke apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-spoke-istio
spec:
  namespace: discovered-spoke-ns
  values:
    global:
      meshID: discovered-spoke-id
      multiCluster:
        clusterName: my-spoke
      network: network2
EOF
```

## 7. Verification

```bash
# MCM CRs and their status
oc --context=my-hub get multiclustermesh --all-namespaces

# All Istio CRs on hub (should be 3: 2 auto-created + 1 standalone)
oc --context=my-hub get istios

# All Istio CRs on spoke (should be 3: 2 auto-created + 1 standalone)
oc --context=my-spoke get istios

# Control plane namespaces on hub (auto-created by controller + manually created)
oc --context=my-hub get namespaces | grep -E 'unsecure|secure|discovered'

# Control plane namespaces on spoke (auto-created by controller + manually created)
oc --context=my-spoke get namespaces | grep -E 'unsecure|secure|discovered'

# Sail operator status on both clusters
oc --context=my-hub get csv -n openshift-operators | grep servicemesh
oc --context=my-spoke get csv -n openshift-operators | grep servicemesh

# ManifestWorks on both clusters
oc --context=my-hub get manifestwork -n local-cluster
oc --context=my-hub get manifestwork -n my-spoke

# Trust distribution (for secure-mcm)
oc --context=my-hub get certificates -n secure-mcm-ns
oc --context=my-hub get manifestwork -n local-cluster | grep cacerts
oc --context=my-hub get manifestwork -n my-spoke | grep cacerts

# Per-cluster conditions (all 4 condition types on both clusters)
oc --context=my-hub get multiclustermesh unsecure-mcm -n unsecure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
oc --context=my-hub get multiclustermesh secure-mcm -n secure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
```

Expected results:

- 2 MCMs with all 4 conditions on both `local-cluster` and `my-spoke`:
  - `OperatorInstalled: True`
  - `ControlPlaneReady: True`
  - `GatewayReady: True`
  - `DiscoveryReady: True`
- 3 Istio CRs on the hub (2 auto-created by controller + `discovered-hub-istio`)
- 3 Istio CRs on the spoke (2 auto-created by controller + `discovered-spoke-istio`)
- IstioCNI deployed on both clusters (auto-created by controller via ManifestWork)
- East-west gateways deployed on both clusters (auto-created by controller via ManifestWork)
- Remote secrets configured for cross-cluster endpoint discovery
- cert-manager Certificate in `secure-mcm-ns` with Ready status
- `multicluster-mesh-cacerts` ManifestWork in both `local-cluster` and `my-spoke` namespaces

6 Istio CRs means 6 istiod instances across 2 clusters (3 per cluster). On
resource-constrained clusters, some control planes may remain Not Ready because their
istiod pod cannot be scheduled due to insufficient memory. Which control planes are
affected (if any) depends on scheduling order and available resources.

## 8. (Optional) Deploy the mesh-hello test application

Deploy a browser-accessible test app that shows cluster identity, cross-cluster
connectivity, and mTLS status. On a multi-cluster setup, the frontend and backend
pods run on the same cluster but communicate through the Istio mesh, demonstrating
sidecar injection and mTLS.

```bash
cd <multicluster-mesh-addon-repo>/frontend

# Deploy into the secure-mcm mesh (with trust — shows mTLS details)
hack/deploy-mesh-hello.sh -m secure-mcm -n secure-mcm-ns install
```

This creates a `secure-mcm-testapp` namespace with Istio sidecar injection,
deploys frontend and backend services, and prints a URL (OpenShift Route) you
can open in your browser (e.g. `http://mesh-hello-secure-mcm-secure-mcm-testapp.apps.hub.example.com/`).
The page auto-refreshes every 10 seconds.

To remove:

```bash
hack/deploy-mesh-hello.sh -m secure-mcm -n secure-mcm-ns uninstall
```

## Demo Tips

### Toggling Degraded / Healthy per cluster

The IstioCNI is managed by the controller via the `multicluster-mesh-istiocni`
ManifestWork. Deleting the IstioCNI resource directly will put running control planes
into the Degraded state, but the controller will restore it on its next reconcile.

To toggle the state persistently, delete the ManifestWork on the target cluster:

Delete the IstioCNI ManifestWork on the spoke only (hub stays healthy, spoke goes degraded):

```bash
oc --context=my-hub delete manifestwork multicluster-mesh-istiocni -n my-spoke
```

Delete the IstioCNI ManifestWork on both clusters to put all control planes into the
Degraded state:

```bash
oc --context=my-hub delete manifestwork multicluster-mesh-istiocni -n local-cluster
oc --context=my-hub delete manifestwork multicluster-mesh-istiocni -n my-spoke
```

Re-create the ManifestWorks by triggering a reconcile (e.g. annotate an MCM):

```bash
oc --context=my-hub annotate multiclustermesh unsecure-mcm -n unsecure-mcm-ns \
  reconcile-trigger="$(date +%s)" --overwrite
```

The transition takes ~30 seconds. The frontend will update automatically via its
Kubernetes watch.

## Teardown

Deleting the MCM CRs triggers automatic cleanup of all controller-managed ManifestWorks
(operator, IstioCNI, Istio CRs, gateways, remote secrets, trust certificates) on both
clusters.

```bash
# Delete standalone Istio CRs (not managed by the controller)
oc --context=my-hub delete istio discovered-hub-istio --ignore-not-found
oc --context=my-spoke delete istio discovered-spoke-istio --ignore-not-found

# Delete MCM CRs (controller cleans up all ManifestWorks automatically)
oc --context=my-hub delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns --ignore-not-found
oc --context=my-hub delete multiclustermesh secure-mcm -n secure-mcm-ns --ignore-not-found

# Wait for controller-managed ManifestWorks to be cleaned up on both clusters
until [ "$(oc --context=my-hub get manifestwork -n local-cluster -o name 2>/dev/null \
  | grep multicluster-mesh | wc -l)" -eq 0 ] && \
  [ "$(oc --context=my-hub get manifestwork -n my-spoke -o name 2>/dev/null \
  | grep multicluster-mesh | wc -l)" -eq 0 ]; do
  echo "Waiting for ManifestWork cleanup..."
  sleep 5
done

# Remove cluster labels
oc --context=my-hub label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset-
oc --context=my-hub label managedcluster my-spoke \
  cluster.open-cluster-management.io/clusterset-

# Delete ManagedClusterSet
oc --context=my-hub delete managedclusterset demo-cluster-set --ignore-not-found

# Delete namespaces on hub (MCM namespaces + standalone discovered namespace)
oc --context=my-hub delete namespace unsecure-mcm-ns secure-mcm-ns \
  discovered-hub-ns --ignore-not-found

# Delete standalone discovered namespace on spoke
oc --context=my-spoke delete namespace discovered-spoke-ns --ignore-not-found

# Remove the Sail operator if it remains on either cluster after ManifestWork cleanup
CSV_HUB=$(oc --context=my-hub get csv -n openshift-operators -o name 2>/dev/null \
  | grep servicemeshoperator3)
if [ -n "${CSV_HUB}" ]; then
  oc --context=my-hub delete ${CSV_HUB} -n openshift-operators
fi

CSV_SPOKE=$(oc --context=my-spoke get csv -n openshift-operators -o name 2>/dev/null \
  | grep servicemeshoperator3)
if [ -n "${CSV_SPOKE}" ]; then
  oc --context=my-spoke delete ${CSV_SPOKE} -n openshift-operators
fi

oc --context=my-hub get crd -o name \
  | grep -E 'sailoperator\.io|istio\.io' | xargs oc --context=my-hub delete 2>/dev/null
oc --context=my-spoke get crd -o name \
  | grep -E 'sailoperator\.io|istio\.io' | xargs oc --context=my-spoke delete 2>/dev/null
```
