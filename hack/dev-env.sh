#!/usr/bin/env bash
# Provisions and manages a local 3-cluster Kind/OCM development environment.
# This file is invoked by Makefile targets with an action argument.
#
# Usage: hack/dev-env.sh <action> [options]
# Actions: install-deps, create-clusters, install-olm, init-ocm, join-clusters, clean

set -euo pipefail

# Required environment variables that should be passed from Makefile
required_vars=(DEV_BIN_DIR DEV_KUBE_DIR KIND_VERSION CLUSTERADM_VERSION K8S_VERSION OLM_VERSION)
for var in "${required_vars[@]}"; do
    if [[ -z "${!var:-}" ]]; then
        echo "ERROR: ${var} must be set" >&2
        exit 1
    fi
done

KIND="${DEV_BIN_DIR}/kind"
CLUSTERADM="${DEV_BIN_DIR}/clusteradm"

HUB_KUBECONFIG="${DEV_KUBE_DIR}/hub.config"
CLUSTER1_KUBECONFIG="${DEV_KUBE_DIR}/cluster1.config"
CLUSTER2_KUBECONFIG="${DEV_KUBE_DIR}/cluster2.config"

HUB_NAME="hub"
CLUSTER1_NAME="cluster1"
CLUSTER2_NAME="cluster2"

log() { echo "==> $*"; }
err() { echo "ERROR: $*" >&2; exit 1; }

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "${arch}" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        arm64)   echo "arm64" ;;
        armv7*)  echo "arm" ;;
        *)       err "Unsupported architecture: ${arch}" ;;
    esac
}

detect_os() {
    uname | tr '[:upper:]' '[:lower:]'
}

install_deps() {
    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"

    mkdir -p "${DEV_BIN_DIR}"

    # Install kind
    if [[ -x "${KIND}" ]] && "${KIND}" version 2>/dev/null | grep -q "${KIND_VERSION}"; then
        log "kind ${KIND_VERSION} already installed"
    else
        log "Installing kind ${KIND_VERSION} (${os}/${arch})..."
        local kind_url="https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-${os}-${arch}"
        curl -sSL "${kind_url}" -o "${KIND}"
        chmod +x "${KIND}"
        log "kind installed: $(${KIND} version)"
    fi

    # Install clusteradm
    local clusteradm_ver
    clusteradm_ver="$("${CLUSTERADM}" version 2>/dev/null || true)"
    if [[ -x "${CLUSTERADM}" ]] && echo "${clusteradm_ver}" | grep -q "${CLUSTERADM_VERSION}"; then
        log "clusteradm ${CLUSTERADM_VERSION} already installed"
    else
        log "Installing clusteradm ${CLUSTERADM_VERSION} (${os}/${arch})..."
        local clusteradm_url="https://github.com/open-cluster-management-io/clusteradm/releases/download/${CLUSTERADM_VERSION}/clusteradm_${os}_${arch}.tar.gz"
        local tmp_dir
        tmp_dir="$(mktemp -d)"
        curl -sSL "${clusteradm_url}" -o "${tmp_dir}/clusteradm.tar.gz"
        tar xzf "${tmp_dir}/clusteradm.tar.gz" -C "${tmp_dir}"
        mv "${tmp_dir}/clusteradm" "${CLUSTERADM}"
        chmod +x "${CLUSTERADM}"
        rm -rf "${tmp_dir}"
        log "clusteradm installed: $(${CLUSTERADM} version 2>&1 | head -1)"
    fi
}

create_clusters() {
    mkdir -p "${DEV_KUBE_DIR}"

    local existing_clusters
    existing_clusters="$(${KIND} get clusters 2>/dev/null || true)"
    local found=()
    for name in "${HUB_NAME}" "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        if echo "${existing_clusters}" | grep -qx "${name}"; then
            found+=("${name}")
        fi
    done

    if [[ ${#found[@]} -gt 0 ]]; then
        err "Kind clusters already exist: ${found[*]}. Run 'make dev-clean' to tear them down first."
    fi

    local kind_node_image="kindest/node:${K8S_VERSION}"

    for cluster_name in "${HUB_NAME}" "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        case "${cluster_name}" in
            "${HUB_NAME}")      kubeconfig="${HUB_KUBECONFIG}" ;;
            "${CLUSTER1_NAME}") kubeconfig="${CLUSTER1_KUBECONFIG}" ;;
            "${CLUSTER2_NAME}") kubeconfig="${CLUSTER2_KUBECONFIG}" ;;
        esac

        log "Creating Kind cluster: ${cluster_name}"
        ${KIND} create cluster \
            --name "${cluster_name}" \
            --kubeconfig "${kubeconfig}" \
            --image "${kind_node_image}" \
            --wait 120s

        log "Waiting for cluster ${cluster_name} API to be ready..."
        kubectl --kubeconfig="${kubeconfig}" wait --for=condition=Ready nodes --all --timeout=120s
    done

    log "All clusters created successfully"
    ${KIND} get clusters
}

