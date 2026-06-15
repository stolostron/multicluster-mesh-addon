# ossm-acm-ui

An OpenShift `ConsolePlugin` that registers a standalone **Fleet Service Mesh** perspective in the OpenShift Console perspective switcher, providing fleet-wide service mesh visibility across ACM-managed clusters.

## What it does

This plugin registers a new **console perspective** — a top-level entry in the perspective switcher dropdown (alongside "Administrator", "Developer", "Fleet Management", "Fleet Virtualization", etc.). Selecting it switches to a dedicated left-hand nav and landing page owned entirely by this plugin. No changes to ACM are required.

The perspective uses these OpenShift Console extension points:

- **`console.perspective`** — registers the "Fleet Service Mesh" perspective in the perspective switcher dropdown.
- **`console.navigation/section` + `console.navigation/href`** — adds a "Service Mesh" nav section with a "Meshes" link within the perspective.
- **`console.page/route`** — registers the landing page at `/service-mesh`.

Data fetching uses `useK8sWatchResource` from the Console SDK to watch `MultiClusterMesh` resources (`mesh.open-cluster-management.io/v1alpha1`) on the hub cluster, and `@stolostron/multicluster-sdk` hooks for querying OSSM resources across managed clusters.

## Project structure

```
ossm-acm-ui/
  console-extensions.ts        # Declares the plugin's extension points
  console-plugin-metadata.ts   # Plugin name, version, exposed modules
  webpack.config.ts            # Build config (ConsoleRemotePlugin)
  src/
    types/
      multiClusterMesh.ts         # TypeScript types for the MultiClusterMesh CRD
    hooks/
      useMultiClusterMeshes.ts    # Hook to watch MultiClusterMesh resources on the hub
    components/
      ServiceMeshPage.tsx         # Landing page for the perspective
      MeshStatus.tsx              # Mesh health/status display component
    perspective.ts               # landingPageURL + importRedirectURL
    perspectiveIcon.tsx           # Icon for the perspective switcher
  deploy/
    consoleplugin.yaml   # ConsolePlugin CR
    deployment.yaml      # Deployment + Service (with TLS annotation)
    nginx.conf           # nginx TLS config (port 9443)
```

## Prerequisites

- [CRC](https://crc.dev) running with ACM installed
- `oc` logged in as `kubeadmin`
- Node.js 20 (Node 22+ may fail due to stricter ESM module resolution in ts-node)

## Building

```bash
npm install
npm run build
```

Output goes to `dist/`.

## Deploying to CRC

### 1. Create the namespace

```bash
oc new-project ossm-acm-plugin
```

### 2. Push the built assets as a ConfigMap

```bash
oc create configmap ossm-acm-plugin-dist \
  --from-file=dist/ \
  -n ossm-acm-plugin
```

### 3. Push the nginx config

```bash
oc create configmap ossm-acm-plugin-nginx \
  --from-file=nginx.conf=deploy/nginx.conf \
  -n ossm-acm-plugin
```

### 4. Apply the Deployment and Service

```bash
oc apply -f deploy/deployment.yaml
```

The Service has the annotation `service.beta.openshift.io/serving-cert-secret-name: ossm-acm-plugin-tls`, which causes OpenShift to auto-provision a TLS cert as a Secret. The nginx container mounts this Secret and terminates TLS on port 9443 — **this is required** because the OpenShift Console operator only connects to plugin backends over HTTPS.

### 5. Register the ConsolePlugin

```bash
oc apply -f deploy/consoleplugin.yaml
```

### 6. Enable the plugin in the Console operator

```bash
oc patch console.operator.openshift.io cluster \
  --type=json \
  --patch='[{"op":"add","path":"/spec/plugins/-","value":"ossm-acm"}]'
```

### 7. Restart the console pod

The plugin manifest is fetched at console startup, so a restart is required after any deployment or plugin registration change:

```bash
oc rollout restart deployment/console -n openshift-console
oc rollout status deployment/console -n openshift-console --timeout=120s
```

### 8. Verify the plugin is loaded

Check the manifest is reachable from within the cluster:

```bash
oc run curl-test --image=curlimages/curl --rm -it --restart=Never \
  -n ossm-acm-plugin -- \
  curl -sk https://ossm-acm-plugin.ossm-acm-plugin.svc.cluster.local:9443/plugin-manifest.json
```

Check the console picked it up:

```bash
oc logs -n openshift-console -l app=console --tail=30 | grep -i "plugin"
```

## Iterating (rebuild + redeploy)

After changing source files:

```bash
npm run build
oc create configmap ossm-acm-plugin-dist \
  --from-file=dist/ \
  -n ossm-acm-plugin --dry-run=client -o yaml | oc apply -f -
oc rollout restart deployment/ossm-acm-plugin -n ossm-acm-plugin
oc rollout restart deployment/console -n openshift-console
oc rollout status deployment/console -n openshift-console --timeout=120s
```

## Navigating to the plugin in CRC

Once the console restarts, open the CRC console URL (accept the self-signed cert) and log in as `kubeadmin`. Open the perspective switcher (top-left dropdown) and select **Fleet Service Mesh**. This opens the dedicated landing page at `/service-mesh`.

## Architecture

See the [Implementation Plan](PLAN.md) for details on the perspective approach, ACM extension architecture, the `MultiClusterMesh` CRD, and data fetching strategies.
