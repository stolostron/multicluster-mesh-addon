# OpenShift ACM and OSSM demo

This walkthrough tells a simple Bookinfo story across three OpenShift clusters.
ACM provides the hub, the mesh add-on prepares the selected clusters, and you create the Istio and application resources just as a customer would.
We start with two clusters, show reviews traffic crossing them, and then add a third cluster to show scale-out.

The commands are deliberately explicit and the demo pauses between sections.
Follow this guide by hand. The interactive script covers the same scenario, but its ordering and readiness checks can differ; the commands here check prerequisites before starting the addon and wait for individual new members during scale-out.
ManifestWorks and other implementation details are used behind the scenes but are not the focus here.

The final section opens the Fleet Service Mesh console plugin so you can continue exploring the same MultiClusterMesh from the OpenShift Console.

This is a demo flow, not a production installation.
Use the certificate issuer, OSSM version, network configuration, and application deployment process required by your organization in production.

## Before you start

The following must already exist:

- An OpenShift cluster that is an ACM hub.
- Three ACM managed clusters that are joined and available.
- The ACM managed-serviceaccount add-on installed and available.
- OLM on all three managed clusters.
- Permission to enable the ManifestWorkReplicaSet feature gate on the ACM hub if it is not already enabled.
- kubectl, helm, clusteradm, istioctl, envsubst, yq, and bat on the machine running the commands.
- A kubeconfig context for the hub and each spoke.
- Permission to install cluster-scoped operators and the add-on.

The third cluster is registered and available, but it is intentionally not in the mesh ClusterSet at the beginning.
Do not install the OSSM operator subscription by hand on the spokes.
A later step installs it with OperatorPolicy before the MultiClusterMesh is created.

The commands below use kubectl.
On OpenShift, oc can be substituted for kubectl.

## Set the demo names

Replace the contexts and cluster names with the values from your ACM inventory.
Keep the mesh namespace, mesh name, and ClusterSet names shown below unless you also edit the sample manifests.

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
export CLUSTER_SET=mesh-cluster-set
export OPERATOR_POLICY_NAMESPACE=mesh-operator-policy
~~~

Confirm that the contexts point to the intended clusters before continuing:

~~~bash
kubectl --context "$HUB_CONTEXT" cluster-info
kubectl --context "$SPOKE_A_CONTEXT" cluster-info
kubectl --context "$SPOKE_B_CONTEXT" cluster-info
kubectl --context "$SPOKE_C_CONTEXT" cluster-info
~~~

## 1. Prepare ACM prerequisites

ACM is already installed for this demo.
This step checks the ACM inventory, makes sure ManifestWorkReplicaSet is enabled, and prepares Gateway API for the east-west gateways.

~~~bash
kubectl --context "$HUB_CONTEXT" get managedclusters
kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=ManagedClusterConditionAvailable=True \
  "managedcluster/$CLUSTER_A" \
  "managedcluster/$CLUSTER_B" \
  "managedcluster/$CLUSTER_C" \
  --timeout=5m
~~~

ManifestWorkReplicaSet lets the add-on distribute resources through ACM.
Enable it on the hub:

~~~bash
kubectl --context "$HUB_CONTEXT" patch clustermanager cluster-manager \
  --type=merge \
  -p '{"spec":{"workConfiguration":{"featureGates":[{"feature":"ManifestWorkReplicaSet","mode":"Enable"}]}}}'
~~~

Gateway API provides the resource used by the east-west gateway.
OpenShift may already provide these CRDs; the local Sail demo installs them here when they are missing.
For production, use the Gateway API version approved for your OpenShift and OSSM releases.

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  kubectl --context "$CONTEXT" get crd \
    gateways.gateway.networking.k8s.io \
    gatewayclasses.gateway.networking.k8s.io \
    --ignore-not-found -o name
done

for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  if ! kubectl --context "$CONTEXT" get crd \
    gateways.gateway.networking.k8s.io \
    gatewayclasses.gateway.networking.k8s.io >/dev/null 2>&1; then
    kubectl --context "$CONTEXT" apply --server-side \
      -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
  fi
done
~~~

The ACM managed-serviceaccount add-on and OLM are prerequisites for this walkthrough.
They are used behind the scenes by the mesh add-on and should already be healthy.