install_olm() {
    local olm_base_url="https://github.com/operator-framework/operator-lifecycle-manager/releases/download/${OLM_VERSION}"

    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        case "${cluster_name}" in
            "${CLUSTER1_NAME}") kubeconfig="${CLUSTER1_KUBECONFIG}" ;;
            "${CLUSTER2_NAME}") kubeconfig="${CLUSTER2_KUBECONFIG}" ;;
        esac

        if [[ ! -f "${kubeconfig}" ]]; then
            err "Kubeconfig not found for ${cluster_name} at ${kubeconfig}. Run 'make create-clusters' first."
        fi

        # Check if OLM is already installed
        if kubectl --kubeconfig="${kubeconfig}" get deployment olm-operator -n olm &>/dev/null; then
            log "OLM already installed on ${cluster_name}, skipping"
            continue
        fi

        log "Installing OLM ${OLM_VERSION} on ${cluster_name}..."

        log "Applying OLM CRDs on ${cluster_name}..."
        kubectl --kubeconfig="${kubeconfig}" apply --server-side -f "${olm_base_url}/crds.yaml"
        kubectl --kubeconfig="${kubeconfig}" wait --for=condition=Established \
            crd/catalogsources.operators.coreos.com \
            crd/subscriptions.operators.coreos.com \
            --timeout=60s

        log "Applying OLM components on ${cluster_name}..."
        kubectl --kubeconfig="${kubeconfig}" apply -f "${olm_base_url}/olm.yaml"

        log "Waiting for OLM components to be ready on ${cluster_name}..."
        kubectl --kubeconfig="${kubeconfig}" rollout status deployment/olm-operator -n olm --timeout=180s
        kubectl --kubeconfig="${kubeconfig}" rollout status deployment/catalog-operator -n olm --timeout=180s

        log "OLM ${OLM_VERSION} installed on ${cluster_name}"
    done

    # Grant the OCM work agent (klusterlet-work-sa) permission to manage OLM resources.
    # On OpenShift, klusterlet-work-sa gets cluster-admin during import. On Kind clusters
    # joined via clusteradm, only minimal permissions are granted.
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        case "${cluster_name}" in
            "${CLUSTER1_NAME}") kubeconfig="${CLUSTER1_KUBECONFIG}" ;;
            "${CLUSTER2_NAME}") kubeconfig="${CLUSTER2_KUBECONFIG}" ;;
        esac

        log "Granting klusterlet-work-sa OLM permissions on ${cluster_name}"
        kubectl --kubeconfig="${kubeconfig}" apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: klusterlet-work-olm
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["create", "get", "update", "patch", "delete"]
  - apiGroups: ["operators.coreos.com"]
    resources: ["operatorgroups", "subscriptions", "catalogsources", "clusterserviceversions"]
    verbs: ["create", "get", "list", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: klusterlet-work-olm
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: klusterlet-work-olm
subjects:
  - kind: ServiceAccount
    name: klusterlet-work-sa
    namespace: open-cluster-management-agent
EOF
    done
}

init_ocm() {
    if [[ ! -f "${HUB_KUBECONFIG}" ]]; then
        err "Hub kubeconfig not found at ${HUB_KUBECONFIG}. Run 'make create-clusters' first."
    fi

    log "Initializing OCM hub on cluster: ${HUB_NAME}"
    ${CLUSTERADM} init --wait --kubeconfig="${HUB_KUBECONFIG}"

    log "Extracting join command..."
    local join_cmd
    join_cmd="$(${CLUSTERADM} get token --kubeconfig="${HUB_KUBECONFIG}" | grep clusteradm)"

    if [[ -z "${join_cmd}" ]]; then
        err "Failed to extract join command from clusteradm get token"
    fi

    echo "${join_cmd}" > "${DEV_KUBE_DIR}/.ocm-join-cmd"
    log "Join command saved to ${DEV_KUBE_DIR}/.ocm-join-cmd"

    log "Waiting for OCM hub components to be ready..."
    kubectl --kubeconfig="${HUB_KUBECONFIG}" wait --for=condition=Available \
        deployment/cluster-manager -n open-cluster-management --timeout=120s
}

