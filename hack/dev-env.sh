#!/usr/bin/env bash
# Provisions and manages a local 3-cluster Kind/OCM development environment.
# This file is invoked by Makefile targets with an action argument.
#
# Usage: hack/dev-env.sh <action>
# Actions: create-clusters, install-olm, init-ocm, join-clusters, clean

set -euo pipefail

log() { echo "==> $*"; }
err() { echo "ERROR: $*" >&2; exit 1; }

kubeconfig_for() {
    echo "${DEV_KUBE_DIR}/${1}.config"
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
        kubeconfig="$(kubeconfig_for "${cluster_name}")"

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
        kubeconfig="$(kubeconfig_for "${cluster_name}")"

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
    local script_dir
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        kubeconfig="$(kubeconfig_for "${cluster_name}")"

        log "Granting klusterlet-work-sa OLM permissions on ${cluster_name}"
        kubectl --kubeconfig="${kubeconfig}" apply -f "${script_dir}/config/deploy/overlays/kind/klusterlet-work-olm.yaml"
    done
}

init_ocm() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB_NAME}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make create-clusters' first."
    fi

    log "Initializing OCM hub on cluster: ${HUB_NAME}"
    ${CLUSTERADM} init --wait --kubeconfig="${hub_kubeconfig}"

    log "Waiting for OCM hub components to be ready..."
    kubectl --kubeconfig="${hub_kubeconfig}" wait --for=condition=Available \
        deployment/cluster-manager -n open-cluster-management --timeout=120s
}

join_clusters() {
    local hub_kubeconfig
    hub_kubeconfig="$(kubeconfig_for "${HUB_NAME}")"

    if [[ ! -f "${hub_kubeconfig}" ]]; then
        err "Hub kubeconfig not found at ${hub_kubeconfig}. Run 'make init-ocm' first."
    fi

    log "Retrieving hub token..."
    local token_output
    token_output="$(${CLUSTERADM} get token --kubeconfig="${hub_kubeconfig}" 2>/dev/null)"

    local hub_token hub_apiserver
    hub_token="$(echo "${token_output}" | grep -oP '(?<=--hub-token )\S+')"
    hub_apiserver="$(echo "${token_output}" | grep -oP '(?<=--hub-apiserver )\S+')"

    if [[ -z "${hub_token}" || -z "${hub_apiserver}" ]]; then
        err "Failed to extract hub token/apiserver from 'clusteradm get token'"
    fi

    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        local kubeconfig
        kubeconfig="$(kubeconfig_for "${cluster_name}")"

        if [[ ! -f "${kubeconfig}" ]]; then
            err "Kubeconfig not found for ${cluster_name} at ${kubeconfig}"
        fi

        if kubectl --kubeconfig="${hub_kubeconfig}" get managedcluster "${cluster_name}" &>/dev/null; then
            log "ManagedCluster ${cluster_name} already exists on hub, skipping join"
            continue
        fi

        log "Joining ${cluster_name} to hub..."
        ${CLUSTERADM} join \
            --hub-token "${hub_token}" \
            --hub-apiserver "${hub_apiserver}" \
            --cluster-name "${cluster_name}" \
            --force-internal-endpoint-lookup \
            --wait \
            --kubeconfig="${kubeconfig}"
    done

    log "Accepting managed clusters on hub..."
    ${CLUSTERADM} accept \
        --clusters="${CLUSTER1_NAME},${CLUSTER2_NAME}" \
        --skip-approve-check \
        --wait \
        --kubeconfig="${hub_kubeconfig}"

    log "Waiting for ManagedCluster conditions..."
    for cluster_name in "${CLUSTER1_NAME}" "${CLUSTER2_NAME}"; do
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster_name}" \
            --for=condition=HubAcceptedManagedCluster=True \
            --timeout=120s
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster_name}" \
            --for=condition=ManagedClusterJoined=True \
            --timeout=300s
        kubectl --kubeconfig="${hub_kubeconfig}" wait managedcluster/"${cluster_name}" \
            --for=condition=ManagedClusterConditionAvailable=True \
            --timeout=300s
        log "Cluster ${cluster_name} joined, accepted, and available"
    done

    log "Creating ManagedClusterSet: mesh-cluster-set"
    kubectl --kubeconfig="${hub_kubeconfig}" apply -f - <<'EOF'
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
        kubectl --kubeconfig="${hub_kubeconfig}" label managedcluster "${cluster_name}" \
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
        kubeconfig="$(kubeconfig_for "${cluster_name}")"

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
            claims="$(kubectl --kubeconfig="${hub_kubeconfig}" get managedcluster "${cluster_name}" \
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
    kubectl --kubeconfig="${hub_kubeconfig}" get managedclusters
    kubectl --kubeconfig="${hub_kubeconfig}" get managedclustersets
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

    log "Clean complete"
}

ACTION="${1:-}"
case "${ACTION}" in
    create-clusters) create_clusters ;;
    install-olm)     install_olm ;;
    init-ocm)        init_ocm ;;
    join-clusters)   join_clusters ;;
    clean)           clean ;;
    *)               err "Unknown action: '${ACTION}'. Valid: create-clusters, install-olm, init-ocm, join-clusters, clean" ;;
esac