~~~bash
for CLUSTER in "$CLUSTER_A" "$CLUSTER_B" "$CLUSTER_C"; do
  kubectl --context "$HUB_CONTEXT" wait --for=condition=Available \
    managedclusteraddon/managed-serviceaccount -n "$CLUSTER" --timeout=10m
done
~~~

## 2. Prepare cert-manager and the demo trust root

If cert-manager is not already installed on the hub, install the Red Hat cert-manager Operator with OLM.
The stable-v1 channel and redhat-operators catalog are the values used here for the demo.

~~~bash
kubectl --context "$HUB_CONTEXT" apply \
  -f hack/demo/cert-manager-operator.yaml

kubectl --context "$HUB_CONTEXT" wait \
  --for=jsonpath='{.status.phase}'=Succeeded \
  --selector=operators.coreos.com/openshift-cert-manager-operator.cert-manager-operator \
  csv \
  -n cert-manager-operator \
  --timeout=10m

kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=Available \
  deployment/cert-manager \
  deployment/cert-manager-cainjector \
  deployment/cert-manager-webhook \
  -n cert-manager \
  --timeout=5m
~~~

If cert-manager was already installed, skip the installation manifest and the CSV wait.
Run the deployment wait commands to verify the existing installation.
Do not install the Red Hat and community cert-manager Operators together.

Create the namespace and a simple self-signed trust chain using the [cert-manager sample](../samples/cert-manager-issuer.yaml):

~~~bash
kubectl --context "$HUB_CONTEXT" apply -f hack/demo/mesh-namespace.yaml
kubectl --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f samples/cert-manager-issuer.yaml

kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=Ready \
  issuer/mesh-selfsigned-issuer \
  certificate/mesh-root-ca \
  issuer/mesh-root-ca \
  -n "$MESH_NAMESPACE" \
  --timeout=5m
~~~

This root is self-signed only to keep the demo self-contained.
Customers should provide the issuer and trust policy required by their environment.

## 3. Install the add-on on the ACM hub

The controller checks for cert-manager and ManagedServiceAccount CRDs at startup. Complete the preceding steps before installing it; otherwise the Helm readiness wait cannot succeed.

~~~bash
kubectl --context "$HUB_CONTEXT" get crd \
  certificates.cert-manager.io \
  managedserviceaccounts.authentication.open-cluster-management.io \
  manifestworkreplicasets.work.open-cluster-management.io
~~~

The add-on is not installed from an ACM or OLM catalog in this demo.
Install it explicitly from its Helm repository.

~~~bash
helm repo add multicluster-mesh-addon \
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

Confirm that the controller is ready:

~~~bash
kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=Available \
  deployment/multicluster-mesh-controller \
  -n multicluster-mesh-system \
  --timeout=5m
~~~

## 4. Prepare the exclusive ClusterSet

Create the ClusterSet and add the first two spokes.
The default clusteradm create clusterset behavior creates an exclusive ClusterSet.

~~~bash
clusteradm --context "$HUB_CONTEXT" create clusterset "$CLUSTER_SET"
clusteradm --context "$HUB_CONTEXT" clusterset set "$CLUSTER_SET" \
  --clusters "$CLUSTER_A,$CLUSTER_B"
~~~

Give the first two clusters stable network identities for the Istio configuration that follows:

~~~bash
kubectl --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_A" \
  topology.istio.io/network=network-a --overwrite
kubectl --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_B" \
  topology.istio.io/network=network-b --overwrite
~~~

Bind the ClusterSet to the mesh namespace and create the Placement that the mesh sample references:

~~~bash
kubectl --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f hack/demo/mesh-placement.yaml
~~~

## 5. Install the mesh operator

The OSSM operator is a prerequisite. The add-on does not install it.
OperatorPolicy installs it on the clusters selected by `mesh-operator-placement`.

Install the governance-policy-framework hub add-on when its CRDs are not already present:

~~~bash
clusteradm --context "$HUB_CONTEXT" install hub-addon --names governance-policy-framework
~~~

Enable the policy add-ons on the first two spokes and turn on OperatorPolicy:

~~~bash
clusteradm --context "$HUB_CONTEXT" addon enable \
  --names governance-policy-framework \
  --namespace open-cluster-management-agent-addon \
  --clusters "$CLUSTER_A,$CLUSTER_B"
