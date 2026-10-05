package mesh

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func TestCheckPrerequisiteCRDs(t *testing.T) {
	certGVK := certmanagerv1.SchemeGroupVersion.WithKind("Certificate")
	msaGVK := msav1beta1.GroupVersion.WithKind("ManagedServiceAccount")

	tests := []struct {
		name        string
		gvks        []schema.GroupVersionKind
		expectError bool
		contains    []string
	}{
		{
			name:        "all present",
			gvks:        []schema.GroupVersionKind{certGVK, msaGVK},
			expectError: false,
		},
		{
			name:        "cert-manager missing",
			gvks:        []schema.GroupVersionKind{msaGVK},
			expectError: true,
			contains:    []string{"cert-manager"},
		},
		{
			name:        "managed-serviceaccount missing",
			gvks:        []schema.GroupVersionKind{certGVK},
			expectError: true,
			contains:    []string{"managed-serviceaccount"},
		},
		{
			name:        "both missing",
			gvks:        nil,
			expectError: true,
			contains:    []string{"cert-manager", "managed-serviceaccount"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper(nil)
			for _, gvk := range tc.gvks {
				mapper.Add(gvk, meta.RESTScopeNamespace)
			}

			err := checkPrerequisiteCRDs(mapper)
			if (err != nil) != tc.expectError {
				t.Fatalf("expectError=%v, got: %v", tc.expectError, err)
			}
			for _, s := range tc.contains {
				if err != nil && !strings.Contains(err.Error(), s) {
					t.Errorf("expected error to contain %q, got: %v", s, err)
				}
			}
		})
	}
}

var errAPIServerUnreachable = errors.New("API server unreachable")

type errorMapper struct {
	meta.RESTMapper
}

func (m *errorMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	return nil, errAPIServerUnreachable
}

func TestCheckPrerequisiteCRDsUnexpectedError(t *testing.T) {
	err := checkPrerequisiteCRDs(&errorMapper{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errAPIServerUnreachable) {
		t.Errorf("expected original error, got: %v", err)
	}
}

func TestGetClustersFromPlacementReturnsSortedClusters(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 3},
	}

	pd := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-placement-decision-1",
			Namespace: "default",
			Labels:    map[string]string{PlacementLabel: "test-placement"},
		},
	}

	clusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(placement, pd, &clusters[0], &clusters[1], &clusters[2]).
		WithStatusSubresource(&clusterv1beta1.PlacementDecision{}).
		Build()

	// Fake client doesn't handle status subresources via WithObjects, so update status separately
	pd.Status = clusterv1beta1.PlacementDecisionStatus{
		Decisions: []clusterv1beta1.ClusterDecision{
			{ClusterName: "cluster-c"},
			{ClusterName: "cluster-a"},
			{ClusterName: "cluster-b"},
		},
	}
	if err := c.Status().Update(context.Background(), pd); err != nil {
		t.Fatalf("failed to update PlacementDecision status: %v", err)
	}

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "test-placement"},
		},
	}

	r := &Reconciler{Client: c, Scheme: scheme}

	result, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
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
				return
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

	r.pruneStaleClusterStatus(mesh, clusters)
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

func TestGetClustersFromPlacementNotFound(t *testing.T) {
	scheme := newTestScheme()

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "missing-placement"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	result, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected placementFound=false")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}

	cond := meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
		return
	}
	if cond.Reason != meshv1alpha1.ReasonPlacementNotFound {
		t.Errorf("expected reason %s, got %s", meshv1alpha1.ReasonPlacementNotFound, cond.Reason)
	}
}

func TestGetClustersFromPlacementNoClustersSelected(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
	}

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "test-placement"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(placement).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	result, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}

	cond := meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
		return
	}
	if cond.Reason != meshv1alpha1.ReasonNoClustersSelected {
		t.Errorf("expected reason %s, got %s", meshv1alpha1.ReasonNoClustersSelected, cond.Reason)
	}
}

