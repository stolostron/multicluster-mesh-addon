package discovery

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	maistrav1informer "maistra.io/api/client/informers/externalversions/core/v1"
	maistrav2informer "maistra.io/api/client/informers/externalversions/core/v2"
	maistrav1lister "maistra.io/api/client/listers/core/v1"
	maistrav2lister "maistra.io/api/client/listers/core/v2"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/stolostron/multicluster-mesh-addon/pkg/translate"
)

type discoveryController struct {
	clusterName     string
	addonNamespace  string
	hubMeshClient   meshclientset.Interface
	spokeSMCPLister maistrav2lister.ServiceMeshControlPlaneLister
	spokeSMMRLister maistrav1lister.ServiceMeshMemberRollLister
	recorder        events.Recorder
}

func NewDiscoveryController(
	clusterName string,
	addonNamespace string,
	hubMeshClient meshclientset.Interface,
	smcpInformer maistrav2informer.ServiceMeshControlPlaneInformer,
	smmrInformer maistrav1informer.ServiceMeshMemberRollInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &discoveryController{
		clusterName:     clusterName,
		addonNamespace:  addonNamespace,
		hubMeshClient:   hubMeshClient,
		spokeSMCPLister: smcpInformer.Lister(),
		spokeSMMRLister: smmrInformer.Lister(),
		recorder:        recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, smcpInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-mesh-discovery-controller", recorder)
}

func (c *discoveryController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(2).Infof("Reconciling SMCP %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	smcp, err := c.spokeSMCPLister.ServiceMeshControlPlanes(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// handling the smcp deleting
	if !smcp.DeletionTimestamp.IsZero() {
		discoveriedMeshName := c.clusterName + "-" + smcp.GetNamespace() + "-" + smcp.GetName()
		err := c.hubMeshClient.MeshV1alpha1().Meshes(c.clusterName).Delete(ctx, discoveriedMeshName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	// smmr named "default" in the namespace
	smmr, err := c.spokeSMMRLister.ServiceMeshMemberRolls(namespace).Get("default")
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	userCreatedMeshList, err := c.hubMeshClient.MeshV1alpha1().Meshes(c.clusterName).List(context.TODO(), metav1.ListOptions{LabelSelector: constants.LabelKeyForDiscoveriedMesh + "!=true"})
	isUserCreatedMesh := false
	for _, userCreatedMesh := range userCreatedMeshList.Items {
		// skip user created mesh for discovery
		if userCreatedMesh.GetName() == name && userCreatedMesh.Spec.ControlPlane.Namespace == namespace {
			isUserCreatedMesh = true
			break
		}
	}
	if isUserCreatedMesh {
		return nil
	}

	mesh, err := meshtranslate.TranslateToLogicMesh(smcp, smmr, c.clusterName)
	if err != nil {
		return err
	}

	_, _, err = meshresourceapply.ApplyMesh(ctx, c.hubMeshClient.MeshV1alpha1(), c.recorder, mesh)
	return err
}
