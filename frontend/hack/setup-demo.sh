#!/usr/bin/env bash
#
# Creates (or tears down) demo resources for the Fleet Service Mesh console plugin.
#
# Usage:
#   hack/setup-demo.sh install     # create all demo resources
#   hack/setup-demo.sh uninstall   # remove all demo resources
#
# Prerequisites (checked automatically on install):
#   - ACM installed and MultiClusterHub in Running phase (DEV-INSTALL.md step 1)
#   - cert-manager installed (DEV-INSTALL.md step 2)
#   - Backend controller deployed in multicluster-mesh-system (DEV-INSTALL.md step 3)
#   - Frontend ConsolePlugin 'ossm-acm' deployed (DEV-INSTALL.md step 5)
#   - oc logged in with cluster-admin privileges
#
# What this creates:
#   Infrastructure:
#     mesh-cluster-set        - ManagedClusterSet with local-cluster bound to it
#     mesh-system namespace   - home for MultiClusterMesh CRs and cert-manager trust chain
#     OSSM 3.x Subscription   - Sail operator (if not already installed)
#     cert-manager trust chain - self-signed root CA Issuer in mesh-system
#
#   MCMs:
#     my-mesh       - primary mesh with trust enabled, controlPlane.namespace: istio-system
#     staging-mesh  - second mesh without trust, controlPlane.namespace: istio-staging
#
#   Istio CRs (4 total, each in its own namespace):
#     default        -> istio-system        -> managed by my-mesh
#     staging        -> istio-staging       -> managed by staging-mesh
#     dev-standalone -> istio-dev           -> standalone (unmanaged)
#     experiments    -> istio-experiments   -> standalone (unmanaged)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ISTIO_VERSION="${ISTIO_VERSION:-v1.28-latest}"
OPERATOR_TIMEOUT="${OPERATOR_TIMEOUT:-180}"

die() { echo "ERROR: $*" >&2; exit 1; }

