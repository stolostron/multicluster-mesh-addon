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

## 1. Start CRC and install ACM

*NOTE: The [install-acm.sh](https://github.com/kiali/kiali/blob/master/hack/install-acm.sh) script in the Kiali repo automates a full CRC/OpenShift + ACM setup. Note that its* `init-openshift` *command depends on other scripts in the same repo, so you need the [kiali server repo](https://github.com/kiali/kiali) cloned locally. All commands below are run from that repo's directory.*

Start CRC with at least 12 CPUs and 100 GB disk, enable cluster monitoring, User Workload Monitoring, and expose the image registry. Using the Kiali helper script:

```bash
./hack/install-acm.sh --crc-pull-secret-file <path-to-your-pull-secret-file> init-openshift
```

Then install ACM (operator, MultiClusterHub, observability). Using the Kiali helper script:

```bash
./hack/install-acm.sh install-acm
```

This installs the ACM operator, creates a MultiClusterHub, sets up MinIO for metrics storage, and enables observability. It also auto-registers `local-cluster` as a managed cluster (the hub acts as its own spoke).

This step takes 15-20 minutes. Verify when done:

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

## 5. Build and deploy the frontend ConsolePlugin

*NOTE: These commands can also be run via `make dev-build dev-deploy` from the `frontend/` directory.*

```bash
cd <multicluster-mesh-addon-repo>/frontend

# Build
npm install
npm run build

# Create the plugin namespace
oc create namespace ossm-acm-plugin --dry-run=client -o yaml | oc apply -f -

# Push built assets as a ConfigMap (delete first for idempotency — oc apply
# hits the 262KB last-applied-configuration annotation limit with large dist/)
oc delete configmap ossm-acm-plugin-dist -n ossm-acm-plugin --ignore-not-found
oc create configmap ossm-acm-plugin-dist \
  --from-file=dist/ \
  -n ossm-acm-plugin

# Push the nginx config
oc delete configmap ossm-acm-plugin-nginx -n ossm-acm-plugin --ignore-not-found
oc create configmap ossm-acm-plugin-nginx \
  --from-file=nginx.conf=deploy/nginx.conf \
  -n ossm-acm-plugin

# Deploy nginx (serves plugin assets over TLS)
oc apply -f deploy/dev-deployment.yaml
oc rollout status deployment/ossm-acm-plugin -n ossm-acm-plugin --timeout=120s

# Register the ConsolePlugin
oc apply -f deploy/consoleplugin.yaml

# Enable the plugin (appends to existing plugins list)
oc patch console.operator.openshift.io cluster \
  --type=json \
  --patch='[{"op":"add","path":"/spec/plugins/-","value":"ossm-acm"}]'

# Restart the console to pick up the new plugin
oc rollout restart deployment/console -n openshift-console
oc rollout status deployment/console -n openshift-console --timeout=120s
```

## 6. Verify

1. Open the CRC console: `oc whoami --show-console`
2. Log in as `kubeadmin`
3. Click the perspective switcher (top-left dropdown)
4. Select **Fleet Service Mesh**
5. The Meshes table should show `my-mesh` with its status

## Iterating on frontend changes

*NOTE: These commands can also be run via `make dev-build dev-deploy` from the `frontend/` directory.*

After modifying frontend source files:

```bash
cd <multicluster-mesh-addon-repo>/frontend

npm run build

oc delete configmap ossm-acm-plugin-dist -n ossm-acm-plugin --ignore-not-found
oc create configmap ossm-acm-plugin-dist \
  --from-file=dist/ \
  -n ossm-acm-plugin

oc rollout restart deployment/ossm-acm-plugin -n ossm-acm-plugin
oc rollout status deployment/ossm-acm-plugin -n ossm-acm-plugin --timeout=120s
oc rollout restart deployment/console -n openshift-console
oc rollout status deployment/console -n openshift-console --timeout=120s
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

*NOTE: The frontend plugin teardown commands can also be run via `make dev-teardown` from the `frontend/` directory.*

```bash
cd <multicluster-mesh-addon-repo>

# Remove the frontend plugin from the console operator plugins list
oc get console.operator.openshift.io cluster -o json | \
  jq '.spec.plugins |= (. // [] | map(select(. != "ossm-acm")))' | \
  oc apply -f -
oc delete -f frontend/deploy/consoleplugin.yaml --ignore-not-found
oc delete -f frontend/deploy/dev-deployment.yaml --ignore-not-found
oc delete namespace ossm-acm-plugin --ignore-not-found

# Remove test mesh resources
oc delete multiclustermesh my-mesh -n mesh-system --ignore-not-found
oc delete namespace mesh-system --ignore-not-found
oc label managedcluster local-cluster cluster.open-cluster-management.io/clusterset- 2>/dev/null; true
oc delete managedclusterset mesh-cluster-set --ignore-not-found

# Remove the backend controller
make undeploy
```

