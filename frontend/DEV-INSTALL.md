# Dev Install Guide — End-to-End on CRC

Complete instructions to go from zero to a working Fleet Service Mesh ConsolePlugin and backend controller on a local CRC OpenShift cluster. If you have a two-cluster ACM environment, see [DEMO-SETUP-MULTICLUSTER.md](DEMO-SETUP-MULTICLUSTER.md) instead.

> **Quick start:** After ACM, the backend controller, and the frontend are deployed (steps 1-3), [`hack/setup-demo.sh install`](hack/setup-demo.sh) automates the rest: cert-manager, infrastructure, trust chain, MCM creation, and discovered CRs.

## Resource Layout

| MCM CR | MCM Namespace | Istio CR (auto-created) | CP Namespace | Mesh ID | Trust |
|--------|---------------|-------------------------|--------------|---------|-------|
| `secure-mcm` | `secure-mcm-ns` | `secure-mcm-ns-secure-mcm-cp` | `secure-ns` | `secure-mcm-ns-secure-mcm` | Yes |
| `unsecure-mcm` | `unsecure-mcm-ns` | `unsecure-mcm-ns-unsecure-mcm-cp` | `unsecure-ns` | `unsecure-mcm-ns-unsecure-mcm` | No |
| — | — | `discovered-standalone` | `istio-discovered` | `standalone-mesh` | — |

The first two Istio CRs are created automatically by the controller when it
reconciles their corresponding MCM CRs. The controller also creates IstioCNI,
east-west gateways, control plane namespaces, and RBAC via ManifestWorks.
The last row is a standalone "discovered" control plane with no MCM association,
created manually.

## Prerequisites

