package deploy

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	maistraclientset "maistra.io/api/client/versioned"

	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/stolostron/multicluster-mesh-addon/pkg/translate"
)

type deployController struct {
	clusterName        string
	addonNamespace     string
	hubMeshLister      meshv1alpha1lister.MeshLister
	spokeKubeClient    kubernetes.Interface
	spokeMaistraClient maistraclientset.Interface
	recorder           events.Recorder
}

func NewDeployController(
	clusterName string,
	addonNamespace string,
	meshInformer meshv1alpha1informer.MeshInformer,
	spokeKubeClient kubernetes.Interface,
	spokeMaistraClient maistraclientset.Interface,
	recorder events.Recorder,
) factory.Controller {
	c := &deployController{
		clusterName:        clusterName,
		addonNamespace:     addonNamespace,
		hubMeshLister:      meshInformer.Lister(),
		spokeKubeClient:    spokeKubeClient,
		spokeMaistraClient: spokeMaistraClient,
		recorder:           recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, meshInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-mesh-deploy-controller", recorder)
}

func (c *deployController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(2).Infof("Reconciling mesh %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	mesh, err := c.hubMeshLister.Meshes(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	smcp, smmr, err := meshtranslate.TranslateToPhysicalMesh(mesh)
	if err != nil {
		return err
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: smcp.GetNamespace()}}
	_, _, err = resourceapply.ApplyNamespace(ctx, c.spokeKubeClient.CoreV1(), c.recorder, ns)
	if err != nil {
		return err
	}

	_, _, err = meshresourceapply.ApplyServiceMeshControlPlane(ctx, c.spokeMaistraClient.CoreV2(), c.recorder, smcp)
	if err != nil {
		return err
	}

	_, _, err = meshresourceapply.ApplyServiceMeshMemberRoll(ctx, c.spokeMaistraClient.CoreV1(), c.recorder, smmr)
	return err
}
