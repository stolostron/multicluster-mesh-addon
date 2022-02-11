package deployment

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
)

type meshDeploymentController struct {
	meshClient           meshclientset.Interface
	meshDeploymentLister meshv1alpha1lister.MeshDeploymentLister
	recorder             events.Recorder
}

func NewMeshDeploymentController(
	meshClient meshclientset.Interface,
	meshDeploymentInformer meshv1alpha1informer.MeshDeploymentInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &meshDeploymentController{
		meshClient:           meshClient,
		meshDeploymentLister: meshDeploymentInformer.Lister(),
		recorder:             recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, meshDeploymentInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-meshdeployment-controller", recorder)
}

func (c *meshDeploymentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling meshdeployment %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	meshDeployment, err := c.meshDeploymentLister.MeshDeployments(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	for _, cluster := range meshDeployment.Spec.Clusters {
		trustDomain := "cluster.local" // default trust domain
		if meshDeployment.Spec.TrustDomain != "" {
			trustDomain = meshDeployment.Spec.TrustDomain
		}
		mesh := &meshv1alpha1.Mesh{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", cluster, meshDeployment.GetName()),
				Namespace: cluster,
			},
			Spec: meshv1alpha1.MeshSpec{
				MeshProvider:   meshDeployment.Spec.MeshProvider,
				Cluster:        cluster,
				ControlPlane:   meshDeployment.Spec.ControlPlane,
				MeshMemberRoll: meshDeployment.Spec.MeshMemberRoll,
				TrustDomain:    trustDomain,
			},
		}
		_, _, err = meshresourceapply.ApplyMesh(ctx, c.meshClient.MeshV1alpha1(), c.recorder, mesh)
		if err != nil {
			return err
		}
	}

	return nil
}
