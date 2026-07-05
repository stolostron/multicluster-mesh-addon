#!/usr/bin/env bash
#
# setup-mesh-cps.sh — Create Istio control planes for a MultiClusterMesh
#
# This script completes the setup of a multi-cluster Istio service mesh.
# Given a MultiClusterMesh CR that has been reconciled by the mesh addon
# controller (operator installed, trust certificates distributed), this
# script creates Istio control planes on each managed cluster, configures
# cross-cluster endpoint discovery, and installs east-west gateways for
# multi-network traffic.
#
# PREREQUISITES
#
#   1. A MultiClusterMesh CR that has been reconciled by the controller.
#      You need to know its name and namespace (passed via -m and -n).
#      Verify with: kubectl get multiclustermesh -n <namespace> <name>
#
#   2. The Sail/OSSM operator must be installed on all managed clusters
#      in the cluster set. The controller does this automatically — verify
#      with: kubectl get csv -n openshift-operators | grep servicemesh
#
#   3. Cluster-admin access to every managed cluster in the cluster set.
#      The script needs to create resources (Istio CRs, gateways, secrets)
#      on each cluster. Provide access in one of these ways:
#        - For a single-cluster setup (local-cluster): your current
#          oc/kubectl login is sufficient.
#        - For multi-cluster: either set up kubectl contexts named after
#          each cluster, or create per-cluster kubeconfig files in a
#          directory and pass it with --kubeconfig-dir.
#          Example: oc login https://api.cluster1.example.com:6443 \
#                     --kubeconfig=my-kubeconfigs/cluster1.config
#
#   4. (Optional) If trust is configured on the MultiClusterMesh, the
#      controller will have distributed cacerts to each cluster. This
#      script transforms those certificates into the format Istio expects.
#
# USAGE
#
#   setup-mesh-cps.sh [OPTIONS] install     Create control planes
#   setup-mesh-cps.sh [OPTIONS] uninstall   Remove control planes
#
#   Run with -h or --help for full option details and examples.
#

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Label used by this script to tag resources it creates, enabling clean uninstall.
SCRIPT_MANAGED_LABEL="multicluster-mesh-addon/setup-mesh-cps"

WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"

##############################################################################
# Helpers
##############################################################################

die() { echo "ERROR: $*" >&2; exit 1; }
log() { echo "=== $* ==="; }
info() { echo "  $*"; }
warn() { echo "WARNING: $*" >&2; }

# Portable base64 encode with no line wrapping (GNU uses -w 0, macOS omits wrapping by default)
b64_nowrap() {
  if base64 --help 2>&1 | grep -q '\-w'; then
    base64 -w 0
  else
    base64
  fi
}

usage() {
  cat <<'USAGE'
Creates Istio control planes across managed clusters for a MultiClusterMesh.

Usage: setup-mesh-cps.sh [OPTIONS] install|uninstall

Required (for both install and uninstall):
  -m, --mesh NAME          MultiClusterMesh CR name
  -n, --namespace NS       MultiClusterMesh CR namespace

Options:
  -t, --topology TYPE      Mesh topology: multi-primary (default) or primary-remote
  -p, --primary-cluster NAME
                           Primary cluster for primary-remote topology
                           (default: first cluster alphabetically in the cluster set)
  --istio-version VER      Istio version for the control plane (default: determined by the operator)
  --deploy-app BOOL        Deploy a test application across clusters (true|false, default: false)
  --client-exe PATH        Path to oc or kubectl (default: auto-detect)
  --kubeconfig-dir DIR     Directory containing per-cluster kubeconfig files ({cluster}.config)
  -h, --help               Show this help message

Examples:
  # Install multi-primary control planes for mesh "prod-mesh" in namespace "mesh-system"
  setup-mesh-cps.sh -m prod-mesh -n mesh-system install

  # Install primary-remote with a specific primary and test app
  setup-mesh-cps.sh -m prod-mesh -n mesh-system -t primary-remote -p cluster1 --deploy-app true install

  # Remove everything that was installed
  setup-mesh-cps.sh -m prod-mesh -n mesh-system -t primary-remote -p cluster1 --deploy-app true uninstall
USAGE
}

##############################################################################
# Argument parsing
##############################################################################

MESH_NAME=""
MESH_NAMESPACE=""
TOPOLOGY="multi-primary"
PRIMARY_CLUSTER=""
ISTIO_VERSION=""
DEPLOY_APP="false"
CLIENT_EXE=""
KUBECONFIG_DIR=""
ACTION=""

parse_args() {
  while [ $# -gt 0 ]; do
    case "${1}" in
      -m|--mesh)
        MESH_NAME="${2:?'--mesh requires a value'}"
        shift 2
        ;;
      -n|--namespace)
        MESH_NAMESPACE="${2:?'--namespace requires a value'}"
        shift 2
        ;;
      -t|--topology)
        TOPOLOGY="${2:?'--topology requires a value'}"
        shift 2
        ;;
      -p|--primary-cluster)
        PRIMARY_CLUSTER="${2:?'--primary-cluster requires a value'}"
        shift 2
        ;;
      --istio-version)
        ISTIO_VERSION="${2:?'--istio-version requires a value'}"
        shift 2
        ;;
      --deploy-app)
        DEPLOY_APP="${2:?'--deploy-app requires true or false'}"
        shift 2
        ;;
      --client-exe)
        CLIENT_EXE="${2:?'--client-exe requires a path'}"
        shift 2
        ;;
      --kubeconfig-dir)
        KUBECONFIG_DIR="${2:?'--kubeconfig-dir requires a directory'}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      install|uninstall)
        ACTION="${1}"
        shift
        ;;
      *)
        die "Unknown option: ${1}. Run with --help for usage."
        ;;
    esac
  done

  [ -n "${MESH_NAME}" ] || die "Missing required option: -m/--mesh"
  [ -n "${MESH_NAMESPACE}" ] || die "Missing required option: -n/--namespace"
  [ -n "${ACTION}" ] || die "Missing action: specify 'install' or 'uninstall'"

  case "${TOPOLOGY}" in
    multi-primary|primary-remote) ;;
    *) die "Invalid topology '${TOPOLOGY}'. Must be 'multi-primary' or 'primary-remote'." ;;
  esac

  case "${DEPLOY_APP}" in
    true|false) ;;
    *) die "Invalid --deploy-app value '${DEPLOY_APP}'. Must be 'true' or 'false'." ;;
  esac
}

##############################################################################
# Client executable detection
##############################################################################

