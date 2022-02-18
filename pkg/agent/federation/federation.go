package federation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	maistraclientset "maistra.io/api/client/versioned"
	maistrafederationv1 "maistra.io/api/federation/v1"

	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
)

type federationController struct {
	clusterName          string
	addonNamespace       string
	hubKubeClient        kubernetes.Interface
	spokeKubeClient      kubernetes.Interface
	spokeMaistraClient   maistraclientset.Interface
	hubConfigMapLister   corev1lister.ConfigMapLister
	spokeServiceLister   corev1lister.ServiceLister
	spokeConfigMapLister corev1lister.ConfigMapLister
	recorder             events.Recorder
}

func NewFederationController(
	clusterName string,
	addonNamespace string,
	hubKubeClient kubernetes.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeMaistraClient maistraclientset.Interface,
	hubConfigMapInformer corev1informer.ConfigMapInformer,
	spokeServiceInformer corev1informer.ServiceInformer,
	spokeConfigMapInformer corev1informer.ConfigMapInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &federationController{
		clusterName:          clusterName,
		addonNamespace:       addonNamespace,
		hubKubeClient:        hubKubeClient,
		spokeKubeClient:      spokeKubeClient,
		spokeMaistraClient:   spokeMaistraClient,
		hubConfigMapLister:   hubConfigMapInformer.Lister(),
		spokeServiceLister:   spokeServiceInformer.Lister(),
		spokeConfigMapLister: spokeConfigMapInformer.Lister(),
		recorder:             recorder,
	}
	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a service with label key "federation.maistra.io/ingress-for"
			labels := accessor.GetLabels()
			_, ok := labels[constants.FederationServiceLabelKey]
			return ok
		}, spokeServiceInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a configmap with label "istio.io/config=true"
			labels := accessor.GetLabels()
			lv, ok := labels[constants.IstioCAConfigmapLabel]
			return ok && (lv == "true")
		}, spokeConfigMapInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue a configmap with label "mesh.open-cluster.io/federation=true"
			labels := accessor.GetLabels()
			lv, ok := labels[constants.FederationResourcesLabelKey]
			return ok && (lv == "true") && strings.Contains(accessor.GetName(), "-to-")
		}, hubConfigMapInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-mesh-federation-controller", recorder)
}

