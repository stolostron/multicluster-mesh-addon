# Dev Install Guide — End-to-End on CRC

Complete instructions to go from zero to a working Fleet Service Mesh ConsolePlugin and backend controller on a local CRC OpenShift cluster.

## Prerequisites

- [crc](https://crc.dev) binary installed
- A [Red Hat pull secret](https://console.redhat.com/openshift/create/local)
- `oc` installed
- `podman` installed
- `jq` installed
- `make` installed
- Node.js 20+
- Go toolchain

## 1. Get an OpenShift cluster with ACM

You need an OpenShift cluster with ACM (Advanced Cluster Management) installed. The image registry must be exposed. How you get this is up to you — any method that produces a working ACM hub cluster will work.

One option is the [install-acm.sh](https://github.com/kiali/kiali/blob/master/hack/install-acm.sh) script in the Kiali repo, which automates a full CRC/OpenShift + ACM setup. Its `init-openshift` command depends on other scripts in the same repo, so you need the [kiali server repo](https://github.com/kiali/kiali) cloned locally. All commands below are run from that repo's directory:

```bash
# Start CRC with 12 CPUs, 100GB disk, exposed image registry
./hack/install-acm.sh --crc-pull-secret-file <path-to-your-pull-secret-file> init-openshift

# Install ACM (operator, MultiClusterHub, observability)
./hack/install-acm.sh install-acm
```

This takes 15-20 minutes. It installs the ACM operator, creates a MultiClusterHub, sets up MinIO for metrics storage, and enables observability. It also auto-registers `local-cluster` as a managed cluster (the hub acts as its own spoke).

Regardless of how you set up your cluster, verify ACM is ready before proceeding:

```bash
oc get mch multiclusterhub -n open-cluster-management -o jsonpath='{.status.phase}'
# Should output: Running

oc get managedcluster local-cluster
# Should show local-cluster as available
```

## 2. Install cert-manager

Required by the backend controller for trust distribution. The version
and install method match what the addon's own `hack/dev-env.sh` uses for
its Kind-based dev environment.

```bash
oc apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
oc rollout status deployment/cert-manager -n cert-manager --timeout=120s
oc rollout status deployment/cert-manager-webhook -n cert-manager --timeout=120s
```

## 3. Build and deploy the backend controller

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

# Deploy the controller using Helm with the internal registry image
helm upgrade --install ${BACKEND_IMAGE_NAME} chart/ \
  --create-namespace \
  --namespace ${BACKEND_NAMESPACE} \
  --set image.repository=${INTERNAL_REGISTRY}/${BACKEND_NAMESPACE}/${BACKEND_IMAGE_NAME} \
  --set image.tag=${BACKEND_IMAGE_TAG} \
  --wait --timeout 180s

# Verify the controller is running
oc rollout status deployment/multicluster-mesh-controller \
  -n ${BACKEND_NAMESPACE} --timeout=120s
```

## 4. Create a test mesh

Create a ManagedClusterSet, bind `local-cluster` to it, and create a
MultiClusterMesh CR:

```bash
# Create a ManagedClusterSet
oc apply -f - <<'EOF'
apiVersion: cluster.open-cluster-management.io/v1beta2
kind: ManagedClusterSet
metadata:
  name: mesh-cluster-set
EOF

# Bind local-cluster to the set
oc label managedcluster local-cluster \
  cluster.open-cluster-management.io/clusterset=mesh-cluster-set --overwrite

# Create a namespace for mesh resources
oc create namespace mesh-system

# Create a MultiClusterMesh
oc apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: my-mesh
  namespace: mesh-system
spec:
  clusterSet: mesh-cluster-set
EOF

# Verify the controller reconciled it
oc get multiclustermesh my-mesh -n mesh-system -o yaml
```

## 4a. (Optional) Enable trust distribution

To test certificate and trust distribution, create a cert-manager Issuer and configure the mesh to use it. This step is optional -- the mesh works without it, but the Trust Status card in the UI will show "not configured" until an issuer is set.

```bash
cd <multicluster-mesh-addon-repo>

# Create a cert-manager trust chain (ClusterIssuer -> root CA Certificate -> CA-backed Issuer)
oc apply -f samples/cert-manager-issuer.yaml

# Configure the mesh to use it
oc patch multiclustermesh my-mesh -n mesh-system --type=merge \
  --patch='{"spec":{"security":{"trust":{"certManager":{"issuerRef":{"name":"mesh-root-ca"}}}}}}'

# Verify the controller created a Certificate
oc get certificates -n mesh-system
```

After this, the controller will create a `cacerts-local-cluster` Certificate in `mesh-system` and cert-manager will mint a TLS secret. The controller will then attempt to distribute it via a ManifestWork to `local-cluster`. The distribution will show as failed until the `istio-system` namespace exists on the target cluster (this namespace is normally created when an Istio control plane is configured). To unblock it for testing, create the namespace manually:

```bash
oc create namespace istio-system
```

Once the namespace exists, the ManifestWork will succeed on its next reconciliation cycle. Verify the full trust chain:

```bash
# Certificate minted by cert-manager on the hub
oc get certificate cacerts-local-cluster -n mesh-system

# ManifestWork delivering the secret to the spoke
oc get manifestwork multicluster-mesh-cacerts -n local-cluster

# The distributed cacerts secret in istio-system (this is what Istio uses for mTLS)
oc get secret cacerts -n istio-system
```

## 4b. (Optional) Create additional test meshes

To test the list page with multiple meshes in different states, create additional `MultiClusterMesh` CRs. If you use the same cluster set as `my-mesh`, they will show various conflict and status conditions if you induce conflicts in their config.

```bash
# A second mesh without trust. Trust is intentionally omitted because on a
# single-node CRC, two trust-enabled meshes sharing the same cluster set will
# fight over the same cacerts ManifestWork, causing status to oscillate.
# To test conflicts, try changing controlPlane.namespace to "istio-system"
# (same as my-mesh's default) to trigger a NamespaceConflict, or add
# operator settings (e.g. spec.operator.channel: "preview") that differ from
# my-mesh to trigger an OperatorConfigConflict. Both show a blocked-mesh
# banner on the detail page and a red status on the list page.
oc apply -f - <<'EOF'
apiVersion: mesh.open-cluster-management.io/v1alpha1
kind: MultiClusterMesh
metadata:
  name: staging-mesh
  namespace: mesh-system
spec:
  clusterSet: mesh-cluster-set
  controlPlane:
    namespace: istio-staging
EOF

# Verify all meshes
oc get multiclustermesh -n mesh-system
```

To clean up the extra meshes later:

```bash
oc delete multiclustermesh staging-mesh dev-mesh -n mesh-system
```

## 5. Build and deploy the frontend ConsolePlugin

From the `frontend/` directory, build the container image and deploy:

```bash
cd <multicluster-mesh-addon-repo>/frontend
make build deploy
```

This builds a container image with the compiled plugin assets baked in (UBI9 nginx), pushes it to the OpenShift internal registry, deploys it, registers the ConsolePlugin, and restarts the console.

## 6. Verify

1. Open the CRC console: `oc whoami --show-console`
2. Log in as `kubeadmin`
3. Click the perspective switcher (top-left dropdown)
4. Select **Fleet Service Mesh**
5. The **Fleet Meshes** table should show `my-mesh` with its status
6. Click **Control Planes** in the left nav — it shows Istio CRs discovered across managed clusters

## 6a. (Optional) Create Istio CRs for the Control Planes page

The Control Planes page discovers sail-operator `Istio` CRs across managed clusters via ACM Search. If you already have OSSM installed on your clusters, those Istio CRs will appear automatically. If not, you can create test data manually.

```bash
# Install the OSSM 3.x operator (if not already installed)
oc apply -f - <<'EOF'
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: servicemeshoperator3
  namespace: openshift-operators
spec:
  channel: stable
  installPlanApproval: Automatic
  name: servicemeshoperator3
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF

# Wait for OLM to install the operator
# Poll until the subscription has a currentCSV, then wait for that CSV to succeed
until oc get subscription.operators.coreos.com servicemeshoperator3 \
  -n openshift-operators -o jsonpath='{.status.currentCSV}' 2>/dev/null | grep -q .; do
  echo "Waiting for subscription to resolve..."
  sleep 5
done
CSV=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
  -n openshift-operators -o jsonpath='{.status.currentCSV}')
echo "Waiting for ${CSV} to succeed..."
oc wait --for=jsonpath='{.status.phase}'=Succeeded \
  csv/${CSV} -n openshift-operators --timeout=180s

# Create an Istio CR with meshID set (multi-cluster scenario).
# Use a namespace that is NOT managed by a MultiClusterMesh CR (e.g. NOT
# istio-system if an MCM already targets that). Otherwise the Control Planes
# page will show this CR as "Managed by" the MCM even though it was created
# independently. In production this wouldn't happen, but in a single-cluster
# test environment the namespaces can easily collide.
oc apply -f - <<'EOF'
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  namespace: istio-system
  values:
    global:
      meshID: mesh1
      multiCluster:
        clusterName: local-cluster
      network: network1
EOF

# Verify the Istio CR was created
oc get istios
```

The Control Planes page polls ACM Search every 30 seconds. After the search collector indexes the new Istio CR (typically within 1-2 minutes), it will appear in the Control Planes table.

To clean up:

```bash
# Delete the Istio CR
oc delete istio default

# Remove the OSSM operator (CSV + subscription + CRDs)
CSV=$(oc get subscription.operators.coreos.com servicemeshoperator3 \
  -n openshift-operators -o jsonpath='{.status.currentCSV}')
oc delete subscription.operators.coreos.com servicemeshoperator3 -n openshift-operators
oc delete csv ${CSV} -n openshift-operators
oc get crd -o name | grep -E 'sailoperator\.io|istio\.io' | xargs oc delete
```

## Local frontend development (fast iteration)

For day-to-day UI work, run the plugin locally with webpack and a local OpenShift Console bridge.

**Prerequisites:** `oc login`, ACM and backend controller deployed on the cluster, Node.js 20+, `podman` or `docker`, and npm dependencies installed.

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

## Iterating on frontend changes (cluster deploy)

For production-like testing (nginx/TLS packaging, in-cluster ConsolePlugin), rebuild and redeploy to the cluster:

```bash
cd <multicluster-mesh-addon-repo>/frontend
make build deploy
```

## Iterating on backend changes

After modifying backend Go code:

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

# Remove test mesh resources
oc delete multiclustermesh my-mesh -n mesh-system --ignore-not-found
oc delete namespace mesh-system --ignore-not-found
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset- 2>/dev/null; true
oc delete managedclusterset mesh-cluster-set --ignore-not-found

# Remove the backend controller
make undeploy
```

