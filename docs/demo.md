# Build and expand a multicluster mesh with ACM and OSSM

Connect two OpenShift clusters in a service mesh, send requests between them, and then add a third cluster without changing how the client calls the service.
Bookinfo's reviews service makes the result visible: each response identifies the cluster that served it.

Red Hat Advanced Cluster Management (ACM) provides the hub and manages the clusters.
The mesh add-on distributes the shared trust and endpoint-discovery resources that connect their Istio control planes.
You'll install Istio and the application on each cluster.
On OpenShift, you'll also explore the mesh in the OpenShift Service Mesh (OSSM) Console.

You can [run an interactive demo](#run-the-interactive-demo) or [follow the OpenShift walkthrough](#openshift-walkthrough) at your own pace.
A local kind demo is also available if you don't have an OpenShift environment.

> **Note:** The walkthrough uses `oc` for OpenShift. Use `kubectl` for these commands if you're using a local kind environment.

## Before you begin

Choose an OpenShift environment or a local kind environment, then prepare the tools for the way you want to run the demo.
Run all commands from the root of a local checkout of this repository.

### Tools to install

| How you run the demo | Tools you need on your workstation |
| --- | --- |
| Interactive demo on OpenShift | Bash 4 or newer, Go, `make`, `oc`, `envsubst`, `yq`, and `bat` |
| Interactive demo on kind | Bash 4 or newer, Go, `make`, `kubectl`, `envsubst`, `yq`, `bat`, and Docker or Podman |
| OpenShift walkthrough by hand | Bash 4 or newer, `oc`, Helm, `istioctl`, `envsubst`, and `yq` |

For either interactive demo, use the Go version specified in [go.mod](../go.mod).

### Using an existing OpenShift environment

Use an ACM hub with three registered OpenShift managed clusters, all available, and administrator access to all four clusters.
The managed clusters need LoadBalancer support for the gateways that carry traffic between them; on bare-metal clusters, you can use MetalLB.

For the interactive demo, save a separate kubeconfig file for the hub and each managed cluster.
For the manual walkthrough, configure a kubeconfig context for each cluster.
You'll install cert-manager if needed, the mesh add-on, and the OSSM operator during the demo.

### Using a local kind environment

Start your container engine and make sure your workstation has enough CPU and memory to run four local Kubernetes clusters.
Then prepare the environment:

~~~bash
make demo-dev-env
~~~

This creates an OCM hub and three managed clusters with the platform components needed for the Sail demo.
Wait for setup to finish before starting the demo.

## Run the interactive demo

Choose the command for your environment.
The demo pauses after applying manifests so you can inspect them and their results before continuing.

### On OpenShift

Provide the hub kubeconfig first, followed by the three spoke kubeconfigs in the same order as the cluster names:

~~~bash
make demo \
  SPOKE_CLUSTERS="cluster1 cluster2 cluster3" \
  DEMO_KUBECONFIG="/path/hub.config:/path/cluster1.config:/path/cluster2.config:/path/cluster3.config"
~~~

The OpenShift demo finishes by opening the OSSM Console on the hub.

### Locally with kind

The local kind demo uses the upstream Sail operator instead of the OSSM operator used on OpenShift.
With the local environment prepared, start the demo:

~~~bash
make demo-sail
~~~

The Sail demo shows the same traffic and scale-out scenario without the OSSM Console, which requires OpenShift.
When finished, remove the local environment with `make demo-dev-clean`.

## OpenShift walkthrough

Follow these steps using your [OpenShift environment](#using-an-existing-openshift-environment) and the tools listed for the manual walkthrough.

### Set your cluster names

Set the kubeconfig contexts and managed-cluster names for your environment.
The hub context connects to ACM; the spoke contexts connect to the clusters that will join the mesh.
The `CLUSTER_A`, `CLUSTER_B`, and `CLUSTER_C` values are the managed-cluster names shown in ACM.
Keep `mesh-system` and `openshift-mesh` as shown, because the sample manifests use those names.

~~~bash
export HUB_CONTEXT=acm-hub
export SPOKE_A_CONTEXT=cluster1
export SPOKE_B_CONTEXT=cluster2
export SPOKE_C_CONTEXT=cluster3

export CLUSTER_A=cluster1
export CLUSTER_B=cluster2
export CLUSTER_C=cluster3

export MESH_NAMESPACE=mesh-system
export MESH_NAME=openshift-mesh
~~~

### 1. Prepare the hub and gateways

Begin with the three managed clusters registered with your hub.
List them to see the starting topology:

~~~bash
oc --context "$HUB_CONTEXT" get managedclusters
~~~

The add-on will use ACM to distribute discovery resources to the mesh members.
Enable the hub feature it needs for this:

~~~bash
oc --context "$HUB_CONTEXT" patch clustermanager cluster-manager \
  --type=merge \
  -p '{"spec":{"workConfiguration":{"featureGates":[{"feature":"ManifestWorkReplicaSet","mode":"Enable"}]}}}'
~~~

Next, prepare the spokes for the gateways that will carry traffic between their networks.
These gateways use Gateway API resources.
If your spokes do not already provide Gateway API, install its CRDs:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  oc --context "$CONTEXT" apply --server-side \
    -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
done
~~~

### 2. Establish shared trust

The workloads on different clusters need to trust one another.
Create a shared root certificate on the hub; the add-on will use it to distribute an intermediate CA certificate to each mesh member.

If cert-manager is already installed on the hub, continue with the trust chain below.
Otherwise, install the Red Hat cert-manager Operator from the `redhat-operators` catalog on the `stable-v1` channel:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -f hack/demo/cert-manager-operator.yaml

oc --context "$HUB_CONTEXT" wait \
  --for=condition=Available \
  deployment/cert-manager \
  deployment/cert-manager-cainjector \
  deployment/cert-manager-webhook \
  -n cert-manager \
  --timeout=5m
~~~

Create the namespace for the mesh and a self-signed trust chain using the [cert-manager sample](../samples/cert-manager-issuer.yaml):

~~~bash
oc --context "$HUB_CONTEXT" apply -f hack/demo/mesh-namespace.yaml
oc --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f samples/cert-manager-issuer.yaml

oc --context "$HUB_CONTEXT" wait \
  --for=condition=Ready \
  issuer/mesh-selfsigned-issuer \
  certificate/mesh-root-ca \
  issuer/mesh-root-ca \
  -n "$MESH_NAMESPACE" \
  --timeout=5m
~~~

> **Note:** The self-signed root keeps this demo self-contained.
> For production, use the issuer and trust policy required by your organization.

### 3. Install the add-on on the ACM hub

With the trust root in place, install the mesh add-on on the hub using Helm:

~~~bash
helm repo add --force-update multicluster-mesh-addon \
  https://stolostron.github.io/multicluster-mesh-addon
helm repo update
helm upgrade --install multicluster-mesh-addon \
  multicluster-mesh-addon/multicluster-mesh-addon \
  --kube-context "$HUB_CONTEXT" \
  --namespace multicluster-mesh-system \
  --create-namespace \
  --wait \
  --timeout 5m
~~~

### 4. Select the initial mesh members

Use cluster1 and cluster2 for the initial mesh, leaving cluster3 for the scale-out step.
An ACM Placement selects the members; the operator installation policy will use the same selection.

Label the first two clusters for the demo and give each an Istio network identity.
Leave cluster3 without the `mesh-demo=enabled` label for now.

> **Note:** If the clusters already have network labels, save those values so you can restore them during cleanup.

~~~bash
oc --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_A" \
  mesh-demo=enabled topology.istio.io/network=network-a --overwrite
oc --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_B" \
  mesh-demo=enabled topology.istio.io/network=network-b --overwrite
~~~

Create `mesh-placement` in the mesh namespace.
The sample binds the `default` ClusterSet to that namespace and selects clusters labeled `mesh-demo=enabled`:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f hack/demo/mesh-placement.yaml
~~~

### 5. Install the mesh operator

Each mesh member needs the OSSM operator to run Istio.
Apply an OperatorPolicy on the hub to install it on cluster1 and cluster2.
The policy uses `mesh-placement`, so it will also install the operator when you select cluster3 later.

Wait for the policy to report `Compliant` for both initial clusters before creating the mesh:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f samples/operator-policy-ossm.yaml
for CLUSTER in "$CLUSTER_A" "$CLUSTER_B"; do
  oc --context "$HUB_CONTEXT" wait \
    --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER}\")].compliant}=Compliant" \
    policy/policy-ossm-operator -n "$MESH_NAMESPACE" --timeout=15m
done
oc --context "$HUB_CONTEXT" get policy/policy-ossm-operator -n "$MESH_NAMESPACE"
~~~

### 6. Create the mesh

Create a MultiClusterMesh on the hub to begin preparing the two clusters.
The [OpenShift sample](../samples/openshift.yaml) references the Placement and certificate issuer you created earlier:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f samples/openshift.yaml

oc --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  -o yaml

oc --context "$HUB_CONTEXT" wait \
  --for=condition=Ready=True \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  --timeout=5m

oc --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
~~~

When the mesh reports `Ready=True`, shared trust is in place for the initial members.
Next, you'll start Istio and verify that the control planes can discover one another.
See [readiness limits](api-reference.md#readiness-limits) for what this condition covers.

The add-on creates a control plane namespace on each member cluster.
Read its name from the mesh status and save it as `CP_NAMESPACE` for the Istio configuration:

~~~bash
export CP_NAMESPACE="$(oc --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  -o jsonpath='{.status.controlPlaneNamespace}')"
echo "$CP_NAMESPACE"
~~~

### 7. Start Istio on the first two clusters

The clusters now have the trust and discovery resources they need.
Use the samples to deploy [Istio CNI](../samples/istio/istiocni.yaml), an [Istio control plane](../samples/istio/istio.yaml), and an [east-west gateway](../samples/istio/eastwest-gateway.yaml) on each cluster.
Both control planes use `openshift-mesh` as their mesh name and trust domain, and the namespace saved in `CP_NAMESPACE`.
Give each control plane its own cluster name and network identity as shown below.

#### 7.1. Start Istio on cluster1

Configure the first control plane for cluster1 on network-a:

~~~bash
export CLUSTER_NAME="$CLUSTER_A"
export NETWORK=network-a
oc --context "$SPOKE_A_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
oc --context "$SPOKE_A_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  oc --context "$SPOKE_A_CONTEXT" apply -f -
envsubst < samples/istio/eastwest-gateway.yaml | \
  oc --context "$SPOKE_A_CONTEXT" apply -f -
~~~

#### 7.2. Start Istio on cluster2

Repeat the setup on cluster2, using its own cluster name and network-b:

~~~bash
export CLUSTER_NAME="$CLUSTER_B"
export NETWORK=network-b
oc --context "$SPOKE_B_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
oc --context "$SPOKE_B_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  oc --context "$SPOKE_B_CONTEXT" apply -f -
envsubst < samples/istio/eastwest-gateway.yaml | \
  oc --context "$SPOKE_B_CONTEXT" apply -f -
~~~

#### 7.3. Verify mesh connectivity

Wait for both control planes to become available and for their east-west gateways to receive LoadBalancer addresses:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT"; do
  oc --context "$CONTEXT" wait --for=condition=Available \
    deployment/istiod -n "$CP_NAMESPACE" --timeout=5m
  oc --context "$CONTEXT" wait \
    --for=jsonpath='{.status.loadBalancer.ingress}' \
    service/eastwestgateway-istio -n "$CP_NAMESPACE" --timeout=5m
done
~~~

With the control planes running, use `istioctl` to see whether each cluster has connected to its peer:

~~~bash
istioctl remote-clusters --context "$SPOKE_A_CONTEXT" -i "$CP_NAMESPACE"
istioctl remote-clusters --context "$SPOKE_B_CONTEXT" -i "$CP_NAMESPACE"
~~~

Each cluster should list the other as a connected peer before you deploy the application.

### 8. Send application traffic across the mesh

Use Bookinfo's reviews service to see requests cross the mesh.
Deploy reviews v1 on cluster1 and reviews v2 on cluster2, using the same Service name on both clusters.
A client can then call one address and receive responses from either cluster.

#### 8.1. Deploy reviews v1 and ratings on cluster1

The examples use Istio 1.30.4 and fetch Bookinfo from the matching release branch.
Set `ISTIO_VERSION` to the version supported by your OSSM installation, and use a matching `istioctl` version.
Start with reviews v1 and the ratings service on cluster1.
The other reviews versions will call this ratings service to retrieve star ratings.
Create the reviews and ratings Services and ServiceAccounts on each cluster so the calls can cross the mesh.

~~~bash
export ISTIO_VERSION=1.30.4
export BOOKINFO_REF="release-${ISTIO_VERSION%.*}"
export BOOKINFO_MANIFEST="https://raw.githubusercontent.com/openshift-service-mesh/istio/$BOOKINFO_REF/samples/bookinfo/platform/kube/bookinfo.yaml"

oc --context "$SPOKE_A_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml

oc --context "$SPOKE_A_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'service in (reviews,ratings)'
oc --context "$SPOKE_A_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'account in (reviews,ratings)'
oc --context "$SPOKE_A_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'app in (ratings,reviews),version=v1'

oc --context "$SPOKE_A_CONTEXT" set env \
  deployment/reviews-v1 CLUSTER_NAME="$CLUSTER_A" -n bookinfo
oc --context "$SPOKE_A_CONTEXT" wait \
  --for=condition=Available \
  deployment/reviews-v1 deployment/ratings-v1 \
  -n bookinfo --timeout=5m
~~~

#### 8.2. Deploy reviews v2 on cluster2

Deploy reviews v2 on cluster2, using the same Service names as cluster1.
Set `CLUSTER_NAME` so its responses identify which cluster served the request:

~~~bash
oc --context "$SPOKE_B_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml
oc --context "$SPOKE_B_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'service in (reviews,ratings)'
oc --context "$SPOKE_B_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'account in (reviews,ratings)'
oc --context "$SPOKE_B_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'app=reviews,version=v2'
oc --context "$SPOKE_B_CONTEXT" set env \
  deployment/reviews-v2 CLUSTER_NAME="$CLUSTER_B" -n bookinfo
oc --context "$SPOKE_B_CONTEXT" wait \
  --for=condition=Available \
  deployment/reviews-v2 \
  -n bookinfo --timeout=5m
~~~

#### 8.3. See which clusters answer

Deploy a [curl client](../test/e2e/testdata/curl.yaml) in the Bookinfo namespace on cluster2.
Its requests will go through the mesh, just like requests from an application:

~~~bash
oc --context "$SPOKE_B_CONTEXT" apply \
  -n bookinfo \
  -f test/e2e/testdata/curl.yaml
oc --context "$SPOKE_B_CONTEXT" wait \
  --for=condition=Available deployment/curl \
  -n bookinfo --timeout=5m
~~~

Call the reviews service a few times from that client on cluster2:

~~~bash
declare -A counts=()
for i in {1..8}; do
  response="$(oc --context "$SPOKE_B_CONTEXT" exec deployment/curl \
    -n bookinfo -c curl -- curl -sS http://reviews:9080/reviews/0 |
    yq -r '.podname + " " + .clustername')"
  echo "$response"
  cluster=${response##* }
  counts[$cluster]=$((${counts[$cluster]:-0} + 1))
done
printf '\nResponses by cluster:\n'
for cluster in "${!counts[@]}"; do
  printf '  %s  %s\n' "$cluster" "${counts[$cluster]}"
done | sort
~~~

Look for reviews-v1 responses from cluster1 and reviews-v2 responses from cluster2.
Each line identifies the serving pod and cluster; the summary counts the responses from each cluster.
The counts can vary between runs.

### 9. Add a third cluster to the mesh

Now expand the mesh while keeping the client and Service address unchanged.
Select cluster3, let the policy and add-on prepare it, then deploy Istio and reviews v3 there.

#### 9.1. Select cluster3

Add the selection label and network identity to cluster3:

~~~bash
oc --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_C" \
  mesh-demo=enabled topology.istio.io/network=network-c --overwrite
~~~

The Placement now selects cluster3 for both the mesh and the operator installation policy.
View the mesh status as the new member joins:

~~~bash
oc --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  -o yaml
~~~

Wait for cluster3 to report `TrustDistributed=True` before starting Istio there:

~~~bash
oc --context "$HUB_CONTEXT" wait \
  --for="jsonpath={.status.clusterStatus[?(@.clusterName==\"${CLUSTER_C}\")].conditions[?(@.type==\"TrustDistributed\")].status}=True" \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  --timeout=5m
~~~

Then wait for the installation policy to report `Compliant` for cluster3:

~~~bash
oc --context "$HUB_CONTEXT" wait \
  --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_C}\")].compliant}=Compliant" \
  policy/policy-ossm-operator \
  -n "$MESH_NAMESPACE" \
  --timeout=15m
oc --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
~~~

Cluster3 now has the operator and shared trust it needs.
Next, start its Istio control plane.

#### 9.2. Start Istio on cluster3

Use the same Istio setup as the first two clusters:

~~~bash
export CLUSTER_NAME="$CLUSTER_C"
export NETWORK=network-c
oc --context "$SPOKE_C_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
oc --context "$SPOKE_C_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  oc --context "$SPOKE_C_CONTEXT" apply -f -
envsubst < samples/istio/eastwest-gateway.yaml | \
  oc --context "$SPOKE_C_CONTEXT" apply -f -
oc --context "$SPOKE_C_CONTEXT" wait \
  --for=condition=Available deployment/istiod \
  -n "$CP_NAMESPACE" --timeout=5m
oc --context "$SPOKE_C_CONTEXT" wait \
  --for=jsonpath='{.status.loadBalancer.ingress}' \
  service/eastwestgateway-istio \
  -n "$CP_NAMESPACE" --timeout=5m
~~~

Check that cluster3 can see the rest of the mesh:

~~~bash
istioctl remote-clusters --context "$SPOKE_C_CONTEXT" -i "$CP_NAMESPACE"
~~~

#### 9.3. Deploy reviews v3 and see all three clusters answer

Deploy reviews v3 on cluster3 with the same Service names.
All three versions will now be reachable through the existing reviews address:

~~~bash
oc --context "$SPOKE_C_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml
oc --context "$SPOKE_C_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'service in (reviews,ratings)'
oc --context "$SPOKE_C_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'account in (reviews,ratings)'
oc --context "$SPOKE_C_CONTEXT" apply \
  -n bookinfo \
  -f "$BOOKINFO_MANIFEST" \
  -l 'app=reviews,version=v3'
oc --context "$SPOKE_C_CONTEXT" set env \
  deployment/reviews-v3 CLUSTER_NAME="$CLUSTER_C" -n bookinfo
oc --context "$SPOKE_C_CONTEXT" wait \
  --for=condition=Available deployment/reviews-v3 \
  -n bookinfo --timeout=5m
~~~

Call the reviews service a few more times from the client on cluster2:

~~~bash
declare -A counts=()
for i in {1..12}; do
  response="$(oc --context "$SPOKE_B_CONTEXT" exec deployment/curl \
    -n bookinfo -c curl -- curl -sS http://reviews:9080/reviews/0 |
    yq -r '.podname + " " + .clustername')"
  echo "$response"
  cluster=${response##* }
  counts[$cluster]=$((${counts[$cluster]:-0} + 1))
done
printf '\nResponses by cluster:\n'
for cluster in "${!counts[@]}"; do
  printf '  %s  %s\n' "$cluster" "${counts[$cluster]}"
done | sort
~~~

Look for reviews-v3 responses from cluster3 alongside the responses from cluster1 and cluster2.
The new responses show that cluster3 is serving requests through the same reviews address, with no changes to the client.

### 10. Explore the mesh in the console

Finish by exploring the three-cluster mesh in the OSSM Console.
Install the Kiali Operator on the hub, then use it to create the console plugin:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -f hack/demo/kiali-ossm-subscription.yaml

oc --context "$HUB_CONTEXT" wait \
  --for=condition=Available \
  deployment/kiali-operator \
  -n openshift-operators \
  --timeout=10m
~~~

Create the OSSMConsole resource with Fleet Service Mesh enabled:

~~~bash
oc --context "$HUB_CONTEXT" apply \
  -f hack/demo/ossm-console.yaml

oc --context "$HUB_CONTEXT" get ossmconsole -A
oc --context "$HUB_CONTEXT" get consoleplugin ossmconsole
~~~

Print the hub console URL and open it in your browser:

~~~bash
oc --context "$HUB_CONTEXT" get route console \
  -n openshift-console \
  -o jsonpath='https://{.spec.host}{"\n"}'
~~~

In the OpenShift Console, navigate to the Fleet Service Mesh perspective:

1. Click the perspective switcher (top-left) and select **Fleet Service Mesh**.
2. Open the **Meshes** page and select your mesh to explore its members and control plane details.

> **Note:** Fleet Service Mesh is a developer preview, so the available views depend on the installed plugin version and its support for the MultiClusterMesh API.

Use the OperatorPolicy to inspect operator installation status.

### Clean up

When you're finished, remove the Istio resources first so the operator stops recreating the control planes:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  oc --context "$CONTEXT" delete istio.sailoperator.io default
done
~~~

Delete the mesh to remove the control plane namespaces and the trust and discovery resources on its members.
Cleanup on the spokes runs asynchronously, so allow time for those resources to disappear.
Then delete the demo namespace on the hub, which also removes its policy and certificate resources:

~~~bash
oc --context "$HUB_CONTEXT" delete \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
oc --context "$HUB_CONTEXT" delete namespace "$MESH_NAMESPACE"
~~~

Remove the Bookinfo namespace from each spoke:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  oc --context "$CONTEXT" delete namespace bookinfo
done
~~~

Remove the demo selection labels and restore the previous network labels.

> **Note:** The example below assumes the clusters had no network labels before the demo.
> If the clusters had network labels before the demo, restore those values instead of removing the labels.

~~~bash
for CLUSTER in "$CLUSTER_A" "$CLUSTER_B" "$CLUSTER_C"; do
  oc --context "$HUB_CONTEXT" label managedcluster "$CLUSTER" mesh-demo- topology.istio.io/network-
done
~~~

> **Note:** The add-on, mesh operator, and shared IstioCNI resources remain installed.
> Keep them if you plan to run another demo or use them for other meshes.

## References

- [User Guide](user-guide.md)
- [OpenShift cert-manager Operator installation](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/security_and_compliance/cert-manager-operator-for-red-hat-openshift)
- [Fleet Service Mesh dev preview guide](https://github.com/kiali/openshift-servicemesh-plugin/blob/main/docs/fleet-mesh/DEV-PREVIEW-GUIDE.md)
- [Bookinfo application](https://istio.io/latest/docs/examples/bookinfo/)
