#!/usr/bin/env bash
#
# Creates (or tears down) demo resources for the Fleet Service Mesh console plugin.
#
# The backend controller automatically creates Istio CRs, IstioCNI, east-west
# gateways, and remote secrets when a MultiClusterMesh CR is created. This script
# sets up the infrastructure (ManagedClusterSet, trust chain) and creates the MCM
# CRs, then waits for the controller to reconcile everything.
#
# Usage:
#   hack/setup-demo.sh install     # create all demo resources
#   hack/setup-demo.sh uninstall   # remove all demo resources
#
# Prerequisites (checked automatically on install):
#   - ACM 2.16+ installed and MultiClusterHub in Running phase
#   - Backend controller deployed in multicluster-mesh-system
#   - oc logged in with cluster-admin privileges
#
# What this creates:
#   Infrastructure:
#     cert-manager            - installed if not already present (idempotent)
#     demo-cluster-set        - ManagedClusterSet with local-cluster bound to it
#     cert-manager trust chain - self-signed root CA Issuer in secure-mcm-ns
#
#   MCMs (controller creates Istio CRs, IstioCNI, gateways, etc. automatically):
#     secure-mcm    - mesh with trust enabled, in secure-mcm-ns, CP: secure-ns
#     unsecure-mcm  - mesh without trust, in unsecure-mcm-ns, CP: unsecure-ns
#
#   Standalone Istio CRs (created manually, not managed by any MCM):
#     discovered-standalone -> istio-discovered -> "discovered" in the UI

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RECONCILE_TIMEOUT="${RECONCILE_TIMEOUT:-300}"

die() { echo "ERROR: $*" >&2; exit 1; }

preflight() {
  echo "=== Checking prerequisites ==="

  if ! oc whoami &>/dev/null; then
    die "Not logged in to a cluster. Run 'oc login' first."
  fi
  echo "  [ok] Logged in as $(oc whoami) on $(oc whoami --show-server)"

  local ok=true

  local mch_phase
  mch_phase=$(oc get multiclusterhub -A -o jsonpath='{.items[0].status.phase}' 2>/dev/null || true)
  if [ "$mch_phase" = "Running" ]; then
    echo "  [ok] ACM MultiClusterHub is Running"
  else
    echo "  [FAIL] ACM is not installed or not ready (MultiClusterHub phase: ${mch_phase:-not found})"
    echo "         See DEV-INSTALL.md for ACM setup instructions"
    ok=false
  fi

  local backend_available
  backend_available=$(oc get deployment multicluster-mesh-controller \
    -n multicluster-mesh-system \
    -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
  if [ "${backend_available:-0}" -ge 1 ]; then
    echo "  [ok] Backend controller is running"
  else
    echo "  [FAIL] Backend controller is not deployed or not ready"
    echo "         See DEV-INSTALL.md for backend controller build/deploy instructions"
    ok=false
  fi

  if [ "$ok" = false ]; then
    die "Prerequisites not met. Fix the issues above and re-run."
  fi
  echo ""
}

CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.20.2}"
CERT_MANAGER_URL="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"

