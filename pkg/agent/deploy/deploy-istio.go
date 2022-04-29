package deploy

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	istioclientset "istio.io/client-go/pkg/clientset/versioned"
	istioctlcmd "istio.io/istio/istioctl/cmd"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshresourceapply "github.com/stolostron/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/stolostron/multicluster-mesh-addon/pkg/translate"
)

const istioMeshFinalizer = "mesh.open-cluster-management.io/istio-mesh-resources-cleanup"

type istioDeployController struct {
	clusterName        string
	addonNamespace     string
	hubMeshClient      meshclientset.Interface
	hubMeshLister      meshv1alpha1lister.MeshLister
	spokeDynamicClient dynamic.Interface
	spokeKubeClient    kubernetes.Interface
	spokeIstioClient   istioclientset.Interface
	recorder           events.Recorder
}

func NewIstioDeployController(
	clusterName string,
	addonNamespace string,
	hubMeshClient meshclientset.Interface,
	meshInformer meshv1alpha1informer.MeshInformer,
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeIstioClient istioclientset.Interface,
	recorder events.Recorder,
) factory.Controller {
	c := &istioDeployController{
		clusterName:        clusterName,
		addonNamespace:     addonNamespace,
		hubMeshClient:      hubMeshClient,
		hubMeshLister:      meshInformer.Lister(),
		spokeDynamicClient: spokeDynamicClient,
		spokeKubeClient:    spokeKubeClient,
		spokeIstioClient:   spokeIstioClient,
		recorder:           recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				key, _ := cache.MetaNamespaceKeyFunc(obj)
				return key
			}, meshInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-istio-deploy-controller", recorder)
}