clusteradm --context "$HUB_CONTEXT" addon enable \
  --names config-policy-controller \
  --namespace open-cluster-management-agent-addon \
  --clusters "$CLUSTER_A,$CLUSTER_B"

for CLUSTER in "$CLUSTER_A" "$CLUSTER_B"; do
  kubectl --context "$HUB_CONTEXT" wait --for=condition=Available \
    managedclusteraddon/governance-policy-framework \
    managedclusteraddon/config-policy-controller \
    -n "$CLUSTER" --timeout=10m
  kubectl --context "$HUB_CONTEXT" annotate managedclusteraddon/config-policy-controller \
    -n "$CLUSTER" operator-policy-disabled=false --overwrite
done
~~~

Create the namespace and Placement used by the OperatorPolicy sample, then apply the OSSM policy.
Wait until the policy is Compliant before creating the mesh:

~~~bash
kubectl --context "$HUB_CONTEXT" apply -f hack/demo/operator-policy-namespace.yaml
kubectl --context "$HUB_CONTEXT" apply \
  -n "$OPERATOR_POLICY_NAMESPACE" \
  -f hack/demo/operator-policy-placement.yaml
kubectl --context "$HUB_CONTEXT" apply \
  -n "$OPERATOR_POLICY_NAMESPACE" \
  -f samples/operator-policy-ossm.yaml
kubectl --context "$HUB_CONTEXT" wait \
  --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-ossm-operator \
  -n "$OPERATOR_POLICY_NAMESPACE" \
  --timeout=15m
for CLUSTER in "$CLUSTER_A" "$CLUSTER_B"; do
  kubectl --context "$HUB_CONTEXT" wait \
    --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER}\")].compliant}=Compliant" \
    policy/policy-ossm-operator -n "$OPERATOR_POLICY_NAMESPACE" --timeout=15m
done
~~~

## 6. Create the MultiClusterMesh resource

The [OpenShift sample](../samples/openshift.yaml) references the Placement and issuer created above.
The OSSM operator is already installed by the OperatorPolicy.

~~~bash
kubectl --context "$HUB_CONTEXT" apply \
  -n "$MESH_NAMESPACE" \
  -f samples/openshift.yaml

kubectl --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  -o yaml | yq .status

kubectl --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"

kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=Ready=True \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  --timeout=15m

kubectl --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
~~~