install_cert_manager() {
  local cm_available
  cm_available=$(oc get deployment cert-manager -n cert-manager \
    -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
  if [ "${cm_available:-0}" -ge 1 ]; then
    echo "  cert-manager already installed, skipping."
    return
  fi

  echo "=== Installing cert-manager ${CERT_MANAGER_VERSION} ==="
  oc apply -f "${CERT_MANAGER_URL}" || die "Failed to install cert-manager"
  oc rollout status deployment/cert-manager -n cert-manager --timeout=120s \
    || die "cert-manager did not become ready"
  oc rollout status deployment/cert-manager-cainjector -n cert-manager --timeout=120s \
    || die "cert-manager-cainjector did not become ready"
  oc rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s \
    || die "cert-manager-webhook did not become ready"

  echo "  Waiting for cert-manager webhook TLS to be provisioned..."
  local elapsed=0
  until oc apply --dry-run=server -f - <<'PROBE' &>/dev/null 2>&1
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: webhook-probe
  namespace: cert-manager
spec:
  selfSigned: {}
PROBE
  do
    if [ "$elapsed" -ge 180 ]; then
      die "cert-manager webhook not ready after 180s"
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done
  echo "  [ok] cert-manager installed"
}

uninstall_cert_manager() {
  if ! oc get namespace cert-manager &>/dev/null; then
    echo "  cert-manager not installed, skipping."
    return
  fi

  echo "=== Removing cert-manager ==="
  oc delete -f "${CERT_MANAGER_URL}" --ignore-not-found 2>/dev/null || true
  oc delete namespace cert-manager --ignore-not-found 2>/dev/null || true
  echo "  [ok] cert-manager removed"
}

wait_for_mesh_ready() {
  local name=$1
  local namespace=$2
  local timeout=$3

  echo "  Waiting for ${namespace}/${name} to be ready (timeout: ${timeout}s)..."
  local elapsed=0
  while true; do
    local ready
    ready=$(oc get multiclustermesh "$name" -n "$namespace" \
      -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
    if [ "$ready" = "True" ]; then
      echo "  [ok] ${name} is ready"
      return 0
    fi

    local op cp gw
    op=$(oc get multiclustermesh "$name" -n "$namespace" \
      -o jsonpath='{.status.clusterStatus[0].conditions[?(@.type=="OperatorInstalled")].reason}' 2>/dev/null || true)
    cp=$(oc get multiclustermesh "$name" -n "$namespace" \
      -o jsonpath='{.status.clusterStatus[0].conditions[?(@.type=="ControlPlaneReady")].reason}' 2>/dev/null || true)
    gw=$(oc get multiclustermesh "$name" -n "$namespace" \
      -o jsonpath='{.status.clusterStatus[0].conditions[?(@.type=="GatewayReady")].reason}' 2>/dev/null || true)
    echo "  ... Operator=${op:-pending} CP=${cp:-pending} GW=${gw:-pending} (${elapsed}s)"

    if [ "$elapsed" -ge "$timeout" ]; then
      echo "  [WARN] Timed out waiting for ${name} (may still be reconciling)"
      return 1
    fi
    sleep 15
    elapsed=$((elapsed + 15))
  done
}

install_infrastructure() {
  echo "=== Creating ManagedClusterSet ==="
  oc apply -f - <<'EOF' || die "Failed to create ManagedClusterSet"
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: demo-cluster-set
EOF

  oc label managedcluster local-cluster \
    cluster.open-cluster-management.io/clusterset=demo-cluster-set --overwrite \
    || die "Failed to label local-cluster"

  echo "=== Creating MCM namespaces ==="
  oc create namespace secure-mcm-ns --dry-run=client -o yaml | oc apply -f -
  oc create namespace unsecure-mcm-ns --dry-run=client -o yaml | oc apply -f -
}

install_trust_chain() {
  echo "=== Deploying cert-manager trust chain in secure-mcm-ns ==="
  oc apply -n secure-mcm-ns -f "${REPO_ROOT}/samples/cert-manager-issuer.yaml" \
    || die "Failed to create cert-manager trust chain"

  oc wait certificate mesh-root-ca -n secure-mcm-ns --for=condition=Ready --timeout=60s \
    || die "Root CA certificate did not become ready"
}

install_meshes() {
  echo "=== Creating secure-mcm (with trust, NodePort) ==="
  oc apply -f - <<'EOF' || die "Failed to create secure-mcm"
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
    serviceType: NodePort
  topology:
    type: MultiPrimary
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
EOF

  echo "=== Creating unsecure-mcm (no trust, NodePort) ==="
  oc apply -f - <<'EOF' || die "Failed to create unsecure-mcm"
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
    serviceType: NodePort
  topology:
    type: MultiPrimary
EOF

  echo ""
  echo "=== Waiting for controller to reconcile ==="
  wait_for_mesh_ready "secure-mcm" "secure-mcm-ns" "$RECONCILE_TIMEOUT"
  wait_for_mesh_ready "unsecure-mcm" "unsecure-mcm-ns" "$RECONCILE_TIMEOUT"
}

install_discovered() {
  echo "=== Creating standalone discovered Istio CR ==="
  echo "  (Sail operator must be installed by the controller first)"

  local elapsed=0
  until oc get crd istios.sailoperator.io &>/dev/null; do
    if [ "$elapsed" -ge 60 ]; then
      die "Timed out waiting for Istio CRD (operator not installed yet?)"
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  oc create namespace istio-discovered --dry-run=client -o yaml | oc apply -f -

  oc apply -f - <<'EOF' || die "Failed to create discovered Istio CR"
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-standalone
spec:
  namespace: istio-discovered
  values:
    global:
      meshID: standalone-mesh
      multiCluster:
        clusterName: local-cluster
      network: network-standalone
EOF
  echo "  [ok] Standalone Istio CR created"
}

install() {
  preflight
  install_cert_manager
  install_infrastructure
  install_trust_chain
  install_meshes
  install_discovered

  echo ""
  echo "=== Verification ==="
  echo ""
  echo "MCMs:"
  oc get multiclustermesh -A
  echo ""
  echo "Istio CRs:"
  oc get istios
  echo ""
  echo "ManifestWorks:"
  oc get manifestwork -n local-cluster | grep multicluster-mesh
  echo ""
  echo "Done. ACM Search may take 1-2 minutes to index the Istio CRs."
}

uninstall() {
  echo "=== Removing standalone Istio CRs ==="
  oc delete istio discovered-standalone --ignore-not-found 2>/dev/null || true

  echo "=== Removing MCMs (controller auto-cleans ManifestWorks) ==="
  oc delete multiclustermesh secure-mcm -n secure-mcm-ns --ignore-not-found 2>/dev/null || true
  oc delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns --ignore-not-found 2>/dev/null || true

  echo "  Waiting for controller to clean up ManifestWorks..."
  local elapsed=0
  while oc get manifestwork -n local-cluster 2>/dev/null | grep -q multicluster-mesh; do
    if [ "$elapsed" -ge 60 ]; then
      echo "  [WARN] ManifestWorks still present after 60s, continuing cleanup"
      break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  echo "=== Removing cert-manager trust chain ==="
  oc delete -n secure-mcm-ns -f "${REPO_ROOT}/samples/cert-manager-issuer.yaml" --ignore-not-found 2>/dev/null || true

  echo "=== Removing namespaces ==="
  oc delete namespace secure-mcm-ns unsecure-mcm-ns istio-discovered --ignore-not-found 2>/dev/null || true

  echo "=== Removing ManagedClusterSet ==="
  oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset- 2>/dev/null || true
  oc delete managedclusterset demo-cluster-set --ignore-not-found 2>/dev/null || true

  uninstall_cert_manager

  echo ""
  echo "Done. All demo resources removed."
}

case "${1:-}" in
  install)
    install
    ;;
  uninstall)
    uninstall
    ;;
  *)
    echo "Usage: $0 {install|uninstall}" >&2
    exit 1
    ;;
esac
