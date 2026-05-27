package mesh

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

// ensureSecretManifestWork creates a ManifestWork to distribute the secret to a cluster
func (r *Reconciler) ensureSecretManifestWork(ctx context.Context, secretName string, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: mesh.Namespace,
	}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			if strings.HasPrefix(secretName, "cacerts-") {
				klog.V(4).Infof("Secret %s/%s not found yet, waiting for cert-manager to create it", mesh.Namespace, secretName)
			}
			if strings.HasSuffix(secretName, "-istio-reader") {
				klog.V(4).Infof("Secret %s/%s not found yet, waiting for ManagedServiceAccount to create it", mesh.Namespace, secretName)
			}
			return nil
		}
		return fmt.Errorf("failed to get secret: %w", err)
	}

	var manifestWorkName string
	if strings.HasPrefix(secret.Name, "cacerts-") {
		manifestWorkName = ManifestWorkNameCacerts
	}

	if strings.HasSuffix(secret.Name, "-istio-reader") {
		manifestWorkName = ManifestWorkNameMsaSecrets
	}

	existingWork := &workv1.ManifestWork{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      manifestWorkName,
		Namespace: cluster.Name,
	}, existingWork)

	if err == nil {
		klog.V(4).Infof("ManifestWork %s/%s already exists, checking if update is needed", cluster.Name, manifestWorkName)
		return r.updateSecretManifestWorkIfNeeded(ctx, mesh, existingWork, secret)
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManifestWork: %w", err)
	}

	if strings.HasPrefix(secretName, "cacerts-") {
		klog.Infof("Creating ManifestWork %s/%s to distribute cacerts secret", cluster.Name, manifestWorkName)
	}
	if strings.HasSuffix(secretName, "-istio-reader") {
		klog.Infof("Creating ManifestWork %s/%s to distribute ManagedServiceAccount secret", cluster.Name, manifestWorkName)
	}

	work := r.buildSecretManifestWork(mesh, cluster.Name, secret)

	if err := r.Create(ctx, work); err != nil {
		return fmt.Errorf("failed to create ManifestWork: %w", err)
	}

	klog.Infof("Successfully created ManifestWork %s/%s", cluster.Name, manifestWorkName)
	return nil
}

// buildSecretManifestWork builds a ManifestWork for distributing the secret
func (r *Reconciler) buildSecretManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, secret *corev1.Secret) *workv1.ManifestWork {
	var newSecretName, manifestWorkName string

	if strings.HasPrefix(secret.Name, "cacerts-") {
		newSecretName = CacertsSecretName
		manifestWorkName = ManifestWorkNameCacerts
	}

	if strings.HasSuffix(secret.Name, "-istio-reader") {
		newSecretName = secret.Name
		manifestWorkName = ManifestWorkNameMsaSecrets
	}

	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      newSecretName,
			Namespace: mesh.GetControlPlaneNamespace(),
		},
		Type: corev1.SecretTypeTLS,
		Data: secret.Data,
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifestWorkName,
			Namespace: clusterName,
			Labels:    meshOwnedLabels(mesh, clusterName),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{{
					RawExtension: runtime.RawExtension{Object: newSecret},
				}},
			},
		},
	}
}

// updateSecretManifestWorkIfNeeded updates the ManifestWork if the secret data has changed
func (r *Reconciler) updateSecretManifestWorkIfNeeded(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, work *workv1.ManifestWork, secret *corev1.Secret) error {
	existingSecret := &corev1.Secret{}
	if err := util.UnmarshalManifest(work.Spec.Workload.Manifests[0], existingSecret); err != nil {
		return fmt.Errorf("failed to unmarshal existing manifest: %w", err)
	}

	if reflect.DeepEqual(existingSecret.Data, secret.Data) {
		klog.V(4).Infof("ManifestWork %s/%s is up to date, no changes needed", work.Namespace, work.Name)
		return nil
	}

	newWork := r.buildSecretManifestWork(mesh, work.Namespace, secret)

	work.Spec = newWork.Spec
	// TODO: handle label reconciliation
	work.Labels = newWork.Labels

	if err := r.Update(ctx, work); err != nil {
		return fmt.Errorf("failed to update ManifestWork: %w", err)
	}

	klog.V(4).Infof("Updated ManifestWork %s/%s", work.Namespace, work.Name)
	return nil
}
