#!/usr/bin/env bash
#
# deploy-mesh-hello.sh — Deploy or remove the mesh-hello test application
#
# A simple frontend+backend Python app that shows cluster identity,
# cross-cluster connectivity, and mTLS status via a browser-accessible
# HTML page. The frontend calls the backend's /api endpoint and displays
# the response alongside its own identity. The page auto-refreshes every
# 10 seconds.
#
# The app is injected into the Istio mesh via revision-based sidecar
# injection. It demonstrates that the mesh is working end-to-end.
#
# Usage:
#   deploy-mesh-hello.sh -m MESH_NAME -n MESH_NAMESPACE install
#   deploy-mesh-hello.sh -m MESH_NAME -n MESH_NAMESPACE uninstall
#
# Prerequisites:
#   - A MultiClusterMesh CR that has been reconciled (ControlPlaneReady)
#   - oc logged in to the hub cluster
#
# The app namespace is {MESH_NAME}-testapp. On OpenShift, a Route is
# created automatically. On vanilla K8s, use port-forward.

set -uo pipefail

die() { echo "ERROR: $*" >&2; exit 1; }
log() { echo "=== $* ==="; }
info() { echo "  $*"; }

MESH_NAME=""
MESH_NAMESPACE=""
ACTION=""
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"

usage() {
  cat <<'USAGE'
Deploy the mesh-hello test application into an Istio mesh.

Usage: deploy-mesh-hello.sh [OPTIONS] install|uninstall

Required:
  -m, --mesh NAME          MultiClusterMesh CR name
  -n, --namespace NS       MultiClusterMesh CR namespace

Options:
  -h, --help               Show this help message

Examples:
  deploy-mesh-hello.sh -m my-mesh -n mesh-system install
  deploy-mesh-hello.sh -m my-mesh -n mesh-system uninstall
USAGE
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "${1}" in
      -m|--mesh) MESH_NAME="${2:?'--mesh requires a value'}"; shift 2 ;;
      -n|--namespace) MESH_NAMESPACE="${2:?'--namespace requires a value'}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      install|uninstall) ACTION="${1}"; shift ;;
      *) die "Unknown option: ${1}. Run with --help for usage." ;;
    esac
  done
  [ -n "${MESH_NAME}" ] || die "Missing required option: -m/--mesh"
  [ -n "${MESH_NAMESPACE}" ] || die "Missing required option: -n/--namespace"
  [ -n "${ACTION}" ] || die "Missing action: specify 'install' or 'uninstall'"
}

MESH_ID=""
CP_NAMESPACE=""
APP_NS=""
REVISION=""

read_mesh() {
  log "Reading MultiClusterMesh ${MESH_NAMESPACE}/${MESH_NAME}"

  oc get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" &>/dev/null \
    || die "MultiClusterMesh '${MESH_NAME}' not found in namespace '${MESH_NAMESPACE}'."

  CP_NAMESPACE=$(oc get multiclustermesh "${MESH_NAME}" -n "${MESH_NAMESPACE}" \
    -o jsonpath='{.spec.controlPlane.namespace}' 2>/dev/null || true)
  CP_NAMESPACE="${CP_NAMESPACE:-istio-system}"

  MESH_ID="${MESH_NAMESPACE}-${MESH_NAME}"
  REVISION="${MESH_ID}-cp"
  APP_NS="${MESH_NAME}-testapp"

  info "Mesh ID: ${MESH_ID}"
  info "CP namespace: ${CP_NAMESPACE}"
  info "Istio revision: ${REVISION}"
  info "App namespace: ${APP_NS}"
}

do_install() {
  local app_image="registry.access.redhat.com/ubi9/python-311"
  local backend_url="http://mesh-hello-backend.${APP_NS}.svc.cluster.local:8080/api"
  local route_name="mesh-hello-${MESH_NAME}"

  log "Creating app namespace with Istio injection"
  oc create namespace "${APP_NS}" --dry-run=client -o yaml | oc apply -f -
  oc label namespace "${APP_NS}" "istio.io/rev=${REVISION}" --overwrite
  oc label namespace "${APP_NS}" "istio-injection-" 2>/dev/null || true

  log "Deploying mesh-hello application"
  oc apply -n "${APP_NS}" -f - <<'PYEOF'
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

  info "Deploying frontend..."
  oc apply -n "${APP_NS}" -f - <<EOF
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
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
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

  info "Deploying backend..."
  oc apply -n "${APP_NS}" -f - <<EOF
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
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
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
  if oc api-resources --api-group=route.openshift.io --no-headers 2>/dev/null | grep -q routes; then
    if ! oc get route "${route_name}" -n "${APP_NS}" &>/dev/null; then
      oc expose svc mesh-hello -n "${APP_NS}" --name="${route_name}" \
        || die "Failed to create Route"
    fi
  fi

  log "Waiting for deployments"
  local elapsed=0
  while true; do
    local fe_ready be_ready
    fe_ready=$(oc get deploy mesh-hello -n "${APP_NS}" \
      -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
    be_ready=$(oc get deploy mesh-hello-backend -n "${APP_NS}" \
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

  echo ""
  log "mesh-hello deployed"
  echo ""

  if oc api-resources --api-group=route.openshift.io --no-headers 2>/dev/null | grep -q routes; then
    local route_host
    route_host=$(oc get route "${route_name}" -n "${APP_NS}" \
      -o jsonpath='{.spec.host}' 2>/dev/null || true)
    echo "Open in your browser: http://${route_host}"
  else
    echo "Access the test app with:"
    echo "  oc port-forward -n ${APP_NS} svc/mesh-hello 8080:8080"
    echo "  Then open: http://localhost:8080"
  fi
  echo ""
}

do_uninstall() {
  log "Removing mesh-hello from ${APP_NS}"
  oc delete namespace "${APP_NS}" --ignore-not-found 2>/dev/null || true
  info "[ok] Removed"
}

parse_args "$@"
read_mesh

case "${ACTION}" in
  install)   do_install ;;
  uninstall) do_uninstall ;;
esac
