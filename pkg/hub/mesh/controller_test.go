package mesh

import (
	"context"
	"errors"
	"strings"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
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
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "default", Generation: 1},
	}

	tests := []struct {
		name                string
		mesh                *meshv1alpha1.MultiClusterMesh
		clusters            []clusterv1.ManagedCluster
		mwrsets             []workv1alpha1.ManifestWorkReplicaSet
		works               []workv1.ManifestWork
		expectReadyStatus   metav1.ConditionStatus
		expectReason        string
		expectClusterStatus map[string]map[string]metav1.ConditionStatus
	}{
		{
			name:     "sets Ready when discovery is distributed without trust configured",
			mesh:     mesh.DeepCopy(),
			clusters: []clusterv1.ManagedCluster{clusterWithAPIEndpoint("cluster-a")},
			mwrsets: []workv1alpha1.ManifestWorkReplicaSet{
				mwrsetWithConditions(mesh, []string{"cluster-a"}, mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)),
			},
			works: []workv1.ManifestWork{
				rbacWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionTrue,
			expectReason:      meshv1alpha1.ReasonAllClustersReady,
			expectClusterStatus: map[string]map[string]metav1.ConditionStatus{
				"cluster-a": {meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue},
			},
		},
		{
			name:     "stays pending when one cluster has discovery pending",
			mesh:     mesh.DeepCopy(),
			clusters: []clusterv1.ManagedCluster{clusterWithAPIEndpoint("cluster-a"), clusterWithAPIEndpoint("cluster-b")},
			mwrsets: []workv1alpha1.ManifestWorkReplicaSet{
				mwrsetWithConditions(mesh, []string{"cluster-a", "cluster-b"}, mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)),
			},
			works: []workv1.ManifestWork{
				rbacWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonClustersNotReady,
			expectClusterStatus: map[string]map[string]metav1.ConditionStatus{
				"cluster-a": {meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue},
				"cluster-b": {meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionFalse},
			},
		},
		{
			name:     "does not set Ready when no clusters",
			mesh:     mesh.DeepCopy(),
			clusters: nil,
		},
		{
			name:     "sets Ready when all clusters have trust and discovery distributed",
			mesh:     meshWithTrust(mesh),
			clusters: []clusterv1.ManagedCluster{clusterWithAPIEndpoint("cluster-a")},
			mwrsets: []workv1alpha1.ManifestWorkReplicaSet{
				mwrsetWithConditions(mesh, []string{"cluster-a"}, mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)),
			},
			works: []workv1.ManifestWork{
				cacertsWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
				rbacWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionTrue,
			expectReason:      meshv1alpha1.ReasonAllClustersReady,
			expectClusterStatus: map[string]map[string]metav1.ConditionStatus{
				"cluster-a": {
					meshv1alpha1.ConditionTrustDistributed:     metav1.ConditionTrue,
					meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue,
				},
			},
		},
		{
			name:     "stays pending when cacerts ManifestWork is not found",
			mesh:     meshWithTrust(mesh),
			clusters: []clusterv1.ManagedCluster{clusterWithAPIEndpoint("cluster-a")},
			mwrsets: []workv1alpha1.ManifestWorkReplicaSet{
				mwrsetWithConditions(mesh, []string{"cluster-a"}, mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)),
			},
			works: []workv1.ManifestWork{
				rbacWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonClustersNotReady,
			expectClusterStatus: map[string]map[string]metav1.ConditionStatus{
				"cluster-a": {
					meshv1alpha1.ConditionTrustDistributed:     metav1.ConditionFalse,
					meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue,
				},
			},
		},
		{
			name:     "stays pending when one of two clusters has trust pending",
			mesh:     meshWithTrust(mesh),
			clusters: []clusterv1.ManagedCluster{clusterWithAPIEndpoint("cluster-a"), clusterWithAPIEndpoint("cluster-b")},
			mwrsets: []workv1alpha1.ManifestWorkReplicaSet{
				mwrsetWithConditions(mesh, []string{"cluster-a", "cluster-b"}, mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)),
			},
			works: []workv1.ManifestWork{
				cacertsWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
				rbacWorkWithConditions(mesh, "cluster-a", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
				rbacWorkWithConditions(mesh, "cluster-b", workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)),
			},
			expectReadyStatus: metav1.ConditionFalse,
			expectReason:      meshv1alpha1.ReasonClustersNotReady,
			expectClusterStatus: map[string]map[string]metav1.ConditionStatus{
				"cluster-a": {
					meshv1alpha1.ConditionTrustDistributed:     metav1.ConditionTrue,
					meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue,
				},
				"cluster-b": {
					meshv1alpha1.ConditionTrustDistributed:     metav1.ConditionFalse,
					meshv1alpha1.ConditionDiscoveryDistributed: metav1.ConditionTrue,
				},
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
			for i := range tc.mwrsets {
				objs = append(objs, &tc.mwrsets[i])
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

			if len(tc.mesh.Status.ClusterStatus) != len(tc.expectClusterStatus) {
				t.Fatalf("expected %d cluster statuses, got %d", len(tc.expectClusterStatus), len(tc.mesh.Status.ClusterStatus))
			}
			for _, cs := range tc.mesh.Status.ClusterStatus {
				expected, ok := tc.expectClusterStatus[cs.ClusterName]
				if !ok {
					t.Errorf("unexpected cluster status for %s", cs.ClusterName)
					continue
				}
				if len(cs.Conditions) != len(expected) {
					t.Errorf("expected %d conditions for cluster %s, got %d", len(expected), cs.ClusterName, len(cs.Conditions))
				}
				for conditionType, status := range expected {
					condition := meta.FindStatusCondition(cs.Conditions, conditionType)
					if condition == nil {
						t.Errorf("expected %s condition for cluster %s", conditionType, cs.ClusterName)
						continue
					}
					if condition.Status != status {
						t.Errorf("expected %s=%s for cluster %s, got %s", conditionType, status, cs.ClusterName, condition.Status)
					}
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
		expectStatus   metav1.ConditionStatus
		expectReason   string
	}{
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
			work := cacertsWorkWithConditions(mesh, "cluster-a", tc.conditions...)
			work.Generation = tc.workGeneration
			r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(&work).Build()}

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

func TestUpdateDiscoveryStatusCondition(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mesh", Namespace: "mesh-ns", Generation: 1},
	}

	tests := []struct {
		name             string
		cluster          clusterv1.ManagedCluster
		workConditions   []metav1.Condition
		workGeneration   int64
		mwrsetConditions []metav1.Condition
		expectStatus     metav1.ConditionStatus
		expectReason     string
	}{
		{
			name:             "reports distributed when RBAC and remote secrets are ready",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionTrue,
			expectReason:     meshv1alpha1.ReasonDistributed,
		},
		{
			name:         "reports no API endpoint before checking distribution",
			cluster:      clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
			expectStatus: metav1.ConditionFalse,
			expectReason: meshv1alpha1.ReasonNoAPIEndpoint,
		},
		{
			name:             "reports pending when RBAC is unavailable",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionFalse)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending when the previous RBAC generation was applied",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionTrue)},
			workGeneration:   2,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending while remote secrets are rolling out despite availability",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionFalse), mwrsApplied(metav1.ConditionTrue)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending when remote-secret rollout is unreported",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsApplied(metav1.ConditionTrue)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending when remote secrets are unavailable",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionFalse)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending when remote-secret availability is unknown",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue), mwrsApplied(metav1.ConditionUnknown)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
		{
			name:             "reports pending when remote-secret availability is unreported",
			cluster:          clusterWithAPIEndpoint("cluster-a"),
			workConditions:   []metav1.Condition{workApplied(metav1.ConditionTrue), workAvailable(metav1.ConditionTrue)},
			mwrsetConditions: []metav1.Condition{mwrsRolledOut(metav1.ConditionTrue)},
			workGeneration:   1,
			expectStatus:     metav1.ConditionFalse,
			expectReason:     meshv1alpha1.ReasonDistributionPending,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := mesh.DeepCopy()
			work := rbacWorkWithConditions(mesh, tc.cluster.Name, tc.workConditions...)
			work.Generation = tc.workGeneration
			mwrset := mwrsetWithConditions(mesh, []string{tc.cluster.Name}, tc.mwrsetConditions...)

			r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(&work).Build()}
			ready, err := r.updateDiscoveryStatusCondition(context.Background(), mesh, &tc.cluster, &mwrset)
			if err != nil {
				t.Fatalf("updateDiscoveryStatusCondition() returned error: %v", err)
			}
			if ready != (tc.expectStatus == metav1.ConditionTrue) {
				t.Errorf("unexpected readiness: %t", ready)
			}
			if len(mesh.Status.ClusterStatus) != 1 {
				t.Fatalf("expected one cluster status, got %v", mesh.Status.ClusterStatus)
			}
			condition := meta.FindStatusCondition(mesh.Status.ClusterStatus[0].Conditions, meshv1alpha1.ConditionDiscoveryDistributed)
			if condition == nil {
				t.Fatal("expected DiscoveryDistributed condition")
			}
			if condition.Status != tc.expectStatus || condition.Reason != tc.expectReason {
				t.Errorf("DiscoveryDistributed = %s/%s, want %s/%s", condition.Status, condition.Reason, tc.expectStatus, tc.expectReason)
			}
		})
	}
}