join_clusters() {
    if [[ ! -f "${DEV_KUBE_DIR}/.ocm-join-cmd" ]]; then
        err "OCM join command not found. Run 'make init-ocm' first."
    fi

    local join_cmd
    join_cmd="$(cat "${DEV_KUBE_DIR}/.ocm-join-cmd")"

    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        case "${cluster_name}" in
            "${CLUSTER1_NAME}") kubeconfig="${CLUSTER1_KUBECONFIG}" ;;
            "${CLUSTER2_NAME}") kubeconfig="${CLUSTER2_KUBECONFIG}" ;;
        esac

        if [[ ! -f "${kubeconfig}" ]]; then
            err "Kubeconfig not found for ${cluster_name} at ${kubeconfig}"
        fi

        log "Joining ${cluster_name} to hub..."
        local cluster_join_cmd
        cluster_join_cmd="$(echo "${join_cmd}" | sed "s/<cluster_name>/${cluster_name}/g")"
        eval "${cluster_join_cmd}" \
            --force-internal-endpoint-lookup \
            --wait \
            --kubeconfig="${kubeconfig}"
    done

    log "Accepting managed clusters on hub..."
    ${CLUSTERADM} accept \
        --clusters="${CLUSTER1_NAME},${CLUSTER2_NAME}" \
        --wait \
        --kubeconfig="${HUB_KUBECONFIG}"

    log "Waiting for ManagedCluster conditions..."
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        kubectl --kubeconfig="${HUB_KUBECONFIG}" wait managedcluster/"${cluster_name}" \
            --for=condition=HubAcceptedManagedCluster=True \
            --timeout=120s
        kubectl --kubeconfig="${HUB_KUBECONFIG}" wait managedcluster/"${cluster_name}" \
            --for=condition=ManagedClusterJoined=True \
            --timeout=120s
        kubectl --kubeconfig="${HUB_KUBECONFIG}" wait managedcluster/"${cluster_name}" \
            --for=condition=ManagedClusterConditionAvailable=True \
            --timeout=120s
        log "Cluster ${cluster_name} joined, accepted, and available"
    done

    log "Creating ManagedClusterSet: mesh-cluster-set"
    kubectl --kubeconfig="${HUB_KUBECONFIG}" apply -f - <<'EOF'
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: mesh-cluster-set
spec:
  clusterSelector:
    selectorType: ExclusiveClusterSetLabel
EOF

    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        log "Labeling ${cluster_name} with clusterset=mesh-cluster-set"
        kubectl --kubeconfig="${HUB_KUBECONFIG}" label managedcluster "${cluster_name}" \
            cluster.open-cluster-management.io/clusterset=mesh-cluster-set \
            --overwrite
    done

    # On OpenShift, the product ClusterClaim is created automatically by the
    # klusterlet agent. On vanilla Kind clusters there is no such agent,
    # so we create it manually. The OCM registration agent syncs it to
    # ManagedCluster.status.clusterClaims on the hub, which the addon controller
    # uses for platform detection.
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        case "${cluster_name}" in
            "${CLUSTER1_NAME}") kubeconfig="${CLUSTER1_KUBECONFIG}" ;;
            "${CLUSTER2_NAME}") kubeconfig="${CLUSTER2_KUBECONFIG}" ;;
        esac

        log "Creating product ClusterClaim on ${cluster_name}"
        kubectl --kubeconfig="${kubeconfig}" apply -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: product.open-cluster-management.io
spec:
  value: Kind
EOF
    done

    log "Waiting for product claims to propagate to hub..."
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local retries=0
        while [[ ${retries} -lt 30 ]]; do
            local claims
            claims="$(kubectl --kubeconfig="${HUB_KUBECONFIG}" get managedcluster "${cluster_name}" \
                -o jsonpath='{.status.clusterClaims[?(@.name=="product.open-cluster-management.io")].value}' 2>/dev/null || true)"
            if [[ -n "${claims}" ]]; then
                log "Cluster ${cluster_name} product claim synced: ${claims}"
                break
            fi
            retries=$((retries + 1))
            sleep 2
        done
        if [[ ${retries} -ge 30 ]]; then
            err "Timed out waiting for product claim on ${cluster_name} to propagate to hub"
        fi
    done

    log "OCM topology ready"
    kubectl --kubeconfig="${HUB_KUBECONFIG}" get managedclusters
    kubectl --kubeconfig="${HUB_KUBECONFIG}" get managedclustersets
}

clean() {
    log "Deleting Kind clusters..."
    for cluster_name in "${HUB_NAME}" "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        if ${KIND} get clusters 2>/dev/null | grep -qx "${cluster_name}"; then
            log "Deleting cluster: ${cluster_name}"
            ${KIND} delete cluster --name "${cluster_name}" || true
        fi
    done

    log "Removing dev environment state..."
    rm -rf "${DEV_KUBE_DIR}"
    # rm -rf "${DEV_BIN_DIR}"

    log "Clean complete"
}

ACTION="${1:-}"
case "${ACTION}" in
    install-deps)    install_deps ;;
    create-clusters) create_clusters ;;
    install-olm)     install_olm ;;
    init-ocm)        init_ocm ;;
    join-clusters)   join_clusters ;;
    clean)           clean ;;
    *)               err "Unknown action: '${ACTION}'. Valid: install-deps, create-clusters, install-olm, init-ocm, join-clusters, clean" ;;
esac
