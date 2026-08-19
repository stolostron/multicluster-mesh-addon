package mesh

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func TestGetClustersFromSetReturnsSortedClusters(t *testing.T) {
	scheme := newTestScheme()

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

func TestDetermineStatusLastTransitionTime(t *testing.T) {
	tests := []struct {
		name              string
		initialStatus     metav1.ConditionStatus
		initialReason     string
		withCSV           bool
		expectStatus      metav1.ConditionStatus
		expectTimeChanged bool
	}{
		{"preserves when unchanged (True->True)", metav1.ConditionTrue, meshv1alpha1.ReasonOperatorInstalled, true, metav1.ConditionTrue, false},
		{"updates on install (False->True)", metav1.ConditionFalse, meshv1alpha1.ReasonInstallationPending, true, metav1.ConditionTrue, true},
		{"preserves when still pending (False->False)", metav1.ConditionFalse, meshv1alpha1.ReasonInstallationPending, false, metav1.ConditionFalse, false},
		{"updates on CSV removal (True->False)", metav1.ConditionTrue, meshv1alpha1.ReasonOperatorInstalled, false, metav1.ConditionFalse, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newTestScheme()
			clusterName := "cluster-a"
			hourAgo := metav1.NewTime(time.Now().Add(-time.Hour))

			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
				Status: meshv1alpha1.MultiClusterMeshStatus{
					ClusterStatus: []meshv1alpha1.ClusterMeshStatus{{
						ClusterName: clusterName,
						Conditions: []metav1.Condition{{
							Type:               meshv1alpha1.ConditionOperatorInstalled,
							Status:             tt.initialStatus,
							Reason:             tt.initialReason,
							LastTransitionTime: hourAgo,
						}},
					}},
				},
			}

			var mw *workv1.ManifestWork
			if tt.withCSV {
				mw = operatorManifestWorkWithInstalledCSV(clusterName)
			} else {
				mw = operatorManifestWork(clusterName)
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(mw).
				WithStatusSubresource(&workv1.ManifestWork{}).
				Build()

			r := &Reconciler{Client: c, Scheme: scheme}
			clusters := []clusterv1.ManagedCluster{{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}}

			if err := r.determineStatus(context.Background(), mesh, clusters); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cond := meta.FindStatusCondition(mesh.Status.ClusterStatus[0].Conditions, meshv1alpha1.ConditionOperatorInstalled)
			if cond == nil {
				t.Fatal("OperatorInstalled condition not found")
			}
			if cond.Status != tt.expectStatus {
				t.Errorf("expected status %s, got %s", tt.expectStatus, cond.Status)
			}
			timeChanged := !cond.LastTransitionTime.Equal(&hourAgo)
			if timeChanged != tt.expectTimeChanged {
				t.Errorf("LastTransitionTime changed=%v, want changed=%v", timeChanged, tt.expectTimeChanged)
			}
		})
	}
}

func TestDetermineStatusPrunesStaleCluster(t *testing.T) {
	scheme := newTestScheme()
	activeCluster := "cluster-a"

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{
				{ClusterName: activeCluster},
				{ClusterName: "removed-cluster"},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(operatorManifestWorkWithInstalledCSV(activeCluster)).
		WithStatusSubresource(&workv1.ManifestWork{}).
		Build()

	r := &Reconciler{Client: client, Scheme: scheme}
	clusters := []clusterv1.ManagedCluster{{ObjectMeta: metav1.ObjectMeta{Name: activeCluster}}}

	if err := r.determineStatus(context.Background(), mesh, clusters); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mesh.Status.ClusterStatus) != 1 {
		t.Fatalf("expected 1 cluster status, got %d", len(mesh.Status.ClusterStatus))
	}
	if mesh.Status.ClusterStatus[0].ClusterName != activeCluster {
		t.Errorf("expected cluster %s, got %s", activeCluster, mesh.Status.ClusterStatus[0].ClusterName)
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

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clusterv1.Install(scheme)
	_ = clusterv1beta2.Install(scheme)
	_ = meshv1alpha1.Install(scheme)
	_ = workv1.Install(scheme)
	return scheme
}

const testInstalledCSV = "istio-operator.v1.0.0"

func operatorManifestWork(clusterName string) *workv1.ManifestWork {
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorManifestWorkName,
			Namespace: clusterName,
		},
	}
}

func operatorManifestWorkWithInstalledCSV(clusterName string) *workv1.ManifestWork {
	csv := testInstalledCSV
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorManifestWorkName,
			Namespace: clusterName,
		},
		Status: workv1.ManifestWorkStatus{
			ResourceStatus: workv1.ManifestResourceStatus{
				Manifests: []workv1.ManifestCondition{{
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{{
							Name:  FeedbackInstalledCSV,
							Value: workv1.FieldValue{String: &csv},
						}},
					},
				}},
			},
		},
	}
}
