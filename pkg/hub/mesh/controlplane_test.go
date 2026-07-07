package mesh

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func unmarshalIstioCRSpec(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("failed to unmarshal Istio CR: %v", err)
	}
	spec, ok := obj["spec"].(map[string]any)
	if !ok {
		t.Fatal("Istio CR missing spec")
	}
	return spec
}

func getNestedString(m map[string]any, keys ...string) string {
	for i, k := range keys {
		if i == len(keys)-1 {
			v, _ := m[k].(string)
			return v
		}
		m, _ = m[k].(map[string]any)
		if m == nil {
			return ""
		}
	}
	return ""
}

func getNestedBool(m map[string]any, keys ...string) (bool, bool) {
	for i, k := range keys {
		if i == len(keys)-1 {
			v, ok := m[k].(bool)
			return v, ok
		}
		m, _ = m[k].(map[string]any)
		if m == nil {
			return false, false
		}
	}
	return false, false
}

func TestBuildIstioCR_MultiPrimary(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("default", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")
	clusters := []clusterv1.ManagedCluster{*cluster}

	r := newTestReconciler(scheme)

	cr, err := r.buildIstioCR(context.Background(), mesh, clusters, cluster, buildBasicTemplate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, err := json.Marshal(cr.Object)
	if err != nil {
		t.Fatalf("failed to marshal CR: %v", err)
	}

	spec := unmarshalIstioCRSpec(t, raw)
	values, _ := spec["values"].(map[string]any)
	global, _ := values["global"].(map[string]any)

	if got := global["meshID"]; got != "default-my-mesh" {
		t.Errorf("meshID = %v, want default-my-mesh", got)
	}

	mc, _ := global["multiCluster"].(map[string]any)
	if got := mc["clusterName"]; got != "cluster-a" {
		t.Errorf("clusterName = %v, want cluster-a", got)
	}

	if got := global["network"]; got != "network-cluster-a" {
		t.Errorf("network = %v, want network-cluster-a", got)
	}

	meshConfig, _ := values["meshConfig"].(map[string]any)
	if got := meshConfig["trustDomain"]; got != "my-mesh" {
		t.Errorf("trustDomain = %v, want my-mesh", got)
	}

	if _, exists := global["externalIstiod"]; exists {
		t.Error("externalIstiod should not be set for MultiPrimary")
	}
	if _, exists := global["remotePilotAddress"]; exists {
		t.Error("remotePilotAddress should not be set for MultiPrimary")
	}
	if _, exists := spec["profile"]; exists {
		t.Error("profile should not be set for MultiPrimary")
	}
	if _, exists := spec["version"]; exists {
		t.Error("version should not be set when empty")
	}

	t.Run("version included when set", func(t *testing.T) {
		mesh.Spec.ControlPlane.Version = "v1.28.8"
		cr, err := r.buildIstioCR(context.Background(), mesh, clusters, cluster, buildBasicTemplate())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		raw, _ := json.Marshal(cr.Object)
		spec := unmarshalIstioCRSpec(t, raw)
		if got := spec["version"]; got != "v1.28.8" {
			t.Errorf("version = %v, want v1.28.8", got)
		}
	})
}

func TestBuildIstioCR_PrimaryRemotePrimary(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("default", "my-mesh", "test-set", meshv1alpha1.TopologyPrimaryRemote, "primary")

	primary := newTestCluster("primary")
	remote := newTestCluster("remote")
	clusters := []clusterv1.ManagedCluster{*primary, *remote}

	r := newTestReconciler(scheme)

	cr, err := r.buildIstioCR(context.Background(), mesh, clusters, primary, buildBasicTemplate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, _ := json.Marshal(cr.Object)
	spec := unmarshalIstioCRSpec(t, raw)
	values, _ := spec["values"].(map[string]any)
	global, _ := values["global"].(map[string]any)

	val, exists := global["externalIstiod"].(bool)
	if !exists || !val {
		t.Error("externalIstiod should be true for PrimaryRemote primary")
	}

	if _, exists := global["remotePilotAddress"]; exists {
		t.Error("remotePilotAddress should not be set for PrimaryRemote primary")
	}
	if _, exists := spec["profile"]; exists {
		t.Error("profile should not be set for PrimaryRemote primary")
	}
}

func TestBuildIstioCR_PrimaryRemoteRemote(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("default", "my-mesh", "test-set", meshv1alpha1.TopologyPrimaryRemote, "primary")

	primary := newTestCluster("primary")
	remote := newTestCluster("remote")
	clusters := []clusterv1.ManagedCluster{*primary, *remote}

	lbIP := "10.0.0.1"
	gwWork := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getGWManifestWorkName(mesh),
			Namespace: "primary",
		},
		Status: workv1.ManifestWorkStatus{
			ResourceStatus: workv1.ManifestResourceStatus{
				Manifests: []workv1.ManifestCondition{{
					ResourceMeta: workv1.ManifestResourceMeta{Kind: "Service"},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{{
							Name:  "lbIP",
							Value: workv1.FieldValue{String: &lbIP},
						}},
					},
				}},
			},
		},
	}

	r := newTestReconciler(scheme, gwWork)

	cr, err := r.buildIstioCR(context.Background(), mesh, clusters, remote, buildBasicTemplate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, _ := json.Marshal(cr.Object)
	spec := unmarshalIstioCRSpec(t, raw)
	values, _ := spec["values"].(map[string]any)
	global, _ := values["global"].(map[string]any)

	if got := spec["profile"]; got != "remote" {
		t.Errorf("profile = %v, want remote", got)
	}
	if got := global["remotePilotAddress"]; got != "10.0.0.1" {
		t.Errorf("remotePilotAddress = %v, want 10.0.0.1", got)
	}
	if _, exists := global["externalIstiod"]; exists {
		t.Error("externalIstiod should not be set for PrimaryRemote remote")
	}
}

func TestBuildControlPlaneManifestWork(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("default", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")
	clusters := []clusterv1.ManagedCluster{*cluster}

	r := newTestReconciler(scheme)

	work, err := r.buildControlPlaneManifestWork(context.Background(), mesh, clusters, cluster, buildBasicTemplate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := len(work.Spec.Workload.Manifests); got != 4 {
		t.Fatalf("expected 4 manifests, got %d", got)
	}

	expectedLabels := meshOwnedLabels(mesh, "cluster-a")
	for k, v := range expectedLabels {
		if work.Labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, work.Labels[k], v)
		}
	}

	if len(work.Spec.ManifestConfigs) != 1 {
		t.Fatalf("expected 1 ManifestConfig, got %d", len(work.Spec.ManifestConfigs))
	}
	mc := work.Spec.ManifestConfigs[0]

	if mc.ResourceIdentifier.Group != "sailoperator.io" || mc.ResourceIdentifier.Resource != "istios" {
		t.Errorf("unexpected resource identifier: %+v", mc.ResourceIdentifier)
	}

	if len(mc.FeedbackRules) != 1 || len(mc.FeedbackRules[0].JsonPaths) != 1 {
		t.Fatal("expected 1 feedback rule with 1 json path")
	}
	jp := mc.FeedbackRules[0].JsonPaths[0]
	if jp.Name != "readyStatus" {
		t.Errorf("feedback name = %q, want readyStatus", jp.Name)
	}

	if len(mc.ConditionRules) != 1 {
		t.Fatal("expected 1 condition rule")
	}
	if mc.ConditionRules[0].Condition != "ControlPlaneReady" {
		t.Errorf("condition = %q, want ControlPlaneReady", mc.ConditionRules[0].Condition)
	}

	// Verify ClusterRole RBAC rules (manifest index 2)
	crRaw := work.Spec.Workload.Manifests[2].RawExtension
	crObj := crRaw.Object
	if crObj == nil {
		t.Fatal("ClusterRole manifest Object is nil")
	}

	type policyRule struct {
		APIGroups []string `json:"apiGroups"`
		Resources []string `json:"resources"`
		Verbs     []string `json:"verbs"`
	}
	crJSON, _ := json.Marshal(crObj)
	var crData struct {
		Rules []policyRule `json:"rules"`
	}
	if err := json.Unmarshal(crJSON, &crData); err != nil {
		t.Fatalf("failed to unmarshal ClusterRole: %v", err)
	}
	if len(crData.Rules) != 3 {
		t.Fatalf("expected 3 ClusterRole rules, got %d", len(crData.Rules))
	}
	if crData.Rules[1].APIGroups[0] != "discovery.k8s.io" {
		t.Errorf("rule[1] apiGroup = %q, want discovery.k8s.io", crData.Rules[1].APIGroups[0])
	}

	// Verify ClusterRoleBinding subjects (manifest index 3)
	crbRaw := work.Spec.Workload.Manifests[3].RawExtension
	crbObj := crbRaw.Object
	if crbObj == nil {
		t.Fatal("ClusterRoleBinding manifest Object is nil")
	}
	crbJSON, _ := json.Marshal(crbObj)
	var crbData struct {
		Subjects []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"subjects"`
	}
	if err := json.Unmarshal(crbJSON, &crbData); err != nil {
		t.Fatalf("failed to unmarshal ClusterRoleBinding: %v", err)
	}
	if len(crbData.Subjects) != 1 {
		t.Fatalf("expected 1 subject, got %d", len(crbData.Subjects))
	}
	subj := crbData.Subjects[0]
	if subj.Kind != "ServiceAccount" {
		t.Errorf("subject kind = %q, want ServiceAccount", subj.Kind)
	}
	if subj.Name != getMSAName(mesh) {
		t.Errorf("subject name = %q, want %q", subj.Name, getMSAName(mesh))
	}
	if subj.Namespace != MSASpokeNamespace {
		t.Errorf("subject namespace = %q, want %q", subj.Namespace, MSASpokeNamespace)
	}
}

func TestIsOperatorInstalled(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("cluster-a")

	csv := "sail-operator.v1.2.3"
	workWithFeedback := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorManifestWorkName,
			Namespace: "cluster-a",
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

	t.Run("returns true when installedCSV feedback exists", func(t *testing.T) {
		r := &Reconciler{
			Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(workWithFeedback).
				WithStatusSubresource(&workv1.ManifestWork{}).
				Build(),
			Scheme: scheme,
		}
		if !r.isOperatorInstalled(context.Background(), cluster) {
			t.Error("expected isOperatorInstalled to return true")
		}
	})

	t.Run("returns false when ManifestWork does not exist", func(t *testing.T) {
		r := &Reconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}
		if r.isOperatorInstalled(context.Background(), cluster) {
			t.Error("expected isOperatorInstalled to return false")
		}
	})

	t.Run("returns false when no feedback values", func(t *testing.T) {
		workNoFeedback := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      OperatorManifestWorkName,
				Namespace: "cluster-a",
			},
		}
		r := &Reconciler{
			Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(workNoFeedback).
				WithStatusSubresource(&workv1.ManifestWork{}).
				Build(),
			Scheme: scheme,
		}
		if r.isOperatorInstalled(context.Background(), cluster) {
			t.Error("expected isOperatorInstalled to return false")
		}
	})
}

func TestIsControlPlaneReady(t *testing.T) {
	scheme := newTestScheme()
	mesh := newTestMesh("default", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")

	workReady := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getCPManifestWorkName(mesh),
			Namespace: "cluster-a",
		},
		Status: workv1.ManifestWorkStatus{
			ResourceStatus: workv1.ManifestResourceStatus{
				Manifests: []workv1.ManifestCondition{{
					Conditions: []metav1.Condition{{
						Type:   "ControlPlaneReady",
						Status: metav1.ConditionTrue,
					}},
				}},
			},
		},
	}

	t.Run("returns true when ControlPlaneReady condition is True", func(t *testing.T) {
		r := &Reconciler{
			Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(workReady).
				WithStatusSubresource(&workv1.ManifestWork{}).
				Build(),
			Scheme: scheme,
		}
		if !r.isControlPlaneReady(context.Background(), mesh, cluster) {
			t.Error("expected isControlPlaneReady to return true")
		}
	})

	t.Run("returns false when ManifestWork does not exist", func(t *testing.T) {
		r := &Reconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme: scheme,
		}
		if r.isControlPlaneReady(context.Background(), mesh, cluster) {
			t.Error("expected isControlPlaneReady to return false")
		}
	})

	t.Run("returns false when condition is not True", func(t *testing.T) {
		workNotReady := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getCPManifestWorkName(mesh),
				Namespace: "cluster-a",
			},
			Status: workv1.ManifestWorkStatus{
				ResourceStatus: workv1.ManifestResourceStatus{
					Manifests: []workv1.ManifestCondition{{
						Conditions: []metav1.Condition{{
							Type:   "ControlPlaneReady",
							Status: metav1.ConditionFalse,
						}},
					}},
				},
			},
		}
		r := &Reconciler{
			Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(workNotReady).
				WithStatusSubresource(&workv1.ManifestWork{}).
				Build(),
			Scheme: scheme,
		}
		if r.isControlPlaneReady(context.Background(), mesh, cluster) {
			t.Error("expected isControlPlaneReady to return false")
		}
	})
}
