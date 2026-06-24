#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
make -C "${SCRIPT_DIR}/.." .ensure-oc-login

OPENSHIFT_VERSION=$(oc version | grep "Server Version: " | awk '{print $3}' | cut -d. -f-2)
CONSOLE_IMAGE=${CONSOLE_IMAGE:="quay.io/openshift/origin-console:$OPENSHIFT_VERSION"}
CONSOLE_PORT=${CONSOLE_PORT:=9000}
CONSOLE_IMAGE_PLATFORM=${CONSOLE_IMAGE_PLATFORM:="linux/amd64"}
PLUGIN_NAME=ossm-acm
PLUGIN_DEV_PORT=${PLUGIN_DEV_PORT:=9001}
ACM_PORT=${ACM_PORT:=9002}
MCE_PORT=${MCE_PORT:=9003}
# Set LOAD_ACM_PLUGINS=false to skip ACM/MCE port-forwards (Fleet Management links will not work).
LOAD_ACM_PLUGINS=${LOAD_ACM_PLUGINS:=true}

ACM_SERVICE=${ACM_SERVICE:=console-chart-console-v2}
ACM_NAMESPACE=${ACM_NAMESPACE:=open-cluster-management}
MCE_SERVICE=${MCE_SERVICE:=console-mce-console}
MCE_NAMESPACE=${MCE_NAMESPACE:=multicluster-engine}

PORT_FORWARD_PIDS=()
CLEANUP_RAN=false

free_local_port() {
    local port=$1
    if command -v lsof >/dev/null 2>&1; then
        lsof -ti ":${port}" 2>/dev/null | xargs -r kill 2>/dev/null || true
    fi
}

stop_port_forwards() {
    local pid
    local stopped=0

    for pid in "${PORT_FORWARD_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            wait "$pid" 2>/dev/null || true
            stopped=$((stopped + 1))
        fi
    done

    # Fallback when Ctrl+C races the tracked PID list (common when run via make).
    if command -v pgrep >/dev/null 2>&1; then
        while read -r pid; do
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                kill "$pid" 2>/dev/null || true
                wait "$pid" 2>/dev/null || true
                stopped=$((stopped + 1))
            fi
        done < <(pgrep -f "port-forward.*:${ACM_PORT}:3000" 2>/dev/null || true)
        while read -r pid; do
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                kill "$pid" 2>/dev/null || true
                wait "$pid" 2>/dev/null || true
                stopped=$((stopped + 1))
            fi
        done < <(pgrep -f "port-forward.*:${MCE_PORT}:3000" 2>/dev/null || true)
    fi

    free_local_port "$ACM_PORT"
    free_local_port "$MCE_PORT"
    echo "$stopped"
}

cleanup() {
    if [ "$CLEANUP_RAN" = "true" ]; then
        return 0
    fi
    CLEANUP_RAN=true

    if [ "$LOAD_ACM_PLUGINS" != "true" ]; then
        return 0
    fi

    local stopped
    stopped=$(stop_port_forwards)
    echo "Stopped ACM/MCE port-forwards on localhost:${ACM_PORT} and localhost:${MCE_PORT} (cleaned ${stopped} process(es))."
}

on_interrupt() {
    cleanup
    exit 0
}

trap cleanup EXIT
trap on_interrupt INT TERM HUP

wait_for_https() {
    local url=$1
    local label=$2
    local attempts=60

    while [ "$attempts" -gt 0 ]; do
        if curl -sk -o /dev/null --fail "$url" 2>/dev/null; then
            return 0
        fi
        sleep 0.5
        attempts=$((attempts - 1))
    done
    echo "Error: timed out waiting for ${label} at ${url}" >&2
    return 1
}

