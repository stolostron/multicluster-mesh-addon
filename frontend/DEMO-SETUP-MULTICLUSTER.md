# Multi-Cluster Demo Environment Setup

Instructions for setting up a 6-control-plane demo environment across two OpenShift clusters.
Refer to [DEV-INSTALL.md](DEV-INSTALL.md) for general guidance on managing a dev install.

This guide targets a real two-cluster ACM environment with a hub (`my-hub` context) and
a spoke (`my-spoke` context). The hub auto-registers itself as `local-cluster`, giving
two managed clusters in total. All MCM meshes span both clusters; standalone "discovered"
Istio CRs are split across clusters to show the cross-cluster discovery story.

This is a UI-only demo: control planes run on both clusters and the Fleet Service Mesh
UI shows correct multi-cluster status, but cross-cluster service traffic is not configured
(east-west gateways and remote secrets are omitted).

## Resource Layout

| MCM CR | MCM Namespace | Istio CR | CP Namespace | Mesh ID | Trust | Clusters |
|--------|---------------|----------|--------------|---------|-------|----------|
| `unsecure-mcm` | `unsecure-mcm-ns` | `unsecure-istio` (on each cluster) | `unsecure-ns` | `unsecure-mcm-ns-unsecure-mcm` | No | local-cluster, my-spoke |
| `secure-mcm` | `secure-mcm-ns` | `secure-istio` (on each cluster) | `secure-ns` | `secure-mcm-ns-secure-mcm` | Yes | local-cluster, my-spoke |
| — | — | `discovered-hub-istio` | `discovered-hub-ns` | `discovered-hub-id` | — | local-cluster only |
| — | — | `discovered-spoke-istio` | `discovered-spoke-ns` | `discovered-spoke-id` | — | my-spoke only |

The managed Istio CRs simulate what the MCM controller would create after reconciling
each MCM CR (a future feature). Each managed mesh has one Istio CR per cluster with the
same `meshID` but different `clusterName` and `network` values. The last two rows are
standalone "discovered" control planes with no MCM association, each on a different
cluster.

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
- ACM installed on the hub with `my-spoke` imported as a managed cluster
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

## 2. Create namespaces and label networks

Create MCM namespaces on the hub, control plane namespaces on both clusters, and label
each CP namespace with its network identity so Istio knows these are different networks.

**Hub namespaces:**

```bash
# MCM namespaces (hub only)
oc --context=my-hub create namespace unsecure-mcm-ns
oc --context=my-hub create namespace secure-mcm-ns

# CP namespaces on hub
oc --context=my-hub create namespace unsecure-ns
oc --context=my-hub create namespace secure-ns
oc --context=my-hub create namespace discovered-hub-ns
oc --context=my-hub create namespace istio-cni
```

**Spoke namespaces:**

```bash
# CP namespaces on spoke
oc --context=my-spoke create namespace unsecure-ns
oc --context=my-spoke create namespace secure-ns
oc --context=my-spoke create namespace discovered-spoke-ns
oc --context=my-spoke create namespace istio-cni
```

**Network labels:**

```bash
# Hub CP namespaces = network1
oc --context=my-hub label namespace unsecure-ns topology.istio.io/network=network1
oc --context=my-hub label namespace secure-ns topology.istio.io/network=network1
oc --context=my-hub label namespace discovered-hub-ns topology.istio.io/network=network1

# Spoke CP namespaces = network2
oc --context=my-spoke label namespace unsecure-ns topology.istio.io/network=network2
oc --context=my-spoke label namespace secure-ns topology.istio.io/network=network2
oc --context=my-spoke label namespace discovered-spoke-ns topology.istio.io/network=network2
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

Create both MultiClusterMesh CRs. The controller will reconcile them and install the
Sail operator on both `local-cluster` and `my-spoke` via ManifestWork.

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
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
EOF
```

## 5. Wait for Sail operator

The MCM controller installs the Sail operator via ManifestWork on both clusters. Wait
for both to complete before creating Istio CRs (the CRDs must exist first).

```bash
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

# Verify the Istio CRD is available on both clusters
oc --context=my-hub get crd istios.sailoperator.io
oc --context=my-spoke get crd istios.sailoperator.io
```

## 6. Create managed Istio CRs

These simulate what the MCM controller would create after reconciling each MCM.
Each targets the namespace matching its MCM's `spec.controlPlane.namespace`.
The mesh ID follows the convention the controller will use: `<MCM namespace>-<MCM name>`.

Each managed mesh gets one Istio CR per cluster with the same `meshID` but different
`clusterName` and `network` values (multi-primary multi-network topology).

**On the hub (local-cluster):**

```bash
oc --context=my-hub apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: unsecure-istio
spec:
  namespace: unsecure-ns
  values:
    global:
      meshID: unsecure-mcm-ns-unsecure-mcm
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF

oc --context=my-hub apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: secure-istio
spec:
  namespace: secure-ns
  values:
    global:
      meshID: secure-mcm-ns-secure-mcm
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF
```

**On the spoke (my-spoke):**

```bash
oc --context=my-spoke apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: unsecure-istio
spec:
  namespace: unsecure-ns
  values:
    global:
      meshID: unsecure-mcm-ns-unsecure-mcm
      multiCluster:
        clusterName: my-spoke
      network: network2
EOF

oc --context=my-spoke apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: secure-istio
spec:
  namespace: secure-ns
  values:
    global:
      meshID: secure-mcm-ns-secure-mcm
      multiCluster:
        clusterName: my-spoke
      network: network2
EOF
```

