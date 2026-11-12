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

func TestGetClustersFromPlacementResolvesClusters(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 3},
	}

	pd := placementDecision("test-placement", "default", "cluster-c", "cluster-a", "cluster-b")

	expectedClusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pd, &expectedClusters[0], &expectedClusters[1], &expectedClusters[2]).
		Build()

	r := &Reconciler{Client: c, Scheme: scheme}

	clusters, err := r.getClustersFromPlacement(context.Background(), placement)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(clusters))
	}
	for _, name := range []string{"cluster-a", "cluster-b", "cluster-c"} {
		cluster, ok := clusters[name]
		if !ok {
			t.Errorf("expected %s in clusters", name)
			continue
		}
		if cluster == nil {
			t.Errorf("expected clusters object for %s", name)
		}
	}
}

func TestGetClustersFromPlacementDeduplicatesDecisions(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 3},
	}

	// cluster-b is listed in both PlacementDecisions, as happens while OCM redistributes decisions one object at a time.
	pd1 := placementDecision("test-placement", "default", "cluster-a", "cluster-b")
	pd2 := placementDecision("test-placement", "default", "cluster-b", "cluster-c")
	pd2.Name = "test-placement-decision-2"

	expectedClusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"}},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pd1, pd2, &expectedClusters[0], &expectedClusters[1], &expectedClusters[2]).
		Build()

	r := &Reconciler{Client: c, Scheme: scheme}

	clusters, err := r.getClustersFromPlacement(context.Background(), placement)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters, got %d: %v", len(clusters), clusters)
	}
	for _, name := range []string{"cluster-a", "cluster-b", "cluster-c"} {
		if clusters[name] == nil {
			t.Errorf("expected cluster object for %s", name)
		}
	}
}