preflight() {
  echo "=== Checking prerequisites ==="

  if ! oc whoami &>/dev/null; then
    die "Not logged in to a cluster. Run 'oc login' first."
  fi
  echo "  [ok] Logged in as $(oc whoami) on $(oc whoami --show-server)"

  local ok=true

  # ACM installed and running
  local mch_phase
  mch_phase=$(oc get multiclusterhub -A -o jsonpath='{.items[0].status.phase}' 2>/dev/null || true)
  if [ "$mch_phase" = "Running" ]; then
    echo "  [ok] ACM MultiClusterHub is Running"
  else
    echo "  [FAIL] ACM is not installed or not ready (MultiClusterHub phase: ${mch_phase:-not found})"
    echo "         See DEV-INSTALL.md step 1"
    ok=false
  fi

  # Backend controller deployed and running
  local backend_available
  backend_available=$(oc get deployment multicluster-mesh-controller \
    -n multicluster-mesh-system \
    -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
  if [ "${backend_available:-0}" -ge 1 ]; then
    echo "  [ok] Backend controller is running"
  else
    echo "  [FAIL] Backend controller is not deployed or not ready"
    echo "         See DEV-INSTALL.md step 3"
    ok=false
  fi

  # Frontend ConsolePlugin deployed and enabled
  local plugin_state
  plugin_state=$(oc get consoleplugin ossm-acm -o name 2>/dev/null || true)
  if [ -n "$plugin_state" ]; then
    echo "  [ok] Frontend ConsolePlugin 'ossm-acm' is registered"
  else
    echo "  [FAIL] Frontend ConsolePlugin 'ossm-acm' is not deployed"
    echo "         Run 'make build deploy' from the frontend/ directory (see DEV-INSTALL.md step 5)"
    ok=false
  fi

  # cert-manager installed
  local cm_available
  cm_available=$(oc get deployment cert-manager -n cert-manager \
    -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
  if [ "${cm_available:-0}" -ge 1 ]; then
    echo "  [ok] cert-manager is running"
  else
    echo "  [FAIL] cert-manager is not installed or not ready"
    echo "         See DEV-INSTALL.md step 2"
    ok=false
  fi

  if [ "$ok" = false ]; then
    die "Prerequisites not met. Fix the issues above and re-run."
  fi
  echo ""
}

install_operator() {
  echo "=== Installing OSSM 3.x operator ==="

  local current_csv
  current_csv=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
    -n openshift-operators -o jsonpath='{.status.currentCSV}' 2>/dev/null || true)

  if [ -n "$current_csv" ]; then
    local phase
    phase=$(oc get csv "$current_csv" -n openshift-operators \
      -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [ "$phase" = "Succeeded" ]; then
      echo "  Sail operator already installed ($current_csv), skipping."
      return
    fi
  fi

  oc apply -f - <<'EOF'
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: servicemeshoperator3
  namespace: openshift-operators
spec:
  channel: stable
  installPlanApproval: Automatic
  name: servicemeshoperator3
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF

  echo "  Waiting for subscription to resolve (timeout: ${OPERATOR_TIMEOUT}s)..."
  local elapsed=0
  while true; do
    local current_csv
    current_csv=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
      -n openshift-operators -o jsonpath='{.status.currentCSV}' 2>/dev/null || true)
    if [ -n "$current_csv" ]; then
      break
    fi

    local resolution_failed
    resolution_failed=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
      -n openshift-operators \
      -o jsonpath='{.status.conditions[?(@.type=="ResolutionFailed")].status}' 2>/dev/null || true)
    if [ "$resolution_failed" = "True" ]; then
      local msg
      msg=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
        -n openshift-operators \
        -o jsonpath='{.status.conditions[?(@.type=="ResolutionFailed")].message}' 2>/dev/null || true)
      die "Subscription resolution failed: ${msg}"
    fi

    if [ "$elapsed" -ge "$OPERATOR_TIMEOUT" ]; then
      die "Timed out waiting for subscription to resolve after ${OPERATOR_TIMEOUT}s"
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  local csv
  csv=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
    -n openshift-operators -o jsonpath='{.status.currentCSV}')
  echo "  Waiting for ${csv} to succeed..."
  oc wait --for=jsonpath='{.status.phase}'=Succeeded \
    csv/"${csv}" -n openshift-operators --timeout="${OPERATOR_TIMEOUT}s" \
    || die "CSV ${csv} did not reach Succeeded phase within ${OPERATOR_TIMEOUT}s"

  echo "  Waiting for Istio CRD to be registered..."
  local crd_elapsed=0
  until oc get crd istios.sailoperator.io &>/dev/null; do
    if [ "$crd_elapsed" -ge 60 ]; then
      die "Timed out waiting for Istio CRD after 60s"
    fi
    sleep 3
    crd_elapsed=$((crd_elapsed + 3))
  done
  echo "  Sail operator installed."
}

install_base_mesh() {
  echo "=== Creating ManagedClusterSet and mesh-system namespace ==="
  oc apply -f - <<'EOF' || die "Failed to create ManagedClusterSet"
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: mesh-cluster-set
EOF

  oc label managedcluster local-cluster \
    cluster.open-cluster-management.io/clusterset=mesh-cluster-set --overwrite \
    || die "Failed to label local-cluster"

  oc create namespace mesh-system --dry-run=client -o yaml | oc apply -f - \
    || die "Failed to create mesh-system namespace"

  echo "=== Creating cert-manager trust chain ==="
  oc apply -f "${REPO_ROOT}/samples/cert-manager-issuer.yaml" \
    || die "Failed to create cert-manager trust chain"

  echo "=== Creating my-mesh MCM (with trust) ==="
  oc apply -f - <<'EOF' || die "Failed to create my-mesh"
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: my-mesh
  namespace: mesh-system
spec:
  clusterSet: mesh-cluster-set
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
EOF
}

install_staging_mesh() {
  echo "=== Creating staging-mesh MCM (no trust) ==="
  oc apply -f - <<'EOF' || die "Failed to create staging-mesh"
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: staging-mesh
  namespace: mesh-system
spec:
  clusterSet: mesh-cluster-set
  controlPlane:
    namespace: istio-staging
EOF
}

