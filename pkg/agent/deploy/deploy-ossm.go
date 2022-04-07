package deploy

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	olmv1 "github.com/operator-framework/api/pkg/operators/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	maistraclientset "maistra.io/api/client/versioned"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/stolostron/multicluster-mesh-addon/pkg/translate"
)

const ossmMeshFinalizer = "mesh.open-cluster-management.io/ossm-mesh-resources-cleanup"

type ossmDeployController struct {
	clusterName        string
	addonNamespace     string
	hubMeshClient      meshclientset.Interface
	hubMeshLister      meshv1alpha1lister.MeshLister
	spokeKubeClient    kubernetes.Interface
	spokeOLMClient     olmclientset.Interface
	spokeMaistraClient maistraclientset.Interface
	recorder           events.Recorder
}

func NewOSSMDeployController(
	clusterName string,
	addonNamespace string,
	hubMeshClient meshclientset.Interface,
	meshInformer meshv1alpha1informer.MeshInformer,
	spokeKubeClient kubernetes.Interface,
	spokeOLMClient olmclientset.Interface,
	spokeMaistraClient maistraclientset.Interface,
	recorder events.Recorder,
) factory.Controller {
	c := &ossmDeployController{
		clusterName:        clusterName,
		addonNamespace:     addonNamespace,
		hubMeshClient:      hubMeshClient,
		hubMeshLister:      meshInformer.Lister(),
		spokeKubeClient:    spokeKubeClient,
		spokeOLMClient:     spokeOLMClient,
		spokeMaistraClient: spokeMaistraClient,
		recorder:           recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, meshInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-ossm-deploy-controller", recorder)
}

func (c *ossmDeployController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(2).Infof("Reconciling Mesh %q", key)

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

	if mesh.Spec.MeshProvider != meshv1alpha1.MeshProviderOpenshift {
		return nil
	}

	mesh = mesh.DeepCopy()
	if mesh.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range mesh.Finalizers {
			if mesh.Finalizers[i] == ossmMeshFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			mesh.Finalizers = append(mesh.Finalizers, ossmMeshFinalizer)
			klog.V(2).Infof("adding finalizer %q to mesh %q/%q", ossmMeshFinalizer, namespace, name)
			_, err := c.hubMeshClient.MeshV1alpha1().Meshes(namespace).Update(ctx, mesh, metav1.UpdateOptions{})
			return err
		}
	}

	// remove mesh related resources after mesh is deleted
	if !mesh.DeletionTimestamp.IsZero() {
		if err := c.removeMeshResources(ctx, mesh); err != nil {
			return err
		}
		return c.removeMeshFinalizer(ctx, mesh)
	}

	if err := c.applyOSSMOperators(ctx); err != nil {
		return err
	}

	smcp, smmr, err := meshtranslate.TranslateToPhysicalOSSM(mesh)
	if err != nil {
		return err
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: smcp.GetNamespace()}}
	klog.V(2).Infof("applying service mesh namespace %q", smcp.GetNamespace())
	_, _, err = resourceapply.ApplyNamespace(ctx, c.spokeKubeClient.CoreV1(), c.recorder, ns)
	if err != nil {
		return err
	}

	klog.V(2).Infof("applying servicemeshcontrtolplane %q/%q", smcp.GetNamespace(), smcp.GetName())
	_, _, err = meshresourceapply.ApplyServiceMeshControlPlane(ctx, c.spokeMaistraClient.CoreV2(), c.recorder, smcp)
	if err != nil {
		return err
	}

	if smmr != nil {
		klog.V(2).Infof("applying servicemeshmemberroll %q/%q", smmr.GetNamespace(), smmr.GetName())
		_, _, err = meshresourceapply.ApplyServiceMeshMemberRoll(ctx, c.spokeMaistraClient.CoreV1(), c.recorder, smmr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ossmDeployController) applyOSSMOperators(ctx context.Context) error {
	elasticsearchOperatorNamespace := "openshift-operators-redhat" // elasticsearch-operator is recommended to be installed in openshift-operators-redhat namespace
	elasticsearchOperatorNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: elasticsearchOperatorNamespace}}
	klog.V(2).Infof("applying olm resources: namespace %q", elasticsearchOperatorNamespace)
	_, _, err := resourceapply.ApplyNamespace(ctx, c.spokeKubeClient.CoreV1(), c.recorder, elasticsearchOperatorNS)
	if err != nil {
		return err
	}

	elasticsearchOG := &olmv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      elasticsearchOperatorNamespace,
			Namespace: elasticsearchOperatorNamespace,
		},
		Spec: olmv1.OperatorGroupSpec{},
	}

	ogList, _ := c.spokeOLMClient.OperatorsV1().OperatorGroups(elasticsearchOperatorNamespace).List(ctx, metav1.ListOptions{})
	if ogList != nil && len(ogList.Items) == 0 {
		klog.V(2).Infof("applying olm resources: operatorgroup %q/%q", elasticsearchOperatorNamespace, elasticsearchOperatorNamespace)
		_, _, err = meshresourceapply.ApplyOperatorGroup(ctx, c.spokeOLMClient.OperatorsV1(), c.recorder, elasticsearchOG)
		if err != nil {
			return err
		}
	}

	elasticsearchSub := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "elasticsearch-operator",
			Namespace: elasticsearchOperatorNamespace,
		},
		Spec: &olmv1alpha1.SubscriptionSpec{
			Channel:                "stable-5.3", // remove the hard-coded channel
			Package:                "elasticsearch-operator",
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
		},
	}
	klog.V(2).Infof("applying olm resources: subscription %q/%q", elasticsearchOperatorNamespace, "elasticsearch-operator")
	_, _, err = meshresourceapply.ApplySubscription(ctx, c.spokeOLMClient.OperatorsV1alpha1(), c.recorder, elasticsearchSub)
	if err != nil {
		return err
	}

	jaegerSub := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jaeger-product",
			Namespace: "openshift-operators",
		},
		Spec: &olmv1alpha1.SubscriptionSpec{
			Channel:                "stable",
			Package:                "jaeger-product",
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
		},
	}
	klog.V(2).Infof("applying olm resources: subscription %q/%q", "openshift-operators", "jaeger-product")
	_, _, err = meshresourceapply.ApplySubscription(ctx, c.spokeOLMClient.OperatorsV1alpha1(), c.recorder, jaegerSub)
	if err != nil {
		return err
	}

	kialiSub := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kiali-ossm",
			Namespace: "openshift-operators",
		},
		Spec: &olmv1alpha1.SubscriptionSpec{
			Channel:                "stable",
			Package:                "kiali-ossm",
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
		},
	}
	klog.V(2).Infof("applying olm resources: subscription %q/%q", "openshift-operators", "kiali-ossm")
	_, _, err = meshresourceapply.ApplySubscription(ctx, c.spokeOLMClient.OperatorsV1alpha1(), c.recorder, kialiSub)
	if err != nil {
		return err
	}

	ossmSub := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "servicemeshoperator",
			Namespace: "openshift-operators",
		},
		Spec: &olmv1alpha1.SubscriptionSpec{
			Channel:                "stable",
			Package:                "servicemeshoperator",
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
		},
	}
	klog.V(2).Infof("applying olm resources: subscription %q/%q", "openshift-operators", "servicemeshoperator")
	_, _, err = meshresourceapply.ApplySubscription(ctx, c.spokeOLMClient.OperatorsV1alpha1(), c.recorder, ossmSub)
	if err != nil {
		return err
	}

	err = wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		csvList, err := c.spokeOLMClient.OperatorsV1alpha1().ClusterServiceVersions("openshift-operators").List(ctx, metav1.ListOptions{LabelSelector: "operators.coreos.com/servicemeshoperator.openshift-operators="})
		if err != nil {
			return false, err
		}
		if len(csvList.Items) != 1 {
			return false, nil
		}
		if csvList.Items[0].Status.Phase != olmv1alpha1.CSVPhaseSucceeded {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *ossmDeployController) removeMeshResources(ctx context.Context, mesh *meshv1alpha1.Mesh) error {
	labels := mesh.GetLabels()
	discoveriedMesh, ok := labels[constants.LabelKeyForDiscoveriedMesh]
	if ok && discoveriedMesh == "true" {
		// for discoveried mesh, won't remove the related resources
		return nil
	}

	smcp, smmr, err := meshtranslate.TranslateToPhysicalOSSM(mesh)
	if err != nil {
		return err
	}

	klog.V(2).Infof("removing servicemeshcontrtolplane %q/%q", smcp.GetNamespace(), smcp.GetName())
	err = c.spokeMaistraClient.CoreV2().ServiceMeshControlPlanes(smcp.GetNamespace()).Delete(ctx, smcp.GetName(), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	klog.V(2).Infof("removing servicemeshmemberroll %q/%q", smmr.GetNamespace(), smmr.GetName())
	err = c.spokeMaistraClient.CoreV1().ServiceMeshMemberRolls(smmr.GetNamespace()).Delete(ctx, smmr.GetName(), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func (c *ossmDeployController) removeMeshFinalizer(ctx context.Context, mesh *meshv1alpha1.Mesh) error {
	copiedFinalizers := []string{}
	for _, finalizer := range mesh.Finalizers {
		if finalizer == ossmMeshFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, finalizer)
	}

	if len(mesh.Finalizers) != len(copiedFinalizers) {
		mesh.Finalizers = copiedFinalizers
		klog.V(2).Infof("removing finalizer %q from mesh %q/%q", ossmMeshFinalizer, mesh.GetNamespace(), mesh.GetName())
		_, err := c.hubMeshClient.MeshV1alpha1().Meshes(mesh.GetNamespace()).Update(ctx, mesh, metav1.UpdateOptions{})
		return err
	}

	return nil
}
