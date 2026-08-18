package mesh

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func msaName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return fmt.Sprintf("%s-istio-reader-%s", mesh.Namespace, mesh.Name)
}

// ensureManagedServiceAccount applies the desired ManagedServiceAccount state for a specific cluster using mesh's TokenValidity.
func (r *Reconciler) ensureManagedServiceAccount(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	msaName := msaName(mesh)
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

// ensureRemoteSecretDistribution builds Istio remote discovery secrets from ManagedServiceAccount tokens and distributes them via ManifestWorkReplicaSet.
func (r *Reconciler) ensureRemoteSecretDistribution(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	msaName := msaName(mesh)
	manifests := []workv1.Manifest{}
	for _, cluster := range clusters {
		if len(cluster.Spec.ManagedClusterClientConfigs) == 0 {
			klog.V(4).Infof("no API endpoint found, skipping secret distribution for cluster %s", cluster.Name)
			continue
		}
		server := cluster.Spec.ManagedClusterClientConfigs[0].URL

		tokenSecret, err := r.getMSATokenSecret(ctx, msaName, cluster.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(4).Infof("managedServiceAccount secret not found yet for cluster %s, skipping", cluster.Name)
				continue
			}
			return err
		}
		if tokenSecret == nil {
			continue
		}

		remoteSecret, err := buildIstioRemoteSecret(tokenSecret, cluster.Name, server, mesh.Spec.ControlPlane.Namespace)
		if err != nil {
			return fmt.Errorf("failed to build Istio remote secret for cluster %s: %w", cluster.Name, err)
		}
		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: remoteSecret},
		})
	}

	mwrset := &workv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: mesh.Name, Namespace: mesh.Namespace},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, mwrset, func() error {
		if mwrset.Labels == nil {
			mwrset.Labels = make(map[string]string)
		}
		mwrset.Labels[ManagedByLabel] = ManagedByValue
		mwrset.Labels[MeshNameLabel] = mesh.Name
		mwrset.Labels[MeshNamespaceLabel] = mesh.Namespace
		mwrset.Spec.PlacementRefs = []workv1alpha1.LocalPlacementReference{{Name: mesh.Name}}
		mwrset.Spec.ManifestWorkTemplate = workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{Manifests: manifests}}
		return controllerutil.SetControllerReference(mesh, mwrset, r.Scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to ensure ManifestWorkReplicaSet %s/%s: %w", mesh.Namespace, mesh.Name, err)
	}

	if result != controllerutil.OperationResultNone {
		klog.Infof("Ensured ManifestWorkReplicaSet %s/%s state: %s", mesh.Namespace, mesh.Name, result)
	}
	return nil
}

func (r *Reconciler) getMSATokenSecret(ctx context.Context, msaName, clusterName string) (*corev1.Secret, error) {
	msa := &msav1beta1.ManagedServiceAccount{}
	if err := r.Get(ctx, key.Of(msaName, clusterName), msa); err != nil {
		return nil, err
	}
	if msa.Status.TokenSecretRef == nil {
		return nil, nil
	}
	secret := &corev1.Secret{}
	return secret, r.Get(ctx, key.Of(msa.Status.TokenSecretRef.Name, clusterName), secret)
}

// buildIstioRemoteSecret builds a remote API server access secret.
// The secret includes required label and annotation for Istio remote endpoint discovery and data from a ManagedServiceAccount secret.
func buildIstioRemoteSecret(tokenSecret *corev1.Secret, clusterName, server, namespace string) (*corev1.Secret, error) {
	ca, ok := tokenSecret.Data[corev1.ServiceAccountRootCAKey]
	if !ok {
		return nil, fmt.Errorf("no %q data found", corev1.ServiceAccountRootCAKey)
	}
	token, ok := tokenSecret.Data[corev1.ServiceAccountTokenKey]
	if !ok {
		return nil, fmt.Errorf("no %q data found", corev1.ServiceAccountTokenKey)
	}

	kubeconfig := &api.Config{
		Clusters:       map[string]*api.Cluster{clusterName: {CertificateAuthorityData: ca, Server: server}},
		AuthInfos:      map[string]*api.AuthInfo{clusterName: {Token: string(token)}},
		Contexts:       map[string]*api.Context{clusterName: {Cluster: clusterName, AuthInfo: clusterName}},
		CurrentContext: clusterName,
	}
	if err := clientcmd.Validate(*kubeconfig); err != nil {
		return nil, fmt.Errorf("invalid kubeconfig: %w", err)
	}

	var buf bytes.Buffer
	if err := latest.Codec.Encode(kubeconfig, &buf); err != nil {
		return nil, fmt.Errorf("failed to encode kubeconfig: %w", err)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "istio-remote-secret-" + clusterName,
			Namespace:   namespace,
			Annotations: map[string]string{"networking.istio.io/cluster": clusterName},
			Labels:      map[string]string{"istio/multiCluster": "true"},
		},
		Data: map[string][]byte{clusterName: buf.Bytes()},
	}, nil
}
