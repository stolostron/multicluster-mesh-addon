package mesh

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func newMSASecret(mesh *meshv1alpha1.MultiClusterMesh, clusterName, ca, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getMSAName(mesh),
			Namespace: clusterName,
		},
		Data: map[string][]byte{
			"ca.crt": []byte(ca),
			"token":  []byte(token),
		},
	}
}

func TestComputeRemoteSecretTargets_MultiPrimary(t *testing.T) {
	scheme := newTestScheme()
	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}

	tests := []struct {
		name             string
		clusters         []clusterv1.ManagedCluster
		wantTargets      int
		wantSourceCounts map[string]int
	}{
		{
			name: "2 clusters, bidirectional",
			clusters: []clusterv1.ManagedCluster{
				{ObjectMeta: metav1.ObjectMeta{Name: "c1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "c2"}},
			},
			wantTargets:      2,
			wantSourceCounts: map[string]int{"c1": 1, "c2": 1},
		},
		{
			name: "3 clusters, each gets 2 sources",
			clusters: []clusterv1.ManagedCluster{
				{ObjectMeta: metav1.ObjectMeta{Name: "c1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "c2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "c3"}},
			},
			wantTargets:      3,
			wantSourceCounts: map[string]int{"c1": 2, "c2": 2, "c3": 2},
		},
		{
			name: "1 cluster, no peers",
			clusters: []clusterv1.ManagedCluster{
				{ObjectMeta: metav1.ObjectMeta{Name: "c1"}},
			},
			wantTargets:      0,
			wantSourceCounts: map[string]int{},
		},
	}

	mesh := newTestMesh("ns", "test-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targets := r.computeRemoteSecretTargets(mesh, tc.clusters)
			if len(targets) != tc.wantTargets {
				t.Fatalf("got %d target clusters, want %d", len(targets), tc.wantTargets)
			}
			for cluster, wantCount := range tc.wantSourceCounts {
				if got := len(targets[cluster]); got != wantCount {
					t.Errorf("target %s: got %d sources, want %d", cluster, got, wantCount)
				}
			}
			for target, sources := range targets {
				for _, src := range sources {
					if src.Name == target {
						t.Errorf("target %s lists itself as a source", target)
					}
				}
			}
		})
	}
}

func TestComputeRemoteSecretTargets_PrimaryRemote(t *testing.T) {
	scheme := newTestScheme()
	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}

	mesh := newTestMesh("ns", "test-mesh", "test-set", meshv1alpha1.TopologyPrimaryRemote, "primary")

	clusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "primary"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "remote1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "remote2"}},
	}

	targets := r.computeRemoteSecretTargets(mesh, clusters)

	if len(targets) != 1 {
		t.Fatalf("got %d target clusters, want 1 (primary only)", len(targets))
	}

	primarySources := targets["primary"]
	if len(primarySources) != 2 {
		t.Fatalf("primary got %d sources, want 2", len(primarySources))
	}

	sourceNames := make(map[string]bool)
	for _, src := range primarySources {
		sourceNames[src.Name] = true
	}
	for _, name := range []string{"remote1", "remote2"} {
		if !sourceNames[name] {
			t.Errorf("expected %s as source for primary, not found", name)
		}
	}

	for _, remote := range []string{"remote1", "remote2"} {
		if _, ok := targets[remote]; ok {
			t.Errorf("remote cluster %s should not be a target", remote)
		}
	}
}

func TestBuildKubeconfig(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"ca.crt": []byte("test-ca-data"),
			"token":  []byte("test-token-value"),
		},
	}

	result := buildKubeconfig("cluster1", "https://api.cluster1.example.com:6443", secret)

	if !strings.Contains(result, "cluster1") {
		t.Error("kubeconfig missing cluster name")
	}
	if !strings.Contains(result, "https://api.cluster1.example.com:6443") {
		t.Error("kubeconfig missing server URL")
	}

	expectedCA := base64.StdEncoding.EncodeToString([]byte("test-ca-data"))
	if !strings.Contains(result, expectedCA) {
		t.Error("kubeconfig missing base64-encoded CA data")
	}
	if !strings.Contains(result, "test-token-value") {
		t.Error("kubeconfig missing token value")
	}

	parsed := make(map[string]any)
	if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("kubeconfig is not valid YAML: %v", err)
	}
	if parsed["apiVersion"] != "v1" {
		t.Errorf("got apiVersion %v, want v1", parsed["apiVersion"])
	}
	if parsed["kind"] != "Config" {
		t.Errorf("got kind %v, want Config", parsed["kind"])
	}
}

