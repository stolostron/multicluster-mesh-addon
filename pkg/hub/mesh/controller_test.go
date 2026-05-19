package mesh

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
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