func TestDetermineStatus(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
	}

	tests := []struct {
		name               string
		mesh               *meshv1alpha1.MultiClusterMesh
		clusterNames       []string
		placementMissing   bool
		works              []workv1.ManifestWork
		expectReadyStatus  metav1.ConditionStatus
		expectReason       string
		expectClusters     []string
		expectClusterTrust map[string]metav1.ConditionStatus
	}{
		{
			name:              "sets PlacementNotFound when the Placement is missing",
			mesh:              mesh.DeepCopy(),
			placementMissing:  true,
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonPlacementNotFound,
		},
		{
			name:              "reports NoClustersSelected when the selection is empty",
			mesh:              mesh.DeepCopy(),
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonNoClustersSelected,
		},
		{
			name:              "sets Ready when trust is not configured",
			mesh:              mesh.DeepCopy(),
			clusterNames:      []string{"cluster-a"},
			expectReadyStatus: metav1.ConditionTrue,
			expectReason:      meshv1alpha1.ReasonAllClustersReady,
			expectClusters:    []string{"cluster-a"},
		},
		{
			name:         "sets Ready when all clusters have trust distributed",
			mesh:         meshWithTrust(mesh),
			clusterNames: []string{"cluster-a"},
			works: []workv1.ManifestWork{
				cacertsWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus:  metav1.ConditionTrue,
			expectReason:       meshv1alpha1.ReasonAllClustersReady,
			expectClusterTrust: map[string]metav1.ConditionStatus{"cluster-a": metav1.ConditionTrue},
		},
		{
			name:               "stays pending when cacerts ManifestWork is not found",
			mesh:               meshWithTrust(mesh),
			clusterNames:       []string{"cluster-a"},
			expectReadyStatus:  metav1.ConditionFalse,
			expectReason:       meshv1alpha1.ReasonClustersNotReady,
			expectClusterTrust: map[string]metav1.ConditionStatus{"cluster-a": metav1.ConditionFalse},
		},
		{
			name:         "stays pending when one of two clusters has trust pending",
			mesh:         meshWithTrust(mesh),
			clusterNames: []string{"cluster-a", "cluster-b"},
			works: []workv1.ManifestWork{
				cacertsWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonClustersNotReady,
			expectClusterTrust: map[string]metav1.ConditionStatus{
				"cluster-a": metav1.ConditionTrue,
				"cluster-b": metav1.ConditionFalse,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newTestScheme()
			objs := make([]runtime.Object, len(tc.works))
			for i := range tc.works {
				objs[i] = &tc.works[i]
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
			r := &Reconciler{Client: c, Scheme: scheme}

			clusters := clusterMap(tc.clusterNames...)

			placement := &clusterv1beta1.Placement{}
			if tc.placementMissing {
				placement = nil
			}

			err := r.determineStatus(context.Background(), tc.mesh, clusters, placement)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cond := meta.FindStatusCondition(tc.mesh.Status.Conditions, meshv1alpha1.ConditionReady)
			if tc.expectReadyStatus == "" {
				if cond != nil {
					t.Errorf("expected no Ready condition, got %v", cond)
				}
			} else {
				if cond == nil {
					t.Fatal("expected Ready condition to be set")
				}
				if cond.Status != tc.expectReadyStatus {
					t.Errorf("expected Ready=%s, got %s", tc.expectReadyStatus, cond.Status)
				}
				if cond.Reason != tc.expectReason {
					t.Errorf("expected reason %s, got %s", tc.expectReason, cond.Reason)
				}
			}

			if tc.expectClusters != nil {
				if len(tc.mesh.Status.ClusterStatus) != len(tc.expectClusters) {
					t.Fatalf("expected %d cluster statuses, got %d", len(tc.expectClusters), len(tc.mesh.Status.ClusterStatus))
				}
				for _, name := range tc.expectClusters {
					if findClusterCondition(tc.mesh, name, meshv1alpha1.ConditionTrustDistributed) != nil {
						t.Errorf("cluster %s has TrustDistributed without an issuer", name)
					}
				}
			}

			for clusterName, expectStatus := range tc.expectClusterTrust {
				trustCond := findClusterCondition(tc.mesh, clusterName, meshv1alpha1.ConditionTrustDistributed)
				if trustCond == nil {
					t.Errorf("expected TrustDistributed condition for cluster %s", clusterName)
				} else if trustCond.Status != expectStatus {
					t.Errorf("expected TrustDistributed=%s for cluster %s, got %s", expectStatus, clusterName, trustCond.Status)
				}
			}
		})
	}
}

func TestDetermineStatusResetsPendingWindow(t *testing.T) {
	scheme := newTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	// A RemovalPending mark from long before the cluster reappeared.
	staleMark := time.Now().Add(-time.Hour)
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{
				clusterPendingRemoval("cluster-a", staleMark),
			},
		},
	}

	// The cluster reappears, then leaves again.
	if err := r.determineStatus(context.Background(), mesh, clusterMap("cluster-a"), &clusterv1beta1.Placement{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.determineStatus(context.Background(), mesh, clusterMap(), &clusterv1beta1.Placement{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := findClusterCondition(mesh, "cluster-a", meshv1alpha1.ConditionPlacementSelected)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != meshv1alpha1.ReasonRemovalPending {
		t.Fatalf("expected False/RemovalPending, got %+v", cond)
	}
	if !cond.LastTransitionTime.After(staleMark) {
		t.Errorf("expected a fresh window, got stale LastTransitionTime %v", cond.LastTransitionTime)
	}
}

func TestDetermineStatusMarksPendingRemoval(t *testing.T) {
	scheme := newTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{
				{ClusterName: "cluster-a"},
				{ClusterName: "cluster-b"},
			},
		},
	}

	// cluster-b left the Placement decisions, so it has no key in clusters.
	clusters := clusterMap("cluster-a")

	if err := r.determineStatus(context.Background(), mesh, clusters, &clusterv1beta1.Placement{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	selected := findClusterCondition(mesh, "cluster-a", meshv1alpha1.ConditionPlacementSelected)
	if selected == nil || selected.Status != metav1.ConditionTrue || selected.Reason != meshv1alpha1.ReasonSelected {
		t.Errorf("expected cluster-a True/Selected, got %+v", selected)
	}

	pending := findClusterCondition(mesh, "cluster-b", meshv1alpha1.ConditionPlacementSelected)
	if pending == nil || pending.Status != metav1.ConditionFalse || pending.Reason != meshv1alpha1.ReasonRemovalPending {
		t.Errorf("expected cluster-b False/RemovalPending, got %+v", pending)
	}
}

func TestDetermineStatusAllUnresolved(t *testing.T) {
	scheme := newTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
	}
	clusters := map[string]*clusterv1.ManagedCluster{"cluster-a": nil}

	if err := r.determineStatus(context.Background(), mesh, clusters, &clusterv1beta1.Placement{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cond := meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
		return
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != meshv1alpha1.ReasonNoClustersSelected {
		t.Errorf("expected False/%s, got %s/%s", meshv1alpha1.ReasonNoClustersSelected, cond.Status, cond.Reason)
	}
}

func TestUpdateTrustStatusCondition(t *testing.T) {
	mesh := meshWithTrust(&meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "mesh-ns", Generation: 1},
	})

	tests := []struct {
		name           string
		conditions     []metav1.Condition
		workGeneration int64
		workAbsent     bool
		expectStatus   metav1.ConditionStatus
		expectReason   string
	}{
		{
			name:         "reports certificate pending when the work does not exist",
			workAbsent:   true,
			expectStatus: metav1.ConditionFalse,
			expectReason: meshv1alpha1.ReasonCertificatePending,
		},
		{
			name:           "reports distributed when current trust is applied and available",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionTrue,
			expectReason:   meshv1alpha1.ReasonDistributed,
		},
		{
			name:           "reports pending when only the previous generation was applied",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			workGeneration: 2,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when application fails despite availability",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionFalse), workAvailable(metav1.ConditionTrue)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when application is unknown despite availability",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionUnknown), workAvailable(metav1.ConditionTrue)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when application condition is missing",
			conditions:     []metav1.Condition{workAvailable(metav1.ConditionTrue)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when applied trust is unavailable",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionFalse)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when applied trust has unknown availability",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionUnknown)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:           "reports pending when availability condition is missing",
			conditions:     []metav1.Condition{workApplied(metav1.ConditionTrue)},
			workGeneration: 1,
			expectStatus:   metav1.ConditionFalse,
			expectReason:   meshv1alpha1.ReasonDistributionPending,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := mesh.DeepCopy()
			var objs []client.Object
			if !tc.workAbsent {
				work := cacertsWorkWithConditions(mesh, "cluster-a", tc.conditions...)
				work.Generation = tc.workGeneration
				objs = append(objs, &work)
			}
			r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(objs...).Build()}

			ready, err := r.updateTrustStatusCondition(context.Background(), mesh, "cluster-a")
			if err != nil {
				t.Fatalf("updateTrustStatusCondition() returned error: %v", err)
			}
			expectReady := tc.expectStatus == metav1.ConditionTrue
			if ready != expectReady {
				t.Errorf("updateTrustStatusCondition() ready = %t, want %t", ready, expectReady)
			}
			if len(mesh.Status.ClusterStatus) != 1 {
				t.Fatalf("expected one cluster status, got %v", mesh.Status.ClusterStatus)
			}
			condition := findClusterCondition(mesh, "cluster-a", meshv1alpha1.ConditionTrustDistributed)
			if condition == nil {
				t.Fatal("expected TrustDistributed condition")
			}
			if condition.Status != tc.expectStatus || condition.Reason != tc.expectReason {
				t.Errorf("TrustDistributed = %s/%s, want %s/%s", condition.Status, condition.Reason, tc.expectStatus, tc.expectReason)
			}
		})
	}
}

