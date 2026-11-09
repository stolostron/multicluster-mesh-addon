package mesh

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
	}

	tests := []struct {
		name               string
		mesh               *meshv1alpha1.MultiClusterMesh
		clusters           []clusterv1.ManagedCluster
		works              []workv1.ManifestWork
		expectReadyStatus  metav1.ConditionStatus
		expectReason       string
		expectClusters     []string
		expectClusterTrust map[string]metav1.ConditionStatus
	}{
		{
			name: "sets Ready when trust is not configured",
			mesh: mesh.DeepCopy(),
			clusters: []clusterv1.ManagedCluster{
				{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
			},
			expectReadyStatus: metav1.ConditionTrue,
			expectReason:      meshv1alpha1.ReasonAllClustersReady,
			expectClusters:    []string{"cluster-a"},
		},
		{
			name:     "does not set Ready when no clusters",
			mesh:     mesh.DeepCopy(),
			clusters: nil,
		},
		{
			name:     "sets Ready when all clusters have trust distributed",
			mesh:     meshWithTrust(mesh),
			clusters: []clusterv1.ManagedCluster{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}},
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
			clusters:           []clusterv1.ManagedCluster{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}},
			expectReadyStatus:  metav1.ConditionFalse,
			expectReason:       meshv1alpha1.ReasonClustersNotReady,
			expectClusterTrust: map[string]metav1.ConditionStatus{"cluster-a": metav1.ConditionFalse},
		},
		{
			name:     "stays pending when one of two clusters has trust pending",
			mesh:     meshWithTrust(mesh),
			clusters: []clusterv1.ManagedCluster{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}}, {ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}}},
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

			err := r.determineStatus(context.Background(), tc.mesh, tc.clusters)
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
				for i, name := range tc.expectClusters {
					cs := tc.mesh.Status.ClusterStatus[i]
					if cs.ClusterName != name {
						t.Errorf("cluster status[%d] = %s, want %s", i, cs.ClusterName, name)
					}
					if meta.FindStatusCondition(cs.Conditions, meshv1alpha1.ConditionTrustDistributed) != nil {
						t.Errorf("cluster %s has TrustDistributed without an issuer", name)
					}
				}
			}

			for clusterName, expectStatus := range tc.expectClusterTrust {
				var found bool
				for _, cs := range tc.mesh.Status.ClusterStatus {
					if cs.ClusterName != clusterName {
						continue
					}
					found = true
					trustCond := meta.FindStatusCondition(cs.Conditions, meshv1alpha1.ConditionTrustDistributed)
					if trustCond == nil {
						t.Errorf("expected TrustDistributed condition for cluster %s", clusterName)
					} else if trustCond.Status != expectStatus {
						t.Errorf("expected TrustDistributed=%s for cluster %s, got %s", expectStatus, clusterName, trustCond.Status)
					}
				}
				if !found {
					t.Errorf("expected cluster status for %s, not found", clusterName)
				}
			}
		})
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
			condition := meta.FindStatusCondition(mesh.Status.ClusterStatus[0].Conditions, meshv1alpha1.ConditionTrustDistributed)
			if condition == nil {
				t.Fatal("expected TrustDistributed condition")
			}
			if condition.Status != tc.expectStatus || condition.Reason != tc.expectReason {
				t.Errorf("TrustDistributed = %s/%s, want %s/%s", condition.Status, condition.Reason, tc.expectStatus, tc.expectReason)
			}
		})
	}
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

