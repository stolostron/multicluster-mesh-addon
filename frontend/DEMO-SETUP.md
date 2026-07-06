# Demo Mesh Environment Setup

Instructions for setting up a 4-control-plane demo environment on a CRC cluster.
Refer to [DEV-INSTALL.md](DEV-INSTALL.md) for general guidance on managing a dev install.

> **Quick start:** [`hack/setup-demo.sh`](hack/setup-demo.sh) automates the entire setup process described below. Run it from the `frontend/` directory as an alternative to following the manual steps.

## Resource Layout

| MCM CR | MCM Namespace | Istio CR | CP Namespace | Mesh ID | Trust |
|--------|---------------|----------|--------------|---------|-------|
| `unsecure-mcm` | `unsecure-mcm-ns` | `unsecure-mcm-ns-unsecure-mcm-cp` | `unsecure-ns` | `unsecure-mcm-ns-unsecure-mcm` | No |
| `secure-mcm` | `secure-mcm-ns` | `secure-mcm-ns-secure-mcm-cp` | `secure-ns` | `secure-mcm-ns-secure-mcm` | Yes |
| — | — | `discovered-alpha-istio` | `discovered-alpha-ns` | `discovered-alpha-id` | — |
| — | — | `discovered-beta-istio` | `discovered-beta-ns` | `discovered-beta-id` | — |

The first two Istio CRs are created by [`hack/setup-mesh-cps.sh`](hack/setup-mesh-cps.sh)
based on their corresponding MCM CRs. The last two are standalone "discovered" control
planes with no MCM association, created manually.

## Prerequisites

The following must already be deployed on the cluster:

- ACM (Advanced Cluster Management) with `local-cluster` registered as a ManagedCluster
- The multicluster-mesh-addon backend controller (in `multicluster-mesh-system`)
- The `ossm-acm` frontend ConsolePlugin
- cert-manager

The following must NOT be present:

- No existing MultiClusterMesh CRs
- No existing Istio CRs
- No ManagedClusterSet bound for mesh use

Verify the clean state:

```bash
oc get multiclustermesh --all-namespaces
# Should return "No resources found"

oc get istio --all-namespaces
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

## 2. Create MCM namespaces

```bash
oc create namespace unsecure-mcm-ns
oc create namespace secure-mcm-ns
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

## 5. Create managed Istio control planes

Use [`hack/setup-mesh-cps.sh`](hack/setup-mesh-cps.sh) to create Istio control planes
for each MCM. The script waits for the Sail operator (installed by the controller via
ManifestWork), transforms trust certificates to Istio format, creates Istio CRs and
IstioCNI, and installs east-west gateways. Run it once per MCM:

```bash
hack/setup-mesh-cps.sh -m unsecure-mcm -n unsecure-mcm-ns install
hack/setup-mesh-cps.sh -m secure-mcm -n secure-mcm-ns --deploy-app true install
```

The `--deploy-app true` flag on the secure mesh deploys a browser-accessible test
application (`mesh-hello`) that shows cluster identity, cross-cluster connectivity,
and mTLS status. After the command completes, it prints a URL you can open in your
browser (e.g. `http://mesh-hello-secure-mcm-....apps-crc.testing`).

Run `hack/setup-mesh-cps.sh --help` for additional options (topology, Istio version).

## 6. Create standalone Istio CRs

These are independent control planes not associated with any MCM. The frontend
discovers them but does not consider them "managed". The Sail operator must already
be installed (step 5 does this via the MCM controller).

```bash
oc create namespace discovered-alpha-ns --dry-run=client -o yaml | oc apply -f -
oc create namespace discovered-beta-ns --dry-run=client -o yaml | oc apply -f -

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

## 7. Verification

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
- 4 Istio CRs (2 managed by `setup-mesh-cps.sh`, 2 standalone) each targeting a unique namespace
- IstioCNI deployed (created by `setup-mesh-cps.sh`)
- cert-manager Certificate in `secure-mcm-ns` with Ready status
- A `multicluster-mesh-cacerts` ManifestWork in `local-cluster` namespace for trust distribution

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
# Remove managed Istio control planes (Istio CRs, IstioCNI, east-west gateways, etc.)
hack/setup-mesh-cps.sh -m unsecure-mcm -n unsecure-mcm-ns uninstall
hack/setup-mesh-cps.sh -m secure-mcm -n secure-mcm-ns --deploy-app true uninstall

# Remove standalone Istio CRs
oc delete istio discovered-alpha-istio discovered-beta-istio --ignore-not-found

# Delete MCM CRs (let the controller clean up operator ManifestWorks)
oc delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns --ignore-not-found
oc delete multiclustermesh secure-mcm -n secure-mcm-ns --ignore-not-found

# Remove cluster label
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset-

# Delete ManagedClusterSet
oc delete managedclusterset demo-cluster-set --ignore-not-found

# Delete namespaces
oc delete namespace unsecure-mcm-ns secure-mcm-ns \
  discovered-alpha-ns discovered-beta-ns --ignore-not-found

# Wait for the MCM controller to clean up the operator ManifestWork, then
# remove the Sail operator if it remains
CSV=$(oc get csv -n openshift-operators -o name 2>/dev/null | grep servicemeshoperator3)
if [ -n "${CSV}" ]; then
  oc delete ${CSV} -n openshift-operators
fi
oc get crd -o name | grep -E 'sailoperator\.io|istio\.io' | xargs oc delete 2>/dev/null
```
