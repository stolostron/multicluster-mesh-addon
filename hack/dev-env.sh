#!/usr/bin/env bash
# Provisions and manages a local 3-cluster Kind/OCM development environment.
# This file is invoked by Makefile targets with an action argument.
#
# Usage: hack/dev-env.sh <action> [args...]
# Actions: check-host, create-cluster <name>, install-olm <name>, install-cert-manager,
#          install-managed-serviceaccount, init-ocm, join-clusters, setup-mesh, clean

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HUB="hub"
CLUSTER1="cluster1"
CLUSTER2="cluster2"

log() { echo "==> $*"; }
warn() { echo "WARNING: $*" >&2; }
err() { echo "ERROR: $*" >&2; exit 1; }

retry() {
    local attempts=3 delay=2 attempt=1
    while (( attempt <= attempts )); do
        local output rc=0
        output=$("$@" 2>&1) || rc=$?
        echo "${output}"
        if (( rc == 0 )); then
            return 0
        fi
        if [[ "${output}" == *"timed out waiting for"* ]]; then
            err "Command timed out, not retrying: $*"
        fi
        if (( attempt == attempts )); then
            err "Command failed after ${attempts} attempts: $*"
        fi
        log "Attempt ${attempt}/${attempts} failed, retrying in ${delay}s..."
        sleep "${delay}"
        (( attempt++ ))
    done
}

MIN_INOTIFY_WATCHES=524288
MIN_INOTIFY_INSTANCES=512
MIN_KERNEL_KEYS=20000
MIN_KERNEL_BYTES=500000

check_inotify_limits() {
    [[ "$(uname -s)" != "Linux" ]] && return 0

    local watches instances msg=""
    watches="$(sysctl -n fs.inotify.max_user_watches 2>/dev/null || echo 0)"
    instances="$(sysctl -n fs.inotify.max_user_instances 2>/dev/null || echo 0)"

    if (( watches < MIN_INOTIFY_WATCHES )); then
        msg="fs.inotify.max_user_watches is ${watches} (need >= ${MIN_INOTIFY_WATCHES})"
    fi
    if (( instances < MIN_INOTIFY_INSTANCES )); then
        msg="${msg:+${msg}; }fs.inotify.max_user_instances is ${instances} (need >= ${MIN_INOTIFY_INSTANCES})"
    fi

    if [[ -n "${msg}" ]]; then
        err "${msg}. Creating 3 Kind clusters requires higher inotify limits. See https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files"
    fi
}

check_kernel_keyring_limits() {
    [[ "$(uname -s)" != "Linux" ]] && return 0
    command -v podman &>/dev/null || return 0

    if (( EUID != 0 )) && ! podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null | grep -q false; then
        local maxkeys maxbytes msg=""
        maxkeys="$(sysctl -n kernel.keys.maxkeys 2>/dev/null || echo 0)"
        maxbytes="$(sysctl -n kernel.keys.maxbytes 2>/dev/null || echo 0)"

        if (( maxkeys < MIN_KERNEL_KEYS )); then
            msg="kernel.keys.maxkeys is ${maxkeys} (recommend >= ${MIN_KERNEL_KEYS})"
        fi
        if (( maxbytes < MIN_KERNEL_BYTES )); then
            msg="${msg:+${msg}; }kernel.keys.maxbytes is ${maxbytes} (recommend >= ${MIN_KERNEL_BYTES})"
        fi

        if [[ -n "${msg}" ]]; then
            warn "${msg}. Rootless podman with 3 Kind clusters may fail with 'could not create session key: disk quota exceeded'. See https://github.com/kubernetes-sigs/kind/issues/3806"
        fi
    fi
}

on() {
    local cluster="${1}"
    shift
    "$@" --kubeconfig="${DEV_KUBE_DIR}/${cluster}.config"
}

require_clusters() {
    for cluster in "$@"; do
        if [[ ! -f "${DEV_KUBE_DIR}/${cluster}.config" ]]; then
            err "Kubeconfig not found for ${cluster}. Run 'make create-clusters' first."
        fi
    done
}