start_port_forward() {
    local namespace=$1
    local service=$2
    local local_port=$3
    local label=$4

    if ! command -v curl >/dev/null 2>&1; then
        echo "Error: curl is required to verify ${label} port-forward readiness" >&2
        exit 1
    fi

    free_local_port "$local_port"

    oc port-forward -n "$namespace" "svc/${service}" "${local_port}:3000" >/dev/null 2>&1 &
    PORT_FORWARD_PIDS+=("$!")
    echo "  ${label} port-forward PID $! (localhost:${local_port})" >&2
    wait_for_https "https://127.0.0.1:${local_port}/plugin/plugin-manifest.json" "$label"
}

build_plugin_proxy_json() {
    local host=$1
    if command -v jq >/dev/null 2>&1; then
        jq -cn \
            --arg acm_endpoint "https://${host}:${ACM_PORT}" \
            --arg mce_endpoint "https://${host}:${MCE_PORT}" \
            '{"services":[
                {"consoleAPIPath":"/api/proxy/plugin/acm/console/","endpoint":$acm_endpoint,"authorize":true},
                {"consoleAPIPath":"/api/proxy/plugin/mce/console/","endpoint":$mce_endpoint,"authorize":true}
            ]}'
    else
        printf '{"services":[{"consoleAPIPath":"/api/proxy/plugin/acm/console/","endpoint":"https://%s:%s","authorize":true},{"consoleAPIPath":"/api/proxy/plugin/mce/console/","endpoint":"https://%s:%s","authorize":true}]}' \
            "$host" "$ACM_PORT" "$host" "$MCE_PORT"
    fi
}

setup_acm_plugins() {
    local host=$1
    local load_acm=false
    local load_mce=false

    BRIDGE_PLUGIN_PROXY=""

    if [ "$LOAD_ACM_PLUGINS" != "true" ]; then
        echo "LOAD_ACM_PLUGINS=false — skipping ACM/MCE (Fleet Management links will not work)" >&2
        BRIDGE_PLUGINS="${PLUGIN_NAME}=http://${host}:${PLUGIN_DEV_PORT}"
        return 0
    fi

    if oc get consoleplugin acm >/dev/null 2>&1; then
        load_acm=true
    else
        echo "Warning: ConsolePlugin 'acm' not found — Fleet Management perspective unavailable" >&2
    fi

    if oc get consoleplugin mce >/dev/null 2>&1; then
        load_mce=true
    else
        echo "Warning: ConsolePlugin 'mce' not found — ACM plugin requires MCE" >&2
    fi

    if [ "$load_acm" = "true" ]; then
        echo "Port-forwarding ACM plugin (${ACM_NAMESPACE}/${ACM_SERVICE}) to localhost:${ACM_PORT}..." >&2
        start_port_forward "$ACM_NAMESPACE" "$ACM_SERVICE" "$ACM_PORT" "ACM plugin"
    fi

    if [ "$load_mce" = "true" ]; then
        echo "Port-forwarding MCE plugin (${MCE_NAMESPACE}/${MCE_SERVICE}) to localhost:${MCE_PORT}..." >&2
        start_port_forward "$MCE_NAMESPACE" "$MCE_SERVICE" "$MCE_PORT" "MCE plugin"
    fi

    if [ "$load_acm" = "true" ] || [ "$load_mce" = "true" ]; then
        BRIDGE_I18N_NAMESPACES="plugin__${PLUGIN_NAME},plugin__acm,plugin__mce"
        BRIDGE_PLUGIN_PROXY=$(build_plugin_proxy_json "$host")
    fi

    if [ "$load_acm" = "true" ] && [ "$load_mce" = "true" ]; then
        BRIDGE_PLUGINS="${PLUGIN_NAME}=http://${host}:${PLUGIN_DEV_PORT},mce=https://${host}:${MCE_PORT}/plugin/,acm=https://${host}:${ACM_PORT}/plugin/"
    elif [ "$load_acm" = "true" ]; then
        BRIDGE_PLUGINS="${PLUGIN_NAME}=http://${host}:${PLUGIN_DEV_PORT},acm=https://${host}:${ACM_PORT}/plugin/"
    elif [ "$load_mce" = "true" ]; then
        BRIDGE_PLUGINS="${PLUGIN_NAME}=http://${host}:${PLUGIN_DEV_PORT},mce=https://${host}:${MCE_PORT}/plugin/"
    else
        BRIDGE_PLUGINS="${PLUGIN_NAME}=http://${host}:${PLUGIN_DEV_PORT}"
    fi
}

