package discovery

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/stolostron/multicluster-mesh-addon/pkg/translate"
)

type istioDiscoveryController struct {
	clusterName        string
	addonNamespace     string
	hubMeshClient      meshclientset.Interface
	spokeKubeClient    kubernetes.Interface
	spokeGenericLister cache.GenericLister
	recorder           events.Recorder
}

func NewIstioDiscoveryController(
	clusterName string,
	addonNamespace string,
	hubMeshClient meshclientset.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeGenericInformer informers.GenericInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &istioDiscoveryController{
		clusterName:        clusterName,
		addonNamespace:     addonNamespace,
		hubMeshClient:      hubMeshClient,
		spokeKubeClient:    spokeKubeClient,
		spokeGenericLister: spokeGenericInformer.Lister(),
		recorder:           recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, spokeGenericInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-istio-discovery-controller", recorder)
}

func (c *istioDiscoveryController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(2).Infof("Reconciling IstioOperator %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	obj, err := c.spokeGenericLister.ByNamespace(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	uns, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("can't convert obj %q to unstructured", key)
	}

	// handling the istiooperator deleting
	if !uns.GetDeletionTimestamp().IsZero() {
		discoveriedMeshName := c.clusterName + "-" + uns.GetNamespace() + "-" + uns.GetName()
		// try to delete the mesh if it is discoveried mesh, if not found the mesh, then the mesh is not discoveried mesh, just ignore the error
		klog.V(2).Infof("trying to remove discoveried mesh %q/%q if existing", c.clusterName, discoveriedMeshName)
		err := c.hubMeshClient.MeshV1alpha1().Meshes(c.clusterName).Delete(ctx, discoveriedMeshName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	istioOperator := &iopv1alpha1.IstioOperator{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), istioOperator)
	if err != nil {
		return err
	}

	userCreatedMeshList, err := c.hubMeshClient.MeshV1alpha1().Meshes(c.clusterName).List(ctx, metav1.ListOptions{LabelSelector: constants.LabelKeyForDiscoveriedMesh + "!=true"})
	isUserCreatedMesh := false
	for _, userCreatedMesh := range userCreatedMeshList.Items {
		// skip user created mesh for discovery
		if (userCreatedMesh.GetName() == name || userCreatedMesh.GetName()+"-gateways" == name) && userCreatedMesh.Spec.ControlPlane.Namespace == namespace && userCreatedMesh.Spec.ControlPlane.Revision == istioOperator.Spec.Revision {
			isUserCreatedMesh = true
			break
		}
	}
	if isUserCreatedMesh {
		return nil
	}

	memberNamespaces := []string{}
	memberNamespacesSelector := "istio-injection=enabled"
	if istioOperator.Spec.Revision != "" {
		memberNamespacesSelector = memberNamespacesSelector + ",istio.io/rev=" + istioOperator.Spec.Revision
	}
	namespaceList, err := c.spokeKubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: memberNamespacesSelector})
	if err != nil {
		return err
	}
	for _, ns := range namespaceList.Items {
		memberNamespaces = append(memberNamespaces, ns.GetName())
	}

	mesh, err := meshtranslate.TranslateIstioToLogicMesh(istioOperator, memberNamespaces, c.clusterName)
	if err != nil {
		return err
	}

	klog.V(2).Infof("applying discoveried mesh %q/%q", mesh.GetNamespace(), mesh.GetName())
	_, _, err = meshresourceapply.ApplyMesh(ctx, c.hubMeshClient.MeshV1alpha1(), c.recorder, mesh)
	return err
}
