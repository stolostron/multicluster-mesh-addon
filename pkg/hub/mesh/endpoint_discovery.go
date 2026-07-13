package mesh

import (
	"context"
	"fmt"
	"maps"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureManagedServiceAccount applies the desired ManagedServiceAccount state for a specific cluster using mesh's TokenValidity.
func (r *Reconciler) ensureManagedServiceAccount(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	msaName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, "istio-reader", mesh.Name)
	existing := &msav1beta1.ManagedServiceAccount{}
	if err := r.Get(ctx, key.Of(msaName, cluster.Name), existing); err == nil {
		return r.ensureManagedServiceAccountUpdated(ctx, mesh, existing)
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManagedServiceAccount %s/%s: %w", cluster.Name, msaName, err)
	}

	msa := &msav1beta1.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      msaName,
			Namespace: cluster.Name,
			Labels:    meshOwnedLabels(mesh, cluster.Name),
		},
		Spec: msav1beta1.ManagedServiceAccountSpec{
			Rotation: msav1beta1.ManagedServiceAccountRotation{
				Validity: *mesh.Spec.Security.Discovery.TokenValidity,
			},
		},
	}

	if err := r.Create(ctx, msa); err != nil {
		return fmt.Errorf("failed to create a ManagedServiceAccount %s/%s: %w", cluster.Name, msaName, err)
	}

	klog.Infof("Successfully created a ManagedServiceAccount %s/%s", cluster.Name, msaName)
	return nil
}

// cleanupManagedServiceAccounts deletes ManagedServiceAccount when the cluster(s) are removed from the given mesh's ClusterSet.
func (r *Reconciler) cleanupManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	clusterNames := clusterNameSet(clusters)

	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList,
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace}); err != nil {
		return fmt.Errorf("failed to list ManagedServiceAccounts: %w", err)
	}

	for _, msa := range msaList.Items {
		clusterName := msa.Namespace
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting ManagedServiceAccount %s/%s (cluster %s no longer in ClusterSet %s)", msa.Namespace, msa.Name, clusterName, mesh.Spec.ClusterSet)
		if err := client.IgnoreNotFound(r.Delete(ctx, &msa)); err != nil {
			return fmt.Errorf("failed to delete ManagedServiceAccount %s/%s: %w", msa.Namespace, msa.Name, err)
		}
	}

	return nil
}

// deleteAllManagedServiceAccounts deletes all ManagedServiceAccount resources managed by a mesh
func (r *Reconciler) deleteAllManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList, client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace}); err != nil {
		return fmt.Errorf("failed to list ManagedServiceAccount resources managed by mesh %s: %w", mesh.Name, err)
	}

	for _, msa := range msaList.Items {
		klog.Infof("Deleting ManagedServiceAccount %s/%s", msa.Namespace, msa.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &msa)); err != nil {
			return fmt.Errorf("failed to delete ManagedServiceAccount %s/%s: %w", msa.Namespace, msa.Name, err)
		}
	}

	return nil
}

func (r *Reconciler) ensureManagedServiceAccountUpdated(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, existing *msav1beta1.ManagedServiceAccount) error {
	desiredLabels := meshOwnedLabels(mesh, existing.Namespace)
	desiredValidity := *mesh.Spec.Security.Discovery.TokenValidity

	if maps.Equal(existing.Labels, desiredLabels) && existing.Spec.Rotation.Validity == desiredValidity {
		return nil
	}

	existing.Labels = desiredLabels
	existing.Spec.Rotation.Validity = desiredValidity

	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ManagedServiceAccount %s/%s: %w", existing.Namespace, existing.Name, err)
	}

	klog.V(4).Infof("Updated ManagedServiceAccount %s/%s", existing.Namespace, existing.Name)
	return nil
}

