package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

// ensureRemoteSecrets distributes Istio remote secrets for cross-cluster endpoint discovery.
// For MultiPrimary topology, every cluster gets remote secrets for all other clusters.
// For PrimaryRemote topology, only the primary cluster receives remote secrets.
func (r *Reconciler) ensureRemoteSecrets(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	if len(clusters) < 2 {
		return nil
	}

	// Cache all MSA secrets upfront to avoid O(N^2) redundant API calls
	// and eliminate TOCTOU risk between hash computation and manifest building.
	msaSecrets, err := r.collectMSASecrets(ctx, mesh, clusters)
	if err != nil {
		return fmt.Errorf("failed to collect MSA secrets: %w", err)
	}

	targets := r.computeRemoteSecretTargets(mesh, clusters)
	tokenHash := computeTokenHashFromCache(msaSecrets)

	for targetCluster, sourceClusters := range targets {
		if err := r.ensureRemoteSecretManifestWork(ctx, mesh, targetCluster, sourceClusters, msaSecrets, tokenHash); err != nil {
			return fmt.Errorf("failed to ensure remote secret ManifestWork for cluster %s: %w", targetCluster, err)
		}
	}

	return nil
}

// collectMSASecrets reads all MSA-synced Secrets for the mesh's clusters into a map.
func (r *Reconciler) collectMSASecrets(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) (map[string]*corev1.Secret, error) {
	msaName := getMSAName(mesh)
	secrets := make(map[string]*corev1.Secret, len(clusters))
	for _, cluster := range clusters {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, key.Of(msaName, cluster.Name), secret); err != nil {
			return nil, fmt.Errorf("failed to get MSA secret %s/%s: %w", cluster.Name, msaName, err)
		}
		secrets[cluster.Name] = secret
	}
	return secrets, nil
}

// computeRemoteSecretTargets returns a map of target cluster name to the source clusters
// whose remote secrets should be placed on that target.
func (r *Reconciler) computeRemoteSecretTargets(mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) map[string][]*clusterv1.ManagedCluster {
	targets := make(map[string][]*clusterv1.ManagedCluster)

	if mesh.Spec.Topology.Type == meshv1alpha1.TopologyPrimaryRemote {
		primary := getPrimaryCluster(mesh, clusters)
		if primary == nil {
			return targets
		}
		remotes := getRemoteClusters(mesh, clusters)
		for _, remote := range remotes {
			targets[primary.Name] = append(targets[primary.Name], remote)
		}
		return targets
	}

	// MultiPrimary: bidirectional - each cluster gets secrets for all others
	for i := range clusters {
		for j := range clusters {
			if clusters[i].Name == clusters[j].Name {
				continue
			}
			targets[clusters[i].Name] = append(targets[clusters[i].Name], &clusters[j])
		}
	}

	return targets
}

// ensureRemoteSecretManifestWork applies the remote secret ManifestWork for a target cluster.
// Short-circuits if the token hash annotation matches (no rotation since last apply).
func (r *Reconciler) ensureRemoteSecretManifestWork(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, targetCluster string, sourceClusters []*clusterv1.ManagedCluster, msaSecrets map[string]*corev1.Secret, tokenHash string) error {
	manifests, err := r.buildRemoteSecretManifests(mesh, sourceClusters, msaSecrets)
	if err != nil {
		return err
	}

	workName := getRSManifestWorkName(mesh)

	existing := &workv1.ManifestWork{}
	if err := r.Get(ctx, key.Of(workName, targetCluster), existing); err == nil {
		if existing.Annotations[TokenHashAnnotation] == tokenHash {
			klog.V(4).Infof("Remote secret ManifestWork %s/%s is up to date", targetCluster, workName)
			return nil
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManifestWork %s/%s: %w", targetCluster, workName, err)
	}

	work := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				TokenHashAnnotation: tokenHash,
			},
			Labels:    meshOwnedLabels(mesh, targetCluster),
			Name:      workName,
			Namespace: targetCluster,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}

	if _, err := r.workApplier.Apply(ctx, work); err != nil {
		return fmt.Errorf("failed to apply remote secret ManifestWork %s/%s: %w", targetCluster, workName, err)
	}

	klog.Infof("Applied remote secret ManifestWork %s/%s", targetCluster, workName)
	return nil
}

