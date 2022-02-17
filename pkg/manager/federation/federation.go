package federation

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
)

const meshFederationFinalizer = "mesh.open-cluster-management.io/meshfederation-resources-cleanup"

type meshFederationController struct {
	kubeClient           kubernetes.Interface
	meshClient           meshclientset.Interface
	configMapLister      corev1lister.ConfigMapLister
	meshFederationLister meshv1alpha1lister.MeshFederationLister
	recorder             events.Recorder
}

func NewMeshFederationController(
	kubeClient kubernetes.Interface,
	meshClient meshclientset.Interface,
	configMapInformer corev1informer.ConfigMapInformer,
	meshFederationInformer meshv1alpha1informer.MeshFederationInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &meshFederationController{
		kubeClient:           kubeClient,
		meshClient:           meshClient,
		configMapLister:      configMapInformer.Lister(),
		meshFederationLister: meshFederationInformer.Lister(),
		recorder:             recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, meshFederationInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key + "-federations"
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a configmap with label "mesh.open-cluster.io/federation=true"
			labels := accessor.GetLabels()
			lv, ok := labels[constants.FederationResourcesLabelKey]
			return ok && (lv == "true") && strings.Contains(accessor.GetName(), "-ep4-")
		}, configMapInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-meshfederation-controller", recorder)
}

