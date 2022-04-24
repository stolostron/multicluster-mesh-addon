package federation

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	istiospiffe "istio.io/istio/pkg/spiffe"
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

	meshv1alpha1informer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions/mesh/v1alpha1"
	meshv1alpha1lister "github.com/stolostron/multicluster-mesh-addon/apis/client/listers/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	certificate "github.com/stolostron/multicluster-mesh-addon/pkg/certificate"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
)

type istioFederationController struct {
	clusterName     string
	addonNamespace  string
	hubKubeClient   kubernetes.Interface
	spokeKubeClient kubernetes.Interface
	hubSecretLister corev1lister.SecretLister
	hubMeshLister   meshv1alpha1lister.MeshLister
	recorder        events.Recorder
}

func NewIstioFederationController(
	clusterName string,
	addonNamespace string,
	hubKubeClient kubernetes.Interface,
	spokeKubeClient kubernetes.Interface,
	hubSecretInformer corev1informer.SecretInformer,
	meshInformer meshv1alpha1informer.MeshInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &istioFederationController{
		clusterName:     clusterName,
		addonNamespace:  addonNamespace,
		hubKubeClient:   hubKubeClient,
		spokeKubeClient: spokeKubeClient,
		hubSecretLister: hubSecretInformer.Lister(),
		hubMeshLister:   meshInformer.Lister(),
		recorder:        recorder,
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, meshInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			// only enqueue secret with label "mesh.open-cluster.io/federation=true"
			labels := accessor.GetLabels()
			lv, ok := labels[constants.FederationResourcesLabelKey]
			return ok && (lv == "true") && strings.HasSuffix(accessor.GetName(), "-intermediateca")
		}, hubSecretInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-istio-federation-controller", recorder)
}

