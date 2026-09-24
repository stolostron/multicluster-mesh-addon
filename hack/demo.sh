#!/usr/bin/env bash
# Runs the customer-facing multi-cluster mesh demo.
#
# The default platform is OpenShift.
# Use PLATFORM=kind for the local Sail environment prepared by `make demo-dev-env`.

set -euo pipefail

fail() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

PLATFORM="${PLATFORM:-openshift}"
DEMO_KUBECONFIG="${DEMO_KUBECONFIG:-}"
read -r -a SPOKE_CLUSTERS <<< "${SPOKE_CLUSTERS}"
((${#SPOKE_CLUSTERS[@]} == 3)) || fail "SPOKE_CLUSTERS must contain three cluster names"

MESH_NAMESPACE=mesh-system
CLUSTER_SET=mesh-cluster-set
ISTIO_VERSION="${ISTIO_VERSION:-1.30.4}"
BOOKINFO_REF="${BOOKINFO_REF:-release-${ISTIO_VERSION%.*}}"
DEMO_TIMEOUT=15m
DEMO_COMMAND_PAUSE=${DEMO_COMMAND_PAUSE:-3}
TYPEWRITER_DELAY=${TYPEWRITER_DELAY:-0.03}
DEMO_DRY_RUN="${DEMO_DRY_RUN:-0}"
HELM="${HELM:-helm}"
CLUSTERADM="${CLUSTERADM:-clusteradm}"
ISTIOCTL="${ISTIOCTL:-istioctl}"

export BAT_STYLE=plain
export BAT_PAGING=never
export BAT_THEME="Catppuccin Frappe"

# Use `bat` instead of `cat` to print YAMLs, to get syntax highlights.
cat() {
    yq -C . "$1" | bat
}
export -f cat

# Expecting a KUBECONFIG style variable with path/to/hub.config:path/to/spoke1.config:...
[[ -n "${DEMO_KUBECONFIG}" ]] || fail "DEMO_KUBECONFIG must contain hub and three spoke kubeconfig paths"
IFS=: read -r -a kubeconfig_paths <<< "${DEMO_KUBECONFIG}"

((${#kubeconfig_paths[@]} == 4)) || fail "DEMO_KUBECONFIG must contain four colon-separated paths"
for index in "${!kubeconfig_paths[@]}"; do
    kubeconfig="${kubeconfig_paths[${index}]}"
    [[ -f "${kubeconfig}" ]] || fail "DEMO_KUBECONFIG file not found: ${kubeconfig}"
    [[ -r "${kubeconfig}" ]] || fail "DEMO_KUBECONFIG file is not readable: ${kubeconfig}"
done

# The kubeconfig expected order is hub, then the spokes in matching order to SPOKE_CLUSTERS.
declare -A KUBECONFIGS=([hub]="${kubeconfig_paths[0]}")
for index in "${!SPOKE_CLUSTERS[@]}"; do
    KUBECONFIGS["${SPOKE_CLUSTERS[${index}]}"]="${kubeconfig_paths[$((index + 1))]}"
done

case "${PLATFORM}" in
    openshift)
        KUBECTL=oc
        MESH_NAME=openshift-mesh
        MESH_SAMPLE=samples/openshift.yaml
        ;;
    kind)
        KUBECTL=kubectl
        MESH_NAME=basic-mesh
        MESH_SAMPLE=samples/basic.yaml
        ;;
    *)
        fail "PLATFORM must be openshift or kind"
        ;;
esac

for tool in "${KUBECTL}" clear envsubst bat yq "${HELM}" "${CLUSTERADM}" "${ISTIOCTL}"; do
    command -v "${tool}" >/dev/null 2>&1 || fail "required command not found: ${tool}"
done

if [[ "${DEMO_DRY_RUN}" == "1" ]]; then
    TYPEWRITER_DELAY=0
    DEMO_COMMAND_PAUSE=0
fi

typewriter() {
    local text="$*"
    local index

    printf '\033[33m$\033[0m '
    sleep 0.2

    for ((index = 0; index < ${#text}; index++)); do
        printf '%s' "${text:index:1}"
        sleep "${TYPEWRITER_DELAY}"
    done
}

demo_comment() {
    typewriter $'\033[2;39m# '"$*"$'\033[0m\n'
}

demo_title() {
    demo_comment "$1"
    demo_comment "${1//?/=}"
}

demo_run() {
    local command="$*"
    typewriter $'\033[93m'"${command}"$'\033[0m\n'

    if [[ "${DEMO_DRY_RUN}" != "1" ]]; then
        bash -o pipefail -c "${command}"
        sleep "${DEMO_COMMAND_PAUSE}"
    fi
}

demo_wait() {
    local clear="${1:-0}"
    if [[ "${DEMO_DRY_RUN}" != "1" ]]; then
        typewriter

        # Any pressed character will release and continue
        read -r -n 1 -s
        [[ "$clear" == 0 ]] || clear
        printf '\n'
    fi
}

run_kubectl() {
    # $1 is the cluster key; the remaining arguments are passed to $KUBECTL.
    demo_run "${KUBECTL}" --kubeconfig "${KUBECONFIGS[$1]}" "${@:2}"
}

run_kubectl_apply() {
    # $1 is the cluster, $2 is the manifest, and later arguments are apply options.
    run_kubectl "$1" apply "${@:3}" -f "$2"
    demo_run cat "$2"
}

run_clusteradm() {
    demo_run "${CLUSTERADM}" --kubeconfig "${KUBECONFIGS[hub]}" "$@"
}

run_helm() {
    demo_run "${HELM}" --kubeconfig "${KUBECONFIGS[hub]}" "$@"
}

run_capture() {
    # Keep command output visible while saving it for a later conditional check.
    demo_run "${*:2} | tee \"$1\""
}

configure_istio_spoke() {
    if [[ "${PLATFORM}" == "openshift" ]]; then
        demo_comment "Create istio-cni namespace and install Istio CNI on $1."
        run_kubectl_apply "$1" hack/demo/istiocni-namespace.yaml
        run_kubectl_apply "$1" samples/istio/istiocni.yaml
    fi
    demo_comment "Create the Istio control plane with $1's mesh and network identity."
    demo_run "MESH_NAME=${MESH_NAME} CLUSTER_NAME=$1 NETWORK=$2 envsubst < samples/istio/istio.yaml | ${KUBECTL} --kubeconfig ${KUBECONFIGS[$1]} apply -f -"
    demo_run cat samples/istio/istio.yaml
    demo_comment "Wait for istiod to become available on $1."
    run_kubectl "$1" wait --for=condition=Available deployment/istiod \
        -n istio-system --timeout="${DEMO_TIMEOUT}"
    demo_comment "Create the east-west Gateway for traffic between networks."
    demo_run "NETWORK=$2 envsubst < samples/istio/eastwest-gateway.yaml | ${KUBECTL} --kubeconfig ${KUBECONFIGS[$1]} apply -f -"
    demo_run cat samples/istio/eastwest-gateway.yaml
    demo_comment "Wait for the east-west Gateway to receive a LoadBalancer address."
    run_kubectl "$1" wait --for=create \
        --for=jsonpath='{.status.loadBalancer.ingress}' service/eastwestgateway-istio \
        -n istio-system --timeout="${DEMO_TIMEOUT}"
}

run_istioctl_remote_clusters() {
    demo_run "${ISTIOCTL}" remote-clusters --kubeconfig "${KUBECONFIGS[$1]}"
}

deploy_bookinfo_version() {
    local cluster="$1"
    local version="$2"
    local bookinfo_base
    local bookinfo_manifest
    local -a disabled_workloads
    local -a ready_workloads

    if [[ "${PLATFORM}" == "openshift" ]]; then
        bookinfo_base="https://raw.githubusercontent.com/openshift-service-mesh/istio/${BOOKINFO_REF}"
    else
        bookinfo_base="https://raw.githubusercontent.com/istio/istio/${BOOKINFO_REF}"
    fi
    bookinfo_manifest="${bookinfo_base}/samples/bookinfo/platform/kube/bookinfo.yaml"

    case "${version}" in
        v1)
            disabled_workloads=(deployment/reviews-v2 deployment/reviews-v3)
            ready_workloads=(deployment/productpage-v1 deployment/details-v1 deployment/reviews-v1 deployment/ratings-v1)
            ;;
        v2)
            disabled_workloads=(deployment/productpage-v1 deployment/details-v1 deployment/reviews-v1 deployment/reviews-v3 deployment/ratings-v1)
            ready_workloads=(deployment/reviews-v2)
            ;;
        v3)
            disabled_workloads=(deployment/productpage-v1 deployment/details-v1 deployment/reviews-v1 deployment/reviews-v2 deployment/ratings-v1)
            ready_workloads=(deployment/reviews-v3)
            ;;
        *)
            fail "unsupported Bookinfo version: ${version}"
            ;;
    esac

    demo_comment "Create the injected Bookinfo namespace on ${cluster}."
    run_kubectl_apply "${cluster}" hack/demo/bookinfo-namespace.yaml
    demo_comment "Install the standard Bookinfo services and workloads on ${cluster}."
    run_kubectl "${cluster}" apply -n bookinfo -f "${bookinfo_manifest}"
    demo_comment "Mark reviews-${version} with ${cluster}'s name so each response shows where it came from."
    run_kubectl "${cluster}" set env "deployment/reviews-${version}" \
        "CLUSTER_NAME=${cluster}" -n bookinfo
    demo_comment "Keep only this cluster's reviews version active."
    run_kubectl "${cluster}" scale "${disabled_workloads[@]}" --replicas=0 -n bookinfo
    demo_comment "Wait for the Bookinfo workloads that serve traffic on ${cluster}."
    run_kubectl "${cluster}" wait --for=condition=Available "${ready_workloads[@]}" \
        -n bookinfo --timeout="${DEMO_TIMEOUT}"
    run_kubectl "${cluster}" get deployment -n bookinfo
}

show_bookinfo_traffic() {
    local expectation="$1"
    local request
    local traffic_loop

    request="${KUBECTL} --kubeconfig ${KUBECONFIGS["${SPOKE_CLUSTERS[0]}"]} exec deployment/curl -n bookinfo -c curl -- curl -sS http://reviews:9080/reviews/0"
    traffic_loop="for i in {1..12}; do ${request} | yq -r '.podname + \" \" + .clustername'; done"
    demo_comment "Call the shared reviews service repeatedly. Each line shows the pod and cluster that answered."
    demo_run "${traffic_loop}"
    demo_comment "${expectation}"
}

show_bookinfo_productpage() {
    local productpage_url
    local opener

    if [[ "${PLATFORM}" == "openshift" ]]; then
        demo_comment "Expose the Bookinfo productpage through an OpenShift Route on ${SPOKE_CLUSTERS[0]}."
        demo_run "${KUBECTL} --kubeconfig ${KUBECONFIGS["${SPOKE_CLUSTERS[0]}"]} create route edge productpage --service=productpage --port=9080 -n bookinfo 2>/dev/null || true"
        if [[ "${DEMO_DRY_RUN}" != "1" ]]; then
            local route_host
            route_host=$("${KUBECTL}" --kubeconfig "${KUBECONFIGS["${SPOKE_CLUSTERS[0]}"]}" get route productpage -n bookinfo -o jsonpath='{.spec.host}' 2>/dev/null) || true
            if [[ -n "${route_host}" ]]; then
                productpage_url="https://${route_host}/productpage"
            else
                demo_comment "Could not retrieve the Route host. Check the Route manually: ${KUBECTL} get route productpage -n bookinfo"
            fi
        fi
    else
        demo_comment "Port-forward the Bookinfo productpage from ${SPOKE_CLUSTERS[0]} so you can view it in a browser."
        demo_run "${KUBECTL} --kubeconfig ${KUBECONFIGS["${SPOKE_CLUSTERS[0]}"]} port-forward svc/productpage 9080:9080 -n bookinfo &"
        productpage_url="http://localhost:9080/productpage"
    fi

    if [[ "${DEMO_DRY_RUN}" == "1" ]]; then
        demo_comment "The productpage URL would be opened here."
    elif [[ -n "${productpage_url:-}" ]]; then
        demo_comment "Productpage URL: ${productpage_url}"
        demo_comment "Refresh a few times to see reviews with no stars (v1), black stars (v2), or red stars (v3)."
        if command -v xdg-open >/dev/null 2>&1; then
            opener=xdg-open
        elif command -v open >/dev/null 2>&1; then
            opener=open
        else
            opener=
        fi
        if [[ -n "${opener}" ]]; then
            "${opener}" "${productpage_url}" 2>/dev/null || true
        fi
    fi
}

clear
printf '\n'
printf '\033[1;36m'
printf '  ███╗   ███╗███████╗███████╗██╗  ██╗    █████╗ ██████╗ ██████╗  █████╗ ███╗   ██╗\n'
printf '  ████╗ ████║██╔════╝██╔════╝██║  ██║   ██╔══██╗██╔══██╗██╔══██╗██╔══██╗████╗  ██║\n'
printf '  ██╔████╔██║█████╗  ███████╗███████║   ███████║██║  ██║██║  ██║██║  ██║██╔██╗ ██║\n'
printf '  ██║╚██╔╝██║██╔══╝  ╚════██║██╔══██║   ██╔══██║██║  ██║██║  ██║██║  ██║██║╚██╗██║\n'
printf '  ██║ ╚═╝ ██║███████╗███████║██║  ██║   ██║  ██║██████╔╝██████╔╝╚█████╔╝██║ ╚████║\n'
printf '  ╚═╝     ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝   ╚═╝  ╚═╝╚═════╝ ╚═════╝  ╚════╝ ╚═╝  ╚═══╝\n'
printf '\033[0m\n'
printf '\033[1;37m                   Multicluster Mesh Addon · DP1\033[0m\n'
printf '\033[36m                      ── ACM × OSSM Demo ──\033[0m\n'
printf '\n'
demo_comment "Welcome to the multi-cluster service mesh demo."
demo_comment "We will use Bookinfo to make traffic between clusters visible."
demo_comment "ACM and the mesh add-on coordinate the clusters and bootstrap OSSM."
demo_comment "You will create the Istio and application resources as a customer would."
demo_comment "We start with two clusters, then add a third cluster and extend the same story."
demo_comment "The demo pauses after each part so you can read the result before continuing."
demo_wait

demo_title "Step 1: Preparing ACM"
demo_comment "ACM is already installed. Check the managed-cluster inventory, enable the add-on distribution feature, and make Gateway API available where the mesh needs it."
demo_comment "Check the hub's managed-cluster inventory."
run_kubectl hub get managedclusters
demo_comment "Wait for the three demo clusters to be available in ACM."
run_kubectl hub wait --for=condition=ManagedClusterConditionAvailable=True \
    "managedcluster/${SPOKE_CLUSTERS[0]}" "managedcluster/${SPOKE_CLUSTERS[1]}" "managedcluster/${SPOKE_CLUSTERS[2]}" \
    --timeout=5m
demo_comment "Enable ManifestWorkReplicaSet so the add-on can distribute resources through ACM."
mwrs_patch='{"spec":{"workConfiguration":{"featureGates":[{"feature":"ManifestWorkReplicaSet","mode":"Enable"}]}}}'
run_kubectl hub patch clustermanager cluster-manager --type=merge -p "'${mwrs_patch}'"
demo_comment "Check whether the spokes already provide the Gateway API resources used by the east-west gateway."
gateway_missing=()
for cluster in "${SPOKE_CLUSTERS[@]}"; do
    gateway_args=("${KUBECTL}" --kubeconfig "${KUBECONFIGS["${cluster}"]}")
    gateway_output="$(mktemp)"
    run_capture "${gateway_output}" "${gateway_args[@]}" \
        get crd gateways.gateway.networking.k8s.io gatewayclasses.gateway.networking.k8s.io \
        --ignore-not-found -o name
    gateway_crds="$(<"${gateway_output}")"
    rm -f -- "${gateway_output}"
    if [[ "${gateway_crds}" != *"gateways.gateway.networking.k8s.io"* ||
        "${gateway_crds}" != *"gatewayclasses.gateway.networking.k8s.io"* ]]; then
        gateway_missing+=("${cluster}")
    fi
done
if ((${#gateway_missing[@]} > 0)); then
    demo_comment "Install the standard Gateway API CRDs on spokes that do not provide them."
    for cluster in "${gateway_missing[@]}"; do
        run_kubectl "${cluster}" apply --server-side \
            -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml"
    done
else
    demo_comment "All spokes already provide the Gateway API resources needed by the demo."
fi
demo_wait

demo_title "Step 2: Installing the mesh add-on"
demo_comment "The add-on is not in an ACM or OLM catalog yet. Install its controller directly from the project's Helm repository."
demo_comment "Add the add-on's Helm repository."
run_helm repo add --force-update multicluster-mesh-addon \
    https://stolostron.github.io/multicluster-mesh-addon
run_helm repo update
demo_comment "Install the add-on controller from the Helm chart."
addon_args=(upgrade --install multicluster-mesh-addon multicluster-mesh-addon/multicluster-mesh-addon \
    --namespace multicluster-mesh-system --create-namespace --wait --timeout 5m)
if [[ "${PLATFORM}" == "kind" ]]; then
    addon_args+=(--set platform=kind --set image.pullPolicy=IfNotPresent)
fi
run_helm "${addon_args[@]}"
demo_comment "Wait for the controller that watches MultiClusterMesh resources."
run_kubectl hub wait --for=condition=Available deployment/multicluster-mesh-controller \
    -n multicluster-mesh-system --timeout=5m
demo_wait

demo_title "Step 3: Preparing certificates"
demo_comment "Use cert-manager to create a small self-signed trust chain for this demo. In production, use the issuer and trust policy required by your organization."
if [[ "${PLATFORM}" == "openshift" ]]; then
    if [[ "${DEMO_DRY_RUN}" != "1" ]] && "${KUBECTL}" --kubeconfig "${KUBECONFIGS[hub]}" get deployment cert-manager -n cert-manager &>/dev/null; then
        demo_comment "cert-manager is already installed on the hub. Skipping operator installation."
    else
        demo_comment "Install the Red Hat cert-manager Operator from the redhat-operators catalog."
        run_kubectl_apply hub hack/demo/cert-manager-operator.yaml
        demo_comment "Wait for the cert-manager Operator CSV to succeed."
        run_kubectl hub wait --for=create \
            --selector=operators.coreos.com/openshift-cert-manager-operator.cert-manager-operator \
            csv -n cert-manager-operator --timeout=10m
        run_kubectl hub wait --for=jsonpath='{.status.phase}'=Succeeded \
            --selector=operators.coreos.com/openshift-cert-manager-operator.cert-manager-operator \
            csv -n cert-manager-operator --timeout=10m
    fi
fi
demo_comment "Wait for cert-manager to become available on the hub."
run_kubectl hub wait --for=condition=Available deployment/cert-manager deployment/cert-manager-cainjector \
    deployment/cert-manager-webhook -n cert-manager --timeout=5m
demo_comment "Create the mesh namespace and apply the self-signed root trust chain."
run_kubectl_apply hub hack/demo/mesh-namespace.yaml
run_kubectl_apply hub samples/cert-manager-issuer.yaml -n "${MESH_NAMESPACE}"
demo_comment "Wait for the bootstrap issuer, root certificate, and CA-backed issuer to become ready."
run_kubectl hub wait --for=condition=Ready issuer/mesh-selfsigned-issuer certificate/mesh-root-ca \
    issuer/mesh-root-ca -n "${MESH_NAMESPACE}" --timeout=5m
demo_wait

demo_title "Step 4: Preparing the exclusive ClusterSet"
demo_comment "Create the exclusive ManagedClusterSet, add the first two clusters and assign the network identities used by Istio."
run_clusteradm create clusterset "${CLUSTER_SET}"
run_clusteradm clusterset set "${CLUSTER_SET}" --clusters "${SPOKE_CLUSTERS[0]},${SPOKE_CLUSTERS[1]}"
demo_comment "Label the first two clusters with their Istio network identities."
run_kubectl hub label managedcluster "${SPOKE_CLUSTERS[0]}" topology.istio.io/network=network-a --overwrite
run_kubectl hub label managedcluster "${SPOKE_CLUSTERS[1]}" topology.istio.io/network=network-b --overwrite
demo_wait

demo_title "Step 5: Creating the MultiClusterMesh"
demo_comment "Create the MultiClusterMesh resource on the hub. The add-on watches this resource and bootstraps OSSM on the selected clusters."
run_kubectl_apply hub "${MESH_SAMPLE}" -n "${MESH_NAMESPACE}"
demo_comment "Let's check in on the mesh while the selected clusters are being prepared."
demo_run "${KUBECTL} --kubeconfig ${KUBECONFIGS[hub]} get multiclustermesh/${MESH_NAME} -n ${MESH_NAMESPACE} -o yaml | yq -C .status | bat"
demo_comment "Wait for the add-on to report the mesh ready. This covers OSSM operator bootstrapping; Istio is still customer-managed."
run_kubectl hub wait --for=condition=Ready=True "multiclustermesh/${MESH_NAME}" \
    -n "${MESH_NAMESPACE}" --timeout="${DEMO_TIMEOUT}"
run_kubectl hub get "multiclustermesh/${MESH_NAME}" -n "${MESH_NAMESPACE}"
demo_wait

demo_title "Step 6: Configuring Istio - Phase 1: Configure Istio on ${SPOKE_CLUSTERS[0]}"
demo_comment "The add-on has finished its part. Now create the customer-owned Istio control plane and east-west gateway on the first cluster."
configure_istio_spoke "${SPOKE_CLUSTERS[0]}" network-a
demo_wait

demo_title "Step 6: Configuring Istio - Phase 2: Configure Istio on ${SPOKE_CLUSTERS[1]}"
demo_comment "Apply the same customer-owned Istio resources on the second cluster with its own cluster and network identity."
configure_istio_spoke "${SPOKE_CLUSTERS[1]}" network-b
demo_wait

demo_title "Step 6: Configuring Istio - Phase 3: Verify mesh connectivity"
demo_comment "Both control planes are installed. Check that they can see each other before deploying the Bookinfo application."
run_istioctl_remote_clusters "${SPOKE_CLUSTERS[0]}"
run_istioctl_remote_clusters "${SPOKE_CLUSTERS[1]}"
demo_comment "The multi-cluster Istio setup is now connected. The add-on supplied shared trust and endpoint discovery; the control planes and gateways are customer-owned."
demo_wait

demo_title "Step 7: Showing Bookinfo traffic - Phase 1: Deploy Bookinfo v1 on ${SPOKE_CLUSTERS[0]}"
demo_comment "Deploy the first reviews version on cluster1. The shared Bookinfo service name will be reused on the other clusters."
deploy_bookinfo_version "${SPOKE_CLUSTERS[0]}" v1
demo_wait

demo_title "Step 7: Showing Bookinfo traffic - Phase 2: Deploy Bookinfo v2 on ${SPOKE_CLUSTERS[1]}"
demo_comment "Deploy a different reviews version on cluster2. Istio will make both versions available through the shared service."
deploy_bookinfo_version "${SPOKE_CLUSTERS[1]}" v2
demo_wait

demo_title "Step 7: Showing Bookinfo traffic - Phase 3: Generate and verify cross-cluster traffic"
demo_comment "Use the small curl client from the e2e test to call reviews repeatedly and make the cross-cluster responses visible."
demo_comment "Create the curl client used by the cross-cluster e2e test in cluster1's injected Bookinfo namespace."
run_kubectl_apply "${SPOKE_CLUSTERS[0]}" test/e2e/testdata/curl.yaml -n bookinfo
run_kubectl "${SPOKE_CLUSTERS[0]}" wait --for=condition=Available deployment/curl -n bookinfo --timeout="${DEMO_TIMEOUT}"
show_bookinfo_traffic "Expected: reviews-v1 answers from ${SPOKE_CLUSTERS[0]} and reviews-v2 answers from ${SPOKE_CLUSTERS[1]}."
demo_wait

demo_title "Step 8: Scaling out to another cluster - Phase 1: Add ${SPOKE_CLUSTERS[2]} to the mesh"
demo_comment "Label a third cluster, add it to the exclusive ClusterSet, and watch the add-on bootstrap it automatically."
demo_comment "Give ${SPOKE_CLUSTERS[2]} its Istio network identity before adding it to the mesh."
run_kubectl hub label managedcluster "${SPOKE_CLUSTERS[2]}" topology.istio.io/network=network-c --overwrite
demo_comment "Add the labeled cluster to the exclusive ClusterSet."
run_clusteradm clusterset set "${CLUSTER_SET}" \
    --clusters "${SPOKE_CLUSTERS[0]},${SPOKE_CLUSTERS[1]},${SPOKE_CLUSTERS[2]}"
demo_comment "Check the MultiClusterMesh while ${SPOKE_CLUSTERS[2]} joins the mesh."
run_kubectl hub get "multiclustermesh/${MESH_NAME}" -n "${MESH_NAMESPACE}"
run_kubectl hub wait --for=condition=Ready=True "multiclustermesh/${MESH_NAME}" \
    -n "${MESH_NAMESPACE}" --timeout="${DEMO_TIMEOUT}"
run_kubectl hub get "multiclustermesh/${MESH_NAME}" -n "${MESH_NAMESPACE}"
demo_wait

demo_title "Step 8: Scaling out to another cluster - Phase 2: Configure Istio on ${SPOKE_CLUSTERS[2]}"
demo_comment "The add-on has bootstrapped OSSM on the new member. Install and configure Istio there as the customer."
configure_istio_spoke "${SPOKE_CLUSTERS[2]}" network-c
#demo_comment "Verify that all three control planes can see each other."
#run_istioctl_remote_clusters "${SPOKE_CLUSTERS[0]}"
#run_istioctl_remote_clusters "${SPOKE_CLUSTERS[1]}"
#run_istioctl_remote_clusters "${SPOKE_CLUSTERS[2]}"
demo_wait

demo_title "Step 8: Scaling out to another cluster - Phase 3: Deploy Bookinfo v3 and verify traffic"
demo_comment "Deploy the third reviews version on the new member, then call the same shared service to see all three clusters answer."
deploy_bookinfo_version "${SPOKE_CLUSTERS[2]}" v3
show_bookinfo_traffic "Expected: the shared reviews service can now answer with reviews-v1, reviews-v2, or reviews-v3 from the three clusters."
demo_comment "Open the Bookinfo productpage in a browser to see the visual difference between review versions."
show_bookinfo_productpage
demo_wait

if [[ "${PLATFORM}" == "openshift" ]]; then
    demo_title "Step 9: Opening the console"
    demo_comment "Install the OSSM Console plugin and open the ACM hub console so you can continue exploring the mesh from the UI."
    demo_comment "Install the OSSM Console operator on the ACM hub."
    run_kubectl_apply hub hack/demo/kiali-ossm-subscription.yaml
    demo_comment "Wait for the console operator to install."
    run_kubectl hub wait --for=create \
        --selector=operators.coreos.com/kiali-ossm.openshift-operators csv \
        -n openshift-operators --timeout=10m
    run_kubectl hub wait --for=jsonpath='{.status.phase}'=Succeeded \
        --selector=operators.coreos.com/kiali-ossm.openshift-operators csv \
        -n openshift-operators --timeout=10m
    run_kubectl hub wait --for=condition=Available deployment/kiali-operator \
        -n openshift-operators --timeout=5m
    demo_comment "Create the OSSMConsole resource that enables the Fleet Service Mesh perspective."
    run_kubectl_apply hub hack/demo/ossm-console.yaml
    demo_comment "Wait for the console plugin resource."
    run_kubectl hub wait --for=create consoleplugin/ossmconsole --timeout=10m
    run_kubectl hub get ossmconsole -A
    run_kubectl hub get consoleplugin ossmconsole
    demo_comment "Open the console and navigate to the Fleet Service Mesh perspective."
    demo_comment "Select the Meshes page, then click ${MESH_NAME} to see cluster status and control plane details."

    route_args=(get route console -n openshift-console -o 'jsonpath=https://{.spec.host}')
    hub_args=("${KUBECTL}" --kubeconfig "${KUBECONFIGS[hub]}")
    route_file="$(mktemp)"
    run_capture "${route_file}" "${hub_args[@]}" "${route_args[@]}"
    if [[ "${DEMO_DRY_RUN}" == "1" ]]; then
        rm -f -- "${route_file}"
        demo_comment "The hub console URL would be opened here."
    else
        console_url="$(<"${route_file}")"
        rm -f -- "${route_file}"
        demo_comment "Console URL: ${console_url}"
        if command -v xdg-open >/dev/null 2>&1; then
            opener=xdg-open
        elif command -v open >/dev/null 2>&1; then
            opener=open
        else
            opener=
        fi
        if [[ -n "${opener}" ]]; then
            if ! demo_run "${opener}" "${console_url}"; then
                demo_comment "Could not open the console automatically. Open the printed URL manually."
            fi
        else
            demo_comment "No desktop opener found. Open the console URL manually."
        fi
    fi
fi

demo_comment "The demo is concluded. You can now continue working with the mesh or tear it down."
if [[ "${PLATFORM}" == "kind" ]]; then
    demo_comment "Tear down the local environment with: make demo-dev-clean"
else
    demo_comment "Delete multiclustermesh/${MESH_NAME} in ${MESH_NAMESPACE} and the demo Bookinfo namespaces when finished."
fi