detect_client_exe() {
  if [ -n "${CLIENT_EXE}" ]; then
    command -v "${CLIENT_EXE}" &>/dev/null || die "Client executable not found: ${CLIENT_EXE}"
    return
  fi
  if command -v oc &>/dev/null; then
    CLIENT_EXE="oc"
  elif command -v kubectl &>/dev/null; then
    CLIENT_EXE="kubectl"
  else
    die "Neither 'oc' nor 'kubectl' found in PATH. Install one or use --client-exe."
  fi
}

##############################################################################
# Cluster access — run a command on a managed cluster
##############################################################################

# kube_on CLUSTER_NAME COMMAND [ARGS...]
# Executes a kubectl/oc command targeted at the given managed cluster.
kube_on() {
  local cluster="${1}"
  shift

  if [ "${cluster}" = "local-cluster" ]; then
    "${CLIENT_EXE}" "$@"
    return $?
  fi

  if [ -n "${KUBECONFIG_DIR}" ] && [ -f "${KUBECONFIG_DIR}/${cluster}.config" ]; then
    "${CLIENT_EXE}" --kubeconfig="${KUBECONFIG_DIR}/${cluster}.config" "$@"
    return $?
  fi

  # Try context named after the cluster, then kind-{cluster}
  if "${CLIENT_EXE}" config get-contexts "${cluster}" &>/dev/null 2>&1; then
    "${CLIENT_EXE}" --context="${cluster}" "$@"
    return $?
  fi

  if "${CLIENT_EXE}" config get-contexts "kind-${cluster}" &>/dev/null 2>&1; then
    "${CLIENT_EXE}" --context="kind-${cluster}" "$@"
    return $?
  fi

  local api_url
  api_url=$("${CLIENT_EXE}" get managedcluster "${cluster}" \
    -o jsonpath='{.spec.managedClusterClientConfigs[0].url}' 2>/dev/null || true)

  die "Cannot access cluster \"${cluster}\"${api_url:+ (API: ${api_url})}.

To provide access, use one of:
  --kubeconfig-dir DIR    Directory with per-cluster kubeconfig files ({cluster}.config)
                          Create one with: ${CLIENT_EXE} login ${api_url:-https://api.CLUSTER:6443} \\
                            --kubeconfig=DIR/${cluster}.config
  kubectl context         Ensure a context named \"${cluster}\" exists in your kubeconfig"
}

##############################################################################
# Platform detection
##############################################################################

# is_openshift CLUSTER_NAME — returns 0 if the cluster is an OpenShift variant
is_openshift() {
  local cluster="${1}"
  local product
  product=$("${CLIENT_EXE}" get managedcluster "${cluster}" \
    -o jsonpath='{.status.clusterClaims[?(@.name=="product.open-cluster-management.io")].value}' 2>/dev/null || true)
  case "${product}" in
    OpenShift|ROSA|ARO|ROKS|OpenShiftDedicated) return 0 ;;
    *) return 1 ;;
  esac
}

##############################################################################
# Read MCM and discover clusters
##############################################################################

CP_NAMESPACE=""
CLUSTER_SET=""
TRUST_ISSUER=""
MESH_ID=""
TRUST_DOMAIN=""
CLUSTERS=()