func (r *Reconciler) buildRemoteSecretManifests(mesh *meshv1alpha1.MultiClusterMesh, sourceClusters []*clusterv1.ManagedCluster, msaSecrets map[string]*corev1.Secret) ([]workv1.Manifest, error) {
	manifests := make([]workv1.Manifest, 0, len(sourceClusters))
	cpNamespace := mesh.GetControlPlaneNamespace()

	for _, source := range sourceClusters {
		secret := msaSecrets[source.Name]
		if secret == nil {
			return nil, fmt.Errorf("MSA secret not found in cache for cluster %s", source.Name)
		}

		apiServerURL := getAPIServerURL(source)
		if apiServerURL == "" {
			return nil, fmt.Errorf("cluster %s has no API server URL", source.Name)
		}

		kubeconfig := buildKubeconfig(source.Name, apiServerURL, secret)

		remoteSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"networking.istio.io/cluster": source.Name,
				},
				Labels: map[string]string{
					"istio/multiCluster": "true",
				},
				Name:      "istio-remote-secret-" + source.Name,
				Namespace: cpNamespace,
			},
			StringData: map[string]string{
				source.Name: kubeconfig,
			},
			Type: corev1.SecretTypeOpaque,
		}

		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: remoteSecret},
		})
	}

	return manifests, nil
}

// buildKubeconfig produces the Istio remote-secret kubeconfig YAML that istiod on
// a peer cluster uses to access this cluster's API server for endpoint discovery.
func buildKubeconfig(clusterName, apiServerURL string, secret *corev1.Secret) string {
	caData := base64.StdEncoding.EncodeToString(secret.Data["ca.crt"])
	token := string(secret.Data["token"])

	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\n")
	fmt.Fprintf(&b, "kind: Config\n")
	fmt.Fprintf(&b, "clusters:\n")
	fmt.Fprintf(&b, "- cluster:\n")
	fmt.Fprintf(&b, "    certificate-authority-data: %s\n", caData)
	fmt.Fprintf(&b, "    server: %s\n", apiServerURL)
	fmt.Fprintf(&b, "  name: %s\n", clusterName)
	fmt.Fprintf(&b, "contexts:\n")
	fmt.Fprintf(&b, "- context:\n")
	fmt.Fprintf(&b, "    cluster: %s\n", clusterName)
	fmt.Fprintf(&b, "    user: %s\n", clusterName)
	fmt.Fprintf(&b, "  name: %s\n", clusterName)
	fmt.Fprintf(&b, "current-context: %s\n", clusterName)
	fmt.Fprintf(&b, "users:\n")
	fmt.Fprintf(&b, "- name: %s\n", clusterName)
	fmt.Fprintf(&b, "  user:\n")
	fmt.Fprintf(&b, "    token: %s\n", token)

	return b.String()
}

func getAPIServerURL(cluster *clusterv1.ManagedCluster) string {
	if len(cluster.Spec.ManagedClusterClientConfigs) > 0 {
		return cluster.Spec.ManagedClusterClientConfigs[0].URL
	}
	return ""
}

// computeTokenHashFromCache computes a SHA-256 hash over all cached MSA tokens,
// providing a stable fingerprint to detect token rotation.
func computeTokenHashFromCache(msaSecrets map[string]*corev1.Secret) string {
	h := sha256.New()

	names := make([]string, 0, len(msaSecrets))
	for name := range msaSecrets {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, clusterName := range names {
		h.Write([]byte(clusterName))
		h.Write(msaSecrets[clusterName].Data["token"])
	}

	return hex.EncodeToString(h.Sum(nil))
}

// areMSATokensAvailable checks whether MSA-synced Secrets exist for all clusters in the mesh.
func (r *Reconciler) areMSATokensAvailable(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) bool {
	msaName := getMSAName(mesh)
	for _, cluster := range clusters {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, key.Of(msaName, cluster.Name), secret); err != nil {
			klog.V(4).Infof("MSA secret %s/%s not yet available: %v", cluster.Name, msaName, err)
			return false
		}
	}
	return true
}

// findMeshesForMSASecret maps MSA-synced Secrets to mesh reconcile requests.
// The mapping path is: Secret namespace (= cluster name) → cluster's ClusterSet label → meshes.
func (r *Reconciler) findMeshesForMSASecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	if secret.Labels[MSASecretLabel] != "true" {
		return nil
	}

	clusterName := secret.Namespace
	cluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, key.Of(clusterName), cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get ManagedCluster %s for MSA secret: %v", clusterName, err)
		}
		return nil
	}

	clusterSetName := cluster.Labels[ClusterSetLabel]
	if clusterSetName == "" {
		return nil
	}

	klog.V(4).Infof("MSA secret %s/%s changed, reconciling meshes in ClusterSet %s", secret.Namespace, secret.Name, clusterSetName)
	return r.reconcileRequestsForClusterSet(ctx, clusterSetName)
}