func (c *meshFederationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()

	// check if reconciling k8s resources instead of meshfederation
	reconcileK8sRes := false
	if strings.HasSuffix(key, "-federations") {
		key = strings.TrimSuffix(key, "-federations")
		reconcileK8sRes = true
		klog.V(2).Infof("Reconciling federation resources %q", key)
	} else {
		klog.V(2).Infof("Reconciling meshfederation %q", key)
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	if !reconcileK8sRes {
		meshFederation, err := c.meshFederationLister.MeshFederations(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		meshFederation = meshFederation.DeepCopy()
		if meshFederation.DeletionTimestamp.IsZero() {
			hasFinalizer := false
			for i := range meshFederation.Finalizers {
				if meshFederation.Finalizers[i] == meshFederationFinalizer {
					hasFinalizer = true
					break
				}
			}
			if !hasFinalizer {
				meshFederation.Finalizers = append(meshFederation.Finalizers, meshFederationFinalizer)
				_, err := c.meshClient.MeshV1alpha1().MeshFederations(namespace).Update(ctx, meshFederation, metav1.UpdateOptions{})
				return err
			}
		}

		// remove meshfederation related resources after meshfederation is deleted
		if !meshFederation.DeletionTimestamp.IsZero() {
			if err := c.removeMeshFederationResources(ctx, meshFederation); err != nil {
				return err
			}
			return c.removeMeshFederationFinalizer(ctx, meshFederation)
		}

		trustType := meshv1alpha1.TrustTypeComplete
		if meshFederation.Spec.TrustConfig != nil && meshFederation.Spec.TrustConfig.TrustType != "" {
			trustType = meshFederation.Spec.TrustConfig.TrustType
		}

		switch trustType {
		case meshv1alpha1.TrustTypeComplete:
			// for upstream istio
			klog.Info("federate meshes with complete trust by shared CA")
		case meshv1alpha1.TrustTypeLimited:
			// for Openshift service mesh
			klog.Info("federate meshes with limited trust gated at gateways")
		default:
			return fmt.Errorf("invalid trust type for meshfederation")
		}

		// create east-west gateways for mesh peers
		meshPeers := meshFederation.Spec.MeshPeers
		for _, meshPeer := range meshPeers {
			peers := meshPeer.Peers
			//TODO(morvencao): add validation webhook to validate the meshFederation resource
			if peers == nil || len(peers) != 2 || peers[0].Name+peers[0].Cluster == peers[1].Name+peers[1].Cluster {
				return fmt.Errorf("two different meshes must specified in peers")
			}

			mesh1, mesh2 := &meshv1alpha1.Mesh{}, &meshv1alpha1.Mesh{}
			mesh1, err := c.meshClient.MeshV1alpha1().Meshes(peers[0].Cluster).Get(context.TODO(), peers[0].Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mesh2, err = c.meshClient.MeshV1alpha1().Meshes(peers[1].Cluster).Get(context.TODO(), peers[1].Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			ewgwExisting := false
			for _, peer := range mesh1.Spec.ControlPlane.Peers {
				if peer.Name == mesh2.GetName() && peer.Cluster == mesh2.GetNamespace() {
					ewgwExisting = true
					break
				}
			}
			if !ewgwExisting {
				mesh1.Spec.ControlPlane.Peers = append(mesh1.Spec.ControlPlane.Peers, meshv1alpha1.Peer{Name: mesh2.GetName(), Cluster: mesh2.GetNamespace()})
				// only update mesh if needed
				_, _, err = meshresourceapply.ApplyMesh(ctx, c.meshClient.MeshV1alpha1(), c.recorder, mesh1)
				if err != nil {
					return err
				}
			}

			ewgwExisting = false
			for _, peer := range mesh2.Spec.ControlPlane.Peers {
				if peer.Name == mesh1.GetName() && peer.Cluster == mesh1.GetNamespace() {
					ewgwExisting = true
					break
				}
			}
			if !ewgwExisting {
				mesh2.Spec.ControlPlane.Peers = append(mesh2.Spec.ControlPlane.Peers, meshv1alpha1.Peer{Name: mesh1.GetName(), Cluster: mesh1.GetNamespace()})
				// only update mesh if needed
				_, _, err = meshresourceapply.ApplyMesh(ctx, c.meshClient.MeshV1alpha1(), c.recorder, mesh2)
				if err != nil {
					return err
				}
			}
		}

		return nil
	} else {
		//  retrieve smcp name and peer mesh name from the reconciling configmap name
		strSplit := strings.Split(name, "-ep4-")
		if len(strSplit) != 2 {
			return fmt.Errorf("invalid federation resource name: %s", name)
		}
		currentSMCPName := strSplit[0]
		peerMeshName := strSplit[1]

		meshList := &meshv1alpha1.MeshList{}
		meshList, err := c.meshClient.MeshV1alpha1().Meshes("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		currentMeshName, currentMeshCluster, currentMeshNamespace, currentMeshTrustDomain, peerMeshCluster, peerMeshNamespace := "", "", "", "", "", ""
		for _, mesh := range meshList.Items {
			meshName := ""
			discoveriedMesh, ok := mesh.GetLabels()[constants.LabelKeyForDiscoveriedMesh]
			if ok && discoveriedMesh == "true" {
				meshName = mesh.Spec.Cluster + "-" + mesh.Spec.ControlPlane.Namespace + "-" + currentSMCPName
			} else {
				meshName = currentSMCPName
			}
			if mesh.GetName() == meshName {
				currentMeshName = meshName
				currentMeshCluster = mesh.Spec.Cluster
				currentMeshNamespace = mesh.Spec.ControlPlane.Namespace
				currentMeshTrustDomain = mesh.Spec.TrustDomain
			} else if mesh.GetName() == peerMeshName {
				peerMeshCluster = mesh.Spec.Cluster
				peerMeshNamespace = mesh.Spec.ControlPlane.Namespace
			}
			if currentMeshCluster != "" && peerMeshCluster != "" {
				break
			}
		}

		endpointConfigMap, err := c.configMapLister.ConfigMaps(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		meshCAConfigMap, err := c.configMapLister.ConfigMaps(namespace).Get(currentMeshNamespace + "-mesh-ca")
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		// create configmap that contains mesh federation information
		federationConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      peerMeshName + "-to-" + currentMeshName,
				Namespace: peerMeshCluster,
				Labels: map[string]string{
					constants.FederationResourcesLabelKey: "true",
				},
			},
			Data: map[string]string{ // need to reverse the source and target mesh in federation configmap
				constants.FederationConfigMapMeshPeerCALabelKey:          meshCAConfigMap.Data[constants.IstioCAConfigmapKey],
				constants.FederationConfigMapMeshPeerEndpointLabelKey:    endpointConfigMap.Data[constants.FederationConfigMapMeshPeerEndpointLabelKey],
				constants.FederationConfigMapMeshPeerTrustDomainLabelKey: currentMeshTrustDomain,
				constants.FederationConfigMapMeshPeerNamespaceLabelKey:   currentMeshNamespace,
				constants.FederationConfigMapMeshNamespaceLabelKey:       peerMeshNamespace,
			},
		}

		_, _, err = resourceapply.ApplyConfigMap(ctx, c.kubeClient.CoreV1(), c.recorder, federationConfigMap)
		return err
	}
}

func (c *meshFederationController) removeMeshFederationResources(ctx context.Context, meshFederation *meshv1alpha1.MeshFederation) error {
	// remove east-west gateways for mesh peers
	meshPeers := meshFederation.Spec.MeshPeers
	for _, meshPeer := range meshPeers {
		peers := meshPeer.Peers
		//TODO(morvencao): add validation webhook to validate the meshFederation resource
		if peers == nil || len(peers) != 2 || peers[0].Name+peers[0].Cluster == peers[1].Name+peers[1].Cluster {
			return fmt.Errorf("two different meshes must specified in peers")
		}

		mesh1, mesh2 := &meshv1alpha1.Mesh{}, &meshv1alpha1.Mesh{}
		mesh1, err := c.meshClient.MeshV1alpha1().Meshes(peers[0].Cluster).Get(context.TODO(), peers[0].Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		mesh2, err = c.meshClient.MeshV1alpha1().Meshes(peers[1].Cluster).Get(context.TODO(), peers[1].Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		copiedPeers := []meshv1alpha1.Peer{}
		for _, peer := range mesh1.Spec.ControlPlane.Peers {
			if peer.Name == mesh2.GetName() && peer.Cluster == mesh2.GetNamespace() {
				continue
			}
			copiedPeers = append(copiedPeers, peer)
		}
		if len(copiedPeers) != len(mesh1.Spec.ControlPlane.Peers) {
			mesh1.Spec.ControlPlane.Peers = copiedPeers
			_, err := c.meshClient.MeshV1alpha1().Meshes(mesh1.GetNamespace()).Update(context.TODO(), mesh1, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}

		copiedPeers = []meshv1alpha1.Peer{}
		for _, peer := range mesh2.Spec.ControlPlane.Peers {
			if peer.Name == mesh1.GetName() && peer.Cluster == mesh1.GetNamespace() {
				continue
			}
			copiedPeers = append(copiedPeers, peer)
		}
		if len(copiedPeers) != len(mesh2.Spec.ControlPlane.Peers) {
			mesh2.Spec.ControlPlane.Peers = copiedPeers
			_, err := c.meshClient.MeshV1alpha1().Meshes(mesh2.GetNamespace()).Update(context.TODO(), mesh2, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *meshFederationController) removeMeshFederationFinalizer(ctx context.Context, meshFederation *meshv1alpha1.MeshFederation) error {
	copiedFinalizers := []string{}
	for _, finalizer := range meshFederation.Finalizers {
		if finalizer == meshFederationFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, finalizer)
	}

	if len(meshFederation.Finalizers) != len(copiedFinalizers) {
		meshFederation.Finalizers = copiedFinalizers
		_, err := c.meshClient.MeshV1alpha1().MeshFederations(meshFederation.GetNamespace()).Update(ctx, meshFederation, metav1.UpdateOptions{})
		return err
	}

	return nil
}
