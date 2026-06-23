package mesh

import (
	"context"
	"fmt"
	"time"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	multiClusterSecretLabel = "istio/multiCluster"
	msaRootWord             = "istio-reader"
)

// createManagedServiceAccounts creates ManagedServiceAccount resources for each cluster.
// It checks and uses the mesh.Spec.Security.Discovery.TokenValidity value.
// If there is an existing ManagedServiceAccount, it skips creation.
func (r *Reconciler) createManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	if len(clusters) == 0 {
		klog.V(4).Info("The ClusterSet has no managed cluster")
		return nil
	}

	msaName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, msaRootWord, mesh.Name)

	var validity metav1.Duration
	if mesh.Spec.Security.Discovery.TokenValidity == nil {
		validity = metav1.Duration{Duration: 360 * time.Hour}
	} else {
		validity = *mesh.Spec.Security.Discovery.TokenValidity
	}

	for _, cluster := range clusters {
		existing := &msav1beta1.ManagedServiceAccount{}
		if err := r.Get(ctx, types.NamespacedName{Name: msaName, Namespace: cluster.Name}, existing); err == nil {
			klog.V(4).Infof("Cluster %s has an existing ManagedServiceAccount resource %s, skipping createManagedServiceAccount", cluster.Name, msaName)
			continue
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
					Enabled:  true,
					Validity: validity,
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

// TODO(yxun): (#120) Update existing ManagedServiceAccount resources when the user changes the tokenValidity on the mesh

// cleanupManagedServiceAccounts deletes ManagedServiceAccount and istio-remote secret,
// when the cluster(s) are removed from the given mesh's ClusterSet.
func (r *Reconciler) cleanupManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	clusterNames := clusterNameSet(clusters)

	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList,
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace}); err != nil {
		return fmt.Errorf("failed to list ManagedServiceAccounts: %w", err)
	}

	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Namespace), client.MatchingLabels{
		multiClusterSecretLabel: "true", MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list istio-remote secrets managed by mesh %s: %w", mesh.Name, err)
	}

	for _, msa := range msaList.Items {
		clusterName := msa.Labels[ClusterNameLabel]
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting ManagedServiceAccount %s/%s (cluster %s no longer in ClusterSet %s)", msa.Namespace, msa.Name, clusterName, mesh.Spec.ClusterSet)
		if err := client.IgnoreNotFound(r.Delete(ctx, &msa)); err != nil {
			return fmt.Errorf("failed to delete ManagedServiceAccount %s/%s: %w", msa.Namespace, msa.Name, err)
		}
	}

	for _, sec := range secretList.Items {
		clusterName := sec.Labels[ClusterNameLabel]
		if clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting istio remote secret %s/%s (cluster %s no longer in ClusterSet %s)", sec.Namespace, sec.Name, clusterName, mesh.Spec.ClusterSet)
		if err := client.IgnoreNotFound(r.Delete(ctx, &sec)); err != nil {
			return fmt.Errorf("failed to delete istio remote secret %s/%s: %w", sec.Namespace, sec.Name, err)
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

	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Namespace), client.MatchingLabels{
		multiClusterSecretLabel: "true", MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list istio-remote secrets managed by mesh %s: %w", mesh.Name, err)
	}

	for _, msa := range msaList.Items {
		klog.Infof("Deleting ManagedServiceAccount %s/%s", msa.Namespace, msa.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &msa)); err != nil {
			return fmt.Errorf("failed to delete ManagedServiceAccount %s/%s: %w", msa.Namespace, msa.Name, err)
		}
	}

	for _, sec := range secretList.Items {
		klog.Infof("Deleting an istio remote secret %s", sec.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &sec)); err != nil {
			return fmt.Errorf("failed to delete an istio remote secret %s: %w", sec.Name, err)
		}
	}

	return nil
}