func TestGetClustersFromPlacementErrorPaths(t *testing.T) {
	// test fixture objects needed for full recovery reconciliation
	basePlacement := &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-placement",
			Namespace: "default",
		},
		Status: clusterv1beta1.PlacementStatus{
			NumberOfSelectedClusters: 1,
		},
	}

	baseDecision := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-placement-decision-1",
			Namespace: "default",
			Labels:    map[string]string{PlacementLabel: "test-placement"},
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{
				{ClusterName: "cluster-a"},
			},
		},
	}

	baseManagedCluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-a",
		},
	}

	tests := []struct {
		name               string
		initObjs           []client.Object
		interceptorFuncs   func(failToggle *atomic.Bool) interceptor.Funcs
		expectErr          bool
		expectRequeueAfter time.Duration
		expectReadyStatus  metav1.ConditionStatus
		expectReason       string
		recover            func(ctx context.Context, t *testing.T, c client.Client, failToggle *atomic.Bool)
	}{
		{
			name: "Placement Get API Failure - returns wrapped error",
			// include decision and cluster so recovery succeeds down to AllClustersReady
			initObjs: []client.Object{basePlacement, baseDecision, baseManagedCluster},
			interceptorFuncs: func(failToggle *atomic.Bool) interceptor.Funcs {
				return interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if failToggle.Load() {
							if _, ok := obj.(*clusterv1beta1.Placement); ok {
								return errors.New("etcd connection timeout")
							}
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}
			},
			expectErr:         true,
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonReconcileError,
			recover: func(ctx context.Context, t *testing.T, c client.Client, failToggle *atomic.Bool) {
				failToggle.Store(false)
			},
		},
		{
			name:     "PlacementDecision List API Failure - returns wrapped error",
			initObjs: []client.Object{basePlacement, baseDecision, baseManagedCluster},
			interceptorFuncs: func(failToggle *atomic.Bool) interceptor.Funcs {
				return interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if failToggle.Load() {
							if _, ok := list.(*clusterv1beta1.PlacementDecisionList); ok {
								return errors.New("failed to reach kube-apiserver")
							}
						}
						return c.List(ctx, list, opts...)
					},
				}
			},
			expectErr:         true,
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonReconcileError,
			recover: func(ctx context.Context, t *testing.T, c client.Client, failToggle *atomic.Bool) {
				failToggle.Store(false)
			},
		},
		{
			name:     "ManagedCluster Get API Failure - returns wrapped error",
			initObjs: []client.Object{basePlacement, baseDecision, baseManagedCluster},
			interceptorFuncs: func(failToggle *atomic.Bool) interceptor.Funcs {
				return interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if failToggle.Load() {
							if _, ok := obj.(*clusterv1.ManagedCluster); ok {
								return apierrors.NewInternalError(errors.New("etcd disk full"))
							}
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}
			},
			expectErr:         true,
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonReconcileError,
			recover: func(ctx context.Context, t *testing.T, c client.Client, failToggle *atomic.Bool) {
				failToggle.Store(false)
			},
		},
		{
			name: "PlacementDecisions pending - requeues without error",
			// Placement claims 2 selected clusters but only one decision entry exists, simulating decisions still being redistributed.
			initObjs: []client.Object{
				&clusterv1beta1.Placement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "default",
					},
					Status: clusterv1beta1.PlacementStatus{
						NumberOfSelectedClusters: 2,
					},
				},
				baseDecision,
				baseManagedCluster,
			},
			expectErr:          false,
			expectRequeueAfter: 2 * time.Second,
			expectReadyStatus:  metav1.ConditionFalse,
			expectReason:       meshv1alpha1.ReasonPlacementDecisionPending,
			recover: func(ctx context.Context, t *testing.T, c client.Client, failToggle *atomic.Bool) {
				placement := &clusterv1beta1.Placement{}
				placementKey := types.NamespacedName{Name: "test-placement", Namespace: "default"}
				if err := c.Get(ctx, placementKey, placement); err != nil {
					t.Fatalf("failed to get Placement during recovery: %v", err)
				}
				placement.Status.NumberOfSelectedClusters = 1
				if err := c.Status().Update(ctx, placement); err != nil {
					t.Fatalf("failed to update Placement status during recovery: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := newTestScheme()
			failToggle := &atomic.Bool{}
			failToggle.Store(true)

			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mesh",
					Namespace:  "default",
					Generation: 1,
					Finalizers: []string{FinalizerName},
				},
				Spec: meshv1alpha1.MultiClusterMeshSpec{
					PlacementRef: meshv1alpha1.PlacementReference{Name: "test-placement"},
				},
			}
			existingWork := &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mesh-work-cluster-a",
					Namespace: "cluster-a",
					Labels:    meshOwnedLabels(mesh, "cluster-a"),
				},
			}

			allInitObjs := append(tc.initObjs, mesh, existingWork)
			var funcs interceptor.Funcs
			if tc.interceptorFuncs != nil {
				funcs = tc.interceptorFuncs(failToggle)
			}
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allInitObjs...).
				WithStatusSubresource(&meshv1alpha1.MultiClusterMesh{}, &clusterv1beta1.Placement{}).
				WithInterceptorFuncs(funcs).
				Build()

			r := &Reconciler{Client: c, Scheme: scheme}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: mesh.Namespace,
					Name:      mesh.Name,
				},
			}

			result, err := r.Reconcile(ctx, req)
			if tc.expectErr && err == nil {
				t.Fatal("expected error on failure path, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if result.RequeueAfter != tc.expectRequeueAfter {
				t.Errorf("expected RequeueAfter=%v, got %v", tc.expectRequeueAfter, result.RequeueAfter)
			}

			updatedMesh := &meshv1alpha1.MultiClusterMesh{}
			err = c.Get(ctx, req.NamespacedName, updatedMesh)
			if err != nil {
				t.Fatalf("unexpected error on getting the existing mesh: %v", err)
			}

			// Verify Ready Condition on failure
			cond := meta.FindStatusCondition(updatedMesh.Status.Conditions, meshv1alpha1.ConditionReady)
			if cond == nil {
				t.Fatal("expected Ready condition to be set during failure")
			}
			if cond.Status != tc.expectReadyStatus {
				t.Errorf("expected Ready=%s, got %s", tc.expectReadyStatus, cond.Status)
			}
			if cond.Reason != tc.expectReason {
				t.Errorf("expected reason %s, got %s", tc.expectReason, cond.Reason)
			}

			// Verify existing ManifestWork resource is preserved
			work := &workv1.ManifestWork{}
			key := types.NamespacedName{Name: existingWork.GetName(), Namespace: existingWork.GetNamespace()}
			if err := c.Get(ctx, key, work); err != nil {
				t.Fatalf("expected mesh resource %s to be preserved during failure, but failed to get: %v", key, err)
			}
			if work.Labels[MeshNameLabel] != updatedMesh.Name {
				t.Errorf("mesh resource %s metadata altered during failure", key)
			}

			// Trigger recovery and verify Readiness recovers
			if tc.recover != nil {
				tc.recover(ctx, t, c, failToggle)

				clusters, _, _, err := r.getClustersFromPlacement(ctx, mesh)
				if err != nil {
					t.Fatalf("unexpected error during recovery reconciliation: %v", err)
				}

				updatedMesh := &meshv1alpha1.MultiClusterMesh{}
				err = c.Get(ctx, req.NamespacedName, updatedMesh)
				if err != nil {
					t.Fatalf("unexpected error on getting the existing mesh: %v", err)
				}

				err = r.determineStatus(context.Background(), updatedMesh, clusters)
				if err != nil {
					t.Fatalf("unexpected error on getting recovery status: %v", err)
				}

				recoveredCond := meta.FindStatusCondition(updatedMesh.Status.Conditions, meshv1alpha1.ConditionReady)
				if recoveredCond == nil {
					t.Fatal("expected Ready condition to be set after recovery")
				}
				if recoveredCond.Status != metav1.ConditionTrue {
					t.Errorf("recovered path: expected Ready=%s, got %s", metav1.ConditionTrue, recoveredCond.Status)
				}
				if recoveredCond.Reason != meshv1alpha1.ReasonAllClustersReady {
					t.Errorf("recovered path: expected reason=%s, got %s", meshv1alpha1.ReasonAllClustersReady, recoveredCond.Reason)
				}

				// Verify mesh resources remain intact post-recovery
				work := &workv1.ManifestWork{}
				key := types.NamespacedName{Name: existingWork.GetName(), Namespace: existingWork.GetNamespace()}
				if err := c.Get(ctx, key, work); err != nil {
					t.Errorf("expected mesh resource %s to persist after recovery: %v", key, err)
				}
			}
		})
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
