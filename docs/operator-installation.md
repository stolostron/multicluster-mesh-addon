# Install the Service Mesh Operator

Install Sail or OSSM on managed clusters using OperatorPolicy.

The service mesh operator is a prerequisite for the addon. The addon creates the control plane namespace, trust material, and discovery infrastructure, but it does not install the operator or the Istio control plane. You can install the operator any way you like (Helm, manual OLM Subscription, etc.), but [OperatorPolicy] is the recommended method for ACM environments because it uses the same Placement-based targeting as the rest of the workflow.

## Prerequisites

OperatorPolicy creates OLM Subscriptions to install the operator. On non-OpenShift managed clusters, such as kind, install Operator Lifecycle Manager (OLM) if it is not already present. OpenShift includes OLM by default.

Each managed cluster needs a LoadBalancer implementation that provides an east-west gateway address reachable from the other mesh clusters.

A Placement and a ManagedClusterSetBinding must exist in the namespace where you apply the policy.
The Placement must select clusters from a ClusterSet bound to that namespace.
Follow [Step 4 of the user guide](user-guide.md#step-4-create-a-placement) to set up ClusterSet membership, the binding, and the Placement.

Before applying an OperatorPolicy, set up the governance framework on the hub and on each managed cluster that will run the operator.

### Install Gateway API CRDs

The east-west gateway samples use [Gateway API] in all three configuration paths: ACM policies, PolicyGenerator, and manual installation. Your clusters may already provide these CRDs. If compatible CRDs are already present, no separate installation is needed.

The following example checks each spoke and installs the CRDs only when they are absent. Use a Gateway API version supported by your Istio/operator release. Replace the example context names with those from your kubeconfig:

```bash
CLUSTER_CONTEXTS=("context-for-cluster1" "context-for-cluster2")

for CONTEXT in "${CLUSTER_CONTEXTS[@]}"; do
  if ! kubectl --context "$CONTEXT" get crd \
    gateways.gateway.networking.k8s.io gatewayclasses.gateway.networking.k8s.io >/dev/null 2>&1; then
    kubectl --context "$CONTEXT" apply --server-side \
      -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
  fi
done
```

The samples require `gateway.networking.k8s.io/v1`. An existing Gateway CRD might only serve older versions, so check the served versions on every spoke before continuing:

```bash
for CONTEXT in "${CLUSTER_CONTEXTS[@]}"; do
  echo "$CONTEXT:"
  kubectl --context "$CONTEXT" get crd gateways.gateway.networking.k8s.io \
    -o jsonpath='{.spec.versions[?(@.served==true)].name}{"\n"}'
done
```

The output for every spoke must include `v1`; `v1beta1` or `v1alpha2` alone is not sufficient.
If `v1` is missing, stop and upgrade the Gateway API CRDs to a version supported by your Istio/operator release, then repeat this check before applying the Istio samples.

### Install the governance hub add-on

If the governance-policy-framework CRDs are not already present on the hub, install the hub-side add-on:

```bash
clusteradm install hub-addon --names governance-policy-framework
```

### Enable the policy add-ons on managed clusters

Each managed cluster needs two add-ons: `governance-policy-framework` and `config-policy-controller`. Enable them for the clusters that will join the mesh:

```bash
clusteradm addon enable \
  --names governance-policy-framework \
  --namespace open-cluster-management-agent-addon \
  --clusters cluster1,cluster2
clusteradm addon enable \
  --names config-policy-controller \
  --namespace open-cluster-management-agent-addon \
  --clusters cluster1,cluster2
```

Wait for both add-ons to become Available on each cluster:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl wait --for=condition=Available \
    managedclusteraddon/governance-policy-framework \
    managedclusteraddon/config-policy-controller \
    -n "$CLUSTER_NAME" --timeout=10m
done
```

### Enable OperatorPolicy

OperatorPolicy is disabled by default on `config-policy-controller`. Enable it by annotating the ManagedClusterAddOn on each cluster:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl annotate managedclusteraddon/config-policy-controller \
    -n "$CLUSTER_NAME" operator-policy-disabled=false --overwrite
done
```

## Apply the OperatorPolicy

The samples bind to a Placement named `mesh-operator-placement`.
The commands below update the PlacementBinding's `placementRef.name` to use `mesh-placement` from Step 4 and apply the policy in the same namespace, `mesh-system`.

### Kubernetes (Sail)

```bash
sed 's/name: mesh-operator-placement/name: mesh-placement/' samples/operator-policy-sail.yaml \
  | kubectl apply -n mesh-system -f -
```

### OpenShift (OSSM)

```bash
sed 's/name: mesh-operator-placement/name: mesh-placement/' samples/operator-policy-ossm.yaml \
  | kubectl apply -n mesh-system -f -
```

## Verify the operator

Wait for the policy to report Compliant. Check the aggregate status first, then confirm each cluster individually:

```bash
kubectl wait --for=jsonpath='{.status.compliant}'=Compliant \
  policy/policy-sail-operator -n mesh-system --timeout=15m
```

Verify per-cluster compliance to make sure every cluster (not just the first) is ready:

```bash
for CLUSTER_NAME in cluster1 cluster2; do
  kubectl wait \
    --for="jsonpath={.status.status[?(@.clustername==\"${CLUSTER_NAME}\")].compliant}=Compliant" \
    policy/policy-sail-operator -n mesh-system --timeout=15m
done
```

> **Note:** Replace `policy-sail-operator` with `policy-ossm-operator` when using the OSSM sample.

You can also verify the operator pod directly on a spoke if you have access. Replace `context-for-cluster1` with the spoke's kubeconfig context.

For Sail:

```bash
kubectl --context context-for-cluster1 get pods -n sail-operator --no-headers
```

For OSSM:

```bash
kubectl --context context-for-cluster1 get pods -n multicluster-mesh-operator --no-headers
```

## Version pinning

The samples use `upgradeApproval: Automatic`, which allows OLM to install the latest version in the channel. To pin a specific version, change `upgradeApproval` to `None` and add `startingCSV` and `versions`:

```yaml
spec:
  upgradeApproval: None
  subscription:
    startingCSV: sail-operator.v1.23.0
  versions:
    - sail-operator.v1.23.0
```

> **Note:** This example uses a Sail CSV name. OSSM uses different CSV names, such as `servicemeshoperator3.v3.0.0` in the [OSSM sample](../samples/operator-policy-ossm.yaml). Set both `startingCSV` and `versions` to the CSV name for your chosen operator release.

With `upgradeApproval: None`, the operator stays on the listed versions until you update the policy.

## Adding clusters later

Before adding a cluster to the mesh, complete the [prerequisites](#prerequisites) on it.
This includes OLM on non-OpenShift clusters and compatible [Gateway API CRDs](#install-gateway-api-crds) for the Istio policies.

Then enable the policy add-ons and the OperatorPolicy annotation on the new cluster:

```bash
NEW_CLUSTER=cluster3

clusteradm addon enable \
  --names governance-policy-framework \
  --namespace open-cluster-management-agent-addon \
  --clusters "$NEW_CLUSTER"
clusteradm addon enable \
  --names config-policy-controller \
  --namespace open-cluster-management-agent-addon \
  --clusters "$NEW_CLUSTER"

kubectl wait --for=condition=Available \
  managedclusteraddon/governance-policy-framework \
  managedclusteraddon/config-policy-controller \
  -n "$NEW_CLUSTER" --timeout=10m

kubectl annotate managedclusteraddon/config-policy-controller \
  -n "$NEW_CLUSTER" operator-policy-disabled=false --overwrite
```

The new cluster must belong to a ClusterSet considered by the Placement used by the OperatorPolicy.
That ClusterSet must be bound to the Placement's namespace, and the cluster must match the Placement's selectors. See [Step 4 of the user guide](user-guide.md#step-4-create-a-placement) for setup.
Once the Placement selects the new cluster, the existing policy propagates to it automatically. No changes to the Policy or PlacementBinding are needed.

## Alternative methods

OperatorPolicy is not the only way to install the operator. The addon only requires the operator to be present on the spoke; it does not care how it got there. Other options:

- **Helm**: install the Sail operator Helm chart directly on each cluster.
- **Manual OLM Subscription**: create the Subscription and OperatorGroup resources yourself.
- **ArgoCD ApplicationSet**: distribute the operator via ArgoCD using a cluster generator.

If you use a different method, check the [Gateway API prerequisites](#install-gateway-api-crds), then proceed to the next step in the [user guide](user-guide.md#step-6-create-a-multiclustermesh).

[OperatorPolicy]: https://open-cluster-management.io/concepts/policy/
[Gateway API]: https://gateway-api.sigs.k8s.io/
