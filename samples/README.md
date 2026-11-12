# Samples

Example manifests for common configurations.

## Hub (MultiClusterMesh)

| Manifest | Use when |
|----------|----------|
| [cert-manager-issuer.yaml](cert-manager-issuer.yaml) | Self-signed trust chain for testing |
| [openshift.yaml](openshift.yaml) | OpenShift mesh (after OSSM operator is installed) |
| [basic.yaml](basic.yaml) | kind / vanilla Kubernetes mesh (after Sail operator is installed) |
| [complete.yaml](complete.yaml) | Fully annotated field reference |

Apply hub samples on the hub cluster in your mesh namespace (for example `mesh-system`).
Update `spec.placementRef.name` to match your Placement name.

The control plane namespace is auto-derived per mesh and reported in
`status.controlPlaneNamespace`. Install the mesh operator before creating a
`MultiClusterMesh` (see OperatorPolicy samples below).

## Hub (Istio installation policy)

`meshctl scaffold istio` generates the [Istio installation policy](istio-policy.yaml)
for a mesh. The CLI is offline and writes a root `Policy` plus `PlacementBinding`
to stdout. ACM resolves the control-plane namespace from MCM status and each
cluster's network from its `ManagedCluster` label, falling back to the cluster
name. Build it with `make build-meshctl` and generate a bundle with:

```bash
bin/meshctl scaffold istio basic-mesh \
  --namespace mesh-system \
  --placement mesh-placement \
  --istio-version v1.30.4 > mesh-installation.yaml
```

The `--placement` and `--istio-version` flags are required. Use a version
supported by the operator installed on the selected spokes. Review the generated
configuration and apply it to the hub, or commit it to GitOps. The root policy
owns the embedded spoke `ConfigurationPolicy`, which creates the Sail `Istio`
and east-west `Gateway` resources. For clusters that need CNI, generate the
separate [IstioCNI policy](istio-cni-policy.yaml) with `meshctl scaffold istiocni`
and pass its policy name using `--depends-on-istiocni` to gate the Istio policy
on its readiness check. See the [meshctl guide](../docs/meshctl.md).

The default sample targets a sidecar Sail installation. The scaffolds assume
the operator, policy engine, Gateway API, load balancer and `MultiClusterMesh`
already exist. They do not install ztunnel or ambient-specific resources.

## Hub (OperatorPolicy)

Install the Sail or OSSM operator on managed clusters before creating a mesh.
These samples require the governance-policy-framework, config-policy-controller,
and a Placement named `mesh-operator-placement` in the apply namespace.

| Manifest | Use when |
|----------|----------|
| [operator-policy-sail.yaml](operator-policy-sail.yaml) | kind / vanilla Kubernetes (Sail) |
| [operator-policy-ossm.yaml](operator-policy-ossm.yaml) | OpenShift (OSSM) |

Apply on the hub, for example:

```bash
kubectl apply -n mesh-operator-policy -f samples/operator-policy-sail.yaml
```

## Spoke (Istio)

| Manifest | Purpose |
|----------|---------|
| [istio/istio.yaml](istio/istio.yaml) | Istio control plane (`MESH_NAME`, `CLUSTER_NAME`, `NETWORK`, `CP_NAMESPACE`) |
| [istio/eastwest-gateway.yaml](istio/eastwest-gateway.yaml) | East-west Gateway API gateway (`NETWORK`, `CP_NAMESPACE`) |
| [istio/istiocni.yaml](istio/istiocni.yaml) | IstioCNI (required on OpenShift and ambient) |

Apply spoke samples on each managed cluster.
See the [User Guide](../docs/user-guide.md#step-8-configure-istio) for the full procedure.
