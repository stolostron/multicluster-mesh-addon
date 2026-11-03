# Install the Service Mesh Operator

Install Sail or OSSM on managed clusters using OperatorPolicy.

The service mesh operator is a prerequisite for the addon. The addon creates the control plane namespace, trust material, and discovery infrastructure, but it does not install the operator or the Istio control plane. You can install the operator any way you like (Helm, manual OLM Subscription, etc.), but [OperatorPolicy] is the recommended method for ACM environments because it uses the same Placement-based targeting as the rest of the workflow.

## Prerequisites

Before applying an OperatorPolicy, set up the governance framework on the hub and on each managed cluster that will run the operator.

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

The samples bind to a Placement named `mesh-operator-placement`. If you are using the same Placement as the mesh (for example, `mesh-placement` from the [user guide](user-guide.md#step-4-create-a-placement)), rename the binding before applying.

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

With `upgradeApproval: None`, the operator stays on the listed versions until you update the policy.

## Adding clusters later

When new clusters join the mesh, enable the policy add-ons and the OperatorPolicy annotation on them:

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

If the new cluster matches the Placement used by the OperatorPolicy, the existing policy propagates to it automatically. No changes to the Policy or PlacementBinding are needed.

## Alternative methods

OperatorPolicy is not the only way to install the operator. The addon only requires the operator to be present on the spoke; it does not care how it got there. Other options:

- **Helm**: install the Sail operator Helm chart directly on each cluster.
- **Manual OLM Subscription**: create the Subscription and OperatorGroup resources yourself.
- **ArgoCD ApplicationSet**: distribute the operator via ArgoCD using a cluster generator.

If you use a different method, skip this page and proceed to the next step in the [user guide](user-guide.md#step-6-create-a-multiclustermesh).

[OperatorPolicy]: https://open-cluster-management.io/concepts/policy/