// The ManagedServiceAccount controller generates an access secret with the name of the ManagedServiceAccount.
// ensureRemoteSecretDistributed creates a ManifestWork to distribute the access secrets.
func (r *Reconciler) ensureRemoteSecretDistributed(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	msaSecretName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, "istio-reader", mesh.Name)
	msaSecrets := make(map[string]*corev1.Secret) // secret name to secret

	for _, cluster := range clusters {
		msaSecret := &corev1.Secret{}
		err := r.Get(ctx, key.Of(msaSecretName, cluster.Name), msaSecret)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(4).Infof("ManagedServiceAccount secret %s/%s not found yet, waiting for its controller to create it", cluster.Name, msaSecretName)
				return nil
			}
		}

		remoteSecret := buildMeshRemoteSecret(mesh, &cluster, msaSecret)
		msaSecrets[remoteSecret.Name] = remoteSecret
	}

	for _, cluster := range clusters {
		work, err := r.workApplier.Apply(ctx, r.buildRemoteSecretManifestWork(mesh, &cluster, msaSecrets))
		if err != nil {
			return fmt.Errorf("failed to apply remote secret ManifestWork %s/%s", work.Namespace, work.Name)
		}
	}

	return nil
}

// buildMeshRemoteSecret builds a remote API server access secret.
// The secret includes required label and annotation for Istio remote endpoint discovery and data from the ManageServiceAccount secret.
func buildMeshRemoteSecret(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster, msaSecret *corev1.Secret) *corev1.Secret {
	istioRemoteSecretName := fmt.Sprintf("%s-%s-%s-%s", mesh.Namespace, "istio-remote-secret", mesh.Name, cluster.Name)
	istioRemoteSecretLabels := meshOwnedLabels(mesh, cluster.Name)
	istioRemoteSecretLabels["istio/multiCluster"] = "true"

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      istioRemoteSecretName,
			Namespace: mesh.GetControlPlaneNamespace(),
			Labels:    istioRemoteSecretLabels,
			Annotations: map[string]string{
				"networking.istio.io/cluster": cluster.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: msaSecret.Data,
	}
}

// buildRemoteSecretManifestWork builds a ManifestWork using remote access secrets from other clusters.
func (r *Reconciler) buildRemoteSecretManifestWork(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster, remoteSecrets map[string]*corev1.Secret) *workv1.ManifestWork {
	manifests := []workv1.Manifest{}

	for _, secret := range remoteSecrets {
		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: secret},
		})
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManifestWorkNameRemoteSecrets,
			Namespace: cluster.Name,
			Labels:    meshOwnedLabels(mesh, cluster.Name),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

// cleanupRemoteSecrets deletes Istio remote access secrets when the cluster(s) are removed from the given mesh's ClusterSet.
func (r *Reconciler) cleanupRemoteSecrets(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	clusterNames := clusterNameSet(clusters)
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Namespace), client.MatchingLabels{
		"networking.istio.io/cluster": "true", MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list Istio remote secrets managed by mesh %s: %w", mesh.Name, err)
	}

	for _, secret := range secretList.Items {
		clusterName := secret.Labels[ClusterNameLabel]
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting Istio remote secret %s/%s (cluster %s no longer in ClusterSet %s)", secret.Namespace, secret.Name, clusterName, mesh.Spec.ClusterSet)
		if err := client.IgnoreNotFound(r.Delete(ctx, &secret)); err != nil {
			return fmt.Errorf("failed to delete Istio remote secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
	}

	return nil
}

// deleteAllRemoteSecrets deletes all Istio remote access secrets managed by a mesh.
func (r *Reconciler) deleteAllRemoteSecrets(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.MatchingLabels{
		"networking.istio.io/cluster": "true",
		MeshNameLabel:                 mesh.Name,
		MeshNamespaceLabel:            mesh.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list Istio remote secret managed by mesh %s: %w", mesh.Name, err)
	}

	for _, secret := range secretList.Items {
		klog.Infof("Deleting Istio remote secret %s/%s", secret.Namespace, secret.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &secret)); err != nil {
			return fmt.Errorf("failed to delete Istio remote secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
	}

	return nil
}
