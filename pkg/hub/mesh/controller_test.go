package mesh

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func TestGetClustersFromSetReturnsSortedClusters(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clusterv1.Install(scheme)
	_ = clusterv1beta2.Install(scheme)
	_ = meshv1alpha1.Install(scheme)

	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-set"},
	}

	clusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c", Labels: map[string]string{ClusterSetLabel: "test-set"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Labels: map[string]string{ClusterSetLabel: "test-set"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b", Labels: map[string]string{ClusterSetLabel: "test-set"}}},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterSet, &clusters[0], &clusters[1], &clusters[2]).
		Build()

	r := &Reconciler{Client: client, Scheme: scheme}

	result, err := r.getClustersFromSet(context.Background(), "test-set")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(result))
	}

	expected := []string{"cluster-a", "cluster-b", "cluster-c"}
	for i, name := range expected {
		if result[i].Name != name {
			t.Errorf("expected cluster[%d] = %s, got %s", i, name, result[i].Name)
		}
	}
}

func TestIsOlderMesh(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Second))

	tests := []struct {
		name     string
		a, b     *meshv1alpha1.MultiClusterMesh
		expected bool
	}{
		{
			name:     "a is older by timestamp",
			a:        meshWith("ns", "mesh-a", now),
			b:        meshWith("ns", "mesh-b", later),
			expected: true,
		},
		{
			name:     "b is older by timestamp",
			a:        meshWith("ns", "mesh-a", later),
			b:        meshWith("ns", "mesh-b", now),
			expected: false,
		},
		{
			name:     "same timestamp, a sorts first by name",
			a:        meshWith("ns", "mesh-a", now),
			b:        meshWith("ns", "mesh-b", now),
			expected: true,
		},
		{
			name:     "same timestamp, b sorts first by name",
			a:        meshWith("ns", "mesh-b", now),
			b:        meshWith("ns", "mesh-a", now),
			expected: false,
		},
		{
			name:     "same timestamp, a sorts first by namespace",
			a:        meshWith("aaa", "mesh", now),
			b:        meshWith("zzz", "mesh", now),
			expected: true,
		},
		{
			name:     "same timestamp and key",
			a:        meshWith("ns", "mesh", now),
			b:        meshWith("ns", "mesh", now),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOlderMesh(tc.a, tc.b); got != tc.expected {
				t.Errorf("isOlderMesh() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestBuildManifestWorkReplicaSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clusterv1.Install(scheme)
	_ = clusterv1beta1.Install(scheme)
	_ = clusterv1beta2.Install(scheme)
	_ = workv1.Install(scheme)
	_ = workv1alpha1.Install(scheme)

	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-set"},
	}

	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Labels: map[string]string{ClusterSetLabel: "test-set"}},
	}

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement"},
		Spec:       clusterv1beta1.PlacementSpec{ClusterSets: []string{clusterSet.Name}},
	}

	r := &Reconciler{}

	mesh := meshWith("ns", "test-mesh", metav1.Now())
	mw := r.buildOperatorManifestWork(mesh, cluster)
	mwrs := r.buildManifestWorkReplicaSet(mesh, placement.Name, mw.Spec)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterSet, cluster, placement, mw, mwrs).
		Build()

	r = &Reconciler{Client: client, Scheme: scheme}

	_, err := r.getManifestWorkReplicaSet(context.Background(), "test-mwrs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func meshWith(namespace, name string, ts metav1.Time) *meshv1alpha1.MultiClusterMesh {
	return &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: ts,
		},
	}
}
