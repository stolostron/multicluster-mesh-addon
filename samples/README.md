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

## Hub (Istio Control Plane Policy)

Pre-built ACM policies that configure the Istio control plane on managed clusters from the hub using hub templates. No spoke access or build tools needed.

| Manifest | Purpose |
|----------|---------|
| [policy-istio-control-plane.yaml](policy-istio-control-plane.yaml) | Istio CR + east-west Gateway (hub templates resolve per-cluster values) |
| [policy-istiocni.yaml](policy-istiocni.yaml) | IstioCNI namespace + CR (cluster-level prerequisite for OpenShift and ambient; apply once, not per mesh) |

Replace `<MESH_NAME>`, `<MESH_NAMESPACE>`, and `<PLACEMENT_NAME>` in the sample with your values, then apply on the hub:

```bash
sed -e 's/<MESH_NAMESPACE>/mesh-system/g' -e 's/<MESH_NAME>/basic-mesh/g' \
  -e 's/<PLACEMENT_NAME>/mesh-placement/g' \
  samples/policy-istio-control-plane.yaml \
  | kubectl apply -n mesh-system -f -
```

See [Step 8 of the User Guide](../docs/user-guide.md#step-8-configure-istio) for the full procedure.

## Hub (PolicyGenerator for GitOps)

Decomposed manifests with hub templates for use with the [PolicyGenerator Kustomize plugin](../docs/istio-policygenerator.md#prerequisites). Suitable for GitOps workflows with Kustomize or ArgoCD.

| Path | Purpose |
|------|---------|
| [policygenerator/kustomization.yaml](policygenerator/kustomization.yaml) | Kustomize entry point |
| [policygenerator/policygenerator.yaml](policygenerator/policygenerator.yaml) | PolicyGenerator CR defining policies |
| [policygenerator/istio-manifests/istio.yaml](policygenerator/istio-manifests/istio.yaml) | Istio CR with hub templates |
| [policygenerator/istio-manifests/eastwest-gateway.yaml](policygenerator/istio-manifests/eastwest-gateway.yaml) | East-west Gateway with hub templates |

IstioCNI (OpenShift/ambient) is a cluster-level prerequisite. Manage it separately using [policy-istiocni.yaml](policy-istiocni.yaml).

Build and apply:

```bash
kustomize build --enable-alpha-plugins samples/policygenerator/ \
  | kubectl apply -n mesh-system -f -
```

See [Configure Istio with PolicyGenerator](../docs/istio-policygenerator.md) for the full guide.

## Spoke (Istio, manual)

Raw Istio manifests with `${VARIABLE}` placeholders for use with `envsubst` on each spoke cluster directly.

| Manifest | Purpose |
|----------|---------|
| [istio/istio.yaml](istio/istio.yaml) | Istio control plane (`MESH_NAME`, `CLUSTER_NAME`, `NETWORK`, `CP_NAMESPACE`) |
| [istio/eastwest-gateway.yaml](istio/eastwest-gateway.yaml) | East-west Gateway API gateway (`NETWORK`, `CP_NAMESPACE`) |
| [istio/istiocni.yaml](istio/istiocni.yaml) | IstioCNI (required on OpenShift and ambient) |

Apply spoke samples on each managed cluster.
See [Configure Istio (Manual)](../docs/istio-manual.md) for the full procedure.