The important result is status.conditions[type=Ready].status=True.
Ready requires discovery distribution and reported remote-secret availability.
When trust is configured, Ready means the cacerts ManifestWork is applied and available on every cluster the controller resolved from the Placement.
It does not mean that an Istio control plane or Bookinfo is running yet.
Check `istioctl remote-clusters -i "$CP_NAMESPACE"` after Istio is running to verify peer connectivity; see [readiness limits](api-reference.md#readiness-limits).

Read the control plane namespace from the mesh. The Istio samples need this value as `CP_NAMESPACE`:

~~~bash
export CP_NAMESPACE="$(kubectl --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  -o jsonpath='{.status.controlPlaneNamespace}')"
test -n "$CP_NAMESPACE" || { echo "Mesh control plane namespace is not available yet" >&2; exit 1; }
echo "$CP_NAMESPACE"
~~~

The add-on also creates trust and endpoint-discovery resources behind the scenes.
Those resources support the mesh but are not the primary demo object.

## 7. Configure Istio on the initial mesh members

The add-on does not create Istio resources.
Apply the Istio configuration on each spoke as the customer would, directly or through GitOps.

The [Istio CNI](../samples/istio/istiocni.yaml), [Istio control plane](../samples/istio/istio.yaml), and [east-west Gateway](../samples/istio/eastwest-gateway.yaml) samples use MESH_NAME, CLUSTER_NAME, NETWORK, and CP_NAMESPACE.
MESH_NAME is already `openshift-mesh`, which matches the sample and is the trust domain.

### 7.1. Configure Istio on cluster1

~~~bash
export CLUSTER_NAME="$CLUSTER_A"
export NETWORK=network-a
kubectl --context "$SPOKE_A_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
kubectl --context "$SPOKE_A_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  kubectl --context "$SPOKE_A_CONTEXT" apply -f -
kubectl --context "$SPOKE_A_CONTEXT" wait --for=create \
  deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
kubectl --context "$SPOKE_A_CONTEXT" wait \
  --for=condition=Available \
  deployment/istiod \
  -n "$CP_NAMESPACE" --timeout=15m
envsubst < samples/istio/eastwest-gateway.yaml | \
  kubectl --context "$SPOKE_A_CONTEXT" apply -f -
~~~

### 7.2. Configure Istio on cluster2

~~~bash
export CLUSTER_NAME="$CLUSTER_B"
export NETWORK=network-b
kubectl --context "$SPOKE_B_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
kubectl --context "$SPOKE_B_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  kubectl --context "$SPOKE_B_CONTEXT" apply -f -
kubectl --context "$SPOKE_B_CONTEXT" wait --for=create \
  deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
kubectl --context "$SPOKE_B_CONTEXT" wait \
  --for=condition=Available \
  deployment/istiod \
  -n "$CP_NAMESPACE" --timeout=15m
envsubst < samples/istio/eastwest-gateway.yaml | \
  kubectl --context "$SPOKE_B_CONTEXT" apply -f -
~~~

### 7.3. Verify mesh connectivity

Wait for each east-west gateway Service to receive a LoadBalancer address. The Gateway is programmed after the service receives an address:

~~~bash
kubectl --context "$SPOKE_A_CONTEXT" wait \
  --for=create \
  service/eastwestgateway-istio -n "$CP_NAMESPACE" --timeout=10m
kubectl --context "$SPOKE_A_CONTEXT" wait \
  --for=jsonpath='{.status.loadBalancer.ingress}' \
  service/eastwestgateway-istio \
  -n "$CP_NAMESPACE" \
  --timeout=10m

kubectl --context "$SPOKE_B_CONTEXT" wait \
  --for=create \
  service/eastwestgateway-istio -n "$CP_NAMESPACE" --timeout=10m
kubectl --context "$SPOKE_B_CONTEXT" wait \
  --for=jsonpath='{.status.loadBalancer.ingress}' \
  service/eastwestgateway-istio \
  -n "$CP_NAMESPACE" \
  --timeout=10m
~~~

Verify that both Istio control planes have connected to their peers through the add-on's trust and endpoint-discovery setup. Gateway readiness alone does not establish that discovery has completed:

~~~bash
istioctl remote-clusters --context "$SPOKE_A_CONTEXT" -i "$CP_NAMESPACE"
istioctl remote-clusters --context "$SPOKE_B_CONTEXT" -i "$CP_NAMESPACE"
~~~

## 8. Deploy Bookinfo and show cross-cluster traffic

Deploy one Bookinfo reviews version on each cluster. Keep the same service names so Istio treats the reviews workloads as one multi-cluster service.

### 8.1. Deploy Bookinfo v1 on cluster1

Set ISTIO_VERSION to the Istio version supported by the OSSM version installed by your catalog.
The repository's demo default is Istio 1.30.4; check that it matches your installed operator and istioctl version before using it.
The Bookinfo manifest uses the matching Istio minor-release branch.

~~~bash
export ISTIO_VERSION=1.30.4
export BOOKINFO_REF="release-${ISTIO_VERSION%.*}"

kubectl --context "$SPOKE_A_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml

kubectl --context "$SPOKE_A_CONTEXT" apply \
  -n bookinfo \
  -f "https://raw.githubusercontent.com/openshift-service-mesh/istio/$BOOKINFO_REF/samples/bookinfo/platform/kube/bookinfo.yaml"

kubectl --context "$SPOKE_A_CONTEXT" set env \
  deployment/reviews-v1 CLUSTER_NAME="$CLUSTER_A" -n bookinfo
kubectl --context "$SPOKE_A_CONTEXT" scale \
  deployment/reviews-v2 deployment/reviews-v3 \
  --replicas=0 -n bookinfo
kubectl --context "$SPOKE_A_CONTEXT" wait \
  --for=condition=Available \
  deployment/productpage-v1 deployment/details-v1 \
  deployment/reviews-v1 deployment/ratings-v1 \
  -n bookinfo --timeout=15m
kubectl --context "$SPOKE_A_CONTEXT" get deployment \
  -n bookinfo
~~~

### 8.2. Deploy Bookinfo v2 on cluster2

~~~bash
kubectl --context "$SPOKE_B_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml
kubectl --context "$SPOKE_B_CONTEXT" apply \
  -n bookinfo \
  -f "https://raw.githubusercontent.com/openshift-service-mesh/istio/$BOOKINFO_REF/samples/bookinfo/platform/kube/bookinfo.yaml"
kubectl --context "$SPOKE_B_CONTEXT" set env \
  deployment/reviews-v2 CLUSTER_NAME="$CLUSTER_B" -n bookinfo
kubectl --context "$SPOKE_B_CONTEXT" scale \
  deployment/productpage-v1 deployment/details-v1 \
  deployment/reviews-v1 deployment/reviews-v3 \
  --replicas=0 -n bookinfo
kubectl --context "$SPOKE_B_CONTEXT" wait \
  --for=condition=Available \
  deployment/reviews-v2 deployment/ratings-v1 \
  -n bookinfo --timeout=15m
kubectl --context "$SPOKE_B_CONTEXT" get deployment \
  -n bookinfo
~~~

### 8.3. Generate and verify cross-cluster traffic

Create the same [curl client used by the cross-cluster e2e test](../test/e2e/testdata/curl.yaml) in the injected Bookinfo namespace. It gives us a simple way to call the shared reviews service without hiding the request in application code:

~~~bash
kubectl --context "$SPOKE_A_CONTEXT" apply \
  -n bookinfo \
  -f test/e2e/testdata/curl.yaml
kubectl --context "$SPOKE_A_CONTEXT" wait \
  --for=condition=Available deployment/curl \
  -n bookinfo --timeout=15m
~~~

Call the reviews service repeatedly from that client on cluster1:

~~~bash
for i in {1..12}; do
  kubectl --context "$SPOKE_A_CONTEXT" exec deployment/curl \
    -n bookinfo -c curl -- curl -sS http://reviews:9080/reviews/0 |
    yq -r '.podname + " " + .clustername'
done
~~~

Expected output includes reviews-v1 from cluster1 and reviews-v2 from cluster2.
The pod and cluster values make the cross-cluster request visible without exposing the add-on's implementation resources.

For a browser view, port-forward the product page from a second terminal:

~~~bash
kubectl --context "$SPOKE_A_CONTEXT" port-forward \
  -n bookinfo \
  svc/productpage 9080:9080
~~~

Open http://localhost:9080/productpage.
The application is the story used for the rest of the demo.

## 9. Scale the mesh out to a third cluster

### 9.1. Add cluster3 to the mesh

Label the third cluster before adding it to the exclusive ClusterSet.
This label becomes the Istio network identity for the new control plane.

~~~bash
kubectl --context "$HUB_CONTEXT" label managedcluster "$CLUSTER_C" \
  topology.istio.io/network=network-c --overwrite
clusteradm --context "$HUB_CONTEXT" clusterset set "$CLUSTER_SET" \
  --clusters "$CLUSTER_A,$CLUSTER_B,$CLUSTER_C"
~~~

Enable the policy add-ons on cluster3 so the existing OperatorPolicy can install OSSM there:

~~~bash
clusteradm --context "$HUB_CONTEXT" addon enable \
  --names governance-policy-framework \
  --namespace open-cluster-management-agent-addon \
  --clusters "$CLUSTER_C"
clusteradm --context "$HUB_CONTEXT" addon enable \
  --names config-policy-controller \
  --namespace open-cluster-management-agent-addon \
  --clusters "$CLUSTER_C"
kubectl --context "$HUB_CONTEXT" wait --for=condition=Available \
  managedclusteraddon/governance-policy-framework \
  managedclusteraddon/config-policy-controller \
  -n "$CLUSTER_C" --timeout=10m
kubectl --context "$HUB_CONTEXT" annotate managedclusteraddon/config-policy-controller \
  -n "$CLUSTER_C" operator-policy-disabled=false --overwrite
~~~

Wait until cluster3's own entry is `Compliant`. The policy's aggregate `status.compliant` can still be `Compliant` from the first two clusters, so a wait on that field can return before the operator is installed on the new member.
The root policy lists each cluster under `status.status`:

~~~bash
kubectl --context "$HUB_CONTEXT" wait \
  --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_C}\")].compliant}=Compliant" \
  policy/policy-ossm-operator \
  -n "$OPERATOR_POLICY_NAMESPACE" \
  --timeout=15m
