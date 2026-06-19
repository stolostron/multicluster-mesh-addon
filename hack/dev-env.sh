#!/usr/bin/env bash
# Provisions and manages a local 3-cluster Kind/OCM development environment.
# This file is invoked by Makefile targets with an action argument.
#
# Usage: hack/dev-env.sh <action>
# Actions: create-clusters, install-olm, install-cert-manager, init-ocm, join-clusters, setup-mesh, clean

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HUB="hub"
CLUSTER1="cluster1"
CLUSTER2="cluster2"

log() { echo "==> $*"; }
warn() { echo "WARNING: $*" >&2; }
err() { echo "ERROR: $*" >&2; exit 1; }

retry() {
    local retries=$1 delay=$2
    shift 2
    local attempt
    for attempt in $(seq 1 "$retries"); do
        if "$@"; then
            return 0
        fi
        if (( attempt < retries )); then
            log "Attempt ${attempt}/${retries} failed, retrying in ${delay}s..."
            sleep "$delay"
        fi
    done
    err "Command failed after ${retries} attempts: $*"
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

kubeconfig_for() {
    echo "${DEV_KUBE_DIR}/${1}.config"
}

create_clusters() {
    check_inotify_limits
    check_kernel_keyring_limits
    mkdir -p "${DEV_KUBE_DIR}"

    local existing_clusters
    existing_clusters="$(${KIND} get clusters 2>/dev/null || true)"
    local found=()
    for cluster in "${HUB}" "${CLUSTER1}" "${CLUSTER2}"; do
        if echo "${existing_clusters}" | grep -qx "${cluster}"; then
            found+=("${cluster}")
        fi
    done

    if [[ ${#found[@]} -gt 0 ]]; then
        err "Kind clusters already exist: ${found[*]}. Run 'make dev-clean' to tear them down first."
    fi

    local kind_node_image="kindest/node:${K8S_VERSION}"

    for cluster in "${HUB}" "${CLUSTER1}" "${CLUSTER2}"; do
        local kubeconfig
        kubeconfig="$(kubeconfig_for "${cluster}")"

        log "Creating Kind cluster: ${cluster}"
        ${KIND} create cluster \
            --name "${cluster}" \
            --kubeconfig "${kubeconfig}" \
            --image "${kind_node_image}" \
            --wait 120s

        log "Waiting for cluster ${cluster} API to be ready..."
        kubectl --kubeconfig="${kubeconfig}" wait --for=condition=Ready nodes --all --timeout=120s
    done

    log "All clusters created successfully"
    ${KIND} get clusters
}

install_olm() {
    local olm_base_url="https://github.com/operator-framework/operator-lifecycle-manager/releases/download/${OLM_VERSION}"

    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        local kubeconfig
        kubeconfig="$(kubeconfig_for "${cluster}")"

        if [[ ! -f "${kubeconfig}" ]]; then
            err "Kubeconfig not found for ${cluster} at ${kubeconfig}. Run 'make create-clusters' first."
        fi

        # Check if OLM is already installed
        if kubectl --kubeconfig="${kubeconfig}" get deployment olm-operator -n olm &>/dev/null; then
            log "OLM already installed on ${cluster}, skipping"
            continue
        fi

        log "Installing OLM ${OLM_VERSION} on ${cluster}..."

        kubectl --kubeconfig="${kubeconfig}" apply --server-side -f "${olm_base_url}/crds.yaml"
        # kubectl wait errors immediately when .status.conditions is nil
        # (before the API server has processed the CRD), so retry a few times.
        retry 5 2 kubectl --kubeconfig="${kubeconfig}" wait --for=condition=Established \
            crd/catalogsources.operators.coreos.com \
            crd/subscriptions.operators.coreos.com \
            --timeout=60s

        log "Applying OLM components on ${cluster}..."
        kubectl --kubeconfig="${kubeconfig}" apply -f "${olm_base_url}/olm.yaml"

        log "Waiting for OLM components to be ready on ${cluster}..."
        kubectl --kubeconfig="${kubeconfig}" rollout status deployment/olm-operator -n olm --timeout=180s
        kubectl --kubeconfig="${kubeconfig}" rollout status deployment/catalog-operator -n olm --timeout=180s

        log "OLM ${OLM_VERSION} installed on ${cluster}"
    done

    # Grant the OCM work agent (klusterlet-work-sa) permission to manage OLM resources.
    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        log "Granting klusterlet-work-sa OLM permissions on ${cluster}"
        kubectl --kubeconfig="$(kubeconfig_for "${cluster}")" apply -f "${SCRIPT_DIR}/hack/kind/klusterlet-work-olm.yaml"
    done
}

install_cert_manager() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make create-clusters' first."
    fi

    if kubectl --kubeconfig="${hub_kubeconfig}" get deployment cert-manager -n cert-manager &>/dev/null; then
        log "cert-manager already installed on hub, skipping"
        return
    fi

    log "Installing cert-manager ${CERT_MANAGER_VERSION} on hub..."
    kubectl --kubeconfig="${hub_kubeconfig}" apply -f \
        "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"

    log "Waiting for cert-manager to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" rollout status deployment/cert-manager -n cert-manager --timeout=120s
    kubectl --kubeconfig="${hub_kubeconfig}" rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s

    log "cert-manager ${CERT_MANAGER_VERSION} installed on hub"
}

init_ocm() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make create-clusters' first."
    fi

    log "Initializing OCM hub on cluster: ${HUB}"
    ${CLUSTERADM} init --wait --kubeconfig="${hub_kubeconfig}"

    log "Waiting for OCM hub components to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" wait --for=condition=Available \
        deployment/cluster-manager -n open-cluster-management --timeout=120s
}

join_clusters() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make init-ocm' first."
    fi

    log "Retrieving hub token..."
    local token_json hub_token hub_apiserver
    token_json="$(${CLUSTERADM} get token --kubeconfig="${hub_kubeconfig}" -o json)"
    hub_token="$(echo "${token_json}" | jq -r '.["hub-token"]')"
    hub_apiserver="$(echo "${token_json}" | jq -r '.["hub-apiserver"]')"

    if [[ -z "${hub_token}" || -z "${hub_apiserver}" ]]; then
        err "Failed to extract hub token/apiserver from 'clusteradm get token'"
    fi

    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        local kubeconfig
        kubeconfig="$(kubeconfig_for "${cluster}")"

        if [[ ! -f "${kubeconfig}" ]]; then
            err "Kubeconfig not found for ${cluster} at ${kubeconfig}"
        fi

        if kubectl --kubeconfig="${hub_kubeconfig}" get managedcluster "${cluster}" &>/dev/null; then
            log "ManagedCluster ${cluster} already exists on hub, skipping join"
            continue
        fi

        log "Joining ${cluster} to hub..."
        ${CLUSTERADM} join \
            --hub-token "${hub_token}" \
            --hub-apiserver "${hub_apiserver}" \
            --cluster-name "${cluster}" \
            --force-internal-endpoint-lookup \
            --wait \
            --kubeconfig="${kubeconfig}"
    done

    log "Accepting managed clusters on hub..."
    ${CLUSTERADM} accept \
        --clusters="${CLUSTER1},${CLUSTER2}" \
        --skip-approve-check \
        --wait \
        --kubeconfig="${hub_kubeconfig}"

    log "Waiting for ManagedCluster conditions..."
    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster}" \
            --for=condition=HubAcceptedManagedCluster=True \
            --timeout=120s
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster}" \
            --for=condition=ManagedClusterJoined=True \
            --timeout=300s
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster}" \
            --for=condition=ManagedClusterConditionAvailable=True \
            --timeout=300s
        log "Cluster ${cluster} joined, accepted, and available"
    done

    log "Creating ManagedClusterSet: mesh-cluster-set"
    ${CLUSTERADM} --kubeconfig="${hub_kubeconfig}" create clusterset mesh-cluster-set
    ${CLUSTERADM} --kubeconfig="${hub_kubeconfig}" clusterset set mesh-cluster-set --clusters "${CLUSTER1},${CLUSTER2}"

    # On OpenShift, the product ClusterClaim is created automatically by the
    # klusterlet agent. On vanilla Kind clusters there is no such agent,
    # so we create it manually. The OCM registration agent syncs it to
    # ManagedCluster.status.clusterClaims on the hub, which the addon controller
    # uses for platform detection.
    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        log "Creating product ClusterClaim on ${cluster}"
        kubectl --kubeconfig="$(kubeconfig_for "${cluster}")" apply -f "${SCRIPT_DIR}/hack/kind/product-clusterclaim.yaml"
    done

    for cluster in "${CLUSTER1}" "${CLUSTER2}"; do
        log "Waiting for product claim on ${cluster} to propagate to the hub..."
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster}" \
            --for='jsonpath={.status.clusterClaims[?(@.name=="product.open-cluster-management.io")].value}=Kind' \
            --timeout=60s
    done

    log "OCM topology ready"
    kubectl --kubeconfig="${hub_kubeconfig}" get managedclusters
    kubectl --kubeconfig="${hub_kubeconfig}" get managedclustersets
}

