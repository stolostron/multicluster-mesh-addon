package mesh

import (
	"context"
	"fmt"
	"maps"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ensureManagedServiceAccount applies the desired ManagedServiceAccount state for a specific cluster using mesh's TokenValidity.
func (r *Reconciler) ensureManagedServiceAccount(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	msaName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, "istio-reader", mesh.Name)
	existing := &msav1beta1.ManagedServiceAccount{}
	if err := r.Get(ctx, key.Of(msaName, cluster.Name), existing); err == nil {
		return r.ensureManagedServiceAccountUpdated(ctx, mesh, existing)
	} else if !apierrors.IsNotFound(err) {
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

// ensureManifestWorkReplicaSet creates a ManifestWorkReplicaSet to distribute remote access secrets for clusters selected by a Placement
func (r *Reconciler) ensureManifestWorkReplicaSet(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	placement := &clusterv1beta1.Placement{}
	if err := r.Get(ctx, key.Of(mesh.Name, mesh.Namespace), placement); err != nil {
		return fmt.Errorf("failed to get Placement %s/%s: %w", mesh.Namespace, mesh.Name, err)
	}

	workTemplate, err := r.buildManifestWorkSpec(ctx, mesh)
	if err != nil {
		return fmt.Errorf("failed to build ManifestWorkSpec Template: %w", err)
	}

	mwrset := &workv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: mesh.Name, Namespace: mesh.Namespace},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, mwrset, func() error {
		if mwrset.Labels == nil {
			mwrset.Labels = make(map[string]string)
		}
		mwrset.Labels[ManagedByLabel] = ManagedByValue
		mwrset.Labels[MeshNameLabel] = mesh.Name
		mwrset.Labels[MeshNamespaceLabel] = mesh.Namespace
		mwrset.Spec.PlacementRefs = []workv1alpha1.LocalPlacementReference{{Name: placement.Name}}
		mwrset.Spec.ManifestWorkTemplate = *workTemplate
		return controllerutil.SetControllerReference(mesh, mwrset, r.Scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to ensure ManifestWorkReplicaSet %s/%s: %w", mesh.Namespace, mesh.Name, err)
	}

	klog.Infof("Successfully created a ManifestWorkReplicaSet %s/%s", mesh.Namespace, mesh.Name)
	return nil
}

// buildMeshRemoteSecret builds a remote API server access secret.
// The secret includes required label and annotation for Istio remote endpoint discovery and data from a ManageServiceAccount secret.
func buildMeshRemoteSecret(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster, msaSecret *corev1.Secret) *corev1.Secret {
	istioRemoteSecretName := fmt.Sprintf("%s-%s-%s-%s", mesh.Namespace, mesh.Name, "istio-remote-secret", cluster.Name)
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
			OwnerReferences: msaSecret.OwnerReferences,
		},
		Type: corev1.SecretTypeOpaque,
		Data: msaSecret.Data,
	}
}

func (r *Reconciler) buildManifestWorkSpec(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (*workv1.ManifestWorkSpec, error) {
	msaName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, "istio-reader", mesh.Name)
	clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters from set %s: %w", mesh.Spec.ClusterSet, err)
	}

	manifests := []workv1.Manifest{}
	for _, cluster := range clusters {
		msaSecret := &corev1.Secret{}
		if err := r.Get(ctx, key.Of(msaName, cluster.Name), msaSecret); err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(4).Infof("ManagedServiceAccount Secret %s/%s not found yet, waiting for ManagedServiceAccount to create it", cluster.Name, msaName)
				continue
			}
			return nil, fmt.Errorf("failed to get ManagedServiceAccount Secret %s/%s: %w", cluster.Name, msaName, err)
		}
		remoteSecret := buildMeshRemoteSecret(mesh, &cluster, msaSecret)
		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: remoteSecret},
		})
	}

	return &workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{Manifests: manifests}}, nil
}

// deleteAllRemoteSecrets deletes all Istio remote access secrets managed by a mesh.
func (r *Reconciler) deleteAllRemoteSecrets(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(mesh.Spec.ControlPlane.Namespace), client.MatchingLabels{
		"istio/multiCluster": "true",
		ManagedByLabel:       ManagedByValue,
		MeshNameLabel:        mesh.Name,
		MeshNamespaceLabel:   mesh.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list Istio remote secret managed by mesh %s: %w", mesh.Name, err)
	}

	for _, secret := range secretList.Items {
		klog.V(4).Infof("Deleting Istio remote secret %s/%s", secret.Namespace, secret.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &secret)); err != nil {
			return fmt.Errorf("failed to delete Istio remote secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
	}

	return nil
}
