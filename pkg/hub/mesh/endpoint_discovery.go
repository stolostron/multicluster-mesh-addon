package mesh

import (
	"context"
	"fmt"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureManagedServiceAccountCreated creates ManagedServiceAccount resources using the mesh.Spec.Security.Discovery.TokenValidity value.
func (r *Reconciler) ensureManagedServiceAccountCreated(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
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

// ensureManagedServiceAccountUpdated updates an existing ManagedServiceAccount with the mesh's spec.Security.Discovery.TokenValidity value
func (r *Reconciler) ensureManagedServiceAccountUpdated(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, existing *msav1beta1.ManagedServiceAccount) error {
	if existing.Labels[MeshNameLabel] != mesh.Name || existing.Labels[MeshNamespaceLabel] != mesh.Namespace {
		return fmt.Errorf("ManagedServiceAccount %s/%s exists but is not owned by mesh %s/%s", existing.Namespace, existing.Name, mesh.Namespace, mesh.Name)
	}

	if existing.Spec.Rotation.Validity == *mesh.Spec.Security.Discovery.TokenValidity {
		return nil
	}
	existing.Spec.Rotation.Validity = *mesh.Spec.Security.Discovery.TokenValidity

	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update a ManagedServiceAccount %s/%s: %w", existing.Namespace, existing.Name, err)
	}

	klog.V(4).Infof("Successfully updated a ManagedServiceAccount %s/%s", existing.Namespace, existing.Name)
	return nil
}
