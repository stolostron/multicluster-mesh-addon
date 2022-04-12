# Multicluster Mesh Addon

The multicluster-mesh-addon is an enhanced service mesh addon created with [addon-framework](http://github.com/open-cluster-management-io/addon-framework), it is used to manage(discovery, deploy and federate) service meshes across multiple clusters in [Red Hat Advanced Cluster Management for Kubernetes](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/). With multicluster-mesh-addon, you can unify the configuration and operation of your services spanning from single cluster to multiple clusters in hybrid cloud.

![multicluster-mesh-addon-overview](assets/multicluster-mesh-addon.drawio.svg)

## Core Concepts

To simplify the configuration and operation of service meshes, we created the following three [custom resource definitions (CRDs)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) than you can configure from the hub cluster of Red Hat Advanced Cluster Management for Kubernetes. Behind the scenes, the multicluster-mesh-addon translates these high level objects into low level Istio resources and then applied into the managed clusters.

1. **Mesh** - a `mesh` resource is mapping to a physical service mesh in a managed cluster, it contains the desired state and status of the backend service mesh.

    For each physical service mesh in a managed cluster, a mesh resource is created in the managed cluster namespace of hub cluster. An example of mesh resource would resemble the following yaml snippet:

    ```yaml
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: Mesh
    metadata:
      name: managedcluster1-istio-system-basic
    spec:
      clusters: managedcluster1
      controlPlane:
        components: ["istio-discovery", "istio-ingress", "mesh-config", "telemetry-common", "tracing"]
        namespace: istio-system
        profiles: ["default"]
        version: v2.1
      meshMemberRoll: ["istio-apps"]
      meshProvider: Openshift Service Mesh
      trustDomain: cluster.local
    status:
      readiness:
        components:
          pending: []
          ready: ["istio-discovery", "istio-ingress", "mesh-config", "telemetry-common", "tracing"]
          unready: []
    ```

2. **MeshDeployment** - `meshdeployment` resource is used to deploy physical service meshes to managed cluster(s), it supports deploying multiple physical service meshes to different managed clusters with one mesh template.

    An example of meshdeployment resource would resemble the following yaml snippet:

    ```yaml
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshDeployment
    metadata:
      name: mesh
    spec:
      clusters: ["managedcluster1", "managedcluster2"]
      controlPlane:
        components: ["prometheus", "istio-discovery", "istio-ingress", "mesh-config", "telemetry-common", "tracing"]
        namespace: mesh-system
        profiles: ["default"]
        version: v2.1
      meshMemberRoll: ["mesh-apps"]
      meshProvider: Openshift Service Mesh
    status:
      appliedMeshes: ["managedcluster1-mesh", "managedcluster2-mesh"]
    ```

3. **MeshFederation** - `meshfederation` resource is used to federate service meshes so that the physical service meshes located in one cluster or different clusters to securely share and manage traffic between meshes while maintaining strong administrative boundaries in a multi-tenant environment.

    An example of meshfederation resource would resemble the following yaml snippet:

    ```yaml
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshFederation
    metadata:
      name: mcsm
    spec:
      meshPeers:
      - peers:
        - name: managedcluster1-mesh
          cluster: managedcluster1
        - name: managedcluster2-mesh
          cluster: managedcluster2
      trustConfig:
        trustType: Limited
    status:
      federatedMeshes:
      - peer:
        - managedcluster1-mesh
        - managedcluster1-mesh
    ```

## Getting Started

### Prerequisites

- Ensure [docker](https://docs.docker.com/get-started) 18.03+ is installed.
- Ensure [golang](https://golang.org/doc/install) 1.17+ is installed.
- Prepare an environment of [Red Hat Advanced Cluster Management for Kubernetes](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/) and login to the hub cluster with `oc` command line tool.
- Make sure at least one managed cluster imported to the hub cluster of Red Hat Advanced Cluster Management for Kubernetes.
- For mesh federation support, make sure at least two managed clusters are imported and the cloud provider must support the network load balancer IP so that the meshes spanning across managed clusters can be connected.

### Build and Deploy

1. Build and push docker image:

    ```bash
    make docker-build docker-push IMAGE=quay.io/<your_quayio_username>/multicluster-mesh-addon:latest
    ```

2. Deploy the multicluster-mesh-addon to hub cluster:

    ```
    make deploy IMAGE=quay.io/<your_quayio_username>/multicluster-mesh-addon:latest
    ```

## How to use

_Note_: For now, [Openshift Service Mesh](https://docs.openshift.com/container-platform/4.6/service_mesh/v2x/ossm-about.html) on Openshift managed cluster(s) and [upstream Istio](https://istio.io/) on *KS managed cluster(s) are supported.

1. Mesh Discovery:

    If you have installed Openshift Service Mesh in any Openshift managed cluster, then you should find a mesh resource created in its namespace of hub cluster:

    ```bash
    # oc -n ocp1 get mesh
    NAME                       CLUSTER    VERSION    REVISION    PEERS    AGE
    ocp1-istio-system-basic    ocp1       v2.1                   80m
    ```

    If you have installed upstream Istio in any *KS managed cluster, then you should also find a mesh resource created in its namespace of hub cluster:

    ```bash
    # oc -n eks1 get mesh
    NAME                                CLUSTER    VERSION   REVISION   PEERS   AGE
    eks1-istio-system-installed-state   eks1       1.13.2                       50s
    ```


2. Mesh Deployment:

    To deploy new service meshes to managed clusters, create a `meshdeployment` resource in hub cluster by specifying `Openshift Service Mesh` meshProvider and selecting the openshift managed cluster(s). For example, create the following `meshdeployment` resource to deploy new Openshift Service Mesh to Openshift managed cluster `ocp1` and `ocp2`:

    ```bash
    cat << EOF | oc apply -f -
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshDeployment
    metadata:
      name: ossm
      namespace: open-cluster-management
    spec:
      clusters: ["ocp1", "ocp2"]
      controlPlane:
        components: ["prometheus", "istio-discovery", "istio-ingress", "mesh-config", "telemetry-common", "tracing"]
        namespace: mesh-system
        profiles: ["default"]
        version: v2.1
      meshMemberRoll: ["bookinfo"]
      meshProvider: Openshift Service Mesh
    EOF
    ```

    To deploy new upstream Istio to managed clusters, create a `meshdeployment` resource by specifying `Community Istio` meshProvider and selecting the *KS managed cluster(s). For example, create the following `meshdeployment` resource to deploy new Istio to managed cluster `eks1` and `eks1`:

    ```bash
    cat << EOF | oc apply -f -
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshDeployment
    metadata:
      name: istio
      namespace: open-cluster-management
    spec:
      clusters: ["eks1", "eks2"]
      controlPlane:
        components: ["base", "istiod", "istio-ingress"]
        namespace: istio-system
        profiles: ["default"]
        version: 1.13.2
      meshMemberRoll: ["bookinfo"]
      meshProvider: Community Istio
    EOF
    ```

    Then verify the service meshes are created:

    ```bash
    # oc get mesh -A
    NAMESPACE    NAME         CLUSTER    VERSION  REVISION  PEERS    AGE
    ocp1         ocp1-ossm    ocp1       v2.1                        19m
    ocp2         ocp2-ossm    ocp2       v2.1                        19m
    eks1         eks1-istio   eks1       1.13.2                      18m
    eks1         eks2-istio   eks2       1.13.2                      18m
    ```

4. Mesh Federation:

    To federate the Openshift service meshes in Openshift managed clusters, create a `meshfederation` resource in hub cluster by specifying the Openshift Service Mesh peers and `Limited` trustType. For example, federate `ocp1-ossm` and `ocp2-ossm` created in last step by creating a `meshfederation` resource with the following command:

    ```bash
    cat << EOF | oc apply -f -
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshFederation
    metadata:
      name: ossmfederation
      namespace: open-cluster-management
    spec:
      meshPeers:
      - peers:
        - name: ocp1-ossm
          cluster: ocp1
        - name: ocp2-ossm
          cluster: ocp2
      trustConfig:
        trustType: Limited
    EOF
    ```

    To federate the Istio service meshes in *KS managed clusters, create a `meshfederation` resource in hub cluster by specifying the Istio Service Mesh peers and `Complete` trustType. For example, federate `eks1-istio` and `eks1-istio` created in last step by creating a `meshfederation` resource with the following command:

    ```bash
    cat << EOF | oc apply -f -
    apiVersion: mesh.open-cluster-management.io/v1alpha1
    kind: MeshFederation
    metadata:
      name: istiofederation
      namespace: open-cluster-management
    spec:
      meshPeers:
      - peers:
        - name: eks1-istio
          cluster: eks1
        - name: eks2-istio
          cluster: eks2
      trustConfig:
        trustType: Complete
    EOF
    ```

    Finally, use the following different instructions to deploy [Bookinfo application](https://istio.io/latest/docs/examples/bookinfo/) spanning across managed clusters to verify the federated meshes are working as expected:

    _Note:_ currently the verify steps have to be executed in the managed cluster, the work for the service discovery and service federation is still in progress.

    - [Mesh federation verify for Openshift Service Mesh](mesh-federation-verify-ossm.md)
    - [Mesh federation verify for upstream Istio](mesh-federation-verify-istio.md)

## Future Work

* Services and workloads discovery
* Federate services across meshes
* Deploy application across meshes