func clusterMap(names ...string) map[string]*clusterv1.ManagedCluster {
	m := make(map[string]*clusterv1.ManagedCluster, len(names))
	for _, name := range names {
		m[name] = &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	return m
}

func findClusterCondition(mesh *meshv1alpha1.MultiClusterMesh, clusterName, conditionType string) *metav1.Condition {
	for _, cs := range mesh.Status.ClusterStatus {
		if cs.ClusterName == clusterName {
			return meta.FindStatusCondition(cs.Conditions, conditionType)
		}
	}
	return nil
}

func meshWithTrust(mesh *meshv1alpha1.MultiClusterMesh) *meshv1alpha1.MultiClusterMesh {
	trustedMesh := mesh.DeepCopy()
	trustedMesh.Spec.Security.Trust.CertManager.IssuerRef.Name = "test-issuer"
	return trustedMesh
}

func cacertsWorkWithConditions(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, conditions ...metav1.Condition) workv1.ManifestWork {
	work := workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       CacertsManifestWorkName(mesh),
			Namespace:  clusterName,
			Labels:     meshOwnedLabels(mesh, clusterName),
			Generation: 1,
		},
	}
	for _, condition := range conditions {
		condition.ObservedGeneration = work.Generation
		meta.SetStatusCondition(&work.Status.Conditions, condition)
	}
	return work
}

func workApplied(status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: workv1.WorkApplied, Status: status}
}

func workAvailable(status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: workv1.WorkAvailable, Status: status}
}

func clusterWithPlacementSelected(name string, status metav1.ConditionStatus, reason string, transitionTime time.Time) meshv1alpha1.ClusterMeshStatus {
	return meshv1alpha1.ClusterMeshStatus{
		ClusterName: name,
		Conditions: []metav1.Condition{{
			Type:               meshv1alpha1.ConditionPlacementSelected,
			Status:             status,
			Reason:             reason,
			LastTransitionTime: metav1.NewTime(transitionTime),
		}},
	}
}

func clusterPendingRemoval(name string, markedAt time.Time) meshv1alpha1.ClusterMeshStatus {
	return clusterWithPlacementSelected(name, metav1.ConditionFalse, meshv1alpha1.ReasonRemovalPending, markedAt)
}

