#!/usr/bin/env bash
# Provisions and manages a local Kind/OCM development environment.
# This file is invoked by Makefile targets with an action argument.
#
# Usage: hack/dev-env.sh <action> [args...]
# Actions: check-host, create-cluster <name>, install-olm <name>, install-cert-manager,
#          install-managed-serviceaccount, init-ocm, join-clusters, create-clusterset,
#          setup-mesh, setup-test-issuer

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HUB="hub"
read -r -a SPOKE_CLUSTERS <<< "${SPOKE_CLUSTERS:-cluster1 cluster2}"
declare -A SPOKE_CLUSTER_INDEX=()
for index in "${!SPOKE_CLUSTERS[@]}"; do
    SPOKE_CLUSTER_INDEX["${SPOKE_CLUSTERS[${index}]}"]="${index}"
done

log() { echo "==> $*"; }
warn() { echo "WARNING: $*" >&2; }
err() { echo "ERROR: $*" >&2; exit 1; }

[[ ${#SPOKE_CLUSTERS[@]} -gt 0 ]] || err "SPOKE_CLUSTERS must contain at least one cluster"

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

join_by_comma() {
    local IFS=,
    printf '%s' "$*"
}

check_host() {
    check_inotify_limits
    check_kernel_keyring_limits
}

create_cluster() {
    local cluster="${1}"
    mkdir -p "${DEV_KUBE_DIR}"

    local existing
    existing="$(${KIND} get clusters 2>/dev/null || true)"
    if echo "${existing}" | grep -qx "${cluster}"; then
        log "Kind cluster ${cluster} already exists, skipping creation"
        if [[ ! -f "${DEV_KUBE_DIR}/${cluster}.config" ]]; then
            log "Exporting kubeconfig for existing cluster ${cluster}"
            ${KIND} get kubeconfig --name "${cluster}" > "${DEV_KUBE_DIR}/${cluster}.config"
        fi
        return
    fi

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
    local olm_base_url="https://github.com/operator-framework/operator-lifecycle-manager/releases/download/${OLM_VERSION}"

    if on "${cluster}" kubectl get deployment olm-operator -n olm &>/dev/null &&
        on "${cluster}" kubectl get deployment catalog-operator -n olm &>/dev/null; then
        log "OLM already installed on ${cluster}"
    else
        log "Installing OLM ${OLM_VERSION} on ${cluster}..."

        on "${cluster}" kubectl apply --server-side -f "${olm_base_url}/crds.yaml"
        on "${cluster}" retry kubectl wait --for=condition=Established \
            crd/catalogsources.operators.coreos.com \
            crd/subscriptions.operators.coreos.com \
            --timeout=60s

        log "Applying OLM components on ${cluster}..."
        on "${cluster}" kubectl apply -f "${olm_base_url}/olm.yaml"
    fi

    log "Waiting for OLM components to be ready on ${cluster}..."
    on "${cluster}" kubectl wait --for=condition=Available \
        deployment/olm-operator deployment/catalog-operator \
        -n olm --timeout=10m

    log "OLM ${OLM_VERSION} installed on ${cluster}"
}

install_cert_manager() {
    require_clusters "${HUB}"
    if on "${HUB}" kubectl get deployment cert-manager -n cert-manager &>/dev/null &&
        on "${HUB}" kubectl get deployment cert-manager-cainjector -n cert-manager &>/dev/null &&
        on "${HUB}" kubectl get deployment cert-manager-webhook -n cert-manager &>/dev/null; then
        log "cert-manager already installed on hub"
    else
        log "Installing cert-manager ${CERT_MANAGER_VERSION} on hub..."
        on "${HUB}" kubectl apply -f \
            "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
    fi

    log "Waiting for cert-manager to be ready..."
    on "${HUB}" kubectl wait --for=condition=Available \
        deployment/cert-manager deployment/cert-manager-cainjector deployment/cert-manager-webhook \
        -n cert-manager --timeout=10m

    log "cert-manager ${CERT_MANAGER_VERSION} installed on hub"
}

setup_test_issuer() {
    if on "${HUB}" kubectl get clusterissuer mesh-test-root-ca &>/dev/null; then
        log "Test ClusterIssuer already exists, skipping"
        return
    fi

    log "Waiting for cert-manager-cainjector to be ready..."
    on "${HUB}" kubectl wait --for=condition=Available deployment/cert-manager-cainjector \
        -n cert-manager --timeout=10m

    log "Creating test ClusterIssuer trust chain"
    on "${HUB}" kubectl apply -f "${SCRIPT_DIR}/hack/kind/cert-manager-test-issuer.yaml"

    log "Waiting for bootstrap ClusterIssuer to be ready..."
    on "${HUB}" retry kubectl wait clusterissuer/mesh-test-selfsigned \
        --for=condition=Ready --timeout=60s

    log "Waiting for test root CA Certificate to be issued..."
    on "${HUB}" retry kubectl wait certificate/mesh-test-root-ca \
        -n cert-manager --for=condition=Ready --timeout=120s

    log "Waiting for CA-backed ClusterIssuer to be ready..."
    on "${HUB}" retry kubectl wait clusterissuer/mesh-test-root-ca \
        --for=condition=Ready --timeout=60s

    log "Test ClusterIssuer trust chain ready"
}

init_ocm() {
    log "Initializing OCM hub on cluster: ${HUB}"
    on "${HUB}" "${CLUSTERADM}" init --feature-gates=ManifestWorkReplicaSet=true --wait

    log "Waiting for OCM hub components to be ready..."
    on "${HUB}" kubectl wait --for=condition=Available \
        deployment/cluster-manager -n open-cluster-management --timeout=10m
}

join_clusters() {
    require_clusters "${HUB}" "${SPOKE_CLUSTERS[@]}"
    log "Retrieving hub token..."
    local token_json hub_token hub_apiserver
    token_json="$(on "${HUB}" "${CLUSTERADM}" get token -o json)"
    hub_token="$(echo "${token_json}" | jq -r '.["hub-token"]')"
    hub_apiserver="$(echo "${token_json}" | jq -r '.["hub-apiserver"]')"

    if [[ -z "${hub_token}" || -z "${hub_apiserver}" ]]; then
        err "Failed to extract hub token/apiserver from 'clusteradm get token'"
    fi

    for cluster in "${SPOKE_CLUSTERS[@]}"; do
        if on "${HUB}" kubectl get managedcluster "${cluster}" &>/dev/null; then
            log "ManagedCluster ${cluster} already exists on hub, skipping join"
            continue
        fi

        log "Joining ${cluster} to hub..."
        on "${cluster}" "${CLUSTERADM}" join \
            --hub-token "${hub_token}" \
            --hub-apiserver "${hub_apiserver}" \
            --cluster-name "${cluster}" \
            --force-internal-endpoint-lookup
    done

    log "Accepting managed clusters on hub..."
    on "${HUB}" "${CLUSTERADM}" accept \
        --clusters="$(join_by_comma "${SPOKE_CLUSTERS[@]}")" \
        --skip-approve-check \
        --wait

    log "Waiting for ManagedCluster conditions..."
    for cluster in "${SPOKE_CLUSTERS[@]}"; do
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

}

create_clusterset() {
    require_clusters "${HUB}" "${SPOKE_CLUSTERS[@]}"

    if on "${HUB}" kubectl get managedclusterset mesh-cluster-set &>/dev/null; then
        log "ManagedClusterSet mesh-cluster-set already exists, skipping creation"
        return
    fi

    log "Creating ManagedClusterSet: mesh-cluster-set"
    on "${HUB}" "${CLUSTERADM}" create clusterset mesh-cluster-set
    on "${HUB}" "${CLUSTERADM}" clusterset set mesh-cluster-set --clusters "$(join_by_comma "${SPOKE_CLUSTERS[@]}")"

    log "OCM topology ready"
    on "${HUB}" kubectl get managedclusters
    on "${HUB}" kubectl get managedclustersets
}

install_managed_serviceaccount() {
    require_clusters "${HUB}" "${SPOKE_CLUSTERS[@]}"
    if on "${HUB}" kubectl get deployment managed-serviceaccount-addon-manager -n open-cluster-management-addon &>/dev/null; then
        log "managed-serviceaccount addon already installed on hub"
    else
        log "Installing managed-serviceaccount addon on hub..."
        ${HELM} repo add ocm "https://open-cluster-management.io/helm-charts/"
        on "${HUB}" "${HELM}" upgrade --install managed-serviceaccount ocm/managed-serviceaccount \
            --version "${MSA_VERSION}" \
            --create-namespace \
            --namespace open-cluster-management-addon \
            --wait --timeout 10m
    fi

    log "Waiting for managed-serviceaccount addon manager to be ready..."
    on "${HUB}" kubectl wait --for=condition=Available \
        deployment/managed-serviceaccount-addon-manager \
        -n open-cluster-management-addon --timeout=10m

    log "Waiting for managed-serviceaccount addon on spokes..."
    for cluster in "${SPOKE_CLUSTERS[@]}"; do
        on "${HUB}" kubectl wait managedclusteraddon/managed-serviceaccount \
            -n "${cluster}" --for=condition=Available --timeout=10m
    done

    log "managed-serviceaccount addon installed on hub"
    on "${HUB}" kubectl get managedclusteraddon -A
}

install_metallb() {
    local cluster="${1}"
    local metallb_version="${METALLB_VERSION}"

    if on "${cluster}" kubectl get deployment controller -n metallb-system &>/dev/null; then
        log "MetalLB already installed on ${cluster}, skipping"
        return
    fi

    # Kind auto-detects its container provider (docker or podman), which may
    # differ from CONTAINER_ENGINE. Detect the actual provider by checking
    # which runtime owns the Kind node containers.
    local kind_provider="docker"
    if podman inspect "${cluster}-control-plane" &>/dev/null; then
        kind_provider="podman"
    fi

    local kind_subnet
    case "${kind_provider}" in
        podman)
            # Select the IPv4 subnet; Kind networks may be dual-stack and index 0 is not guaranteed IPv4.
            kind_subnet="$(podman network inspect kind -f '{{range .Subnets}}{{.Subnet}}{{"\n"}}{{end}}' | grep -v ':' | head -1)" \
                || err "Failed to inspect Kind network with Podman" ;;
        docker)
            # Select the IPv4 subnet; Kind networks may be dual-stack and index 0 is not guaranteed IPv4.
            kind_subnet="$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}}{{"\n"}}{{end}}' | grep -v ':' | head -1)" \
                || err "Failed to inspect Kind network with Docker" ;;
    esac

    local base_prefix
    base_prefix="$(echo "${kind_subnet}" | cut -d'/' -f1 | cut -d'.' -f1-3)"

    local idx="${SPOKE_CLUSTER_INDEX[${cluster}]}"
    local range_start="${base_prefix}.$((200 + idx * 10 + 1))"
    local range_end="${base_prefix}.$((200 + idx * 10 + 10))"

    log "Installing MetalLB ${metallb_version} on ${cluster}..."
    on "${cluster}" kubectl apply -f \
        "https://raw.githubusercontent.com/metallb/metallb/${metallb_version}/config/manifests/metallb-native.yaml"

    log "Waiting for MetalLB controller to be ready on ${cluster}..."
    on "${cluster}" kubectl rollout status deployment/controller -n metallb-system --timeout=120s

    log "Waiting for MetalLB speaker to be ready on ${cluster}..."
    on "${cluster}" kubectl rollout status daemonset/speaker -n metallb-system --timeout=120s

    log "Configuring MetalLB IP pool ${range_start}-${range_end} on ${cluster}..."
    sed "s|__ADDRESS_RANGE__|${range_start}-${range_end}|" \
        "${SCRIPT_DIR}/hack/kind/metallb-pool.yaml" \
        | on "${cluster}" kubectl apply -f -
    log "MetalLB configured on ${cluster}"
}

install_gateway_api() {
    local cluster="${1}"
    local gw_api_version="${GATEWAY_API_VERSION}"

    if on "${cluster}" kubectl get crd gateways.gateway.networking.k8s.io &>/dev/null; then
        log "Gateway API CRDs already installed on ${cluster}, skipping"
        return
    fi

    log "Installing Gateway API CRDs ${gw_api_version} on ${cluster}..."
    on "${cluster}" kubectl apply --server-side -f \
        "https://github.com/kubernetes-sigs/gateway-api/releases/download/${gw_api_version}/standard-install.yaml"

    on "${cluster}" retry kubectl wait --for=condition=Established \
        crd/gateways.gateway.networking.k8s.io --timeout=60s

    log "Gateway API CRDs installed on ${cluster}"
}

setup_mesh() {
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

ACTION="${1:-}"
case "${ACTION}" in
    check-host)                      check_host ;;
    create-cluster)                  create_cluster "${2}" ;;
    install-olm)                     install_olm "${2}" ;;
    install-cert-manager)            install_cert_manager ;;
    install-managed-serviceaccount)  install_managed_serviceaccount ;;
    init-ocm)                        init_ocm ;;
    join-clusters)                   join_clusters ;;
    create-clusterset)               create_clusterset ;;
    setup-mesh)                      setup_mesh ;;
    install-metallb)                 install_metallb "${2}" ;;
    install-gateway-api)             install_gateway_api "${2}" ;;
    setup-test-issuer)               setup_test_issuer ;;
    *)
        err "Unknown action: '${ACTION}'. Valid: check-host, create-cluster, install-olm, install-cert-manager, install-managed-serviceaccount, init-ocm, join-clusters, create-clusterset, setup-mesh, install-metallb, install-gateway-api, setup-test-issuer" ;;
esac
