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

const meshDeploymentFinalizer = "mesh.open-cluster-management.io/meshdeployment-resources-cleanup"

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
	klog.V(2).Infof("Reconciling meshdeployment %q", key)

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

	meshDeployment = meshDeployment.DeepCopy()
	if meshDeployment.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range meshDeployment.Finalizers {
			if meshDeployment.Finalizers[i] == meshDeploymentFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			meshDeployment.Finalizers = append(meshDeployment.Finalizers, meshDeploymentFinalizer)
			klog.V(2).Infof("adding finalizer %q to meshdeployment %q/%q", meshDeploymentFinalizer, namespace, name)
			_, err := c.meshClient.MeshV1alpha1().MeshDeployments(namespace).Update(ctx, meshDeployment, metav1.UpdateOptions{})
			return err
		}
	}

	// remove meshdeployment related resources after meshdeployment is deleted
	if !meshDeployment.DeletionTimestamp.IsZero() {
		if err := c.removeMeshDeploymentResources(ctx, meshDeployment); err != nil {
			return err
		}
		return c.removeMeshDeploymentFinalizer(ctx, meshDeployment)
	}

	for _, cluster := range meshDeployment.Spec.Clusters {
		// add cluster name prefix to mesh name to distinguish meshes in mesh federation
		meshName := fmt.Sprintf("%s-%s", cluster, meshDeployment.GetName())
		// trust domain for each mesh need to be different, since the self generated CA for each mesh is different
		trustDomain := fmt.Sprintf("%s.local", meshName)
		if meshDeployment.Spec.MeshProvider == meshv1alpha1.MeshProviderCommunityIstio {
			// for community mesh, the trust is built wirth shared CA, so the trust domain should be the same
			// TODO(morvencao): fix the trust domain issue
			trustDomain = "cluster.local"
		}
		mesh := &meshv1alpha1.Mesh{
			ObjectMeta: metav1.ObjectMeta{
				Name:      meshName,
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

		_, err := c.meshClient.MeshV1alpha1().Meshes(cluster).Get(ctx, meshName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			klog.V(2).Infof("applying mesh %q/%q for meshdeployment", mesh.GetNamespace(), mesh.GetName())
			_, _, err = meshresourceapply.ApplyMesh(ctx, c.meshClient.MeshV1alpha1(), c.recorder, mesh)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func (c *meshDeploymentController) removeMeshDeploymentResources(ctx context.Context, meshDeployment *meshv1alpha1.MeshDeployment) error {
	for _, cluster := range meshDeployment.Spec.Clusters {
		meshName := fmt.Sprintf("%s-%s", cluster, meshDeployment.GetName())
		klog.V(2).Infof("removing mesh %q/%q for meshdeployment cleanup", cluster, meshName)
		if err := c.meshClient.MeshV1alpha1().Meshes(cluster).Delete(ctx, meshName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (c *meshDeploymentController) removeMeshDeploymentFinalizer(ctx context.Context, meshDeployment *meshv1alpha1.MeshDeployment) error {
	copiedFinalizers := []string{}
	for _, finalizer := range meshDeployment.Finalizers {
		if finalizer == meshDeploymentFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, finalizer)
	}

	if len(meshDeployment.Finalizers) != len(copiedFinalizers) {
		meshDeployment.Finalizers = copiedFinalizers
		klog.V(2).Infof("removing finalizer %q from meshdeployment %q/%q", meshDeploymentFinalizer, meshDeployment.GetNamespace(), meshDeployment.GetName())
		_, err := c.meshClient.MeshV1alpha1().MeshDeployments(meshDeployment.GetNamespace()).Update(ctx, meshDeployment, metav1.UpdateOptions{})
		return err
	}

	return nil
}