~~~

Wait for cluster3's trust distribution. Mesh `Ready=True` can likewise still describe the previous membership until the controller updates it.
`TrustDistributed=True` confirms latest trust material is distributed and available on that cluster:

~~~bash
kubectl --context "$HUB_CONTEXT" wait \
  --for="jsonpath={.status.clusterStatus[?(@.clusterName==\"${CLUSTER_C}\")].conditions[?(@.type==\"TrustDistributed\")].status}=True" \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE" \
  --timeout=15m
kubectl --context "$HUB_CONTEXT" get \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
~~~

The OperatorPolicy has installed the OSSM operator on the new member, and cluster3's cacerts ManifestWork is applied.
The customer still needs to install and configure Istio there.

### 9.2. Configure Istio on cluster3

Configure Istio on cluster3:

~~~bash
export CLUSTER_NAME="$CLUSTER_C"
export NETWORK=network-c
kubectl --context "$SPOKE_C_CONTEXT" apply -f hack/demo/istiocni-namespace.yaml
kubectl --context "$SPOKE_C_CONTEXT" apply -f samples/istio/istiocni.yaml
envsubst < samples/istio/istio.yaml | \
  kubectl --context "$SPOKE_C_CONTEXT" apply -f -
kubectl --context "$SPOKE_C_CONTEXT" wait --for=create \
  deployment/istiod -n "$CP_NAMESPACE" --timeout=15m