func (c *istioDeployController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
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

	if mesh.Spec.MeshProvider != meshv1alpha1.MeshProviderUpstreamIstio {
		return nil
	}

	labels := mesh.GetLabels()
	isDiscoveriedMesh, ok := labels[constants.LabelKeyForDiscoveriedMesh]
	if ok && isDiscoveriedMesh == "true" {
		return nil
	}

	mesh = mesh.DeepCopy()
	if mesh.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range mesh.Finalizers {
			if mesh.Finalizers[i] == istioMeshFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			mesh.Finalizers = append(mesh.Finalizers, istioMeshFinalizer)
			klog.V(2).Infof("adding finalizer %q to mesh %q/%q", istioMeshFinalizer, namespace, name)
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

	if mesh.Spec.ControlPlane == nil {
		return fmt.Errorf("empty controlPlane field in mesh spec: %v", mesh)
	}
	if err := c.applyIstioOperator(ctx, mesh.Spec.ControlPlane.Version, mesh.Spec.ControlPlane.Revision, mesh.Spec.ControlPlane.Namespace); err != nil {
		return nil
	}

	controlPlaneIOP, gatewaysIOP, eastwestgw, err := meshtranslate.TranslateToPhysicalIstio(mesh)
	if err != nil {
		return err
	}

	klog.V(2).Infof("applying istiooperator cr for control plane: %q/%q", controlPlaneIOP.GetNamespace(), controlPlaneIOP.GetName())
	cpIOPObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(controlPlaneIOP)
	if err != nil {
		return err
	}
	_, _, err = meshresourceapply.ApplyIstioOperator(ctx, c.spokeDynamicClient, c.recorder, &unstructured.Unstructured{Object: cpIOPObj})
	if err != nil {
		return err
	}

	klog.V(2).Infof("wait until the istiod is started up and running before install gateway resource...")
	istiodNamespace := mesh.Spec.ControlPlane.Namespace
	istiodName := "istiod"
	if mesh.Spec.ControlPlane.Revision != "" {
		istiodName = istiodName + "-" + mesh.Spec.ControlPlane.Revision
	}
	err = wait.Poll(5*time.Second, 300*time.Second, func() (bool, error) {
		istiod, err := c.spokeKubeClient.AppsV1().Deployments(istiodNamespace).Get(ctx, istiodName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if istiod.Status.ReadyReplicas != istiod.Status.Replicas || istiod.Status.UpdatedReplicas != istiod.Status.Replicas {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("timeout wait for the istiod(%q/%q) is started", istiodNamespace, istiodName)
	}

	if gatewaysIOP != nil {
		klog.V(2).Infof("applying istiooperator cr for gateways: %q/%q", gatewaysIOP.GetNamespace(), gatewaysIOP.GetName())
		gwIOPObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(gatewaysIOP)
		if err != nil {
			return err
		}
		_, _, err = meshresourceapply.ApplyIstioOperator(ctx, c.spokeDynamicClient, c.recorder, &unstructured.Unstructured{Object: gwIOPObj})
		if err != nil {
			return err
		}
	}

	if eastwestgw != nil {
		klog.V(2).Infof("applying eastwest Gateway resource: %q/%q", eastwestgw.GetNamespace(), eastwestgw.GetName())
		err = wait.Poll(5*time.Second, 300*time.Second, func() (bool, error) {
			_, _, err = meshresourceapply.ApplyIstioGateway(ctx, c.spokeIstioClient.NetworkingV1alpha3(), c.recorder, eastwestgw)
			if err != nil {
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("timeout for applying eastwest Gateway resource: %q/%q", eastwestgw.GetNamespace(), eastwestgw.GetName())
		}
	} else {
		// try to clean up the istio gateway resource if the eastwest gateway exists
		klog.V(2).Infof("trying to remove eastwest Gateway resource: %q/%q for current mesh: %q/%q", mesh.Spec.ControlPlane.Namespace, "cross-network-gateway", mesh.GetNamespace(), mesh.GetName())
		err := c.spokeIstioClient.NetworkingV1alpha3().Gateways(mesh.Spec.ControlPlane.Namespace).Delete(ctx, "cross-network-gateway", metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (c *istioDeployController) applyIstioOperator(ctx context.Context, version, revision, controlPlaneNamespace string) error {
	klog.V(2).Infof("installing istio operator in version: %s revision: %s in ns: %s", version, revision, controlPlaneNamespace)
	operatorInitCmd := []string{
		"operator",
		"init",
		"--hub=docker.io/istio",
	}
	if version != "" {
		operatorInitCmd = append(operatorInitCmd, "--tag="+version)
	}
	if revision != "" {
		operatorInitCmd = append(operatorInitCmd, "--revision="+revision)
	}
	if controlPlaneNamespace != "" {
		operatorInitCmd = append(operatorInitCmd, "--operatorNamespace="+controlPlaneNamespace, "--watchedNamespaces="+controlPlaneNamespace)
	}

	istioCtl := istioctlcmd.GetRootCmd(operatorInitCmd)
	var outBuf, errBuf bytes.Buffer
	istioCtl.SetOut(&outBuf)
	istioCtl.SetErr(&errBuf)
	err := istioCtl.Execute()
	if err != nil {
		klog.V(2).Infof("Unwanted exception for 'istioctl %s': %v", strings.Join(operatorInitCmd, " "), err)
		klog.V(2).Infof("Output:\n%v", outBuf.String())
		klog.V(2).Infof("Error:\n%v", errBuf.String())
	}

	return err
}

func (c *istioDeployController) removeIstioOperator(ctx context.Context, revision, controlPlaneNamespace string) error {
	klog.V(2).Infof("removing istio operator with revision: %s in ns: %s", revision, controlPlaneNamespace)
	operatorRemoveCmd := []string{
		"operator",
		"remove",
	}
	if revision != "" {
		operatorRemoveCmd = append(operatorRemoveCmd, "--revision="+revision)
	}
	if controlPlaneNamespace != "" {
		operatorRemoveCmd = append(operatorRemoveCmd, "--operatorNamespace="+controlPlaneNamespace)
	}

	istioCtl := istioctlcmd.GetRootCmd(operatorRemoveCmd)
	var outBuf, errBuf bytes.Buffer
	istioCtl.SetOut(&outBuf)
	istioCtl.SetErr(&errBuf)
	err := istioCtl.Execute()
	if err != nil {
		klog.V(2).Infof("Unwanted exception for 'istioctl %s': %v", strings.Join(operatorRemoveCmd, " "), err)
		klog.V(2).Infof("Output:\n%v", outBuf.String())
		klog.V(2).Infof("Error:\n%v", errBuf.String())
	}

	return err
}

func (c *istioDeployController) removeMeshResources(ctx context.Context, mesh *meshv1alpha1.Mesh) error {
	iopGVR := schema.GroupVersionResource{Group: "install.istio.io", Version: "v1alpha1", Resource: "istiooperators"}
	labels := mesh.GetLabels()
	isDiscoveriedMesh, ok := labels[constants.LabelKeyForDiscoveriedMesh]
	if ok && isDiscoveriedMesh == "true" {
		// for discoveried mesh, won't remove the related resources
		return nil
	}

	controlPlaneIOP, gatewaysIOP, eastwestgw, err := meshtranslate.TranslateToPhysicalIstio(mesh)
	if err != nil {
		return err
	}

	if eastwestgw != nil {
		klog.V(2).Infof("removing eastwest Gateway resource: %q/%q", eastwestgw.GetNamespace(), eastwestgw.GetName())
		err = c.spokeIstioClient.NetworkingV1alpha3().Gateways(eastwestgw.GetNamespace()).Delete(ctx, eastwestgw.GetName(), metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

		err = wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
			_, err := c.spokeIstioClient.NetworkingV1alpha3().Gateways(eastwestgw.GetNamespace()).Get(ctx, eastwestgw.GetName(), metav1.GetOptions{})
			if err != nil && errors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return err
		}
	}

	if gatewaysIOP != nil {
		klog.V(2).Infof("removing istiooperator cr for gateways: %q/%q", gatewaysIOP.GetNamespace(), gatewaysIOP.GetName())
		err = c.spokeDynamicClient.Resource(iopGVR).Namespace(gatewaysIOP.GetNamespace()).Delete(ctx, gatewaysIOP.GetName(), metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

		err = wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
			_, err := c.spokeDynamicClient.Resource(iopGVR).Namespace(gatewaysIOP.GetNamespace()).Get(ctx, gatewaysIOP.GetName(), metav1.GetOptions{})
			if err != nil && errors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return err
		}
	}

	klog.V(2).Infof("removing istiooperator cr for control plane: %q/%q", controlPlaneIOP.GetNamespace(), controlPlaneIOP.GetName())
	err = c.spokeDynamicClient.Resource(iopGVR).Namespace(controlPlaneIOP.GetNamespace()).Delete(ctx, controlPlaneIOP.GetName(), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		_, err := c.spokeDynamicClient.Resource(iopGVR).Namespace(controlPlaneIOP.GetNamespace()).Get(ctx, controlPlaneIOP.GetName(), metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	return c.removeIstioOperator(ctx, controlPlaneIOP.Spec.Revision, controlPlaneIOP.GetNamespace())
}

func (c *istioDeployController) removeMeshFinalizer(ctx context.Context, mesh *meshv1alpha1.Mesh) error {
	copiedFinalizers := []string{}
	for _, finalizer := range mesh.Finalizers {
		if finalizer == istioMeshFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, finalizer)
	}

	if len(mesh.Finalizers) != len(copiedFinalizers) {
		mesh.Finalizers = copiedFinalizers
		klog.V(2).Infof("removing finalizer %q from mesh %q/%q", istioMeshFinalizer, mesh.GetNamespace(), mesh.GetName())
		_, err := c.hubMeshClient.MeshV1alpha1().Meshes(mesh.GetNamespace()).Update(ctx, mesh, metav1.UpdateOptions{})
		return err
	}

	return nil
}