func TestComputeTokenHash(t *testing.T) {
	mesh := newTestMesh("ns", "test-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")

	t.Run("same tokens produce same hash", func(t *testing.T) {
		cache1 := map[string]*corev1.Secret{
			"c1": newMSASecret(mesh, "c1", "ca1", "token1"),
			"c2": newMSASecret(mesh, "c2", "ca2", "token2"),
		}
		cache2 := map[string]*corev1.Secret{
			"c1": newMSASecret(mesh, "c1", "ca1", "token1"),
			"c2": newMSASecret(mesh, "c2", "ca2", "token2"),
		}

		hash1 := computeTokenHashFromCache(cache1)
		hash2 := computeTokenHashFromCache(cache2)

		if hash1 != hash2 {
			t.Errorf("same tokens produced different hashes: %s vs %s", hash1, hash2)
		}
	})

	t.Run("different tokens produce different hash", func(t *testing.T) {
		cache1 := map[string]*corev1.Secret{
			"c1": newMSASecret(mesh, "c1", "ca1", "token1"),
			"c2": newMSASecret(mesh, "c2", "ca2", "token2"),
		}
		cache2 := map[string]*corev1.Secret{
			"c1": newMSASecret(mesh, "c1", "ca1", "token1"),
			"c2": newMSASecret(mesh, "c2", "ca2", "different-token"),
		}

		hash1 := computeTokenHashFromCache(cache1)
		hash2 := computeTokenHashFromCache(cache2)

		if hash1 == hash2 {
			t.Error("different tokens produced the same hash")
		}
	})

	t.Run("deterministic across calls", func(t *testing.T) {
		cache := map[string]*corev1.Secret{
			"c1": newMSASecret(mesh, "c1", "ca1", "token1"),
			"c2": newMSASecret(mesh, "c2", "ca2", "token2"),
		}

		hash1 := computeTokenHashFromCache(cache)
		hash2 := computeTokenHashFromCache(cache)

		if hash1 != hash2 {
			t.Errorf("non-deterministic: %s vs %s", hash1, hash2)
		}
	})
}

func TestAreMSATokensAvailable(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("ns", "test-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")

	clusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "c1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "c2"}},
	}

	t.Run("all secrets exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			newMSASecret(mesh, "c1", "ca1", "token1"),
			newMSASecret(mesh, "c2", "ca2", "token2"),
		).Build()
		r := &Reconciler{Client: cl, Scheme: scheme}

		if !r.areMSATokensAvailable(context.Background(), mesh, clusters) {
			t.Error("expected true when all secrets exist")
		}
	})

	t.Run("missing secret", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			newMSASecret(mesh, "c1", "ca1", "token1"),
		).Build()
		r := &Reconciler{Client: cl, Scheme: scheme}

		if r.areMSATokensAvailable(context.Background(), mesh, clusters) {
			t.Error("expected false when a secret is missing")
		}
	})
}

func TestGetAPIServerURL(t *testing.T) {
	tests := []struct {
		name    string
		cluster *clusterv1.ManagedCluster
		want    string
	}{
		{
			name:    "returns URL from first ClientConfig",
			cluster: newTestClusterWithURL("c1", "set", "https://api.c1.example.com:6443"),
			want:    "https://api.c1.example.com:6443",
		},
		{
			name:    "returns empty when no configs",
			cluster: newTestCluster("c1"),
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getAPIServerURL(tc.cluster); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindMeshesForMSASecret(t *testing.T) {
	scheme := newTestScheme()

	mesh := newTestMesh("ns", "test-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")

	cluster := newTestClusterWithURL("c1", "test-set", "https://api.c1.example.com:6443")

	buildClient := func(objs ...client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(scheme).
			WithIndex(&meshv1alpha1.MultiClusterMesh{}, "spec.clusterSet", func(obj client.Object) []string {
				return []string{obj.(*meshv1alpha1.MultiClusterMesh).Spec.ClusterSet}
			}).
			WithObjects(objs...).
			Build()
	}

	t.Run("returns reconcile requests for matching meshes", func(t *testing.T) {
		msaSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-msa-secret",
				Namespace: "c1",
				Labels:    map[string]string{MSASecretLabel: "true"},
			},
		}

		cl := buildClient(mesh, cluster, msaSecret)
		r := &Reconciler{Client: cl, Scheme: scheme}

		requests := r.findMeshesForMSASecret(context.Background(), msaSecret)
		if len(requests) != 1 {
			t.Fatalf("got %d requests, want 1", len(requests))
		}
		want := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(mesh)}
		if requests[0] != want {
			t.Errorf("got request %v, want %v", requests[0], want)
		}
	})

	t.Run("returns nil for secret without MSA label", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "plain-secret",
				Namespace: "c1",
			},
		}

		cl := buildClient(mesh, cluster, secret)
		r := &Reconciler{Client: cl, Scheme: scheme}

		if requests := r.findMeshesForMSASecret(context.Background(), secret); requests != nil {
			t.Errorf("got %v, want nil", requests)
		}
	})

	t.Run("returns nil when cluster not found", func(t *testing.T) {
		msaSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "orphan-msa-secret",
				Namespace: "nonexistent-cluster",
				Labels:    map[string]string{MSASecretLabel: "true"},
			},
		}

		cl := buildClient(mesh, msaSecret)
		r := &Reconciler{Client: cl, Scheme: scheme}

		if requests := r.findMeshesForMSASecret(context.Background(), msaSecret); requests != nil {
			t.Errorf("got %v, want nil", requests)
		}
	})
}