func (c *istioFederationController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	if strings.HasSuffix(key, "-intermediateca") {
		klog.V(2).Infof("Reconciling federation resources %q", key)
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			// ignore addon whose key is not in format: namespace/name
			return nil
		}

		intermediateCASecret, err := c.hubSecretLister.Secrets(namespace).Get(name)
		switch {
		case errors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}

		meshName := strings.TrimSuffix(name, "-intermediateca")
		mesh, err := c.hubMeshLister.Meshes(namespace).Get(meshName)
		if err != nil {
			return err
		}

		annotations := mesh.GetAnnotations()
		if annotations == nil {
			return fmt.Errorf("mesh(%q/%q) with peer should have federation owner annotation", mesh.GetNamespace(), mesh.GetName())
		}
		meshFederationOwner, ok := annotations[constants.AnnotationKeyForMeshFederationOwner]
		if !ok || meshFederationOwner == "" {
			return fmt.Errorf("mesh(%q/%q) with peer should have federation owner annotation", mesh.GetNamespace(), mesh.GetName())
		}

		privateKeySecretName := strings.Replace(name, "-intermediateca", "-privatekey", 1)
		privateKeySecretNamespace := mesh.Spec.ControlPlane.Namespace
		privateKeySecret, err := c.spokeKubeClient.CoreV1().Secrets(privateKeySecretNamespace).Get(ctx, privateKeySecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		intermediateCAData := intermediateCASecret.Data
		intermediateCAData["ca-key.pem"] = privateKeySecret.Data["ca-key.pem"]

		// finally create the intermediate CA for the mesh
		intermediateCAForMeshSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.IstioCASecretName,
				Namespace: privateKeySecretNamespace,
				Labels: map[string]string{
					constants.FederationResourcesLabelKey: "true",
				},
				Annotations: map[string]string{
					constants.AnnotationKeyForMeshFederationOwner: meshFederationOwner,
				},
			},
			Data: intermediateCAData,
			Type: corev1.SecretTypeOpaque,
		}

		klog.V(2).Infof("applying CA secret(%q/%q) for mesh(%q/%q)", intermediateCAForMeshSecret.GetNamespace(), intermediateCAForMeshSecret.GetName(), mesh.GetNamespace(), mesh.GetName())
		_, caChanged, err := resourceapply.ApplySecret(ctx, c.spokeKubeClient.CoreV1(), c.recorder, intermediateCAForMeshSecret)
		if err != nil {
			return err
		}
		if caChanged {
			// restart istiod pod(s) to take the new CA
			klog.V(2).Infof("restarting the istiod pod(s) to take the new CA for mesh(%q/%q)", mesh.GetNamespace(), mesh.GetName())
			return c.spokeKubeClient.CoreV1().Pods(mesh.Spec.ControlPlane.Namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istiod"})
		}
		return nil
	} else {
		klog.V(2).Infof("Reconciling mesh resources %q", key)

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

		// skip other mesh provider
		if mesh.Spec.MeshProvider != meshv1alpha1.MeshProviderUpstreamIstio {
			return nil
		}

		// if current mesh has peer(s), then create the certificate sign request to build trust with peer(s)
		if mesh.Spec.ControlPlane != nil && len(mesh.Spec.ControlPlane.Peers) > 0 {
			_, err1 := c.hubKubeClient.CoreV1().Secrets(mesh.Spec.Cluster).Get(ctx, mesh.GetName()+"-csr", metav1.GetOptions{})
			_, err2 := c.spokeKubeClient.CoreV1().Secrets(mesh.Spec.ControlPlane.Namespace).Get(ctx, mesh.GetName()+"-privatekey", metav1.GetOptions{})
			if errors.IsNotFound(err1) && errors.IsNotFound(err2) {
				csrSecret, privateKeySecret, err := c.buildCSRAndPrivateKeyForMesh(mesh)
				if err != nil {
					return err
				}
				klog.V(2).Infof("applying CSR secret(%q/%q) for mesh(%q/%q)", csrSecret.GetNamespace(), csrSecret.GetName(), mesh.GetNamespace(), mesh.GetName())
				if _, _, err = resourceapply.ApplySecret(ctx, c.hubKubeClient.CoreV1(), c.recorder, csrSecret); err != nil {
					return err
				}
				klog.V(2).Infof("applying private key secret(%q/%q) for mesh(%q/%q)", privateKeySecret.GetNamespace(), privateKeySecret.GetName(), mesh.GetNamespace(), mesh.GetName())
				if _, _, err = resourceapply.ApplySecret(ctx, c.spokeKubeClient.CoreV1(), c.recorder, privateKeySecret); err != nil {
					return err
				}
			}
		} else {
			// remove existing intermediate CA, CSR and privatekey secret when peers are removed
			klog.V(2).Infof("trying to remove intermediate CA, CSR and privatekey secrets if existing, because no peers for current mesh(%q/%q)", mesh.GetNamespace(), mesh.GetName())
			err := c.spokeKubeClient.CoreV1().Secrets(mesh.Spec.ControlPlane.Namespace).Delete(ctx, constants.IstioCASecretName, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			err = c.spokeKubeClient.CoreV1().Secrets(mesh.Spec.ControlPlane.Namespace).Delete(ctx, mesh.GetName()+"-privatekey", metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			err = c.hubKubeClient.CoreV1().Secrets(mesh.Spec.Cluster).Delete(ctx, mesh.GetName()+"-csr", metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			err = c.hubKubeClient.CoreV1().Secrets(mesh.Spec.Cluster).Delete(ctx, mesh.GetName()+"-intermediateca", metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
			err = c.spokeKubeClient.CoreV1().Pods(mesh.Spec.ControlPlane.Namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "app=istiod"})
			if err != nil {
				return err
			}
		}

		return nil
	}
}

func (c *istioFederationController) buildCSRAndPrivateKeyForMesh(mesh *meshv1alpha1.Mesh) (*corev1.Secret, *corev1.Secret, error) {
	annotations := mesh.GetAnnotations()
	if annotations == nil {
		return nil, nil, fmt.Errorf("mesh(%q/%q) with peer should have federation owner annotation", mesh.GetNamespace(), mesh.GetName())
	}
	meshFederationOwner, ok := annotations[constants.AnnotationKeyForMeshFederationOwner]
	if !ok || meshFederationOwner == "" {
		return nil, nil, fmt.Errorf("mesh(%q/%q) with peer should have federation owner annotation", mesh.GetNamespace(), mesh.GetName())
	}

	trustDomain := "cluster.local"
	if mesh.Spec.MeshConfig != nil && mesh.Spec.MeshConfig.TrustDomain != "" {
		trustDomain = mesh.Spec.MeshConfig.TrustDomain
	}
	spiffeURI := fmt.Sprintf("%s%s/ns/%s/sa/%s", istiospiffe.URIPrefix, trustDomain, mesh.Spec.ControlPlane.Namespace, "istiod-"+mesh.Spec.ControlPlane.Revision)
	hosts := []string{spiffeURI}

	// create the CSR and private key for the mesh
	klog.V(2).Infof("creating the CSR and private key for the mesh(%q/%q)", mesh.GetNamespace(), mesh.GetName())
	csrData, privateKeyData, err := certificate.BuildCSRAndPrivateKeyForIntermediateCert(hosts, mesh.GetName()+"-"+mesh.GetNamespace())
	if err != nil {
		return nil, nil, err
	}

	// CSR secret for the current mesh wil be created in managedcluster namespace of hub cluster
	csrSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mesh.GetName() + "-csr",
			Namespace: mesh.Spec.Cluster,
			Labels: map[string]string{
				constants.FederationResourcesLabelKey: "true",
			},
			Annotations: map[string]string{
				constants.AnnotationKeyForMeshFederationOwner: meshFederationOwner,
			},
		},
		Data: csrData,
		Type: corev1.SecretTypeOpaque,
	}

	// private secret will be kept in managed cluster, should never leave managedcluster for security concern
	privateKeySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mesh.GetName() + "-privatekey",
			Namespace: mesh.Spec.ControlPlane.Namespace,
			Labels: map[string]string{
				constants.FederationResourcesLabelKey: "true",
			},
			Annotations: map[string]string{
				constants.AnnotationKeyForMeshFederationOwner: meshFederationOwner,
			},
		},
		Data: privateKeyData,
		Type: corev1.SecretTypeOpaque,
	}

	return csrSecret, privateKeySecret, nil
}