func TestGetClustersFromPlacementSkipsMissingCluster(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 2},
	}

	pd := placementDecision("test-placement", "default", "cluster-exists", "cluster-missing")

	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-exists"},
	}

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "test-placement"},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(placement, pd, cluster).
		WithStatusSubresource(&clusterv1beta1.PlacementDecision{}).
		Build()
	pd.Status.Decisions = []clusterv1beta1.ClusterDecision{
		{ClusterName: "cluster-exists"},
		{ClusterName: "cluster-missing"},
	}
	if err := c.Status().Update(context.Background(), pd); err != nil {
		t.Fatalf("failed to update PlacementDecision status: %v", err)
	}

	r := &Reconciler{Client: c, Scheme: scheme}

	result, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(result))
	}
	if result[0].Name != "cluster-exists" {
		t.Errorf("expected cluster-exists, got %s", result[0].Name)
	}
}

func TestGetClustersFromPlacementAllMissing(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 2},
	}

	pd := placementDecision("test-placement", "default", "gone-cluster-1", "gone-cluster-2")

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "test-placement"},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(placement, pd).
		WithStatusSubresource(&clusterv1beta1.PlacementDecision{}).
		Build()
	pd.Status.Decisions = []clusterv1beta1.ClusterDecision{
		{ClusterName: "gone-cluster-1"},
		{ClusterName: "gone-cluster-2"},
	}
	if err := c.Status().Update(context.Background(), pd); err != nil {
		t.Fatalf("failed to update PlacementDecision status: %v", err)
	}

	r := &Reconciler{Client: c, Scheme: scheme}

	result, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}

	cond := meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
		return
	}
	if cond.Reason != meshv1alpha1.ReasonNoClustersSelected {
		t.Errorf("expected reason %s, got %s", meshv1alpha1.ReasonNoClustersSelected, cond.Reason)
	}
	if !strings.Contains(cond.Message, "none of the selected ManagedClusters exist") {
		t.Errorf("expected message about missing ManagedClusters, got: %s", cond.Message)
	}
}

func TestFindMeshesForCluster(t *testing.T) {
	scheme := newTestScheme()

	tests := []struct {
		name     string
		works    []workv1.ManifestWork
		expected []types.NamespacedName
	}{
		{
			name: "finds mesh from labeled ManifestWork",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "some-work", "mesh-a", "ns-a"),
			},
			expected: []types.NamespacedName{{Name: "mesh-a", Namespace: "ns-a"}},
		},
		{
			name: "skips ManifestWorks without mesh labels",
			works: []workv1.ManifestWork{{
				ObjectMeta: metav1.ObjectMeta{
					Name: OperatorManifestWorkName, Namespace: "cluster-a",
					Labels: map[string]string{ManagedByLabel: ManagedByValue},
				},
			}},
			expected: nil,
		},
		{
			name: "deduplicates multiple ManifestWorks for same mesh",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "mesh-a", "ns-a"),
				meshOwnedWork("cluster-a", "work-2", "mesh-a", "ns-a"),
			},
			expected: []types.NamespacedName{{Name: "mesh-a", Namespace: "ns-a"}},
		},
		{
			name: "returns multiple meshes from different ManifestWorks",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "mesh-a", "ns-a"),
				meshOwnedWork("cluster-a", "work-2", "mesh-b", "ns-b"),
			},
			expected: []types.NamespacedName{
				{Name: "mesh-a", Namespace: "ns-a"},
				{Name: "mesh-b", Namespace: "ns-b"},
			},
		},
		{
			name:     "returns nil for no ManifestWorks",
			works:    nil,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := make([]runtime.Object, len(tc.works))
			for i := range tc.works {
				objs[i] = &tc.works[i]
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}

			cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}
			requests := r.findMeshesForCluster(context.Background(), cluster)

			if len(requests) != len(tc.expected) {
				t.Fatalf("expected %d requests, got %d", len(tc.expected), len(requests))
			}
			for i, exp := range tc.expected {
				if requests[i].NamespacedName != exp {
					t.Errorf("request[%d] = %v, want %v", i, requests[i].NamespacedName, exp)
				}
			}
		})
	}
}

