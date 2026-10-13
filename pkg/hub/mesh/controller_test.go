package mesh

import (
	"context"
	"errors"
	"strings"
	"testing"

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

	result, desiredClusters, found, err := r.getClustersFromPlacement(context.Background(), mesh)
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

	if len(desiredClusters) != 3 {
		t.Fatalf("expected 3 desiredClusters, got %d", len(desiredClusters))
	}
	for i, name := range expected {
		if desiredClusters[i] != name {
			t.Errorf("expected desiredClusters[%d] = %s, got %s", i, name, desiredClusters[i])
		}
	}
}

func TestDetermineStatus(t *testing.T) {
	tests := []struct {
		name         string
		clusters     []clusterv1.ManagedCluster
		expectReady  bool
		expectReason string
	}{
		{
			name: "sets Ready when clusters exist",
			clusters: []clusterv1.ManagedCluster{
				{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
			},
			expectReady:  true,
			expectReason: meshv1alpha1.ReasonAllClustersReady,
		},
		{
			name:         "does not set Ready when no clusters",
			clusters:     nil,
			expectReady:  false,
			expectReason: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
			}
			r := &Reconciler{}
			r.determineStatus(mesh, tc.clusters)

			cond := meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)
			if !tc.expectReady {
				if cond != nil {
					t.Errorf("expected no Ready condition, got %v", cond)
				}
				return
			}
			if cond == nil {
				t.Fatal("expected Ready condition to be set")
			}
			if cond.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready=True, got %s", cond.Status)
			}
			if cond.Reason != tc.expectReason {
				t.Errorf("expected reason %s, got %s", tc.expectReason, cond.Reason)
			}
		})
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

	result, desiredClusters, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected placementFound=false")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}
	if desiredClusters != nil {
		t.Fatalf("expected nil desiredClusters, got %v", desiredClusters)
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

	result, desiredClusters, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}
	if desiredClusters != nil {
		t.Fatalf("expected nil desiredClusters, got %v", desiredClusters)
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

	result, desiredClusters, found, err := r.getClustersFromPlacement(context.Background(), mesh)
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

	// desiredClusters includes ALL PlacementDecision entries, even the missing
	// cluster. This prevents cleanup functions from prematurely deleting
	// resources for clusters whose ManagedCluster is temporarily unavailable.
	if len(desiredClusters) != 2 {
		t.Fatalf("expected 2 desiredClusters, got %d: %v", len(desiredClusters), desiredClusters)
	}
	expectedNames := []string{"cluster-exists", "cluster-missing"}
	for i, name := range expectedNames {
		if desiredClusters[i] != name {
			t.Errorf("expected desiredClusters[%d] = %s, got %s", i, name, desiredClusters[i])
		}
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

	result, desiredClusters, found, err := r.getClustersFromPlacement(context.Background(), mesh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected placementFound=true")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(result))
	}

	// desiredClusters includes the PlacementDecision entries even though all
	// ManagedClusters are gone, preventing premature cleanup.
	if len(desiredClusters) != 2 {
		t.Fatalf("expected 2 desiredClusters, got %d: %v", len(desiredClusters), desiredClusters)
	}
	expectedNames := []string{"gone-cluster-1", "gone-cluster-2"}
	for i, name := range expectedNames {
		if desiredClusters[i] != name {
			t.Errorf("expected desiredClusters[%d] = %s, got %s", i, name, desiredClusters[i])
		}
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
					Name: "unlabeled-work", Namespace: "cluster-a",
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
		name     string
		work     *workv1.ManifestWork
		expected []types.NamespacedName
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
			name: "ManifestWork without mesh labels returns empty",
			work: &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unlabeled-work", Namespace: "cluster-a",
					Labels: map[string]string{ManagedByLabel: ManagedByValue},
				},
			},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.work).Build()
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

			r := &Reconciler{}
			r.pruneStaleClusterStatus(mesh, tc.active)

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

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clusterv1.Install(scheme)
	_ = clusterv1beta1.Install(scheme)
	_ = meshv1alpha1.Install(scheme)
	_ = workv1.Install(scheme)
	return scheme
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