kubectl --context "$SPOKE_C_CONTEXT" wait \
  --for=condition=Available deployment/istiod \
  -n "$CP_NAMESPACE" --timeout=15m
envsubst < samples/istio/eastwest-gateway.yaml | \
  kubectl --context "$SPOKE_C_CONTEXT" apply -f -
kubectl --context "$SPOKE_C_CONTEXT" wait \
  --for=create service/eastwestgateway-istio -n "$CP_NAMESPACE" --timeout=15m
kubectl --context "$SPOKE_C_CONTEXT" wait \
  --for=jsonpath='{.status.loadBalancer.ingress}' \
  service/eastwestgateway-istio \
  -n "$CP_NAMESPACE" --timeout=15m
~~~

Verify that all three control planes can see each other:

~~~bash
istioctl remote-clusters --context "$SPOKE_A_CONTEXT" -i "$CP_NAMESPACE"
istioctl remote-clusters --context "$SPOKE_B_CONTEXT" -i "$CP_NAMESPACE"
istioctl remote-clusters --context "$SPOKE_C_CONTEXT" -i "$CP_NAMESPACE"
~~~

### 9.3. Deploy Bookinfo v3 and verify traffic

Deploy Bookinfo v3 on cluster3. Keep the same service names so the three reviews versions form one multi-cluster service:

~~~bash
kubectl --context "$SPOKE_C_CONTEXT" apply \
  -f hack/demo/bookinfo-namespace.yaml
kubectl --context "$SPOKE_C_CONTEXT" apply \
  -n bookinfo \
  -f "https://raw.githubusercontent.com/openshift-service-mesh/istio/$BOOKINFO_REF/samples/bookinfo/platform/kube/bookinfo.yaml"
kubectl --context "$SPOKE_C_CONTEXT" set env \
  deployment/reviews-v3 CLUSTER_NAME="$CLUSTER_C" -n bookinfo
kubectl --context "$SPOKE_C_CONTEXT" scale \
  deployment/productpage-v1 deployment/details-v1 \
  deployment/reviews-v1 deployment/reviews-v2 \
  --replicas=0 -n bookinfo
kubectl --context "$SPOKE_C_CONTEXT" wait \
  --for=condition=Available \
  deployment/reviews-v3 deployment/ratings-v1 \
  -n bookinfo --timeout=15m
kubectl --context "$SPOKE_C_CONTEXT" get deployment \
  -n bookinfo
~~~

Call the reviews service again from the client on cluster1:

~~~bash
for i in {1..12}; do
  kubectl --context "$SPOKE_A_CONTEXT" exec deployment/curl \
    -n bookinfo -c curl -- curl -sS http://reviews:9080/reviews/0 |
    yq -r '.podname + " " + .clustername'
done
~~~

Expected output now includes reviews-v1, reviews-v2, and reviews-v3, showing traffic across all three mesh members.

To see the Bookinfo productpage in a browser, expose it through an OpenShift Route:

~~~bash
kubectl --context "$SPOKE_A_CONTEXT" create route edge productpage \
  --service=productpage --port=9080 -n bookinfo
