package mesh

import (
	"context"
	"encoding/json"
	"testing"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureRemoteSecretDistributionPreservesSelectedClusters(t *testing.T) {
	tests := []struct {
		name               string
		existing           []string
		desired            []string
		resolved           []string
		missingEndpoint    string
		missingMSA         string
		missingTokenRef    string
		missingTokenSecret string
		expected           map[string]string
	}{
		{
			name:     "preserves missing member while refreshing resolved member",
			existing: []string{"a", "b"}, desired: []string{"a", "b"}, resolved: []string{"a"},
			expected: map[string]string{"a": "new-token", "b": "old-token"},
		},
		{
			name:     "preserves all missing members",
			existing: []string{"a", "b"}, desired: []string{"a", "b"},
			expected: map[string]string{"a": "old-token", "b": "old-token"},
		},
		{
			name:     "removes deselected member while preserving missing member",
			existing: []string{"a", "b"}, desired: []string{"b"},
			expected: map[string]string{"b": "old-token"},
		},
		{
			name:     "refreshes restored member",
			existing: []string{"a", "b"}, desired: []string{"a", "b"}, resolved: []string{"a", "b"},
			expected: map[string]string{"a": "new-token", "b": "new-token"},
		},
		{
			name:     "waits for missing member without an existing manifest",
			existing: []string{"a"}, desired: []string{"a", "b"}, resolved: []string{"a"},
			expected: map[string]string{"a": "new-token"},
		},
		{
			name:    "creates empty template when all members are initially missing",
			desired: []string{"a", "b"},
		},
		{
			name:     "clears template when no members remain selected",
			existing: []string{"a", "b"},
		},
		{
			name:     "preserves member without endpoint while refreshing and removing other members",
			existing: []string{"a", "b", "c"}, desired: []string{"a", "b"}, resolved: []string{"a", "b"},
			missingEndpoint: "b",
			expected:        map[string]string{"a": "new-token", "b": "old-token"},
		},
		{
			name:     "preserves member without MSA",
			existing: []string{"a", "b"}, desired: []string{"a", "b"}, resolved: []string{"a", "b"},
			missingMSA: "b",
			expected:   map[string]string{"a": "new-token", "b": "old-token"},
		},
		{
			name:     "preserves member without token reference",
			existing: []string{"a", "b"}, desired: []string{"a", "b"}, resolved: []string{"a", "b"},
			missingTokenRef: "b",
			expected:        map[string]string{"a": "new-token", "b": "old-token"},
		},
		{
			name:     "preserves member without token Secret",
			existing: []string{"a", "b"}, desired: []string{"a", "b"}, resolved: []string{"a", "b"},
			missingTokenSecret: "b",
			expected:           map[string]string{"a": "new-token", "b": "old-token"},
		},
		{
			name:    "waits for endpoint when no previous manifest exists",
			desired: []string{"a"}, resolved: []string{"a"},
			missingEndpoint: "a",
		},
		{
			name:    "waits for token when no previous manifest exists",
			desired: []string{"a"}, resolved: []string{"a"},
			missingTokenRef: "a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			for _, addToScheme := range []func(*runtime.Scheme) error{
				corev1.AddToScheme, meshv1alpha1.Install, workv1alpha1.Install, msav1beta1.AddToScheme,
			} {
				if err := addToScheme(scheme); err != nil {
					t.Fatal(err)
				}
			}
			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{Name: "mesh", Namespace: "mesh-ns", UID: "mesh-uid"},
				Spec: meshv1alpha1.MultiClusterMeshSpec{
					PlacementRef: meshv1alpha1.PlacementReference{Name: "placement"},
				},
			}
			var objects []client.Object
			if tc.existing != nil {
				mwrs := &workv1alpha1.ManifestWorkReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: mesh.Name, Namespace: mesh.Namespace},
				}
				for _, name := range tc.existing {
					secret, err := buildIstioRemoteSecret(&corev1.Secret{Data: map[string][]byte{
						"ca.crt": []byte("old-ca"), "token": []byte("old-token"),
					}}, name, "https://old-"+name+":6443", mesh.GetControlPlaneNamespace())
					if err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(secret)
					if err != nil {
						t.Fatal(err)
					}
					mwrs.Spec.ManifestWorkTemplate.Workload.Manifests = append(mwrs.Spec.ManifestWorkTemplate.Workload.Manifests,
						workv1.Manifest{RawExtension: runtime.RawExtension{Raw: data}})
				}
				objects = append(objects, mwrs)
			}
			var clusters []clusterv1.ManagedCluster
			for _, name := range tc.resolved {
				cluster := clusterv1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec: clusterv1.ManagedClusterSpec{
						ManagedClusterClientConfigs: []clusterv1.ClientConfig{{URL: "https://new-" + name + ":6443"}},
					},
				}
				if name == tc.missingEndpoint {
					cluster.Spec.ManagedClusterClientConfigs = nil
				}
				clusters = append(clusters, cluster)
				msa := &msav1beta1.ManagedServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: EndpointDiscoveryName(mesh), Namespace: name},
					Status:     msav1beta1.ManagedServiceAccountStatus{TokenSecretRef: &msav1beta1.SecretRef{Name: "token"}},
				}
				if name == tc.missingTokenRef {
					msa.Status.TokenSecretRef = nil
				}
				if name != tc.missingMSA {
					objects = append(objects, msa)
				}
				if name != tc.missingTokenSecret {
					objects = append(objects, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: "token", Namespace: name},
						Data:       map[string][]byte{"ca.crt": []byte("new-ca"), "token": []byte("new-token")},
					})
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}
			if err := r.ensureRemoteSecretDistribution(ctx, mesh, clusters, tc.desired); err != nil {
				t.Fatal(err)
			}
			mwrs := &workv1alpha1.ManifestWorkReplicaSet{}
			if err := c.Get(ctx, key.For(mesh), mwrs); err != nil {
				t.Fatal(err)
			}
			manifests := mwrs.Spec.ManifestWorkTemplate.Workload.Manifests
			if len(manifests) != len(tc.expected) {
				t.Fatalf("expected %d manifests, got %d", len(tc.expected), len(manifests))
			}
			for _, manifest := range manifests {
				secret := &corev1.Secret{}
				if err := json.Unmarshal(manifest.Raw, secret); err != nil {
					t.Fatal(err)
				}
				name := secret.Annotations["networking.istio.io/cluster"]
				token, ok := tc.expected[name]
				if !ok {
					t.Fatalf("unexpected remote secret for %s", name)
				}
				config, err := clientcmd.Load(secret.Data[name])
				if err != nil {
					t.Fatal(err)
				}
				if config.AuthInfos[name].Token != token {
					t.Errorf("unexpected token for %s", name)
				}
				prefix := "old-"
				if token == "new-token" {
					prefix = "new-"
				}
				if config.Clusters[name].Server != "https://"+prefix+name+":6443" {
					t.Errorf("unexpected API endpoint for %s: %s", name, config.Clusters[name].Server)
				}
			}
		})
	}
}