check_host() {
    check_inotify_limits
    check_kernel_keyring_limits

    local existing
    existing="$(${KIND} get clusters 2>/dev/null || true)"
    local found=()
    for cluster in "${HUB}" "${CLUSTER1}" "${CLUSTER2}"; do
        if echo "${existing}" | grep -qx "${cluster}"; then
            found+=("${cluster}")
        fi
    done
    if [[ ${#found[@]} -gt 0 ]]; then
        err "Kind clusters already exist: ${found[*]}. Run 'make dev-clean' to tear them down first."
    fi
}

create_cluster() {
    local cluster="${1}"
    mkdir -p "${DEV_KUBE_DIR}"

    log "Creating Kind cluster: ${cluster}"
    on "${cluster}" "${KIND}" create cluster \
        --name "${cluster}" \
        --image "kindest/node:${K8S_VERSION}" \
        --wait 120s

    log "Waiting for cluster ${cluster} API to be ready..."
    on "${cluster}" kubectl wait --for=condition=Ready nodes --all --timeout=120s
    log "Cluster ${cluster} ready"
}

install_olm() {
    local cluster="${1}"
    require_clusters "${cluster}"
    local olm_base_url="https://github.com/operator-framework/operator-lifecycle-manager/releases/download/${OLM_VERSION}"

    if on "${cluster}" kubectl get deployment olm-operator -n olm &>/dev/null; then
        log "OLM already installed on ${cluster}, skipping"
        return
    fi

    log "Installing OLM ${OLM_VERSION} on ${cluster}..."

    on "${cluster}" kubectl apply --server-side -f "${olm_base_url}/crds.yaml"
    on "${cluster}" retry kubectl wait --for=condition=Established \
        crd/catalogsources.operators.coreos.com \
        crd/subscriptions.operators.coreos.com \
        --timeout=60s

    log "Applying OLM components on ${cluster}..."
    on "${cluster}" kubectl apply -f "${olm_base_url}/olm.yaml"

    log "Waiting for OLM components to be ready on ${cluster}..."
    on "${cluster}" kubectl rollout status deployment/olm-operator -n olm --timeout=180s
    on "${cluster}" kubectl rollout status deployment/catalog-operator -n olm --timeout=180s

    log "Granting klusterlet-work-sa OLM permissions on ${cluster}"
    on "${cluster}" kubectl apply -f "${SCRIPT_DIR}/hack/kind/klusterlet-work-olm.yaml"

    log "OLM ${OLM_VERSION} installed on ${cluster}"
}

install_cert_manager() {
    require_clusters "${HUB}"
    if on "${HUB}" kubectl get deployment cert-manager -n cert-manager &>/dev/null; then
        log "cert-manager already installed on hub, skipping"
        return
    fi

    log "Installing cert-manager ${CERT_MANAGER_VERSION} on hub..."
    on "${HUB}" kubectl apply -f \
        "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"

    log "Waiting for cert-manager to be ready..."
    on "${HUB}" kubectl rollout status deployment/cert-manager -n cert-manager --timeout=120s
    on "${HUB}" kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s

    log "cert-manager ${CERT_MANAGER_VERSION} installed on hub"
}

init_ocm() {
    require_clusters "${HUB}"
    log "Initializing OCM hub on cluster: ${HUB}"
    on "${HUB}" "${CLUSTERADM}" init --wait

    log "Waiting for OCM hub components to be ready..."
    on "${HUB}" retry kubectl wait --for=condition=Available \
        deployment/cluster-manager -n open-cluster-management --timeout=120s
}

join_clusters() {
    require_clusters "${HUB}" "${CLUSTER1}" "${CLUSTER2}"
    log "Retrieving hub token..."
    local token_json hub_token hub_apiserver
    token_json="$(on "${HUB}" "${CLUSTERADM}" get token -o json)"
    hub_token="$(echo "${token_json}" | jq -r '.["hub-token"]')"
    hub_apiserver="$(echo "${token_json}" | jq -r '.["hub-apiserver"]')"

    if [[ -z "${hub_token}" || -z "${hub_apiserver}" ]]; then
        err "Failed to extract hub token/apiserver from 'clusteradm get token'"
    fi

    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        if on "${HUB}" kubectl get managedcluster "${cluster}" &>/dev/null; then
            log "ManagedCluster ${cluster} already exists on hub, skipping join"
            continue
        fi

        log "Joining ${cluster} to hub..."
        on "${cluster}" "${CLUSTERADM}" join \
            --hub-token "${hub_token}" \
            --hub-apiserver "${hub_apiserver}" \
            --cluster-name "${cluster}" \
            --force-internal-endpoint-lookup \
            --wait
    done

    log "Accepting managed clusters on hub..."
    on "${HUB}" "${CLUSTERADM}" accept \
        --clusters="${CLUSTER1},${CLUSTER2}" \
        --skip-approve-check \
        --wait

    log "Waiting for ManagedCluster conditions..."
    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        on "${HUB}" retry kubectl wait managedcluster/"${cluster}" \
            --for=condition=HubAcceptedManagedCluster=True \
            --timeout=120s
        on "${HUB}" retry kubectl wait managedcluster/"${cluster}" \
            --for=condition=ManagedClusterJoined=True \
            --timeout=300s
        on "${HUB}" retry kubectl wait managedcluster/"${cluster}" \
            --for=condition=ManagedClusterConditionAvailable=True \
            --timeout=300s
        log "Cluster ${cluster} joined, accepted, and available"
    done

    log "Creating ManagedClusterSet: mesh-cluster-set"
    on "${HUB}" "${CLUSTERADM}" create clusterset mesh-cluster-set
    on "${HUB}" "${CLUSTERADM}" clusterset set mesh-cluster-set --clusters "${CLUSTER1},${CLUSTER2}"

    log "OCM topology ready"
    on "${HUB}" kubectl get managedclusters
    on "${HUB}" kubectl get managedclustersets
}

install_managed_serviceaccount() {
    require_clusters "${HUB}" "${CLUSTER1}" "${CLUSTER2}"
    if on "${HUB}" kubectl get deployment managed-serviceaccount-addon-manager -n open-cluster-management-addon &>/dev/null; then
        log "managed-serviceaccount addon already installed on hub, skipping"
        return
    fi

    log "Installing managed-serviceaccount addon on hub..."
    ${HELM} repo add ocm "https://open-cluster-management.io/helm-charts/"
    on "${HUB}" ${HELM} upgrade --install managed-serviceaccount ocm/managed-serviceaccount \
        --version "${MSA_VERSION}" \
        --create-namespace \
        --namespace open-cluster-management-addon \
        --wait --timeout 180s

    log "Waiting for managed-serviceaccount addon to be ready..."
    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        on "${HUB}" retry kubectl wait managedclusteraddon/managed-serviceaccount \
            -n "${cluster}" --for=condition=Available --timeout=60s
    done

    log "managed-serviceaccount addon installed on hub"
    on "${HUB}" kubectl get managedclusteraddon -A
}

setup_mesh() {
    require_clusters "${HUB}"
    if on "${HUB}" kubectl get namespace mesh-system &>/dev/null; then
        log "Namespace mesh-system already exists, skipping"
    else
        log "Creating namespace mesh-system"
        on "${HUB}" kubectl create namespace mesh-system
    fi

    log "Waiting for cert-manager-cainjector to be ready..."
    on "${HUB}" kubectl rollout status deployment/cert-manager-cainjector \
        -n cert-manager --timeout=120s

    log "Applying cert-manager trust chain (self-signed Issuer, root CA Certificate, CA-backed Issuer)"
    on "${HUB}" kubectl apply -n mesh-system -f "${SCRIPT_DIR}/samples/cert-manager-issuer.yaml"

    log "Waiting for bootstrap Issuer to be ready..."
    on "${HUB}" retry kubectl wait issuer/mesh-selfsigned-issuer \
        -n mesh-system --for=condition=Ready --timeout=60s

    log "Waiting for root CA Certificate to be issued..."
    on "${HUB}" retry kubectl wait certificate/mesh-root-ca \
        -n mesh-system --for=condition=Ready --timeout=120s

    log "Waiting for CA-backed Issuer to be ready..."
    on "${HUB}" retry kubectl wait issuer/mesh-root-ca \
        -n mesh-system --for=condition=Ready --timeout=60s

    log "Creating MultiClusterMesh CR"
    on "${HUB}" kubectl apply -n mesh-system -f "${SCRIPT_DIR}/samples/basic.yaml"

    log "Mesh setup complete. The controller will now reconcile the mesh."
    log "Monitor progress: $(on "${HUB}" echo kubectl get multiclustermesh -n mesh-system)"
}

clean() {
    log "Deleting Kind clusters..."
    for cluster in "${HUB}" "${CLUSTER1}" "${CLUSTER2}"; do
        if ${KIND} get clusters 2>/dev/null | grep -qx "${cluster}"; then
            log "Deleting cluster: ${cluster}"
            ${KIND} delete cluster --name "${cluster}" || true
        fi
    done

    log "Removing dev environment state..."
    rm -rf "${DEV_KUBE_DIR}"

    log "Clean complete"
}

ACTION="${1:-}"
case "${ACTION}" in
    check-host)                      check_host ;;
    create-cluster)                  create_cluster "${2}" ;;
    install-olm)                     install_olm "${2}" ;;
    install-cert-manager)            install_cert_manager ;;
    install-managed-serviceaccount)  install_managed_serviceaccount ;;
    init-ocm)                        init_ocm ;;
    join-clusters)                   join_clusters ;;
    setup-mesh)                      setup_mesh ;;
    clean)                           clean ;;
    *)
        err "Unknown action: '${ACTION}'. Valid: check-host, create-cluster, install-olm, install-cert-manager, install-managed-serviceaccount, init-ocm, join-clusters, setup-mesh, clean" ;;
esac
