// REVISIT: Delete this entire file when the multicluster-mesh-addon controller
// handles remote secret construction and distribution.
// This is a temporary e2e test helper that manually creates Istio remote secrets
// to enable cross-cluster endpoint discovery until the controller implements this.

package util

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

const (
	istioReaderSA       = "istio-reader-service-account"
	remoteSecretPrefix  = "istio-remote-secret-"
	multiClusterLabel   = "istio/multiCluster"
	clusterAnnotation   = "networking.istio.io/cluster"
	saTokenSecretPrefix = "istio-reader-token-"
)

// REVISIT: Delete when multicluster-mesh-addon controller handles remote secret
// construction and distribution.
//
// CreateAndDistributeRemoteSecrets creates Istio remote secrets for cross-cluster
// endpoint discovery. For each cluster, it:
//  1. Creates a SA token secret for istio-reader-service-account
//  2. Reads the API server URL from the ManagedCluster on the hub
//  3. Builds a kubeconfig-style secret and applies it on each peer cluster
func CreateAndDistributeRemoteSecrets(ctx context.Context, hubClient client.Client, spokeClients map[string]client.Client, clusterNames []string, cpNamespace string) {
	type clusterInfo struct {
		apiServerURL string
		caData       []byte
		token        string
	}
	infos := make(map[string]*clusterInfo)

	for _, cluster := range clusterNames {
		spokeClient := spokeClients[cluster]

		tokenSecretName := saTokenSecretPrefix + cluster
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tokenSecretName,
				Namespace: cpNamespace,
				Annotations: map[string]string{
					"kubernetes.io/service-account.name": istioReaderSA,
				},
			},
			Type: corev1.SecretTypeServiceAccountToken,
		}
		Expect(spokeClient.Create(ctx, tokenSecret)).To(Succeed(),
			"failed to create SA token secret for %s on %s", istioReaderSA, cluster)

		var token string
		var caData []byte
		Eventually(func(g Gomega) {
			s := &corev1.Secret{}
			g.Expect(spokeClient.Get(ctx, key.Of(tokenSecretName, cpNamespace), s)).To(Succeed())
			g.Expect(s.Data).To(HaveKey("token"), "token not yet populated for %s", cluster)
			g.Expect(s.Data).To(HaveKey("ca.crt"), "ca.crt not yet populated for %s", cluster)
			token = string(s.Data["token"])
			caData = s.Data["ca.crt"]
		}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(Succeed())

		mc := &clusterv1.ManagedCluster{}
		Expect(hubClient.Get(ctx, key.Of(cluster), mc)).To(Succeed(),
			"failed to get ManagedCluster %s", cluster)
		Expect(mc.Spec.ManagedClusterClientConfigs).NotTo(BeEmpty(),
			"ManagedCluster %s has no client configs", cluster)
		apiServerURL := mc.Spec.ManagedClusterClientConfigs[0].URL

		infos[cluster] = &clusterInfo{
			apiServerURL: apiServerURL,
			caData:       caData,
			token:        token,
		}
	}

	for _, source := range clusterNames {
		for _, target := range clusterNames {
			if source == target {
				continue
			}
			info := infos[source]
			kubeconfig := buildRemoteKubeconfig(source, info.apiServerURL, info.caData, info.token)

			remoteSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      remoteSecretPrefix + source,
					Namespace: cpNamespace,
					Labels: map[string]string{
						multiClusterLabel: "true",
					},
					Annotations: map[string]string{
						clusterAnnotation: source,
					},
				},
				Data: map[string][]byte{
					source: []byte(kubeconfig),
				},
			}

			targetClient := spokeClients[target]
			Expect(targetClient.Create(ctx, remoteSecret)).To(Succeed(),
				"failed to create remote secret for %s on %s", source, target)
		}
	}
}

// REVISIT: Delete when multicluster-mesh-addon controller handles remote secret
// construction and distribution.
func buildRemoteKubeconfig(clusterName, apiServerURL string, caData []byte, token string) string {
	caB64 := base64.StdEncoding.EncodeToString(caData)
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: %s
  name: %s
contexts:
- context:
    cluster: %s
    user: %s
  name: %s
current-context: %s
users:
- name: %s
  user:
    token: %s
`, caB64, apiServerURL, clusterName,
		clusterName, clusterName, clusterName,
		clusterName,
		clusterName, token)
}

// REVISIT: Delete when multicluster-mesh-addon controller handles remote secret
// construction and distribution.
//
// CleanupRemoteSecrets deletes the temporary remote secrets and SA token secrets
// created by CreateAndDistributeRemoteSecrets.
func CleanupRemoteSecrets(ctx context.Context, spokeClients map[string]client.Client, clusterNames []string, cpNamespace string) {
	for _, source := range clusterNames {
		for _, target := range clusterNames {
			if source == target {
				continue
			}
			targetClient := spokeClients[target]
			_ = client.IgnoreNotFound(targetClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      remoteSecretPrefix + source,
					Namespace: cpNamespace,
				},
			}))
		}
	}
	for _, cluster := range clusterNames {
		spokeClient := spokeClients[cluster]
		_ = client.IgnoreNotFound(spokeClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      saTokenSecretPrefix + cluster,
				Namespace: cpNamespace,
			},
		}))
	}
}