- [crc](https://crc.dev) binary installed
- A [Red Hat pull secret](https://console.redhat.com/openshift/create/local)
- `oc` installed
- `podman` installed
- `jq` installed
- `make` installed
- `helm` installed
- Node.js `^20.19.0 || >=22.12.0`
- Go toolchain

## 1. Get an OpenShift cluster with ACM

You need an OpenShift cluster with ACM (Advanced Cluster Management) 2.16+ installed. The image registry must be exposed. How you get this is up to you — any method that produces a working ACM hub cluster will work.

One option is the [install-acm.sh](https://github.com/kiali/kiali/blob/master/hack/install-acm.sh) script in the Kiali repo, which automates a full CRC/OpenShift + ACM setup. Its `init-openshift` command depends on other scripts in the same repo, so you need the [kiali server repo](https://github.com/kiali/kiali) cloned locally. All commands below are run from that repo's directory:

```bash
# Start CRC with 12 CPUs, 100GB disk, exposed image registry
./hack/install-acm.sh --crc-pull-secret-file <path-to-your-pull-secret-file> init-openshift

# Install ACM 2.16+ (operator, MultiClusterHub, observability)
# ACM 2.16 is required for the v1beta1 addon API used by the backend Helm chart.
./hack/install-acm.sh -c release-2.16 install-acm
```

This takes 15-20 minutes. It installs the ACM operator, creates a MultiClusterHub, sets up MinIO for metrics storage, and enables observability. It also auto-registers `local-cluster` as a managed cluster (the hub acts as its own spoke).

Regardless of how you set up your cluster, verify ACM is ready before proceeding:

```bash
oc get mch multiclusterhub -n open-cluster-management -o jsonpath='{.status.phase}'
# Should output: Running

oc get managedcluster local-cluster
# Should show local-cluster as available
```

## 2. Build and deploy the backend controller

The backend deploys via Helm. We build the image, push it to the OpenShift internal registry, then use `helm upgrade --install` to deploy.

```bash
cd <multicluster-mesh-addon-repo>

REGISTRY=$(oc get image.config.openshift.io/cluster \
  -o jsonpath='{.status.externalRegistryHostnames[0]}')
INTERNAL_REGISTRY=image-registry.openshift-image-registry.svc:5000
BACKEND_NAMESPACE=multicluster-mesh-system
BACKEND_IMAGE_NAME=multicluster-mesh-addon
BACKEND_IMAGE_TAG=dev

# Login to the OpenShift image registry
podman login --tls-verify=false \
  -u $(oc whoami | tr -d ':') \
  -p $(oc whoami -t) \
  ${REGISTRY}

# Create the controller namespace if it doesn't exist (required before pushing to the internal registry)
oc create namespace ${BACKEND_NAMESPACE} --dry-run=client -o yaml | oc apply -f -

# Build and push the controller image
make images IMG=${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}
podman push --tls-verify=false ${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}

# Update the CRD (Helm does not update CRDs on upgrade, only on first install)
oc apply -f chart/crds/mesh.open-cluster-management.io_multiclustermeshes.yaml

# Deploy the controller using Helm with the internal registry image
helm upgrade --install ${BACKEND_IMAGE_NAME} chart/ \
  --create-namespace \
  --namespace ${BACKEND_NAMESPACE} \
  --set image.repository=${INTERNAL_REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME} \
  --set image.tag=${BACKEND_IMAGE_TAG} \
  --wait --timeout 180s

# Restart the controller to ensure it picks up the new image
# (required because the image tag "dev" doesn't change between rebuilds)
oc rollout restart deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE}
oc rollout status deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE} --timeout=120s
```

## 3. Build and deploy the frontend ConsolePlugin

From the `frontend/` directory, build the container image and deploy:

```bash
cd <multicluster-mesh-addon-repo>/frontend
make build deploy
```

This builds a container image with the compiled plugin assets baked in (UBI9 nginx), pushes it to the OpenShift internal registry, deploys it, registers the ConsolePlugin, and restarts the console.

## 4. Install cert-manager

Required by the backend controller for trust distribution.

```bash
oc apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
oc rollout status deployment/cert-manager -n cert-manager --timeout=120s
oc rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s
```

## 5. Set up infrastructure

Create the ManagedClusterSet and MCM namespaces.

```bash
# Create a ManagedClusterSet and bind local-cluster to it
oc apply -f - <<'EOF'
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: demo-cluster-set
EOF

oc label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset=demo-cluster-set --overwrite

# Create MCM namespaces
oc create namespace secure-mcm-ns
oc create namespace unsecure-mcm-ns
```

## 6. Set up the trust chain

Deploy a cert-manager trust chain in the secure mesh's namespace. This
establishes the root CA that the controller will use to mint per-cluster
intermediate certificates for mTLS.

```bash
cd <multicluster-mesh-addon-repo>

# Create a cert-manager trust chain (self-signed Issuer -> root CA Certificate -> CA-backed Issuer)
oc apply -n secure-mcm-ns -f samples/cert-manager-issuer.yaml

# Wait for the root CA to be ready
oc wait certificate mesh-root-ca -n secure-mcm-ns --for=condition=Ready --timeout=60s
```

## 7. Create meshes

Create both MultiClusterMesh CRs. The controller will automatically install the
operator, IstioCNI, Istio CRs, east-west gateways, and trust certificates on
all clusters in the set.

```bash
# secure-mcm: mesh with trust enabled, using the built-in basic template
# - templateSource.basic: uses the controller's built-in Istio CR template
#   (suitable for demos). To customize, use configMapRef, git, or none. See step 7a.
# - topology.type: MultiPrimary (every cluster runs its own control plane;
#   use PrimaryRemote if you want one primary and the rest as remotes)
# - gateway.serviceType: NodePort (CRC has no LoadBalancer controller;
#   use LoadBalancer on cloud platforms)
oc apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: secure-mcm
  namespace: secure-mcm-ns
spec:
  clusterSet: demo-cluster-set
  controlPlane:
    namespace: secure-ns
    templateSource:
      basic: {}
  gateway:
    serviceType: NodePort
  topology:
    type: MultiPrimary
  security:
    trust:
      certManager:
        issuerRef:
          name: mesh-root-ca
EOF

# unsecure-mcm: mesh without trust, using the built-in basic template
oc apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: unsecure-mcm
  namespace: unsecure-mcm-ns
spec:
  clusterSet: demo-cluster-set
  controlPlane:
    namespace: unsecure-ns
    templateSource:
      basic: {}
  gateway:
    serviceType: NodePort
  topology:
    type: MultiPrimary
EOF
```

Monitor the controller's progress through the reconciliation phases:

```bash
# Watch per-cluster conditions (OperatorInstalled → ControlPlaneReady → GatewayReady)
oc get multiclustermesh secure-mcm -n secure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .
oc get multiclustermesh unsecure-mcm -n unsecure-mcm-ns \
  -o jsonpath='{.status.clusterStatus}' | jq .

# Check ManifestWorks created by the controller
oc get manifestwork -n local-cluster | grep multicluster-mesh
```

## 7a. (Optional) Template source alternatives

The meshes above use `templateSource.basic`, the controller's built-in Istio CR
template. The controller supports three other template sources for customizing
the Istio CR that gets deployed to managed clusters.

**ConfigMap** — provide custom Istio CR YAML in a ConfigMap:

```bash
# Create a ConfigMap with a custom Istio CR template
oc apply -n secure-mcm-ns -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-istio-template
data:
  istio.yaml: |
    apiVersion: sailoperator.io/v1
    kind: Istio
    spec:
      values:
        pilot:
          resources:
            requests:
              cpu: 500m
              memory: 2Gi
        meshConfig:
          accessLogFile: /dev/stdout
EOF

# Then set templateSource.configMapRef in the MCM spec:
oc patch multiclustermesh secure-mcm -n secure-mcm-ns --type merge -p '
spec:
  controlPlane:
    templateSource:
      configMapRef:
        name: my-istio-template
'
```

**Git** — pull the template from a git repository:

```yaml
spec:
  controlPlane:
    templateSource:
      git:
        url: https://github.com/myorg/mesh-configs.git
        path: istio/production/istio.yaml
        ref:
          branch: main
        # secretRef is optional — omit for public repos
        # secretRef:
        #   name: git-credentials
```

**None** — skip mesh resource creation entirely (for ArgoCD/Flux users who
manage their own Istio CRs). The controller only installs the operator, creates
ManagedServiceAccounts, and distributes trust certificates:

```yaml
spec:
  controlPlane:
    templateSource:
      none: {}
```

In all cases, the controller deep-merges its required fields (meshID, network,
trustDomain, topology) on top of whatever the template provides. See
[docs/design.md](../docs/design.md) for details.

## 8. (Optional) Create a standalone discovered Istio CR

The Control Planes page discovers all `Istio` CRs across managed clusters via
ACM Search — including ones not managed by any MultiClusterMesh. These appear
as "discovered" control planes in the UI.

```bash
# The Sail operator must be installed first (the controller does this when reconciling the MCMs).
oc get csv -n openshift-operators | grep servicemesh

# Create a standalone Istio CR not associated with any MCM
oc create namespace istio-discovered --dry-run=client -o yaml | oc apply -f -

oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: discovered-standalone
spec:
  namespace: istio-discovered
  values:
    global:
      meshID: standalone-mesh
      multiCluster:
        clusterName: local-cluster
      network: network-standalone
EOF

# Verify all Istio CRs (2 managed + 1 discovered)
oc get istios
```

The Control Planes page polls ACM Search every 30 seconds. After the search
collector indexes the Istio CRs (typically within 1-2 minutes), they will
appear in the table.

To clean up:

```bash
oc delete istio discovered-standalone
oc delete namespace istio-discovered --ignore-not-found
```

## 9. Verify

1. Open the CRC console: `oc whoami --show-console`
2. Log in as `kubeadmin`
3. Click the perspective switcher (top-left dropdown)
4. Select **Fleet Service Mesh**
5. The **Overview** page should appear with donut charts for Meshes and Control Planes health
6. Click **Meshes** in the left nav — the table should show `secure-mcm` and `unsecure-mcm` with their statuses
7. Click a mesh to see per-cluster conditions: Operator, Control Plane, Gateway, Discovery
8. Click **Control Planes** in the left nav — it shows all Istio CRs (managed + discovered)

## 10. (Optional) Deploy the mesh-hello test application

Deploy a browser-accessible test app that shows cluster identity, cross-cluster
connectivity, and mTLS status. The app consists of a frontend and backend service
injected into the Istio mesh.

```bash
cd <multicluster-mesh-addon-repo>/frontend

# Deploy into the secure-mcm mesh (with trust — shows mTLS details)
hack/deploy-mesh-hello.sh -m secure-mcm -n secure-mcm-ns install
```

The script creates a `secure-mcm-testapp` namespace with Istio sidecar injection,
deploys the frontend and backend, and prints a URL you can open in your browser
(e.g. `http://mesh-hello-secure-mcm-secure-mcm-testapp.apps-crc.testing/` on CRC).
The page auto-refreshes every 10 seconds showing the frontend's identity,
the backend's cross-cluster response, and mTLS certificate details.

To remove:

```bash
hack/deploy-mesh-hello.sh -m secure-mcm -n secure-mcm-ns uninstall
```

## Frontend Development

### Fast iteration (local webpack)

For day-to-day UI work, run the plugin locally with webpack and a local OpenShift Console bridge.

**Prerequisites:** `oc login`, ACM and backend controller deployed on the cluster, Node.js `^20.19.0 || >=22.12.0`, `podman` or `docker`, and npm dependencies installed.

```bash
cd <multicluster-mesh-addon-repo>/frontend
make prepare-dev-env   # one-time, or after package.json changes
```

Run in **two terminals**:

```bash
# Terminal 1 — webpack dev server on localhost:9001
make start

# Terminal 2 — local OpenShift Console on localhost:9000
make start-console
```

`start-console` automatically port-forwards the in-cluster ACM and MCE console plugins (ports 9002 and 9003) so Fleet Management perspective and cross-plugin links work. The console bridge forwards your `oc` bearer token to those backends (`authorize: true`). Port-forwards are stopped when you exit `start-console` (Ctrl+C). Set `LOAD_ACM_PLUGINS=false` to skip ACM/MCE if you do not need Fleet Management links.

Open http://localhost:9000 and switch to the **Fleet Service Mesh** perspective. After editing source files, wait for webpack to rebuild and refresh the browser. The cluster plugin deploy is not required for this workflow — the local console loads the plugin from webpack.

### Cluster deploy (production-like)

For production-like testing (nginx/TLS packaging, in-cluster ConsolePlugin), rebuild and redeploy to the cluster:

```bash
cd <multicluster-mesh-addon-repo>/frontend
make build deploy
```

## Backend Development

After modifying backend Go code, rebuild the image, push it to the cluster registry, update the CRD if it changed, and restart the controller:

```bash
cd <multicluster-mesh-addon-repo>

REGISTRY=$(oc get image.config.openshift.io/cluster \
  -o jsonpath='{.status.externalRegistryHostnames[0]}')
BACKEND_NAMESPACE=multicluster-mesh-system
BACKEND_IMAGE_NAME=multicluster-mesh-addon
BACKEND_IMAGE_TAG=dev

make images IMG=${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}
podman push --tls-verify=false \
  ${REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME}:${BACKEND_IMAGE_TAG}

# Update the CRD if types.go changed (Helm does not update CRDs on upgrade)
oc apply -f chart/crds/mesh.open-cluster-management.io_multiclustermeshes.yaml

oc rollout restart deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE}
oc rollout status deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE} --timeout=120s
```

## Teardown

```bash
cd <multicluster-mesh-addon-repo>

# Remove the frontend plugin
cd frontend && make teardown && cd ..

# Remove standalone discovered Istio CR
oc delete istio discovered-standalone --ignore-not-found
oc delete namespace istio-discovered --ignore-not-found

# Remove MCM CRs (controller auto-cleans ManifestWorks, operator, IstioCNI, etc.)
oc delete multiclustermesh secure-mcm -n secure-mcm-ns --ignore-not-found
oc delete multiclustermesh unsecure-mcm -n unsecure-mcm-ns --ignore-not-found

# Remove namespaces
oc delete namespace secure-mcm-ns unsecure-mcm-ns --ignore-not-found

# Remove cluster label and set
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset- 2>/dev/null; true
oc delete managedclusterset demo-cluster-set --ignore-not-found

# Remove the backend controller
make undeploy
```