install_istio_crs() {
  echo "=== Creating control plane namespaces ==="
  for ns in istio-system istio-staging istio-dev istio-experiments; do
    oc create namespace "$ns" --dry-run=client -o yaml | oc apply -f - \
      || die "Failed to create namespace $ns"
  done

  echo "=== Creating Istio CR: default (managed by my-mesh) ==="
  oc apply -f - <<EOF || die "Failed to create Istio CR 'default'"
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  namespace: istio-system
  version: ${ISTIO_VERSION}
  values:
    global:
      meshID: production
      multiCluster:
        clusterName: local-cluster
      network: network-prod
EOF

  echo "=== Creating Istio CR: staging (managed by staging-mesh) ==="
  oc apply -f - <<EOF || die "Failed to create Istio CR 'staging'"
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: staging
spec:
  namespace: istio-staging
  version: ${ISTIO_VERSION}
  values:
    global:
      meshID: staging
      multiCluster:
        clusterName: local-cluster
      network: network-staging
EOF

  echo "=== Creating Istio CR: dev-standalone (unmanaged) ==="
  oc apply -f - <<EOF || die "Failed to create Istio CR 'dev-standalone'"
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: dev-standalone
spec:
  namespace: istio-dev
  version: ${ISTIO_VERSION}
  values:
    global:
      meshID: dev-sandbox
      network: network-dev
EOF

  echo "=== Creating Istio CR: experiments (unmanaged) ==="
  oc apply -f - <<EOF || die "Failed to create Istio CR 'experiments'"
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: experiments
spec:
  namespace: istio-experiments
  version: ${ISTIO_VERSION}
  values:
    global:
      meshID: experiments
      network: network-lab
EOF
}

install() {
  preflight
  install_operator
  install_base_mesh
  install_staging_mesh
  install_istio_crs

  echo ""
  echo "=== Verifying ==="
  oc get multiclustermesh -n mesh-system
  echo ""
  oc get istio
  echo ""
  echo "Done. ACM Search may take 1-2 minutes to index the Istio CRs."
  echo "Override the Istio version with: ISTIO_VERSION=v1.27-latest hack/setup-demo.sh install"
}

uninstall() {
  echo "=== Removing Istio CRs ==="
  oc delete istio default staging dev-standalone experiments --ignore-not-found 2>/dev/null || true

  echo "=== Removing MCMs ==="
  oc delete multiclustermesh my-mesh staging-mesh -n mesh-system --ignore-not-found 2>/dev/null || true

  echo "=== Removing cert-manager trust chain ==="
  oc delete -f "${REPO_ROOT}/samples/cert-manager-issuer.yaml" --ignore-not-found 2>/dev/null || true

  echo "=== Removing mesh-system namespace ==="
  oc delete namespace mesh-system --ignore-not-found 2>/dev/null || true

  echo "=== Removing control plane namespaces ==="
  oc delete namespace istio-staging istio-dev istio-experiments --ignore-not-found 2>/dev/null || true

  echo "=== Removing ManagedClusterSet binding and set ==="
  oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset- 2>/dev/null || true
  oc delete managedclusterset mesh-cluster-set --ignore-not-found 2>/dev/null || true

  echo "=== Removing istio-system namespace ==="
  oc delete namespace istio-system --ignore-not-found 2>/dev/null || true

  echo "=== Removing OSSM operator ==="
  local csv
  csv=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
    -n openshift-operators -o jsonpath='{.status.currentCSV}' 2>/dev/null || true)
  oc delete subscription.operators.coreos.com servicemeshoperator3 \
    -n openshift-operators --ignore-not-found 2>/dev/null || true
  if [ -n "$csv" ]; then
    oc delete csv "$csv" -n openshift-operators --ignore-not-found 2>/dev/null || true
  fi
  for orphan_csv in $(oc get csv -n openshift-operators -o name 2>/dev/null | grep servicemeshoperator3 || true); do
    oc delete "$orphan_csv" -n openshift-operators --ignore-not-found 2>/dev/null || true
  done
  oc get crd -o name 2>/dev/null | grep -E 'sailoperator\.io|istio\.io' \
    | xargs -r oc delete --ignore-not-found 2>/dev/null || true

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
