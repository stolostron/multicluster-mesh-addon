# Demo Mesh Environment Setup

Instructions for setting up a 4-control-plane demo environment on a CRC cluster.
Refer to [DEV-INSTALL.md](DEV-INSTALL.md) for general guidance on managing a dev install.

## Resource Layout

| MCM CR | MCM Namespace | Istio CR | CP Namespace | Mesh ID | Trust |
|--------|---------------|----------|--------------|---------|-------|
| `unsecure-mcm` | `unsecure-mcm-ns` | `unsecure-istio` | `unsecure-ns` | `unsecure-id` | No |
| `secure-mcm` | `secure-mcm-ns` | `secure-istio` | `secure-ns` | `secure-id` | Yes |
| — | — | `discovered-alpha-istio` | `discovered-alpha-ns` | `discovered-alpha-id` | — |
| — | — | `discovered-beta-istio` | `discovered-beta-ns` | `discovered-beta-id` | — |

The first two Istio CRs simulate what the MCM controller would create after reconciling
its MCM CR (a future feature). The last two are standalone "discovered" control planes
with no MCM association.

## Prerequisites

The following must already be deployed on the cluster:

- ACM (Advanced Cluster Management) with `local-cluster` registered as a ManagedCluster
- The multicluster-mesh-addon backend controller (in `multicluster-mesh-system`)
- The `ossm-acm` frontend ConsolePlugin
- cert-manager

The following must NOT be present:

- No Sail/OSSM operator (no CSV, no subscription, no `sailoperator.io` or `istio.io` CRDs)
- No existing MultiClusterMesh CRs
- No existing Istio CRs
- No ManagedClusterSet bound for mesh use

Verify the clean state:

```bash
oc get csv --all-namespaces | grep -i servicemesh
# Should return nothing

oc get crd | grep -E 'sailoperator|istio'
# Should return nothing

oc get multiclustermesh --all-namespaces
# Should return "No resources found"
```

## 1. Create a ManagedClusterSet

```bash
oc apply -f - <<'EOF'
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: demo-cluster-set
EOF

oc label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset=demo-cluster-set --overwrite
```

## 2. Create namespaces

Create 2 MCM namespaces, 4 control plane namespaces, and the IstioCNI namespace:

```bash
oc create namespace unsecure-mcm-ns
oc create namespace secure-mcm-ns
oc create namespace unsecure-ns
oc create namespace secure-ns
oc create namespace discovered-alpha-ns
oc create namespace discovered-beta-ns
oc create namespace istio-cni
```

## 3. Deploy cert-manager Issuer chain

The `secure-mcm` MCM uses cert-manager for trust distribution. The controller creates
Certificates in the MCM's namespace, so the Issuer must live there too.

```bash
oc apply -n secure-mcm-ns -f - <<'EOF'
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
oc wait certificate mesh-root-ca -n secure-mcm-ns \
  --for=condition=Ready --timeout=60s
```

## 4. Create MCM CRs

Create both MultiClusterMesh CRs. The controller will reconcile them and install the
Sail operator on `local-cluster` via ManifestWork.

```bash
oc apply -f - <<'EOF'
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

oc apply -f - <<'EOF'
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

The MCM controller installs the Sail operator via a ManifestWork. Wait for it to
complete before creating Istio CRs (the CRDs must exist first).

```bash
# Wait for the operator ManifestWork to be applied
oc wait manifestwork multicluster-mesh-operator -n local-cluster \
  --for=condition=Applied --timeout=180s

# Wait for the Sail operator CSV to succeed
until oc get csv -n openshift-operators 2>/dev/null | grep -q servicemeshoperator3; do
  echo "Waiting for Sail operator CSV to appear..."
  sleep 10
done

CSV=$(oc get csv -n openshift-operators -o name | grep servicemeshoperator3)
oc wait ${CSV} -n openshift-operators \
  --for=jsonpath='{.status.phase}'=Succeeded --timeout=300s

# Verify the Istio CRD is available
oc get crd istios.sailoperator.io
```

## 6. Create managed Istio CRs

These simulate what the MCM controller would create after reconciling each MCM.
Each targets the namespace matching its MCM's `spec.controlPlane.namespace` and
shares its MCM's mesh-id.

```bash
oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: unsecure-istio
spec:
  namespace: unsecure-ns
  values:
    global:
      meshID: unsecure-id
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF

oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: secure-istio
spec:
  namespace: secure-ns
  values:
    global:
      meshID: secure-id
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF
```

## 7. Create standalone Istio CRs

These are independent control planes not associated with any MCM. The frontend
discovers them but does not consider them "managed".

```bash
oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-alpha-istio
spec:
  namespace: discovered-alpha-ns
  values:
    global:
      meshID: discovered-alpha-id
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF

oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-beta-istio
spec:
  namespace: discovered-beta-ns
  values:
    global:
      meshID: discovered-beta-id
      multiCluster:
        clusterName: local-cluster
      network: network2
EOF
```

## 8. Deploy IstioCNI (optional)

Without an IstioCNI resource, control planes whose istiod is running show as **Degraded**
in the frontend (`Ready: True` but `DependenciesHealthy: False`). Creating the IstioCNI
moves them to **Healthy**. Skip this step to start the demo in the Degraded state — you
can create and delete the IstioCNI at any time to toggle between states (see
[Demo Tips](#demo-tips) below).

```bash
oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  version: v1.28.8
EOF
```

Wait for the CNI to become ready:

```bash
oc wait istiocni default --for=condition=Ready --timeout=60s
```

## 9. Verification

```bash
# MCM CRs and their status
oc get multiclustermesh --all-namespaces

# All Istio CRs
oc get istios

# Namespaces
oc get namespaces | grep -E 'unsecure|secure|discovered'

# Sail operator status
oc get csv -n openshift-operators | grep servicemesh

# Trust distribution (for secure-mcm)
oc get certificates -n secure-mcm-ns
oc get manifestwork -n local-cluster | grep cacerts

# Per-cluster operator status from MCM
oc get multiclustermesh unsecure-mcm -n unsecure-mcm-ns -o jsonpath='{.status.clusterStatus}' | jq .
oc get multiclustermesh secure-mcm -n secure-mcm-ns -o jsonpath='{.status.clusterStatus}' | jq .
```

Expected results:

- 2 MCMs with `OperatorInstalled: True` on `local-cluster`
- 4 Istio CRs each targeting a unique namespace
- cert-manager Certificate in `secure-mcm-ns` with Ready status
- A `multicluster-mesh-cacerts` ManifestWork in `local-cluster` namespace for trust distribution

Control plane status depends on whether IstioCNI was deployed (step 8):

| Condition | Without IstioCNI | With IstioCNI |
|-----------|------------------|---------------|
| istiod running | Degraded (orange) | Healthy (green) |
| istiod not schedulable | Not Ready (red) | Not Ready (red) |

On resource-constrained environments (e.g. CRC) one or more control planes may remain
Not Ready because their istiod pod cannot be scheduled due to insufficient memory.
Which control plane is affected (if any) depends on scheduling order and available
resources.

## Demo Tips

### Toggling Degraded ↔ Healthy

Delete the IstioCNI to put running control planes into the Degraded state:

```bash
oc delete istiocni default
```

Re-create it to move them back to Healthy:

```bash
oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  namespace: istio-cni
  version: v1.28.8
EOF
```

The transition takes ~30 seconds. The frontend will update automatically via its
Kubernetes watch.

## Teardown

To remove everything created by this guide:

```bash
# Delete IstioCNI (if deployed)
oc delete istiocni default 2>/dev/null

# Delete Istio CRs
oc delete istio unsecure-istio secure-istio discovered-alpha-istio discovered-beta-istio

# Delete MCM CRs
oc delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns
oc delete multiclustermesh secure-mcm -n secure-mcm-ns

# Remove cluster label
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset-

# Delete ManagedClusterSet
oc delete managedclusterset demo-cluster-set

# Delete namespaces
oc delete namespace unsecure-mcm-ns secure-mcm-ns unsecure-ns secure-ns \
  discovered-alpha-ns discovered-beta-ns istio-cni

# Wait for the MCM controller to clean up the operator ManifestWork, then
# remove the Sail operator if it remains
CSV=$(oc get csv -n openshift-operators -o name 2>/dev/null | grep servicemeshoperator3)
if [ -n "${CSV}" ]; then
  oc delete ${CSV} -n openshift-operators
fi
oc get crd -o name | grep -E 'sailoperator\.io|istio\.io' | xargs oc delete 2>/dev/null
```