echo "Starting local OpenShift console..."

BRIDGE_USER_AUTH="disabled"
BRIDGE_K8S_MODE="off-cluster"
BRIDGE_K8S_AUTH="bearer-token"
BRIDGE_K8S_MODE_OFF_CLUSTER_SKIP_VERIFY_TLS=true
BRIDGE_K8S_MODE_OFF_CLUSTER_ENDPOINT=$(oc whoami --show-server)
BRIDGE_K8S_AUTH_BEARER_TOKEN=$(oc whoami --show-token 2>/dev/null)
BRIDGE_USER_SETTINGS_LOCATION="localstorage"
BRIDGE_I18N_NAMESPACES="plugin__${PLUGIN_NAME}"

PLUGIN_HOST="host.docker.internal"
CONSOLE_NETWORK_ARGS=(-p "${CONSOLE_PORT}:9000")

if [ -x "$(command -v podman)" ]; then
    if [ "$(uname -s)" = "Linux" ]; then
        PLUGIN_HOST="localhost"
        CONSOLE_NETWORK_ARGS=(--network=host)
    else
        PLUGIN_HOST="host.containers.internal"
    fi
else
    PLUGIN_HOST="host.docker.internal"
fi

# Must not use command substitution here — it runs in a subshell and drops port-forward PIDs
# and BRIDGE_PLUGIN_PROXY before the console container starts.
setup_acm_plugins "$PLUGIN_HOST"

echo "API Server: $BRIDGE_K8S_MODE_OFF_CLUSTER_ENDPOINT"
echo "Console Image: $CONSOLE_IMAGE"
echo "Console URL: http://localhost:${CONSOLE_PORT}"
echo "Console Platform: $CONSOLE_IMAGE_PLATFORM"
echo "Plugin dev server: http://localhost:${PLUGIN_DEV_PORT}"
echo "BRIDGE_PLUGINS: ${BRIDGE_PLUGINS}"
if [ -n "${BRIDGE_PLUGIN_PROXY:-}" ]; then
    echo "BRIDGE_PLUGIN_PROXY: ${BRIDGE_PLUGIN_PROXY}"
fi

run_console() {
    local engine=$1
    local -a plugin_env=(--env "BRIDGE_PLUGINS=${BRIDGE_PLUGINS}")

    if [ -n "${BRIDGE_PLUGIN_PROXY:-}" ]; then
        plugin_env+=(--env "BRIDGE_PLUGIN_PROXY=${BRIDGE_PLUGIN_PROXY}")
    fi

    # Pass plugin env vars explicitly; stolostron/console does this because JSON values
    # in BRIDGE_PLUGIN_PROXY are easy to mis-parse from --env-file alone.
    "$engine" run --pull always --platform "$CONSOLE_IMAGE_PLATFORM" --rm \
        "${CONSOLE_NETWORK_ARGS[@]}" \
        --env-file <(for var in "${!BRIDGE_@}"; do echo "$var=${!var}"; done) \
        "${plugin_env[@]}" \
        "$CONSOLE_IMAGE"
}

console_exit=0
if [ -x "$(command -v podman)" ]; then
    run_console podman || console_exit=$?
else
    run_console docker || console_exit=$?
fi

# podman exits 2 on Ctrl+C; treat interrupt as a normal shutdown after cleanup.
if [ "$console_exit" -eq 130 ] || [ "$console_exit" -eq 143 ] || [ "$console_exit" -eq 2 ]; then
    exit 0
fi

exit "$console_exit"