kubectl --context "$SPOKE_A_CONTEXT" get route productpage \
  -n bookinfo -o jsonpath='https://{.spec.host}/productpage{"\n"}'
~~~

Open the printed URL and refresh a few times.
Reviews with no stars come from v1, black stars from v2, and red stars from v3.
The version that appears changes because Istio load-balances the reviews service across all three clusters.

## 10. Open the Fleet Service Mesh console plugin

The OSSM Console plugin is a separate final step on the ACM hub.
Install the Kiali Operator from OperatorHub, or create the equivalent subscription explicitly:

~~~bash
kubectl --context "$HUB_CONTEXT" apply \
  -f hack/demo/kiali-ossm-subscription.yaml

kubectl --context "$HUB_CONTEXT" wait \
  --for=jsonpath='{.status.phase}'=Succeeded \
  --selector=operators.coreos.com/kiali-ossm.openshift-operators \
  csv \
  -n openshift-operators \
  --timeout=10m
kubectl --context "$HUB_CONTEXT" wait \
  --for=condition=Available \
  deployment/kiali-operator \
  -n openshift-operators \
  --timeout=5m
~~~

Create the OSSMConsole resource with Fleet Service Mesh enabled:

~~~bash
kubectl --context "$HUB_CONTEXT" apply \
  -f hack/demo/ossm-console.yaml

kubectl --context "$HUB_CONTEXT" wait \
  --for=create \
  consoleplugin/ossmconsole \
  --timeout=10m
kubectl --context "$HUB_CONTEXT" get ossmconsole -A
kubectl --context "$HUB_CONTEXT" get consoleplugin ossmconsole
~~~

Refresh the OpenShift Console on the ACM hub and navigate to the Fleet Service Mesh perspective:

1. Click the perspective switcher (top-left) and select **Fleet Service Mesh**.
2. Open the **Meshes** page and inspect the mesh if the installed plugin supports this Placement-based MultiClusterMesh API.
3. Use the plugin's supported inventory views for control-plane details. The addon publishes `spec.placementRef`, its derived namespace, and trust conditions; it does not publish ClusterSet or per-cluster operator-installation status.
4. Verify operator health separately using the OperatorPolicy and spoke operator deployments.

Fleet Service Mesh is a developer preview.
It is an inventory and status view.
It does not replace Kiali and it does not create the Istio resources owned by the customer.

The hub console URL can be printed with:

~~~bash
kubectl --context "$HUB_CONTEXT" get route console \
  -n openshift-console \
  -o jsonpath='https://{.spec.host}{"\n"}'
~~~

## Cleanup

First remove the user-owned, cluster-scoped Istio resources so the operator stops recreating their control-plane components. The sample used `Istio/default` on each spoke:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  kubectl --context "$CONTEXT" delete istio.sailoperator.io default --ignore-not-found
done
~~~

Deleting the MultiClusterMesh requests asynchronous cleanup of each spoke's mesh namespace, cacerts, discovery resources, and istio-reader RBAC. The finalizer does not wait for all spoke resources to disappear. Deleting the demo hub namespace also deletes its issuers and retained certificate Secrets:

~~~bash
kubectl --context "$HUB_CONTEXT" delete \
  "multiclustermesh/$MESH_NAME" \
  -n "$MESH_NAMESPACE"
kubectl --context "$HUB_CONTEXT" delete namespace "$MESH_NAMESPACE"
~~~

Remove the Bookinfo namespace from each spoke:

~~~bash
for CONTEXT in "$SPOKE_A_CONTEXT" "$SPOKE_B_CONTEXT" "$SPOKE_C_CONTEXT"; do
  kubectl --context "$CONTEXT" delete namespace bookinfo --ignore-not-found
done
~~~

The separately installed mesh operator, its OperatorPolicy in `$OPERATOR_POLICY_NAMESPACE`, the ClusterSet, and cluster-scoped IstioCNI resources remain. Remove those separately only when they are no longer needed by other meshes.

## References

- [User Guide](user-guide.md)
- [OpenShift cert-manager Operator installation](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/security_and_compliance/cert-manager-operator-for-red-hat-openshift)
- [Fleet Service Mesh dev preview guide](https://github.com/kiali/openshift-servicemesh-plugin/blob/main/docs/fleet-mesh/DEV-PREVIEW-GUIDE.md)
- [Bookinfo application](https://istio.io/latest/docs/examples/bookinfo/)