func TestFindMeshesForManifestWork(t *testing.T) {
	scheme := newTestScheme()

	tests := []struct {
		name      string
		work      *workv1.ManifestWork
		peerWorks []workv1.ManifestWork
		expected  []types.NamespacedName
	}{
		{
			name: "mesh-owned ManifestWork returns owning mesh directly",
			work: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "some-mesh-work", Namespace: "cluster-a",
					Labels: map[string]string{
						ManagedByLabel:     ManagedByValue,
						MeshNameLabel:      "my-mesh",
						MeshNamespaceLabel: "my-ns",
					},
				},
			},
			expected: []types.NamespacedName{{Name: "my-mesh", Namespace: "my-ns"}},
		},
		{
			name: "operator ManifestWork falls back to cluster-based lookup",
			work: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: OperatorManifestWorkName, Namespace: "cluster-a",
					Labels: map[string]string{ManagedByLabel: ManagedByValue},
				},
			},
			peerWorks: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "peer-work", "mesh-x", "ns-x"),
			},
			expected: []types.NamespacedName{{Name: "mesh-x", Namespace: "ns-x"}},
		},
		{
			name: "operator ManifestWork with no peers returns empty",
			work: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: OperatorManifestWorkName, Namespace: "cluster-a",
					Labels: map[string]string{ManagedByLabel: ManagedByValue},
				},
			},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := []runtime.Object{tc.work}
			for i := range tc.peerWorks {
				objs = append(objs, &tc.peerWorks[i])
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}

			requests := r.findMeshesForManifestWork(context.Background(), tc.work)

			if len(requests) != len(tc.expected) {
				t.Fatalf("expected %d requests, got %d", len(tc.expected), len(requests))
			}
			for i, exp := range tc.expected {
				if requests[i].NamespacedName != exp {
					t.Errorf("request[%d] = %v, want %v", i, requests[i].NamespacedName, exp)
				}
			}
		})
	}
}

func TestFindMeshesForPlacementDecision(t *testing.T) {
	scheme := newTestScheme()

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mesh", Namespace: "default"},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			PlacementRef: meshv1alpha1.PlacementReference{Name: "my-placement"},
		},
	}

	tests := []struct {
		name     string
		pd       *clusterv1beta1.PlacementDecision
		expected int
	}{
		{
			name: "PlacementDecision with matching label returns mesh",
			pd: &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-placement-decision-1", Namespace: "default",
					Labels: map[string]string{PlacementLabel: "my-placement"},
				},
			},
			expected: 1,
		},
		{
			name: "PlacementDecision without label returns empty",
			pd: &clusterv1beta1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{Name: "no-label-pd", Namespace: "default"},
			},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(mesh).
				WithIndex(&meshv1alpha1.MultiClusterMesh{}, "spec.placementRef.name", func(obj client.Object) []string {
					return []string{obj.(*meshv1alpha1.MultiClusterMesh).Spec.PlacementRef.Name}
				}).
				Build()

			r := &Reconciler{Client: c, Scheme: scheme}
			requests := r.findMeshesForPlacementDecision(context.Background(), tc.pd)

			if len(requests) != tc.expected {
				t.Fatalf("expected %d requests, got %d", tc.expected, len(requests))
			}
			if tc.expected > 0 {
				if requests[0].Name != "my-mesh" || requests[0].Namespace != "default" {
					t.Errorf("expected my-mesh/default, got %v", requests[0].NamespacedName)
				}
			}
		})
	}
}