func TestEnsureRemoteSecretDistributionDoesNotUpdateUnchangedReplicaSet(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := msav1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "mesh", Namespace: "mesh-ns", UID: "mesh-uid"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "placement"},
		},
	}
	cluster := clusterWithAPIEndpoint("cluster-a")
	msa := &msav1beta1.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: EndpointDiscoveryName(mesh), Namespace: cluster.Name},
		Status:     msav1beta1.ManagedServiceAccountStatus{TokenSecretRef: &msav1beta1.SecretRef{Name: "token"}},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "token", Namespace: cluster.Name},
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey: []byte("test-ca"),
			corev1.ServiceAccountTokenKey:  []byte("test-token"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(msa, tokenSecret).Build()
	r := &Reconciler{Client: c, Scheme: scheme}
	clusters := []clusterv1.ManagedCluster{cluster}
	desiredClusters := []string{cluster.Name}
	if err := r.ensureRemoteSecretDistribution(ctx, mesh, clusters, desiredClusters); err != nil {
		t.Fatal(err)
	}
	mwrs := &workv1alpha1.ManifestWorkReplicaSet{}
	if err := c.Get(ctx, key.For(mesh), mwrs); err != nil {
		t.Fatal(err)
	}
	resourceVersion := mwrs.ResourceVersion
	if err := r.ensureRemoteSecretDistribution(ctx, mesh, clusters, desiredClusters); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key.For(mesh), mwrs); err != nil {
		t.Fatal(err)
	}
	if mwrs.ResourceVersion != resourceVersion {
		t.Errorf("unchanged distribution updated the ReplicaSet: resourceVersion %s became %s", resourceVersion, mwrs.ResourceVersion)
	}
}