read_mcm() {
  log "Reading MultiClusterMesh ${MESH_NAMESPACE}/${MESH_NAME}"

  "${CLIENT_EXE}" get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" &>/dev/null \
    || die "MultiClusterMesh '${MESH_NAME}' not found in namespace '${MESH_NAMESPACE}'."

  CLUSTER_SET=$("${CLIENT_EXE}" get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" \
    -o jsonpath='{.spec.clusterSet}' 2>/dev/null || true)
  [ -n "${CLUSTER_SET}" ] || die "MultiClusterMesh has no spec.clusterSet."

  CP_NAMESPACE=$("${CLIENT_EXE}" get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" \
    -o jsonpath='{.spec.controlPlane.namespace}' 2>/dev/null || true)
  CP_NAMESPACE="${CP_NAMESPACE:-istio-system}"

  TRUST_ISSUER=$("${CLIENT_EXE}" get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" \
    -o jsonpath='{.spec.security.trust.certManager.issuerRef.name}' 2>/dev/null || true)

  MESH_ID="${MESH_NAMESPACE}-${MESH_NAME}"
  TRUST_DOMAIN="${MESH_NAME}"

  info "Cluster set: ${CLUSTER_SET}"
  info "Control plane namespace: ${CP_NAMESPACE}"
  info "Mesh ID: ${MESH_ID}"
  info "Trust domain: ${TRUST_DOMAIN}"
  info "Trust issuer: ${TRUST_ISSUER:-<not configured>}"
}

discover_clusters() {
  log "Discovering clusters in cluster set '${CLUSTER_SET}'"

  local cluster_names
  cluster_names=$("${CLIENT_EXE}" get managedcluster \
    -l "cluster.open-cluster-management.io/clusterset=${CLUSTER_SET}" \
    -o jsonpath='{.items[*].metadata.name}') \
    || die "Failed to list managed clusters in cluster set '${CLUSTER_SET}'."

  [ -n "${cluster_names}" ] || die "No clusters found in cluster set '${CLUSTER_SET}'."

  # Sort alphabetically (matching backend controller behavior)
  IFS=' ' read -ra CLUSTERS <<< "$(echo "${cluster_names}" | tr ' ' '\n' | sort | tr '\n' ' ')"

  info "Found ${#CLUSTERS[@]} cluster(s): ${CLUSTERS[*]}"

  if [ "${TOPOLOGY}" = "primary-remote" ]; then
    if [ -z "${PRIMARY_CLUSTER}" ]; then
      PRIMARY_CLUSTER="${CLUSTERS[0]}"
      info "No --primary-cluster specified, using '${PRIMARY_CLUSTER}' (first alphabetically)"
    else
      local found=false
      for c in "${CLUSTERS[@]}"; do
        [ "${c}" = "${PRIMARY_CLUSTER}" ] && found=true
      done
      [ "${found}" = true ] || die "Primary cluster '${PRIMARY_CLUSTER}' is not in cluster set '${CLUSTER_SET}'."
    fi
  fi

  # Validate access to every cluster
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" cluster-info &>/dev/null \
      || die "Cannot reach cluster '${cluster}'. Ensure you have cluster-admin access."
    info "  [ok] ${cluster}"
  done
}

##############################################################################
# Cacerts key transformation
##############################################################################

transform_cacerts() {
  if [ -z "${TRUST_ISSUER}" ]; then
    warn "Trust is not configured on this MultiClusterMesh. The mesh will use self-signed certificates — cross-cluster mTLS will not work without a shared trust root."
    return
  fi

  log "Transforming trust certificates to Istio format"

  for cluster in "${CLUSTERS[@]}"; do
    info "Waiting for cacerts secret on ${cluster}..."

    local elapsed=0
    local retried_mw=false
    while ! kube_on "${cluster}" get secret cacerts -n "${CP_NAMESPACE}" &>/dev/null; do
      # If the ManifestWork failed (e.g. because the namespace didn't exist when it
      # was first created), delete it so the controller recreates it with a fresh state.
      if [ "${retried_mw}" = "false" ] && [ "${elapsed}" -ge 30 ]; then
        local mw_applied
        mw_applied=$("${CLIENT_EXE}" get manifestwork multicluster-mesh-cacerts -n "${cluster}" \
          -o jsonpath='{.status.conditions[?(@.type=="Applied")].status}' 2>/dev/null || true)
        if [ "${mw_applied}" = "False" ]; then
          info "  Cacerts ManifestWork failed — deleting to trigger controller re-creation..."
          "${CLIENT_EXE}" delete manifestwork multicluster-mesh-cacerts -n "${cluster}" --ignore-not-found 2>/dev/null || true
          "${CLIENT_EXE}" annotate multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" \
            "setup-mesh-cps/trigger=$(date +%s)" --overwrite 2>/dev/null || true
          retried_mw=true
        fi
      fi

      if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
        die "Timed out waiting for cacerts secret on cluster '${cluster}'. Ensure the MultiClusterMesh controller has reconciled and the ManifestWork has been applied."
      fi
      sleep 5
      elapsed=$((elapsed + 5))
    done

    # Check if already transformed (idempotent)
    local has_istio_keys
    has_istio_keys=$(kube_on "${cluster}" get secret cacerts -n "${CP_NAMESPACE}" \
      -o jsonpath='{.data.ca-cert\.pem}' 2>/dev/null || true)
    if [ -n "${has_istio_keys}" ]; then
      info "  [ok] ${cluster}: cacerts already in Istio format, skipping"
      continue
    fi

    info "  Transforming cacerts on ${cluster}..."

    local tls_crt tls_key ca_crt
    tls_crt=$(kube_on "${cluster}" get secret cacerts -n "${CP_NAMESPACE}" -o jsonpath='{.data.tls\.crt}') \
      || die "Failed to read tls.crt from cacerts on ${cluster}"
    tls_key=$(kube_on "${cluster}" get secret cacerts -n "${CP_NAMESPACE}" -o jsonpath='{.data.tls\.key}') \
      || die "Failed to read tls.key from cacerts on ${cluster}"
    ca_crt=$(kube_on "${cluster}" get secret cacerts -n "${CP_NAMESPACE}" -o jsonpath='{.data.ca\.crt}') \
      || die "Failed to read ca.crt from cacerts on ${cluster}"

    # cert-chain.pem = intermediate cert + root cert (concatenated, base64)
    local cert_chain
    cert_chain=$(echo "${tls_crt}" | base64 -d | cat - <(echo "${ca_crt}" | base64 -d) | b64_nowrap)

    # Secret type is immutable, so delete and recreate (not apply)
    kube_on "${cluster}" delete secret cacerts -n "${CP_NAMESPACE}" --ignore-not-found \
      || die "Failed to delete old cacerts secret on ${cluster}"

    kube_on "${cluster}" create -f - <<EOF || die "Failed to create transformed cacerts on ${cluster}"
apiVersion: v1
kind: Secret
metadata:
  name: cacerts
  namespace: ${CP_NAMESPACE}
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
type: Opaque
data:
  ca-cert.pem: ${tls_crt}
  ca-key.pem: ${tls_key}
  root-cert.pem: ${ca_crt}
  cert-chain.pem: ${cert_chain}
EOF
    info "  [ok] ${cluster}"
  done
}

##############################################################################
# IstioCNI
##############################################################################

install_istiocni() {
  for cluster in "${CLUSTERS[@]}"; do
    if ! is_openshift "${cluster}"; then
      continue
    fi

    if kube_on "${cluster}" get istiocni default &>/dev/null; then
      info "  [ok] IstioCNI already exists on ${cluster}, skipping"
      continue
    fi

    info "Creating IstioCNI on ${cluster}..."
    kube_on "${cluster}" create namespace istio-cni --dry-run=client -o yaml | kube_on "${cluster}" apply -f - \
      || die "Failed to create istio-cni namespace on ${cluster}"

    local version_field=""
    if [ -n "${ISTIO_VERSION}" ]; then
      version_field="  version: ${ISTIO_VERSION}"
    fi

    kube_on "${cluster}" apply -f - <<EOF || die "Failed to create IstioCNI on ${cluster}"
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  namespace: istio-cni
${version_field:+${version_field}}
EOF
    info "  [ok] ${cluster}"
  done
}

uninstall_istiocni() {
  for cluster in "${CLUSTERS[@]}"; do
    if ! is_openshift "${cluster}"; then
      continue
    fi

    local managed
    managed=$(kube_on "${cluster}" get istiocni default \
      -o jsonpath="{.metadata.labels['${SCRIPT_MANAGED_LABEL}']}" 2>/dev/null || true)
    if [ "${managed}" != "true" ]; then
      info "  IstioCNI on ${cluster} was not created by this script, skipping"
      continue
    fi

    kube_on "${cluster}" delete istiocni default --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete namespace istio-cni --ignore-not-found 2>/dev/null || true
    info "  [ok] Removed IstioCNI on ${cluster}"
  done
}

##############################################################################
# Istio CR creation
##############################################################################

create_istio_cr() {
  local cluster="${1}"
  local profile="${2:-}"
  local external_istiod="${3:-false}"
  local remote_pilot_address="${4:-}"

  local network_id="network-${cluster}"
  local cr_name="${MESH_ID}-cp"

  info "Creating Istio control plane on ${cluster}..."

  kube_on "${cluster}" create namespace "${CP_NAMESPACE}" --dry-run=client -o yaml \
    | kube_on "${cluster}" apply -f - \
    || die "Failed to create namespace ${CP_NAMESPACE} on ${cluster}"

  kube_on "${cluster}" label namespace "${CP_NAMESPACE}" \
    "topology.istio.io/network=${network_id}" --overwrite \
    || die "Failed to label namespace ${CP_NAMESPACE} on ${cluster}"

  # Build the Istio CR YAML
  local yaml="apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: ${cr_name}
  labels:
    ${SCRIPT_MANAGED_LABEL}: \"true\"
spec:
  namespace: ${CP_NAMESPACE}"

  if [ -n "${ISTIO_VERSION}" ]; then
    yaml="${yaml}
  version: ${ISTIO_VERSION}"
  fi

  if [ -n "${profile}" ]; then
    yaml="${yaml}
  profile: ${profile}"
  fi

  yaml="${yaml}
  values:
    meshConfig:
      trustDomain: ${TRUST_DOMAIN}
      defaultConfig:
        proxyMetadata:
          ISTIO_META_DNS_CAPTURE: \"true\"
          ISTIO_META_DNS_AUTO_ALLOCATE: \"true\"
    global:
      meshID: ${MESH_ID}
      multiCluster:
        clusterName: ${cluster}
      network: ${network_id}"

  if [ "${external_istiod}" = "true" ]; then
    yaml="${yaml}
      externalIstiod: true"
  fi

  if [ -n "${remote_pilot_address}" ]; then
    yaml="${yaml}
      remotePilotAddress: ${remote_pilot_address}"
  fi

  echo "${yaml}" | kube_on "${cluster}" apply -f - \
    || die "Failed to create Istio CR on ${cluster}"
  info "  [ok] ${cluster}"
}

wait_istio_ready() {
  local cluster="${1}"
  local cr_name="${MESH_ID}-cp"

  info "Waiting for Istio control plane to be ready on ${cluster}..."

  local elapsed=0
  while true; do
    local ready
    ready=$(kube_on "${cluster}" get istio "${cr_name}" \
      -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
    if [ "${ready}" = "True" ]; then
      info "  [ok] Istio ready on ${cluster}"
      return
    fi

    if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
      local msg
      msg=$(kube_on "${cluster}" get istio "${cr_name}" \
        -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || true)
      die "Timed out waiting for Istio to be ready on ${cluster}. Status: ${msg:-unknown}"
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
}

##############################################################################
# East-west gateway
##############################################################################

install_eastwest_gateway() {
  local cluster="${1}"
  local network_id="network-${cluster}"
  local cr_name="${MESH_ID}-cp"
  local revision="${cr_name}"

  info "Installing east-west gateway on ${cluster}..."

  kube_on "${cluster}" apply -f - <<EOF || die "Failed to create east-west gateway on ${cluster}"
apiVersion: v1
kind: ServiceAccount
metadata:
  name: istio-eastwestgateway
  namespace: ${CP_NAMESPACE}
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-eastwestgateway
  namespace: ${CP_NAMESPACE}
  labels:
    app: istio-eastwestgateway
    istio: eastwestgateway
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: istio-eastwestgateway
      istio: eastwestgateway
  template:
    metadata:
      labels:
        app: istio-eastwestgateway
        istio: eastwestgateway
        istio.io/rev: ${revision}
        topology.istio.io/network: ${network_id}
      annotations:
        inject.istio.io/templates: gateway
    spec:
      serviceAccountName: istio-eastwestgateway
      containers:
      - name: istio-proxy
        image: auto
        env:
        - name: ISTIO_META_REQUESTED_NETWORK_VIEW
          value: "${network_id}"
---
apiVersion: v1
kind: Service
metadata:
  name: istio-eastwestgateway
  namespace: ${CP_NAMESPACE}
  labels:
    app: istio-eastwestgateway
    istio: eastwestgateway
    topology.istio.io/network: ${network_id}
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  type: LoadBalancer
  selector:
    app: istio-eastwestgateway
    istio: eastwestgateway
  ports:
  - name: status-port
    port: 15021
    targetPort: 15021
  - name: tls
    port: 15443
    targetPort: 15443
  - name: tls-istiod
    port: 15012
    targetPort: 15012
  - name: tls-webhook
    port: 15017
    targetPort: 15017
EOF

  info "  Waiting for east-west gateway pod on ${cluster}..."
  local elapsed=0
  while true; do
    local available
    available=$(kube_on "${cluster}" get deploy istio-eastwestgateway -n "${CP_NAMESPACE}" \
      -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
    if [ "${available:-0}" -ge 1 ]; then
      break
    fi
    if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
      die "Timed out waiting for east-west gateway to be ready on ${cluster}."
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  info "  [ok] East-west gateway running on ${cluster}"
}

apply_expose_services() {
  local cluster="${1}"

  info "Exposing services on ${cluster}..."
  kube_on "${cluster}" apply -f - <<EOF || die "Failed to apply expose-services on ${cluster}"
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: cross-network-gateway
  namespace: ${CP_NAMESPACE}
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 15443
      name: tls
      protocol: TLS
    tls:
      mode: AUTO_PASSTHROUGH
    hosts:
    - "*.local"
EOF
  info "  [ok] ${cluster}"
}

apply_expose_istiod() {
  local cluster="${1}"

  info "Exposing istiod on ${cluster}..."
  kube_on "${cluster}" apply -f - <<EOF || die "Failed to apply expose-istiod on ${cluster}"
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: istiod-gateway
  namespace: ${CP_NAMESPACE}
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 15012
      name: tls-istiod
      protocol: TLS
    tls:
      mode: PASSTHROUGH
    hosts:
    - "*"
  - port:
      number: 15017
      name: tls-istiodwebhook
      protocol: TLS
    tls:
      mode: PASSTHROUGH
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: istiod-vs
  namespace: ${CP_NAMESPACE}
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
spec:
  hosts:
  - "*"
  gateways:
  - istiod-gateway
  tls:
  - match:
    - port: 15012
      sniHosts:
      - "*"
    route:
    - destination:
        host: istiod.${CP_NAMESPACE}.svc.cluster.local
        port:
          number: 15012
  - match:
    - port: 15017
      sniHosts:
      - "*"
    route:
    - destination:
        host: istiod.${CP_NAMESPACE}.svc.cluster.local
        port:
          number: 443
EOF
  info "  [ok] ${cluster}"
}

##############################################################################
# Remote secrets
##############################################################################

get_eastwest_lb_ip() {
  local cluster="${1}"
  local elapsed=0

  while true; do
    local ip
    ip=$(kube_on "${cluster}" get svc istio-eastwestgateway -n "${CP_NAMESPACE}" \
      -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)
    if [ -n "${ip}" ]; then
      echo "${ip}"
      return
    fi
    # Also check hostname (some cloud providers use hostname instead of IP)
    ip=$(kube_on "${cluster}" get svc istio-eastwestgateway -n "${CP_NAMESPACE}" \
      -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
    if [ -n "${ip}" ]; then
      echo "${ip}"
      return
    fi
    if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
      die "Timed out waiting for east-west gateway LoadBalancer IP on ${cluster}."
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done
}

# create_remote_secret SOURCE_CLUSTER TARGET_CLUSTER
# Creates a remote secret for SOURCE_CLUSTER and applies it on TARGET_CLUSTER.
create_remote_secret() {
  local source_cluster="${1}"
  local target_cluster="${2}"
  local token_secret_name="istio-reader-${source_cluster}-token"

  info "Creating remote secret: ${source_cluster} -> ${target_cluster}..."

  # Wait for the istio-reader-service-account SA to exist
  local elapsed=0
  while ! kube_on "${source_cluster}" get sa istio-reader-service-account -n "${CP_NAMESPACE}" &>/dev/null; do
    if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
      die "Timed out waiting for istio-reader-service-account on ${source_cluster}."
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  # Create a bound token Secret if one doesn't already exist (K8s 1.24+ no longer auto-creates SA token secrets)
  if ! kube_on "${source_cluster}" get secret "${token_secret_name}" -n "${CP_NAMESPACE}" &>/dev/null; then
    kube_on "${source_cluster}" apply -f - <<EOF || die "Failed to create SA token secret on ${source_cluster}"
apiVersion: v1
kind: Secret
metadata:
  name: ${token_secret_name}
  namespace: ${CP_NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: istio-reader-service-account
  labels:
    ${SCRIPT_MANAGED_LABEL}: "true"
type: kubernetes.io/service-account-token
EOF

    # Wait for the token controller to populate the secret
    elapsed=0
    while true; do
      local token
      token=$(kube_on "${source_cluster}" get secret "${token_secret_name}" -n "${CP_NAMESPACE}" \
        -o jsonpath='{.data.token}' 2>/dev/null || true)
      if [ -n "${token}" ]; then
        break
      fi
      if [ "${elapsed}" -ge 60 ]; then
        die "Timed out waiting for token to be populated in secret ${token_secret_name} on ${source_cluster}."
      fi
      sleep 3
      elapsed=$((elapsed + 3))
    done
  fi

  local token ca_data api_server
  token=$(kube_on "${source_cluster}" get secret "${token_secret_name}" -n "${CP_NAMESPACE}" \
    -o jsonpath='{.data.token}') \
    || die "Failed to read token from ${token_secret_name} on ${source_cluster}"
  ca_data=$(kube_on "${source_cluster}" get secret "${token_secret_name}" -n "${CP_NAMESPACE}" \
    -o jsonpath='{.data.ca\.crt}') \
    || die "Failed to read ca.crt from ${token_secret_name} on ${source_cluster}"

  api_server=$("${CLIENT_EXE}" get managedcluster "${source_cluster}" \
    -o jsonpath='{.spec.managedClusterClientConfigs[0].url}' 2>/dev/null || true)

  if [ -z "${api_server}" ]; then
    die "Cannot determine API server URL for cluster '${source_cluster}'. Ensure ManagedCluster has managedClusterClientConfigs."
  fi

  # Decode token for the kubeconfig (it's stored base64-encoded in the Secret)
  local decoded_token
  decoded_token=$(echo "${token}" | base64 -d)

  kube_on "${target_cluster}" apply -f - <<EOF || die "Failed to apply remote secret for ${source_cluster} on ${target_cluster}"
apiVersion: v1
kind: Secret
metadata:
  name: istio-remote-secret-${source_cluster}
  namespace: ${CP_NAMESPACE}
  labels:
    istio/multiCluster: "true"
    ${SCRIPT_MANAGED_LABEL}: "true"
  annotations:
    networking.istio.io/cluster: ${source_cluster}
type: Opaque
stringData:
  ${source_cluster}: |
    apiVersion: v1
    kind: Config
    clusters:
    - cluster:
        certificate-authority-data: ${ca_data}
        server: ${api_server}
      name: ${source_cluster}
    contexts:
    - context:
        cluster: ${source_cluster}
        user: ${source_cluster}
      name: ${source_cluster}
    current-context: ${source_cluster}
    users:
    - name: ${source_cluster}
      user:
        token: ${decoded_token}
EOF
  info "  [ok] ${source_cluster} -> ${target_cluster}"
}

##############################################################################
# Test application (mesh-hello)
##############################################################################

TEST_APP_NS=""

deploy_test_app() {
  local cluster_a="${1}"
  local cluster_b="${2}"

  TEST_APP_NS="${MESH_NAME}-testapp"
  local route_name="mesh-hello-${MESH_NAME}"
  local revision="${MESH_ID}-cp"
  local backend_url="http://mesh-hello-backend.${TEST_APP_NS}.svc.cluster.local:8080/api"
  local app_image="registry.access.redhat.com/ubi9/python-311"

  [ ${#TEST_APP_NS} -le 63 ] || die "Test app namespace '${TEST_APP_NS}' exceeds 63 characters. Use a shorter mesh name."
  [ ${#route_name} -le 63 ] || die "Test app route name '${route_name}' exceeds 63 characters. Use a shorter mesh name."

  log "Deploying test application (mesh-hello)"
  info "Namespace: ${TEST_APP_NS}"
  info "Frontend: ${cluster_a}, Backend: ${cluster_b}"

  # Create namespace on both clusters with revision-based injection
  for cluster in "${cluster_a}" "${cluster_b}"; do
    kube_on "${cluster}" create namespace "${TEST_APP_NS}" --dry-run=client -o yaml \
      | kube_on "${cluster}" apply -f - \
      || die "Failed to create ${TEST_APP_NS} namespace on ${cluster}"
    kube_on "${cluster}" label namespace "${TEST_APP_NS}" \
      "istio.io/rev=${revision}" --overwrite \
      || die "Failed to label ${TEST_APP_NS} namespace on ${cluster}"
    # Ensure istio-injection label is NOT set (conflicts with rev-based injection)
    kube_on "${cluster}" label namespace "${TEST_APP_NS}" "istio-injection-" 2>/dev/null || true
  done

  # Apply the shared ConfigMap on both clusters
  for cluster in "${cluster_a}" "${cluster_b}"; do
    kube_on "${cluster}" apply -n "${TEST_APP_NS}" -f - <<'PYEOF' || die "Failed to create ConfigMap on ${cluster}"
apiVersion: v1
kind: ConfigMap
metadata:
  name: mesh-hello-app
data:
  app.py: |
    import http.server
    import json
    import os
    import socket
    import datetime
    import urllib.request
    import urllib.error
    import time
    import re

    def get_identity():
        return {
            "cluster": os.environ.get("CLUSTER_NAME", "unknown"),
            "pod": os.environ.get("POD_NAME", socket.gethostname()),
            "namespace": os.environ.get("POD_NAMESPACE", "unknown"),
            "node": os.environ.get("NODE_NAME", "unknown"),
            "meshID": os.environ.get("MESH_ID", "unknown"),
            "timestamp": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC"),
        }

    def parse_xfcc(xfcc):
        if not xfcc:
            return {"enabled": False}
        result = {"enabled": True}
        uri_match = re.search(r'URI=([^;,]+)', xfcc)
        if uri_match:
            uri = uri_match.group(1)
            result["clientIdentity"] = uri
            spiffe_match = re.match(r'spiffe://([^/]+)/', uri)
            if spiffe_match:
                result["trustDomain"] = spiffe_match.group(1)
        hash_match = re.search(r'Hash=([^;,]+)', xfcc)
        if hash_match:
            result["certHash"] = hash_match.group(1)
        return result

    def identity_row(label, value, badge=False):
        if badge:
            val_html = f'<span class="badge">{value}</span>'
        else:
            val_html = value
        return f'<tr><td>{label}</td><td>{val_html}</td></tr>'

    def mtls_section(mtls):
        if not mtls or not mtls.get("enabled"):
            return '''<div class="card">
              <h2>mTLS Status</h2>
              <p class="status-grey">Not detected &mdash; the cross-cluster call did not pass through an mTLS-enabled sidecar.</p>
            </div>'''
        rows = identity_row("Status", '<span class="status-green">Enabled</span>')
        if "clientIdentity" in mtls:
            rows += identity_row("Client Identity", f'<code>{mtls["clientIdentity"]}</code>')
        if "trustDomain" in mtls:
            rows += identity_row("Trust Domain", mtls["trustDomain"], badge=True)
        if "certHash" in mtls:
            short = mtls["certHash"][:16] + "..." if len(mtls["certHash"]) > 16 else mtls["certHash"]
            rows += identity_row("Certificate Hash", f'<code title="{mtls["certHash"]}">{short}</code>')
        return f'''<div class="card">
          <h2>mTLS Status</h2>
          <table>{rows}</table>
        </div>'''

    STYLES = """
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           max-width: 700px; margin: 40px auto; background: #f0f2f5; color: #333; padding: 0 20px; }
    .card { background: white; border-radius: 12px; padding: 24px 28px; margin-bottom: 16px;
            box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    h1 { color: #0066cc; margin: 0 0 4px 0; font-size: 1.6em; }
    h2 { color: #444; margin: 0 0 12px 0; font-size: 1.1em; font-weight: 600; }
    .subtitle { color: #888; font-size: 0.85em; margin-bottom: 16px; }
    table { border-collapse: collapse; width: 100%; }
    td { padding: 6px 10px; border-bottom: 1px solid #f0f0f0; font-size: 0.92em; }
    td:first-child { font-weight: 600; color: #666; width: 130px; white-space: nowrap; }
    .badge { display: inline-block; background: #0066cc; color: white; padding: 1px 10px;
             border-radius: 12px; font-size: 0.85em; }
    code { background: #f5f5f5; padding: 2px 6px; border-radius: 4px; font-size: 0.88em; }
    .status-green { color: #16a34a; font-weight: 600; }
    .status-grey { color: #999; font-style: italic; }
    .error-box { background: #fef2f2; border: 1px solid #fecaca; border-radius: 8px;
                 padding: 12px 16px; color: #991b1b; font-size: 0.9em; }
    .latency { color: #888; font-size: 0.85em; }
    .refresh { text-align: center; color: #aaa; font-size: 0.8em; margin-top: 8px; }
    """

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/api":
                self.handle_api()
            else:
                self.handle_page()

        def handle_api(self):
            data = get_identity()
            data["mtls"] = parse_xfcc(self.headers.get("X-Forwarded-Client-Cert", ""))
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(data).encode())

        def handle_page(self):
            mode = os.environ.get("APP_MODE", "frontend")
            me = get_identity()

            me_rows = (
                identity_row("Cluster", me["cluster"], badge=True)
                + identity_row("Pod", me["pod"])
                + identity_row("Namespace", me["namespace"])
                + identity_row("Node", me["node"])
                + identity_row("Mesh ID", me["meshID"])
                + identity_row("Timestamp", me["timestamp"])
            )

            backend_html = ""
            mtls_html = ""

            if mode == "frontend":
                backend_url = os.environ.get("BACKEND_URL", "")
                if backend_url:
                    try:
                        start = time.time()
                        with urllib.request.urlopen(backend_url, timeout=5) as resp:
                            latency_ms = (time.time() - start) * 1000
                            backend = json.loads(resp.read().decode())
                        be_rows = (
                            identity_row("Cluster", backend.get("cluster","?"), badge=True)
                            + identity_row("Pod", backend.get("pod","?"))
                            + identity_row("Namespace", backend.get("namespace","?"))
                            + identity_row("Mesh ID", backend.get("meshID","?"))
                            + identity_row("Timestamp", backend.get("timestamp","?"))
                            + identity_row("Latency", f'<span class="latency">{latency_ms:.0f} ms</span>')
                        )
                        backend_html = f'''<div class="card">
                          <h2>Cross-Cluster Call</h2>
                          <table>{be_rows}</table>
                        </div>'''
                        mtls_html = mtls_section(backend.get("mtls"))
                    except Exception as e:
                        backend_html = f'''<div class="card">
                          <h2>Cross-Cluster Call</h2>
                          <div class="error-box">Failed to reach backend: {e}</div>
                        </div>'''
                        mtls_html = mtls_section(None)

            html = f"""<!DOCTYPE html>
    <html>
    <head>
      <title>Mesh Hello - {me["cluster"]}</title>
      <meta http-equiv="refresh" content="10">
      <style>{STYLES}</style>
    </head>
    <body>
      <div class="card">
        <h1>Mesh Hello</h1>
        <p class="subtitle">{"Frontend" if mode == "frontend" else "Backend"} instance</p>
        <table>{me_rows}</table>
      </div>
      {backend_html}
      {mtls_html}
      <p class="refresh">Auto-refreshes every 10 seconds</p>
    </body>
    </html>"""

            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(html.encode())

        def log_message(self, fmt, *args):
            pass

    http.server.HTTPServer(("", 8080), Handler).serve_forever()
PYEOF
  done

  # Deploy frontend on cluster A
  info "Deploying mesh-hello frontend on ${cluster_a}..."
  kube_on "${cluster_a}" apply -n "${TEST_APP_NS}" -f - <<EOF || die "Failed to deploy mesh-hello frontend on ${cluster_a}"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mesh-hello
  labels:
    app: mesh-hello
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mesh-hello
  template:
    metadata:
      labels:
        app: mesh-hello
    spec:
      containers:
      - name: mesh-hello
        image: ${app_image}
        command: ["python3", "/app/app.py"]
        ports:
        - containerPort: 8080
        env:
        - name: APP_MODE
          value: "frontend"
        - name: BACKEND_URL
          value: "${backend_url}"
        - name: CLUSTER_NAME
          value: "${cluster_a}"
        - name: MESH_ID
          value: "${MESH_ID}"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: app
          mountPath: /app
      volumes:
      - name: app
        configMap:
          name: mesh-hello-app
---
apiVersion: v1
kind: Service
metadata:
  name: mesh-hello
spec:
  selector:
    app: mesh-hello
  ports:
  - port: 8080
    targetPort: 8080
EOF

  # Deploy backend on cluster B
  info "Deploying mesh-hello backend on ${cluster_b}..."
  kube_on "${cluster_b}" apply -n "${TEST_APP_NS}" -f - <<EOF || die "Failed to deploy mesh-hello backend on ${cluster_b}"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mesh-hello-backend
  labels:
    app: mesh-hello-backend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mesh-hello-backend
  template:
    metadata:
      labels:
        app: mesh-hello-backend
    spec:
      containers:
      - name: mesh-hello
        image: ${app_image}
        command: ["python3", "/app/app.py"]
        ports:
        - containerPort: 8080
        env:
        - name: APP_MODE
          value: "backend"
        - name: CLUSTER_NAME
          value: "${cluster_b}"
        - name: MESH_ID
          value: "${MESH_ID}"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: app
          mountPath: /app
      volumes:
      - name: app
        configMap:
          name: mesh-hello-app
---
apiVersion: v1
kind: Service
metadata:
  name: mesh-hello-backend
spec:
  selector:
    app: mesh-hello-backend
  ports:
  - port: 8080
    targetPort: 8080
EOF

  # Create Route on OpenShift
  if is_openshift "${cluster_a}"; then
    if ! kube_on "${cluster_a}" get route "${route_name}" -n "${TEST_APP_NS}" &>/dev/null; then
      kube_on "${cluster_a}" expose svc mesh-hello -n "${TEST_APP_NS}" --name="${route_name}" \
        || die "Failed to create Route on ${cluster_a}"
    fi
  fi

  # Wait for deployments
  info "Waiting for deployments to be ready..."
  local elapsed=0
  while true; do
    local fe_ready be_ready
    fe_ready=$(kube_on "${cluster_a}" get deploy mesh-hello -n "${TEST_APP_NS}" \
      -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
    be_ready=$(kube_on "${cluster_b}" get deploy mesh-hello-backend -n "${TEST_APP_NS}" \
      -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
    if [ "${fe_ready:-0}" -ge 1 ] && [ "${be_ready:-0}" -ge 1 ]; then
      break
    fi
    if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
      die "Timed out waiting for test app deployments."
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  info "[ok] Test application deployed"
  echo ""

  if is_openshift "${cluster_a}"; then
    local route_host
    route_host=$(kube_on "${cluster_a}" get route "${route_name}" -n "${TEST_APP_NS}" \
      -o jsonpath='{.spec.host}' 2>/dev/null || true)
    echo "Open in your browser: http://${route_host}"
  else
    echo "Access the test app with:"
    echo "  ${CLIENT_EXE} port-forward -n ${TEST_APP_NS} svc/mesh-hello 8080:8080"
    echo "  Then open: http://localhost:8080"
  fi
  echo ""
}

##############################################################################
# Install — multi-primary
##############################################################################

install_multi_primary() {
  log "Setting up multi-primary mesh topology"

  for cluster in "${CLUSTERS[@]}"; do
    create_istio_cr "${cluster}" "" "false" ""
  done

  for cluster in "${CLUSTERS[@]}"; do
    wait_istio_ready "${cluster}"
  done

  log "Installing east-west gateways"
  for cluster in "${CLUSTERS[@]}"; do
    install_eastwest_gateway "${cluster}"
    apply_expose_services "${cluster}"
  done

  log "Exchanging remote secrets (bidirectional)"
  if [ "${#CLUSTERS[@]}" -le 1 ]; then
    info "Single cluster — skipping remote secret exchange"
  fi
  for source in "${CLUSTERS[@]}"; do
    for target in "${CLUSTERS[@]}"; do
      if [ "${source}" != "${target}" ]; then
        create_remote_secret "${source}" "${target}"
      fi
    done
  done
}

##############################################################################
# Install — primary-remote
##############################################################################

install_primary_remote() {
  log "Setting up primary-remote mesh topology (primary: ${PRIMARY_CLUSTER})"

  create_istio_cr "${PRIMARY_CLUSTER}" "" "true" ""
  wait_istio_ready "${PRIMARY_CLUSTER}"

  install_eastwest_gateway "${PRIMARY_CLUSTER}"
  apply_expose_services "${PRIMARY_CLUSTER}"
  apply_expose_istiod "${PRIMARY_CLUSTER}"

  local primary_lb_ip
  primary_lb_ip=$(get_eastwest_lb_ip "${PRIMARY_CLUSTER}")
  info "Primary east-west gateway address: ${primary_lb_ip}"

  log "Creating remote control planes"
  for cluster in "${CLUSTERS[@]}"; do
    if [ "${cluster}" = "${PRIMARY_CLUSTER}" ]; then
      continue
    fi

    # create_istio_cr handles namespace creation and network label;
    # we add the controlPlaneClusters annotation after.
    create_istio_cr "${cluster}" "remote" "false" "${primary_lb_ip}"

    kube_on "${cluster}" annotate namespace "${CP_NAMESPACE}" \
      "topology.istio.io/controlPlaneClusters=${PRIMARY_CLUSTER}" --overwrite \
      || die "Failed to annotate namespace on ${cluster}"
  done

  log "Installing east-west gateways on remote clusters"
  for cluster in "${CLUSTERS[@]}"; do
    if [ "${cluster}" = "${PRIMARY_CLUSTER}" ]; then
      continue
    fi
    # Remote clusters with profile:remote may take time to become ready
    # (they need to connect to the primary's istiod first)
    local cr_name="${MESH_ID}-cp"
    info "Waiting for remote Istio CR on ${cluster}..."
    local elapsed=0
    while true; do
      local ready
      ready=$(kube_on "${cluster}" get istio "${cr_name}" \
        -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
      if [ "${ready}" = "True" ]; then
        break
      fi
      if [ "${elapsed}" -ge "${WAIT_TIMEOUT}" ]; then
        local msg
        msg=$(kube_on "${cluster}" get istio "${cr_name}" \
          -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || true)
        die "Remote Istio on ${cluster} is not Ready after ${WAIT_TIMEOUT}s (${msg:-unknown}). The remote cluster cannot connect to the primary's istiod. Check network connectivity and that the east-west gateway on the primary cluster has an accessible LoadBalancer IP."
      fi
      sleep 10
      elapsed=$((elapsed + 10))
    done

    install_eastwest_gateway "${cluster}"
    apply_expose_services "${cluster}"
  done

  log "Creating remote secrets (remote -> primary)"
  for cluster in "${CLUSTERS[@]}"; do
    if [ "${cluster}" = "${PRIMARY_CLUSTER}" ]; then
      continue
    fi
    create_remote_secret "${cluster}" "${PRIMARY_CLUSTER}"
  done
}

##############################################################################
# Uninstall
##############################################################################

do_uninstall() {
  log "Uninstalling mesh control planes for ${MESH_NAMESPACE}/${MESH_NAME}"

  if [ "${DEPLOY_APP}" = "true" ]; then
    local test_ns="${MESH_NAME}-testapp"
    log "Removing test application (${test_ns})"
    for cluster in "${CLUSTERS[@]}"; do
      kube_on "${cluster}" delete namespace "${test_ns}" --ignore-not-found 2>/dev/null || true
      info "  [ok] ${cluster}"
    done
  fi

  log "Removing remote secrets and SA token secrets"
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" delete secret -n "${CP_NAMESPACE}" \
      -l "${SCRIPT_MANAGED_LABEL}=true,istio/multiCluster=true" \
      --ignore-not-found 2>/dev/null || true
    # Clean up SA token secrets created for remote secret construction (named istio-reader-{cluster}-token)
    for peer in "${CLUSTERS[@]}"; do
      kube_on "${cluster}" delete secret "istio-reader-${peer}-token" -n "${CP_NAMESPACE}" \
        --ignore-not-found 2>/dev/null || true
    done
    info "  [ok] ${cluster}"
  done

  log "Removing east-west gateways and network resources"
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" delete gateway cross-network-gateway -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete gateway istiod-gateway -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete virtualservice istiod-vs -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete deploy istio-eastwestgateway -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete svc istio-eastwestgateway -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kube_on "${cluster}" delete sa istio-eastwestgateway -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    info "  [ok] ${cluster}"
  done

  log "Removing Istio control planes"
  local cr_name="${MESH_ID}-cp"
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" delete istio "${cr_name}" --ignore-not-found 2>/dev/null || true
    info "  [ok] ${cluster}"
  done

  uninstall_istiocni

  if [ -n "${TRUST_ISSUER}" ]; then
    log "Restoring cacerts to controller-managed format"
    for cluster in "${CLUSTERS[@]}"; do
      # Delete the transformed secret; the controller's next reconcile will re-apply
      # the original via ManifestWork.
      kube_on "${cluster}" delete secret cacerts -n "${CP_NAMESPACE}" --ignore-not-found 2>/dev/null || true
      info "  [ok] ${cluster}"
    done
  fi

  echo ""
  echo "Done. Control planes removed for ${MESH_NAMESPACE}/${MESH_NAME}."
  echo "The MultiClusterMesh CR and its operator/trust plumbing are untouched."
}

##############################################################################
# Install
##############################################################################

do_install() {
  log "Installing mesh control planes for ${MESH_NAMESPACE}/${MESH_NAME}"
  info "Topology: ${TOPOLOGY}"

  log "Verifying Sail operator on managed clusters"
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" get crd istios.sailoperator.io &>/dev/null \
      || die "Istio CRD not found on cluster '${cluster}'. Ensure the Sail/OSSM operator is installed."
    info "  [ok] ${cluster}"
  done

  log "Creating control plane namespace on managed clusters"
  for cluster in "${CLUSTERS[@]}"; do
    kube_on "${cluster}" create namespace "${CP_NAMESPACE}" --dry-run=client -o yaml \
      | kube_on "${cluster}" apply -f - \
      || die "Failed to create namespace ${CP_NAMESPACE} on ${cluster}"
    info "  [ok] ${cluster}: ${CP_NAMESPACE}"
  done

  transform_cacerts

  log "Creating IstioCNI on OpenShift clusters"
  install_istiocni

  case "${TOPOLOGY}" in
    multi-primary)
      install_multi_primary
      ;;
    primary-remote)
      install_primary_remote
      ;;
  esac

  if [ "${DEPLOY_APP}" = "true" ]; then
    local app_cluster_a app_cluster_b
    if [ "${TOPOLOGY}" = "primary-remote" ]; then
      app_cluster_a="${PRIMARY_CLUSTER}"
      for c in "${CLUSTERS[@]}"; do
        if [ "${c}" != "${PRIMARY_CLUSTER}" ]; then
          app_cluster_b="${c}"
          break
        fi
      done
      app_cluster_b="${app_cluster_b:-${PRIMARY_CLUSTER}}"
    else
      app_cluster_a="${CLUSTERS[0]}"
      app_cluster_b="${CLUSTERS[1]:-${CLUSTERS[0]}}"
    fi
    deploy_test_app "${app_cluster_a}" "${app_cluster_b}"
  fi

  echo ""
  echo "Done. Istio control planes created for ${MESH_NAMESPACE}/${MESH_NAME}."
  echo ""
  echo "Summary:"
  echo "  Topology:    ${TOPOLOGY}"
  echo "  Mesh ID:     ${MESH_ID}"
  echo "  Trust:       ${TRUST_ISSUER:-<not configured>}"
  echo "  Clusters:    ${CLUSTERS[*]}"
  if [ "${TOPOLOGY}" = "primary-remote" ]; then
    echo "  Primary:     ${PRIMARY_CLUSTER}"
  fi
}

##############################################################################
# Main
##############################################################################

parse_args "$@"
detect_client_exe
read_mcm
discover_clusters

case "${ACTION}" in
  install)
    do_install
    ;;
  uninstall)
    do_uninstall
    ;;
esac