func TestFindOlderPeerMeshes(t *testing.T) {
	scheme := newTestScheme()
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-time.Minute))

	currentMesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name: "current-mesh", Namespace: "default",
			CreationTimestamp: now,
		},
	}

	tests := []struct {
		name        string
		works       []workv1.ManifestWork
		otherMeshes []*meshv1alpha1.MultiClusterMesh
		expectPeers []string
	}{
		{
			name:        "no ManifestWorks on cluster",
			works:       nil,
			expectPeers: nil,
		},
		{
			name: "only operator ManifestWork (no mesh labels)",
			works: []workv1.ManifestWork{{
				ObjectMeta: metav1.ObjectMeta{
					Name: OperatorManifestWorkName, Namespace: "cluster-a",
					Labels: map[string]string{ManagedByLabel: ManagedByValue},
				},
			}},
			expectPeers: nil,
		},
		{
			name: "own ManifestWork is skipped",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "current-mesh", "default"),
			},
			expectPeers: nil,
		},
		{
			name: "older mesh on same cluster is returned",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "older-mesh", "default"),
			},
			otherMeshes: []*meshv1alpha1.MultiClusterMesh{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "older-mesh", Namespace: "default",
					CreationTimestamp: earlier,
				},
			}},
			expectPeers: []string{"older-mesh"},
		},
		{
			name: "newer mesh on same cluster is not returned",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "newer-mesh", "default"),
			},
			otherMeshes: []*meshv1alpha1.MultiClusterMesh{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "newer-mesh", Namespace: "default",
					CreationTimestamp: metav1.NewTime(now.Add(time.Minute)),
				},
			}},
			expectPeers: nil,
		},
		{
			name: "deleting mesh is not returned",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "deleting-mesh", "default"),
			},
			otherMeshes: []*meshv1alpha1.MultiClusterMesh{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "deleting-mesh", Namespace: "default",
					CreationTimestamp: earlier,
					DeletionTimestamp: &now,
					Finalizers:        []string{"test"},
				},
			}},
			expectPeers: nil,
		},
		{
			name: "returns all older peers not just the first",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "mesh-a", "default"),
				meshOwnedWork("cluster-a", "work-2", "mesh-b", "default"),
			},
			otherMeshes: []*meshv1alpha1.MultiClusterMesh{
				{ObjectMeta: metav1.ObjectMeta{Name: "mesh-a", Namespace: "default", CreationTimestamp: earlier}},
				{ObjectMeta: metav1.ObjectMeta{Name: "mesh-b", Namespace: "default", CreationTimestamp: earlier}},
			},
			expectPeers: []string{"mesh-a", "mesh-b"},
		},
		{
			name: "deduplicates multiple ManifestWorks from same mesh",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "mesh-a", "default"),
				meshOwnedWork("cluster-a", "work-2", "mesh-a", "default"),
			},
			otherMeshes: []*meshv1alpha1.MultiClusterMesh{
				{ObjectMeta: metav1.ObjectMeta{Name: "mesh-a", Namespace: "default", CreationTimestamp: earlier}},
			},
			expectPeers: []string{"mesh-a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0)
			for i := range tc.works {
				objs = append(objs, &tc.works[i])
			}
			for _, m := range tc.otherMeshes {
				objs = append(objs, m)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}

			result, err := r.findOlderPeerMeshes(context.Background(), currentMesh, "cluster-a")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tc.expectPeers) {
				names := make([]string, len(result))
				for i, p := range result {
					names[i] = p.Name
				}
				t.Fatalf("expected %d peers %v, got %d: %v", len(tc.expectPeers), tc.expectPeers, len(result), names)
			}
			resultNames := map[string]bool{}
			for _, p := range result {
				resultNames[p.Name] = true
			}
			for _, exp := range tc.expectPeers {
				if !resultNames[exp] {
					t.Errorf("expected peer %s not found in result", exp)
				}
			}
		})
	}
}

func TestGetClusterNamespacesFromManifestWorks(t *testing.T) {
	scheme := newTestScheme()

	tests := []struct {
		name     string
		works    []workv1.ManifestWork
		expected []string
	}{
		{
			name: "collects unique cluster namespaces",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "my-mesh", "default"),
				meshOwnedWork("cluster-b", "work-2", "my-mesh", "default"),
				meshOwnedWork("cluster-a", "work-3", "my-mesh", "default"),
			},
			expected: []string{"cluster-a", "cluster-b"},
		},
		{
			name: "ignores ManifestWorks from other meshes",
			works: []workv1.ManifestWork{
				meshOwnedWork("cluster-a", "work-1", "my-mesh", "default"),
				meshOwnedWork("cluster-b", "work-2", "other-mesh", "other-ns"),
			},
			expected: []string{"cluster-a"},
		},
		{
			name:     "returns nil for no ManifestWorks",
			works:    nil,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := make([]runtime.Object, len(tc.works))
			for i := range tc.works {
				objs[i] = &tc.works[i]
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}

			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{Name: "my-mesh", Namespace: "default"},
			}
			result, err := r.getClusterNamespacesFromManifestWorks(context.Background(), mesh)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d namespaces, got %d: %v", len(tc.expected), len(result), result)
			}
			resultSet := make(map[string]bool)
			for _, ns := range result {
				resultSet[ns] = true
			}
			for _, exp := range tc.expected {
				if !resultSet[exp] {
					t.Errorf("expected namespace %s not found in result %v", exp, result)
				}
			}
		})
	}
}

