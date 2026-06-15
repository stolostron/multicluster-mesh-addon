package mesh

import (
	"context"
	"fmt"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	multiClusterSecretLabel   = "istio/multiCluster"
	manifestWorkNameMsaPrefix = "multicluster-mesh-msa-secrets"
	remoteSecretPrefix        = "istio-remote-secret"
	clusterNameAnnotationKey  = "networking.istio.io/cluster"
)

// createManagedServiceAccounts creates ManagedServiceAccount resources for each cluster in the ClusterSet
func (r *Reconciler) createManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	if len(clusters) == 0 {
		klog.V(4).Info("The ClusterSet has no managed cluster")
		return nil
	}

	klog.Info("Creating ManagedServiceAccount resources for each managed cluster in the ClusterSet")
	for _, cluster := range clusters {
		existing := &msav1beta1.ManagedServiceAccount{}
		msaName := fmt.Sprintf("%s-istio-reader", mesh.Name)

		if err := r.Get(ctx, types.NamespacedName{Name: msaName, Namespace: cluster.Name}, existing); err == nil {
			klog.Infof("Cluster %s has an existing ManagedServiceAccount resource %s, skipping createManagedServiceAccount", cluster.Name, msaName)
			continue
		}

		msa := &msav1beta1.ManagedServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      msaName,
				Namespace: cluster.Name,
				Labels:    meshOwnedLabels(mesh, cluster.Name),
			},
			Spec: msav1beta1.ManagedServiceAccountSpec{
				Rotation: msav1beta1.ManagedServiceAccountRotation{
					Enabled:  true,                                        // the ServiceAccount token will be rotated before it expires
					Validity: *mesh.Spec.Security.Discovery.TokenValidity, // the duration of validity for requesting the signed ServiceAccount token
				},
			},
		}

		if err := r.Create(ctx, msa); err != nil {
			return fmt.Errorf("failed to create a ManagedServiceAccount %s/%s: %w", cluster.Name, msaName, err)
		}

		klog.Infof("Successfully created a ManagedServiceAccount %s/%s", cluster.Name, msaName)
	}
	return nil
}

// cleanupManagedServiceAccounts deletes ManagedServiceAccount on clusters that are removed from the given ClusterSet.
func (r *Reconciler) cleanupManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	clusterNames := clusterNameSet(clusters)

	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList,
		client.MatchingLabels{ManagedByLabel: ManagedByValue, MeshNameLabel: mesh.Name}); err != nil {
		return fmt.Errorf("failed to list ManagedServiceAccounts: %w", err)
	}

	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Namespace), client.MatchingLabels{
		multiClusterSecretLabel: "true", ManagedByLabel: ManagedByValue, MeshNameLabel: mesh.Name,
	}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to list istio-remote secrets managed by mesh %s: %w", mesh.Name, err)
	}

	for _, msa := range msaList.Items {
		clusterName := msa.Labels[ClusterNameLabel]
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting ManagedServiceAccount %s/%s (cluster %s no longer in ClusterSet %s)", msa.Namespace, msa.Name, clusterName, mesh.Spec.ClusterSet)
		if err := r.Delete(ctx, &msa); err != nil {
			return fmt.Errorf("failed to delete ManagedServiceAccount %s/%s: %w", msa.Namespace, msa.Name, err)
		}
	}

	for _, sec := range secretList.Items {
		clusterName := sec.Labels[ClusterNameLabel]
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting istio remote secret %s/%s (cluster %s no longer in ClusterSet %s)", sec.Namespace, sec.Name, clusterName, mesh.Spec.ClusterSet)
		if err := r.Delete(ctx, &sec); err != nil {
			return fmt.Errorf("failed to delete istio remote secret %s/%s: %w", sec.Namespace, sec.Name, err)
		}
	}

	return nil
}

// deleteAllManagedServiceAccounts deletes all ManagedServiceAccount resources managed by a mesh
func (r *Reconciler) deleteAllManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList, client.MatchingLabels{ManagedByLabel: ManagedByValue, MeshNameLabel: mesh.Name}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to list ManagedServiceAccount resources managed by mesh %s: %w", mesh.Name, err)
	}

	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Namespace), client.MatchingLabels{
		multiClusterSecretLabel: "true", ManagedByLabel: ManagedByValue, MeshNameLabel: mesh.Name,
	}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to list istio-remote secrets managed by mesh %s: %w", mesh.Name, err)
	}

	for _, msa := range msaList.Items {
		if err := r.Delete(ctx, &msa); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete a ManagedServiceAccount resource %s: %w", msa.Name, err)
		} else {
			klog.Infof("Deleting a ManagedServiceAccount resource %s", msa.Name)
		}
	}

	for _, sec := range secretList.Items {
		if err := r.Delete(ctx, &sec); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete an istio remote secret %s: %w", sec.Name, err)
		} else {
			klog.Infof("Deleting an istio remote secret %s", sec.Name)
		}
	}

	return nil
}

// ensureMsaSecretsDistributed creates ManifestWorks to distribute ManagedServiceAccount secrets to clusters
func (r *Reconciler) ensureMsaSecretsDistributed(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	for _, cluster := range clusters {
		if err := r.ensureMsaManifestWork(ctx, mesh, &cluster); err != nil {
			return fmt.Errorf("failed to ensure ManagedServiceAccount secrets ManifestWork for cluster %s: %w", cluster.Name, err)
		}
	}
	return nil
}

// ensureMsaManifestWork creates a ManifestWork to distribute the ManagedServiceAccount secret to a cluster
func (r *Reconciler) ensureMsaManifestWork(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	msaSecretName := fmt.Sprintf("%s-istio-reader", mesh.Name)
	secret := &corev1.Secret{}
	err := r.Get(ctx, key.Of(msaSecretName, cluster.Name), secret)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("Secret %s/%s not found yet, waiting for ManagedServiceAccount to create it", cluster.Name, msaSecretName)
			return nil
		}
		return fmt.Errorf("failed to get secret: %w", err)
	}

	work, err := r.workApplier.Apply(ctx, r.buildMsaManifestWork(mesh, cluster.Name, secret))
	if err != nil {
		return fmt.Errorf("failed to apply ManagedServiceAccount ManifestWork on cluster %s: %w", cluster.Name, err)
	}

	klog.Infof("Successfully applied ManagedServiceAccount ManifestWork %s/%s", work.Namespace, work.Name)
	return nil
}

// buildMsaManifestWork builds a ManifestWork for distributing the ManagedServiceAccount secret
func (r *Reconciler) buildMsaManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, secret *corev1.Secret) *workv1.ManifestWork {
	istioRemoteSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", remoteSecretPrefix, clusterName),
			Namespace: mesh.GetControlPlaneNamespace(),
			Labels: map[string]string{
				multiClusterSecretLabel: "true",
				ManagedByLabel:          ManagedByValue,
				MeshNameLabel:           mesh.Name,
				ClusterSetLabel:         mesh.Spec.ClusterSet,
				ClusterNameLabel:        clusterName,
			},
			Annotations: map[string]string{
				clusterNameAnnotationKey: clusterName,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secret.Data,
	}

	workName := fmt.Sprintf("%s-%s", manifestWorkNameMsaPrefix, mesh.Name)
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: clusterName,
			Labels:    meshOwnedLabels(mesh, clusterName),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{{
					RawExtension: runtime.RawExtension{Object: istioRemoteSecret},
				}},
			},
			ManifestConfigs: []workv1.ManifestConfigOption{{
				UpdateStrategy: &workv1.UpdateStrategy{
					Type: workv1.UpdateStrategyTypeServerSideApply,
				},
			}},
		},
	}
}
