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

var (
	errMissingRootCAKey = fmt.Errorf("no %q data found", corev1.ServiceAccountRootCAKey)
	errMissingTokenKey  = fmt.Errorf("no %q data found", corev1.ServiceAccountTokenKey)
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
	clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if err != nil {
		return fmt.Errorf("failed to get clusters from set %s: %w", mesh.Spec.ClusterSet, err)
	}

	msaName := fmt.Sprintf("%s-%s-%s", mesh.Namespace, "istio-reader", mesh.Name)
	manifests := []workv1.Manifest{}
	for _, cluster := range clusters {
		tokenSecret, err := r.getServiceAccountSecret(ctx, msaName, cluster.Name)
		if err != nil {
			return err
		}

		remoteSecret, err := r.createRemoteSecret(ctx, mesh, &cluster, tokenSecret)
		if err != nil {
			return fmt.Errorf("failed create Istio remote secret for cluster %s: %w", cluster.Name, err)
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

	var state string
	switch result {
	case controllerutil.OperationResultCreated:
		state = "created"
	case controllerutil.OperationResultUpdated:
		state = "updated"
	case controllerutil.OperationResultNone:
		state = "up to date"
	}

	klog.Infof("ManifestWorkReplicaSet %s/%s successfully %s", mesh.Namespace, mesh.Name, state)
	return nil
}

func (r *Reconciler) getServiceAccountSecret(ctx context.Context, serviceAccountName, clusterName string) (*corev1.Secret, error) {
	sa := &corev1.ServiceAccount{}
	if err := r.Get(ctx, key.Of(serviceAccountName, clusterName), sa); err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, key.Of(serviceAccountName, clusterName), secret); err != nil {
		return nil, err
	}

	if err := secretReferencesServiceAccount(sa, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func secretReferencesServiceAccount(serviceAccount *corev1.ServiceAccount, secret *corev1.Secret) error {
	if secret.Type != corev1.SecretTypeServiceAccountToken ||
		secret.Annotations[corev1.ServiceAccountNameKey] != serviceAccount.Name {
		return fmt.Errorf("secret %s/%s does not reference ServiceAccount %s",
			secret.Namespace, secret.Name, serviceAccount.Name)
	}
	return nil
}

// createRemoteSecret builds a remote API server access secret.
// The secret includes required label and annotation for Istio remote endpoint discovery and data from a ManageServiceAccount secret.
func (r *Reconciler) createRemoteSecret(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh,
	cluster *clusterv1.ManagedCluster, tokenSecret *corev1.Secret) (*corev1.Secret, error) {
	secName := "istio-remote-secret-" + cluster.Name
	server := cluster.Spec.ManagedClusterClientConfigs[0].URL
	// TODO: Get TLSServerName (SNI) from ManagedCluster if needed. If tlsServerName is empty, the hostname used to contact the server is used.
	tlsServerName := ""

	remoteSecret, err := createRemoteSecretFromTokenAndServer(tokenSecret, server, tlsServerName, cluster.Name, secName)
	if err != nil {
		return nil, err
	}
	remoteSecret.Namespace = mesh.Spec.ControlPlane.Namespace
	maps.Copy(remoteSecret.Labels, meshOwnedLabels(mesh, cluster.Name))
	return remoteSecret, nil
}

func createRemoteSecretFromTokenAndServer(tokenSecret *corev1.Secret, server, tlsServerName, clusterName, secName string) (*corev1.Secret, error) {
	caData, token, err := tokenDataFromSecret(tokenSecret)
	if err != nil {
		return nil, err
	}

	// Create a Kubeconfig to access the remote cluster using the remote service account credentials.
	kubeconfig := createBearerTokenKubeconfig(caData, token, clusterName, server, tlsServerName)
	if err := clientcmd.Validate(*kubeconfig); err != nil {
		return nil, fmt.Errorf("invalid kubeconfig: %w", err)
	}

	// Encode the Kubeconfig in a secret that can be loaded by Istio to dynamically discover and access the remote cluster.
	return createRemoteServiceAccountSecret(kubeconfig, clusterName, secName)
}

func tokenDataFromSecret(tokenSecret *corev1.Secret) (ca, token []byte, err error) {
	var ok bool
	ca, ok = tokenSecret.Data[corev1.ServiceAccountRootCAKey]
	if !ok {
		err = errMissingRootCAKey
		return ca, token, err
	}
	token, ok = tokenSecret.Data[corev1.ServiceAccountTokenKey]
	if !ok {
		err = errMissingTokenKey
		return ca, token, err
	}
	return ca, token, err
}

func createBearerTokenKubeconfig(caData, token []byte, clusterName, server, tlsServerName string) *api.Config {
	c := createBaseKubeconfig(caData, clusterName, server, tlsServerName)
	c.AuthInfos[c.CurrentContext] = &api.AuthInfo{
		Token: string(token),
	}
	return c
}

func createBaseKubeconfig(caData []byte, clusterName, server, tlsServerName string) *api.Config {
	return &api.Config{
		Clusters: map[string]*api.Cluster{
			clusterName: {
				CertificateAuthorityData: caData,
				Server:                   server,
				TLSServerName:            tlsServerName,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{},
		Contexts: map[string]*api.Context{
			clusterName: {
				Cluster:  clusterName,
				AuthInfo: clusterName,
			},
		},
		CurrentContext: clusterName,
	}
}

func createRemoteServiceAccountSecret(kubeconfig *api.Config, clusterName, secName string) (*corev1.Secret, error) {
	var data bytes.Buffer
	if err := latest.Codec.Encode(kubeconfig, &data); err != nil {
		return nil, err
	}
	key := clusterName

	out := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secName,
			Annotations: map[string]string{
				"networking.istio.io/cluster": clusterName,
			},
			Labels: map[string]string{
				"istio/multiCluster": "true",
			},
		},
		Data: map[string][]byte{
			key: data.Bytes(),
		},
	}
	return out, nil
}