## 7. Create standalone Istio CRs

These are independent control planes not associated with any MCM. The frontend
discovers them via ACM Search but does not consider them "managed". Each lives on a
different cluster to demonstrate cross-cluster discovery.

**On the hub:**

```bash
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

## 8. Deploy IstioCNI (optional)

Without an IstioCNI resource, control planes whose istiod is running show as **Degraded**
in the frontend (`Ready: True` but `DependenciesHealthy: False`). Creating the IstioCNI
moves them to **Healthy**. Skip this step to start the demo in the Degraded state -- you
can create and delete the IstioCNI at any time to toggle between states (see
[Demo Tips](#demo-tips) below).

Deploy on both clusters:

```bash
# Hub
oc --context=my-hub apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  version: v1.28.8
EOF

oc --context=my-hub wait istiocni default --for=condition=Ready --timeout=60s

# Spoke
oc --context=my-spoke apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  version: v1.28.8
EOF

oc --context=my-spoke wait istiocni default --for=condition=Ready --timeout=60s
```

## 9. Verification

```bash
# MCM CRs and their status
oc --context=my-hub get multiclustermesh --all-namespaces

# All Istio CRs on hub
oc --context=my-hub get istios

# All Istio CRs on spoke
oc --context=my-spoke get istios

# Namespaces on hub
oc --context=my-hub get namespaces | grep -E 'unsecure|secure|discovered'

# Namespaces on spoke
oc --context=my-spoke get namespaces | grep -E 'unsecure|secure|discovered'

# Sail operator status on both clusters
oc --context=my-hub get csv -n openshift-operators | grep servicemesh
oc --context=my-spoke get csv -n openshift-operators | grep servicemesh

# Trust distribution (for secure-mcm)
oc --context=my-hub get certificates -n secure-mcm-ns
oc --context=my-hub get manifestwork -n local-cluster | grep cacerts
oc --context=my-hub get manifestwork -n my-spoke | grep cacerts

# Per-cluster operator status from MCM (should show both local-cluster and my-spoke)
oc --context=my-hub get multiclustermesh unsecure-mcm -n unsecure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
oc --context=my-hub get multiclustermesh secure-mcm -n secure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
```

Expected results:

- 2 MCMs with `OperatorInstalled: True` on both `local-cluster` and `my-spoke`
- 3 Istio CRs on the hub (`unsecure-istio`, `secure-istio`, `discovered-hub-istio`)
- 3 Istio CRs on the spoke (`unsecure-istio`, `secure-istio`, `discovered-spoke-istio`)
- cert-manager Certificate in `secure-mcm-ns` with Ready status
- `multicluster-mesh-cacerts` ManifestWork in both `local-cluster` and `my-spoke` namespaces

Control plane status depends on whether IstioCNI was deployed (step 8):

| Condition | Without IstioCNI | With IstioCNI |
|-----------|------------------|---------------|
| istiod running | Degraded (orange) | Healthy (green) |
| istiod not schedulable | Not Ready (red) | Not Ready (red) |

6 Istio CRs means 6 istiod instances across 2 clusters (3 per cluster). On
resource-constrained clusters, some control planes may remain Not Ready because their
istiod pod cannot be scheduled due to insufficient memory. Which control planes are
affected (if any) depends on scheduling order and available resources.

## Demo Tips

### Toggling Degraded / Healthy per cluster

You can toggle IstioCNI independently on each cluster to show per-cluster status
differences within the same mesh.

Delete IstioCNI on the spoke only (hub stays healthy, spoke goes degraded):

```bash
oc --context=my-spoke delete istiocni default
```

Re-create it to move the spoke back to healthy:

```bash
oc --context=my-spoke apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  version: v1.28.8
EOF
```

Delete IstioCNI on both clusters to put all control planes into the Degraded state:

```bash
oc --context=my-hub delete istiocni default
oc --context=my-spoke delete istiocni default
```

The transition takes ~30 seconds. The frontend will update automatically via its
Kubernetes watch.

## Teardown

To remove everything created by this guide:

```bash
# Delete IstioCNI on both clusters (if deployed)
oc --context=my-hub delete istiocni default 2>/dev/null
oc --context=my-spoke delete istiocni default 2>/dev/null

# Delete Istio CRs on hub
oc --context=my-hub delete istio unsecure-istio secure-istio discovered-hub-istio

# Delete Istio CRs on spoke
oc --context=my-spoke delete istio unsecure-istio secure-istio discovered-spoke-istio

# Delete MCM CRs
oc --context=my-hub delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns
oc --context=my-hub delete multiclustermesh secure-mcm -n secure-mcm-ns

# Remove cluster labels
oc --context=my-hub label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset-
oc --context=my-hub label managedcluster my-spoke \
  cluster.open-cluster-management.io/clusterset-

# Delete ManagedClusterSet
oc --context=my-hub delete managedclusterset demo-cluster-set

# Delete namespaces on hub
oc --context=my-hub delete namespace unsecure-mcm-ns secure-mcm-ns \
  unsecure-ns secure-ns discovered-hub-ns istio-cni

# Delete namespaces on spoke
oc --context=my-spoke delete namespace unsecure-ns secure-ns \
  discovered-spoke-ns istio-cni

# Wait for the MCM controller to clean up the operator ManifestWorks, then
# remove the Sail operator if it remains on either cluster
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