func (c *federationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(2).Infof("Reconciling federation resources %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	if strings.Contains(name, "-to-") { // federation configmap from cluster namespace
		federationConfigMap, err := c.hubConfigMapLister.ConfigMaps(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		sourceMeshNamespace, ok := federationConfigMap.Data[constants.FederationConfigMapMeshNamespaceLabelKey]
		if !ok {
			return fmt.Errorf("no source mesh namespace found in federation configmap :%s/%s", namespace, name)
		}
		targetMeshNamespace, ok := federationConfigMap.Data[constants.FederationConfigMapMeshPeerNamespaceLabelKey]
		if !ok {
			return fmt.Errorf("no target mesh namespace found in federation configmap :%s/%s", namespace, name)
		}
		targetMeshCA, ok := federationConfigMap.Data[constants.FederationConfigMapMeshPeerCALabelKey]
		if !ok {
			return fmt.Errorf("no target mesh CA found in federation configmap :%s/%s", namespace, name)
		}
		targetMeshEndpoint, ok := federationConfigMap.Data[constants.FederationConfigMapMeshPeerEndpointLabelKey]
		if !ok {
			return fmt.Errorf("no target mesh endpoint found in federation configmap :%s/%s", namespace, name)
		}
		targetMeshTrustDomain, ok := federationConfigMap.Data[constants.FederationConfigMapMeshPeerTrustDomainLabelKey]
		if !ok {
			return fmt.Errorf("no target mesh trust domain found in federation configmap :%s/%s", namespace, name)
		}

		strSplit := strings.Split(name, "-to-")
		if len(strSplit) != 2 {
			return fmt.Errorf("invalid federation configmap name: %s", name)
		}
		sourceMeshName := strSplit[0]
		targetMeshName := strSplit[1]

		meshPeerCAConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetMeshName + "-ca-root-cert",
				Namespace: sourceMeshNamespace,
			},
			Data: map[string]string{
				constants.IstioCAConfigmapKey: targetMeshCA,
			},
		}

		klog.V(2).Infof("applying federation resources: configmap %q/%q", sourceMeshNamespace, targetMeshName+"-ca-root-cert")
		_, _, err = resourceapply.ApplyConfigMap(ctx, c.spokeKubeClient.CoreV1(), c.recorder, meshPeerCAConfigMap)
		if err != nil {
			return err
		}

		serviceMeshPeer := &maistrafederationv1.ServiceMeshPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetMeshName,
				Namespace: sourceMeshNamespace,
			},
			Spec: maistrafederationv1.ServiceMeshPeerSpec{
				Remote: maistrafederationv1.ServiceMeshPeerRemote{
					Addresses:     []string{targetMeshEndpoint},
					DiscoveryPort: 8188,
					ServicePort:   15443,
				},
				Gateways: maistrafederationv1.ServiceMeshPeerGateways{
					Ingress: corev1.LocalObjectReference{
						Name: targetMeshName + "-ingress",
					},
					Egress: corev1.LocalObjectReference{
						Name: targetMeshName + "-egress",
					},
				},
				Security: maistrafederationv1.ServiceMeshPeerSecurity{
					ClientID:    targetMeshTrustDomain + "/ns/" + targetMeshNamespace + "/sa/" + sourceMeshName + "-egress-service-account",
					TrustDomain: targetMeshTrustDomain,
					CertificateChain: corev1.TypedLocalObjectReference{
						Kind: "ConfigMap",
						Name: targetMeshName + "-ca-root-cert",
					},
				},
			},
		}
		klog.V(2).Infof("applying federation resources: servicemeshpeer %q/%q", sourceMeshNamespace, targetMeshName)
		_, _, err = meshresourceapply.ApplyServiceMeshPeer(ctx, c.spokeMaistraClient.FederationV1(), c.recorder, serviceMeshPeer)
		return err
	} else if name == constants.IstioCAConfigmapName { // ca configmap from mesh control plane namespace
		meshCAConfigMap, err := c.spokeConfigMapLister.ConfigMaps(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		newMeshCAConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      meshCAConfigMap.GetNamespace() + "-mesh-ca",
				Namespace: c.clusterName,
				Labels: map[string]string{
					constants.FederationResourcesLabelKey: "true",
				},
			},
			Data: meshCAConfigMap.Data,
		}
		klog.V(2).Infof("applying federation resources: configmap %q/%q", c.clusterName, meshCAConfigMap.GetNamespace()+"-mesh-ca")
		_, _, err = resourceapply.ApplyConfigMap(ctx, c.hubKubeClient.CoreV1(), c.recorder, newMeshCAConfigmap)
		return err
	} else { // ingressgateway service from mesh control plane namespace
		ingSvc, err := c.spokeServiceLister.Services(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		svcLabels := ingSvc.GetLabels()
		peerMeshName := svcLabels[constants.FederationServiceLabelKey]
		smcpName := ""
		for _, ref := range ingSvc.GetOwnerReferences() {
			if ref.Kind == "ServiceMeshControlPlane" {
				smcpName = ref.Name
				break
			}
		}

		// remove ingress svc related resources after mesh is deleted
		if !ingSvc.DeletionTimestamp.IsZero() {
			return c.removeMeshFederationResources(ctx, namespace, smcpName, peerMeshName)
		}

		endpointAddr := ""
		err = wait.Poll(5*time.Second, 60*time.Second, func() (done bool, err error) {
			ingSvc, err = c.spokeServiceLister.Services(namespace).Get(name)
			if err != nil {
				return false, err
			}
			if len(ingSvc.Status.LoadBalancer.Ingress) <= 0 {
				return false, nil
			}
			if ingSvc.Status.LoadBalancer.Ingress[0].IP != "" {
				endpointAddr = ingSvc.Status.LoadBalancer.Ingress[0].IP
				return true, nil
			} else if ingSvc.Status.LoadBalancer.Ingress[0].Hostname != "" {
				endpointAddr = ingSvc.Status.LoadBalancer.Ingress[0].Hostname
				return true, nil
			} else {
				return false, nil
			}
		})
		if err != nil {
			return err
		}

		ingressEndpointConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      smcpName + "-ep4-" + peerMeshName, // smcp ingress endpoint for peer mesh
				Namespace: c.clusterName,
				Labels: map[string]string{
					constants.FederationResourcesLabelKey: "true",
				},
			},
			Data: map[string]string{
				constants.FederationConfigMapMeshPeerEndpointLabelKey: endpointAddr,
			},
		}
		klog.V(2).Infof("applying federation resources: configmap %q/%q", c.clusterName, smcpName+"-ep4-"+peerMeshName)
		_, _, err = resourceapply.ApplyConfigMap(ctx, c.hubKubeClient.CoreV1(), c.recorder, ingressEndpointConfigmap)
		return err
	}
}

func (c *federationController) removeMeshFederationResources(ctx context.Context, namespace, smcpName, peerMeshName string) error {
	klog.V(2).Infof("removing federation resources: configmap %q/%q", c.clusterName, smcpName+"-ep4-"+peerMeshName)
	err := c.hubKubeClient.CoreV1().ConfigMaps(c.clusterName).Delete(ctx, smcpName+"-ep4-"+peerMeshName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	klog.V(2).Infof("removing federation resources: servicemeshpeer %q/%q", namespace, peerMeshName)
	err = c.spokeMaistraClient.FederationV1().ServiceMeshPeers(namespace).Delete(ctx, peerMeshName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	klog.V(2).Infof("removing federation resources: configmap %q/%q", namespace, peerMeshName+"-ca-root-cert")
	err = c.spokeKubeClient.CoreV1().ConfigMaps(namespace).Delete(ctx, peerMeshName+"-ca-root-cert", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}