// The rounding absorbs the time between a test capturing now and effectiveMembership reading it again.
func assertRequeueAfter(t *testing.T, expected, requeueAfter time.Duration) {
	t.Helper()
	if requeueAfter.Round(time.Second) != expected {
		t.Errorf("expected requeueAfter of %v, got %v", expected, requeueAfter)
	}
}

func TestGetClustersFromPlacementNoClustersSelected(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{Client: c, Scheme: scheme}

	clusters, err := r.getClustersFromPlacement(context.Background(), placement)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
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

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pd, cluster).
		Build()

	r := &Reconciler{Client: c, Scheme: scheme}

	clusters, err := r.getClustersFromPlacement(context.Background(), placement)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusters["cluster-exists"] == nil {
		t.Error("expected cluster object for cluster-exists")
	}

	// clusters includes ALL PlacementDecision entries, even the missing cluster.
	// This prevents cleanup functions from prematurely deleting resources for clusters whose ManagedCluster is temporarily unavailable.
	if _, ok := clusters["cluster-missing"]; !ok {
		t.Error("expected cluster-missing in clusters")
	}
	if clusters["cluster-missing"] != nil {
		t.Error("expected nil object for cluster-missing")
	}
}

func TestGetClustersFromPlacementAllMissing(t *testing.T) {
	scheme := newTestScheme()

	placement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: "test-placement", Namespace: "default"},
		Status:     clusterv1beta1.PlacementStatus{NumberOfSelectedClusters: 2},
	}

	pd := placementDecision("test-placement", "default", "gone-cluster-1", "gone-cluster-2")

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pd).
		Build()

	r := &Reconciler{Client: c, Scheme: scheme}

	clusters, err := r.getClustersFromPlacement(context.Background(), placement)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// clusters includes the PlacementDecision entries even though all ManagedClusters are gone, preventing premature cleanup.
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d: %v", len(clusters), clusters)
	}
	for _, name := range []string{"gone-cluster-1", "gone-cluster-2"} {
		if clusters[name] != nil {
			t.Errorf("expected nil object for %s", name)
		}
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

func TestEffectiveMembership(t *testing.T) {
	now := time.Now()

	mesh := &meshv1alpha1.MultiClusterMesh{
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{
				{ClusterName: "observed-a"},
				{ClusterName: "observed-b"},
				clusterPendingRemoval("pending-fresh", now),
				clusterPendingRemoval("pending-recent", now.Add(-RemovalGracePeriod/2)),
				clusterPendingRemoval("pending-expired", now.Add(-RemovalGracePeriod-time.Second)),
				{ClusterName: "missing-unmarked"},
			},
		},
	}

	effective, requeueAfter := effectiveMembership(mesh, clusterMap("observed-a", "observed-b"))

	expected := []string{"missing-unmarked", "observed-a", "observed-b", "pending-fresh", "pending-recent"}
	if len(effective) != len(expected) {
		t.Fatalf("expected %d effective clusters, got %d: %v", len(expected), len(effective), effective)
	}
	for _, name := range expected {
		if _, ok := effective[name]; !ok {
			t.Errorf("expected %s in effective", name)
		}
	}
	if _, ok := effective["pending-expired"]; ok {
		t.Error("expected pending-expired to be dropped")
	}

	assertRequeueAfter(t, RemovalGracePeriod/2, requeueAfter)
}

func TestEffectiveMembershipNothingPending(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{{ClusterName: "observed-a"}},
		},
	}

	effective, requeueAfter := effectiveMembership(mesh, clusterMap("observed-a"))

	if len(effective) != 1 || effective["observed-a"] == nil {
		t.Errorf("expected only observed-a with its object, got %v", effective)
	}
	if requeueAfter != 0 {
		t.Errorf("expected zero requeueAfter, got %v", requeueAfter)
	}
}

func TestEffectiveMembershipNewlyMissing(t *testing.T) {
	// A cluster selected long ago with no RemovalPending mark is newly missing:
	// its window starts now, not at the True condition's LastTransitionTime.
	mesh := &meshv1alpha1.MultiClusterMesh{
		Status: meshv1alpha1.MultiClusterMeshStatus{
			ClusterStatus: []meshv1alpha1.ClusterMeshStatus{
				clusterWithPlacementSelected("selected-stale", metav1.ConditionTrue, meshv1alpha1.ReasonSelected, time.Now().Add(-RemovalGracePeriod*2)),
			},
		},
	}

	effective, requeueAfter := effectiveMembership(mesh, nil)

	if _, ok := effective["selected-stale"]; !ok {
		t.Fatal("expected newly missing cluster to stay in the effective membership")
	}
	if effective["selected-stale"] != nil {
		t.Error("expected newly missing cluster to map to nil")
	}
	assertRequeueAfter(t, RemovalGracePeriod, requeueAfter)
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