func clusterWithAPIEndpoint(name string) clusterv1.ManagedCluster {
	return clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1.ManagedClusterSpec{
			ManagedClusterClientConfigs: []clusterv1.ClientConfig{{URL: "https://" + name + ":6443"}},
		},
	}
}

func mwrsetWithConditions(mesh *meshv1alpha1.MultiClusterMesh, clusterNames []string, conditions ...metav1.Condition) workv1alpha1.ManifestWorkReplicaSet {
	mwrset := workv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: mesh.Name, Namespace: mesh.Namespace},
	}
	for _, condition := range conditions {
		meta.SetStatusCondition(&mwrset.Status.Conditions, condition)
	}
	for _, name := range clusterNames {
		mwrset.Spec.ManifestWorkTemplate.Workload.Manifests = append(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: &corev1.Secret{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{Name: RemoteSecretName(name), Namespace: mesh.GetControlPlaneNamespace()},
			}},
		})
	}
	return mwrset
}

func meshWithTrust(mesh *meshv1alpha1.MultiClusterMesh) *meshv1alpha1.MultiClusterMesh {
	trustedMesh := mesh.DeepCopy()
	trustedMesh.Spec.Security.Trust.CertManager.IssuerRef.Name = "test-issuer"
	return trustedMesh
}

func cacertsWorkWithConditions(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, conditions ...metav1.Condition) workv1.ManifestWork {
	return workWithConditions(mesh, CacertsManifestWorkName(mesh), clusterName, conditions...)
}

func rbacWorkWithConditions(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, conditions ...metav1.Condition) workv1.ManifestWork {
	return workWithConditions(mesh, IstioReaderManifestWorkName(mesh), clusterName, conditions...)
}

func workWithConditions(mesh *meshv1alpha1.MultiClusterMesh, name, clusterName string, conditions ...metav1.Condition) workv1.ManifestWork {
	work := workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
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

func mwrsApplied(status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: workv1alpha1.ManifestWorkReplicaSetConditionManifestworkApplied, Status: status}
}

func mwrsRolledOut(status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: workv1alpha1.ManifestWorkReplicaSetConditionPlacementRolledOut, Status: status}
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
	_ = workv1alpha1.Install(scheme)
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