setup_mesh() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make deploy-addon' first."
    fi

    if kubectl --kubeconfig="${hub_kubeconfig}" get namespace mesh-system &>/dev/null; then
        log "Namespace mesh-system already exists, skipping"
    else
        log "Creating namespace mesh-system"
        kubectl --kubeconfig="${hub_kubeconfig}" create namespace mesh-system
    fi

    log "Waiting for cert-manager-cainjector to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" rollout status deployment/cert-manager-cainjector \
        -n cert-manager --timeout=120s

    log "Applying cert-manager trust chain (self-signed Issuer, root CA Certificate, CA-backed Issuer)"
    kubectl --kubeconfig="${hub_kubeconfig}" apply -f "${SCRIPT_DIR}/samples/cert-manager-issuer.yaml"

    log "Waiting for bootstrap Issuer to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" wait issuer/mesh-selfsigned-issuer \
        -n mesh-system --for=condition=Ready --timeout=60s

    log "Waiting for root CA Certificate to be issued..."
    kubectl --kubeconfig="${hub_kubeconfig}" wait certificate/mesh-root-ca \
        -n mesh-system --for=condition=Ready --timeout=120s

    log "Waiting for CA-backed Issuer to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" wait issuer/mesh-root-ca \
        -n mesh-system --for=condition=Ready --timeout=60s

    log "Creating MultiClusterMesh CR"
    kubectl --kubeconfig="${hub_kubeconfig}" apply -f "${SCRIPT_DIR}/samples/basic.yaml"

    log "Mesh setup complete. The controller will now reconcile the mesh."
    log "Monitor progress: kubectl --kubeconfig=${hub_kubeconfig} get multiclustermesh -n mesh-system"
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
    create-clusters)       create_clusters ;;
    install-olm)           install_olm ;;
    install-cert-manager)  install_cert_manager ;;
    init-ocm)              init_ocm ;;
    join-clusters)         join_clusters ;;
    setup-mesh)            setup_mesh ;;
    clean)                 clean ;;
    *)
        err "Unknown action: '${ACTION}'. Valid: create-clusters, install-olm, install-cert-manager, init-ocm, join-clusters, setup-mesh, clean" ;;
esac