func TestPruneStaleClusterStatus(t *testing.T) {
	tests := []struct {
		name     string
		initial  []string
		active   []string
		expected []string
	}{
		{
			name:     "prunes removed cluster",
			initial:  []string{"cluster-a", "cluster-b"},
			active:   []string{"cluster-a"},
			expected: []string{"cluster-a"},
		},
		{
			name:     "prunes all when no active clusters",
			initial:  []string{"cluster-a", "cluster-b"},
			active:   nil,
			expected: nil,
		},
		{
			name:     "preserves all when all active",
			initial:  []string{"cluster-a", "cluster-b"},
			active:   []string{"cluster-a", "cluster-b"},
			expected: []string{"cluster-a", "cluster-b"},
		},
		{
			name:     "noop when already empty",
			initial:  nil,
			active:   []string{"cluster-a"},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := make([]meshv1alpha1.ClusterMeshStatus, len(tc.initial))
			for i, name := range tc.initial {
				status[i] = meshv1alpha1.ClusterMeshStatus{ClusterName: name}
			}
			mesh := &meshv1alpha1.MultiClusterMesh{
				Status: meshv1alpha1.MultiClusterMeshStatus{ClusterStatus: status},
			}

			clusters := make([]clusterv1.ManagedCluster, len(tc.active))
			for i, name := range tc.active {
				clusters[i] = clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
			}

			r := &Reconciler{}
			r.pruneStaleClusterStatus(mesh, clusters)

			if len(mesh.Status.ClusterStatus) != len(tc.expected) {
				t.Fatalf("expected %d cluster statuses, got %d", len(tc.expected), len(mesh.Status.ClusterStatus))
			}
			for i, exp := range tc.expected {
				if mesh.Status.ClusterStatus[i].ClusterName != exp {
					t.Errorf("status[%d] = %s, want %s", i, mesh.Status.ClusterStatus[i].ClusterName, exp)
				}
			}
		})
	}
}

func TestBuildOperatorManifestWorkLabels(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mesh", Namespace: "my-ns"},
	}
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
	}

	r := &Reconciler{}
	work := r.buildOperatorManifestWork(mesh, cluster)

	if work.Labels[ManagedByLabel] != ManagedByValue {
		t.Errorf("expected %s=%s, got %s", ManagedByLabel, ManagedByValue, work.Labels[ManagedByLabel])
	}
	if _, ok := work.Labels[MeshNameLabel]; ok {
		t.Error("operator ManifestWork should not have MeshNameLabel (shared infrastructure)")
	}
	if _, ok := work.Labels[MeshNamespaceLabel]; ok {
		t.Error("operator ManifestWork should not have MeshNamespaceLabel (shared infrastructure)")
	}
	if work.Namespace != "cluster-a" {
		t.Errorf("expected namespace cluster-a, got %s", work.Namespace)
	}
	if work.Name != OperatorManifestWorkName {
		t.Errorf("expected name %s, got %s", OperatorManifestWorkName, work.Name)
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
	_ = clusterv1beta1.Install(scheme)
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

func meshOwnedWork(namespace, name, meshName, meshNamespace string) workv1.ManifestWork {
	return workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabel:     ManagedByValue,
				MeshNameLabel:      meshName,
				MeshNamespaceLabel: meshNamespace,
			},
		},
	}
}

func placementDecision(placementName, namespace string, clusterNames ...string) *clusterv1beta1.PlacementDecision {
	decisions := make([]clusterv1beta1.ClusterDecision, len(clusterNames))
	for i, name := range clusterNames {
		decisions[i] = clusterv1beta1.ClusterDecision{ClusterName: name}
	}
	return &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      placementName + "-decision-1",
			Namespace: namespace,
			Labels:    map[string]string{PlacementLabel: placementName},
		},
		Status: clusterv1beta1.PlacementDecisionStatus{Decisions: decisions},
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
