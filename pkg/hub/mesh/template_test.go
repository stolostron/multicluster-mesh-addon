package mesh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

func TestBuildBasicTemplate(t *testing.T) {
	tmpl := buildBasicTemplate()

	if tmpl["apiVersion"] != "sailoperator.io/v1" {
		t.Errorf("apiVersion = %v, want sailoperator.io/v1", tmpl["apiVersion"])
	}
	if tmpl["kind"] != "Istio" {
		t.Errorf("kind = %v, want Istio", tmpl["kind"])
	}

	spec := tmpl["spec"].(map[string]any)
	values := spec["values"].(map[string]any)
	mc := values["meshConfig"].(map[string]any)
	dc := mc["defaultConfig"].(map[string]any)
	pm := dc["proxyMetadata"].(map[string]any)

	if pm["ISTIO_META_DNS_CAPTURE"] != "true" {
		t.Error("DNS_CAPTURE not set in basic template")
	}
	if pm["ISTIO_META_DNS_AUTO_ALLOCATE"] != "true" {
		t.Error("DNS_AUTO_ALLOCATE not set in basic template")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template map[string]any
		wantErr  bool
	}{
		{
			name:     "valid Istio CR",
			template: map[string]any{"apiVersion": "sailoperator.io/v1", "kind": "Istio"},
			wantErr:  false,
		},
		{
			name:     "wrong apiVersion",
			template: map[string]any{"apiVersion": "apps/v1", "kind": "Istio"},
			wantErr:  true,
		},
		{
			name:     "wrong kind",
			template: map[string]any{"apiVersion": "sailoperator.io/v1", "kind": "Deployment"},
			wantErr:  true,
		},
		{
			name:     "missing apiVersion",
			template: map[string]any{"kind": "Istio"},
			wantErr:  true,
		},
		{
			name:     "missing kind",
			template: map[string]any{"apiVersion": "sailoperator.io/v1"},
			wantErr:  true,
		},
		{
			name:     "empty template",
			template: map[string]any{},
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTemplate(tc.template)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateTemplate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestResolveIstioCRTemplate_Dispatch(t *testing.T) {
	scheme := newTestScheme()

	t.Run("nil templateSource returns basic template", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		r := newTestReconciler(scheme)

		tmpl, err := r.resolveIstioCRTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl == nil {
			t.Fatal("expected non-nil template")
		}
		if tmpl["apiVersion"] != "sailoperator.io/v1" {
			t.Error("expected basic template")
		}
	})

	t.Run("none set returns nil", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			None: &meshv1alpha1.NoneTemplateSource{},
		}
		r := newTestReconciler(scheme)

		tmpl, err := r.resolveIstioCRTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl != nil {
			t.Error("expected nil template for None mode")
		}
	})

	t.Run("configMapRef resolves from ConfigMap", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "my-template"},
		}

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "my-template", Namespace: "ns"},
			Data: map[string]string{
				"istio.yaml": "apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      resources:\n        requests:\n          cpu: 500m\n",
			},
		}

		r := newTestReconciler(scheme, cm)

		tmpl, err := r.resolveIstioCRTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := tmpl["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		pilot := values["pilot"].(map[string]any)
		resources := pilot["resources"].(map[string]any)
		requests := resources["requests"].(map[string]any)
		if requests["cpu"] != "500m" {
			t.Errorf("pilot.resources.requests.cpu = %v, want 500m", requests["cpu"])
		}
	})
}

func TestResolveConfigMapTemplate(t *testing.T) {
	scheme := newTestScheme()

	t.Run("valid ConfigMap returns parsed template", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "cm"},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
			Data:       map[string]string{"istio.yaml": "apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      enabled: true\n"},
		}
		r := newTestReconciler(scheme, cm)

		tmpl, err := r.resolveConfigMapTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := tmpl["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		pilot := values["pilot"].(map[string]any)
		if pilot["enabled"] != true {
			t.Errorf("pilot.enabled = %v, want true", pilot["enabled"])
		}
	})

	t.Run("missing ConfigMap returns error", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "nonexistent"},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveConfigMapTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for missing ConfigMap")
		}
	})

	t.Run("missing key returns error", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "cm", Key: "wrong-key"},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
			Data:       map[string]string{"istio.yaml": "apiVersion: sailoperator.io/v1\nkind: Istio\n"},
		}
		r := newTestReconciler(scheme, cm)

		_, err := r.resolveConfigMapTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "cm"},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
			Data:       map[string]string{"istio.yaml": "not: valid: yaml: ["},
		}
		r := newTestReconciler(scheme, cm)

		_, err := r.resolveConfigMapTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})

	t.Run("wrong apiVersion returns validation error", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "cm"},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
			Data:       map[string]string{"istio.yaml": "apiVersion: apps/v1\nkind: Deployment\n"},
		}
		r := newTestReconciler(scheme, cm)

		_, err := r.resolveConfigMapTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestApplyControllerManagedFields(t *testing.T) {
	scheme := newTestScheme()

	t.Run("MultiPrimary", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := buildBasicTemplate()
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		global := values["global"].(map[string]any)

		if global["meshID"] != "default-my-mesh" {
			t.Errorf("meshID = %v, want default-my-mesh", global["meshID"])
		}
		if global["network"] != "network-cluster-a" {
			t.Errorf("network = %v, want network-cluster-a", global["network"])
		}
		mc := global["multiCluster"].(map[string]any)
		if mc["clusterName"] != "cluster-a" {
			t.Errorf("clusterName = %v, want cluster-a", mc["clusterName"])
		}

		meshConfig := values["meshConfig"].(map[string]any)
		if meshConfig["trustDomain"] != "my-mesh" {
			t.Errorf("trustDomain = %v, want my-mesh", meshConfig["trustDomain"])
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
	})

	t.Run("PrimaryRemote primary", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyPrimaryRemote, "primary")
		primary := newTestCluster("primary")
		remote := newTestCluster("remote")
		clusters := []clusterv1.ManagedCluster{*primary, *remote}
		r := newTestReconciler(scheme)

		tmpl := buildBasicTemplate()
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, primary)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		global := values["global"].(map[string]any)

		if val, ok := global["externalIstiod"].(bool); !ok || !val {
			t.Error("externalIstiod should be true for PrimaryRemote primary")
		}
		if _, exists := global["remotePilotAddress"]; exists {
			t.Error("remotePilotAddress should not be set for primary")
		}
		if _, exists := spec["profile"]; exists {
			t.Error("profile should not be set for primary")
		}
	})

	t.Run("PrimaryRemote remote", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyPrimaryRemote, "primary")
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

		tmpl := buildBasicTemplate()
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, remote)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		global := values["global"].(map[string]any)

		if spec["profile"] != "remote" {
			t.Errorf("profile = %v, want remote", spec["profile"])
		}
		if global["remotePilotAddress"] != "10.0.0.1" {
			t.Errorf("remotePilotAddress = %v, want 10.0.0.1", global["remotePilotAddress"])
		}
		if _, exists := global["externalIstiod"]; exists {
			t.Error("externalIstiod should not be set for remote")
		}
	})

	t.Run("version included when set", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.Version = "v1.28.8"
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := buildBasicTemplate()
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := cr.Object["spec"].(map[string]any)
		if spec["version"] != "v1.28.8" {
			t.Errorf("version = %v, want v1.28.8", spec["version"])
		}
	})

	t.Run("version omitted when empty", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := buildBasicTemplate()
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := cr.Object["spec"].(map[string]any)
		if _, exists := spec["version"]; exists {
			t.Error("version should not be set when empty")
		}
	})

	t.Run("user template with empty spec gets controller fields", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := map[string]any{
			"apiVersion": "sailoperator.io/v1",
			"kind":       "Istio",
			"spec":       map[string]any{},
		}
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		global := values["global"].(map[string]any)
		if global["meshID"] != "default-my-mesh" {
			t.Errorf("meshID = %v, want default-my-mesh", global["meshID"])
		}
	})

	t.Run("user template with conflicting meshID gets overridden", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := map[string]any{
			"apiVersion": "sailoperator.io/v1",
			"kind":       "Istio",
			"spec": map[string]any{
				"values": map[string]any{
					"global": map[string]any{
						"meshID": "wrong-id",
					},
					"meshConfig": map[string]any{
						"trustDomain": "wrong-domain",
					},
				},
			},
		}
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		global := values["global"].(map[string]any)

		if global["meshID"] != "default-my-mesh" {
			t.Errorf("meshID should be overridden: got %v", global["meshID"])
		}
		mc := values["meshConfig"].(map[string]any)
		if mc["trustDomain"] != "my-mesh" {
			t.Errorf("trustDomain should be overridden: got %v", mc["trustDomain"])
		}
	})

	t.Run("user custom values preserved alongside controller fields", func(t *testing.T) {
		mesh := newTestMesh("default", "my-mesh", "set", meshv1alpha1.TopologyMultiPrimary, "")
		cluster := newTestCluster("cluster-a")
		clusters := []clusterv1.ManagedCluster{*cluster}
		r := newTestReconciler(scheme)

		tmpl := map[string]any{
			"apiVersion": "sailoperator.io/v1",
			"kind":       "Istio",
			"spec": map[string]any{
				"values": map[string]any{
					"pilot": map[string]any{
						"resources": map[string]any{
							"requests": map[string]any{"cpu": "500m", "memory": "2Gi"},
						},
					},
					"meshConfig": map[string]any{
						"accessLogFile": "/dev/stdout",
					},
				},
			},
		}
		cr, err := r.applyControllerManagedFields(context.Background(), tmpl, mesh, clusters, cluster)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := cr.Object["spec"].(map[string]any)
		values := spec["values"].(map[string]any)

		pilot := values["pilot"].(map[string]any)
		resources := pilot["resources"].(map[string]any)
		requests := resources["requests"].(map[string]any)
		if requests["cpu"] != "500m" {
			t.Errorf("user pilot cpu not preserved: got %v", requests["cpu"])
		}
		if requests["memory"] != "2Gi" {
			t.Errorf("user pilot memory not preserved: got %v", requests["memory"])
		}

		mc := values["meshConfig"].(map[string]any)
		if mc["accessLogFile"] != "/dev/stdout" {
			t.Errorf("user accessLogFile not preserved: got %v", mc["accessLogFile"])
		}
		if mc["trustDomain"] != "my-mesh" {
			t.Errorf("controller trustDomain not set: got %v", mc["trustDomain"])
		}

		global := values["global"].(map[string]any)
		if global["meshID"] != "default-my-mesh" {
			t.Errorf("controller meshID not set: got %v", global["meshID"])
		}
	})
}

func TestIsNoneMode(t *testing.T) {
	t.Run("nil templateSource", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		if mesh.IsNoneMode() {
			t.Error("expected false")
		}
	})

	t.Run("none set", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			None: &meshv1alpha1.NoneTemplateSource{},
		}
		if !mesh.IsNoneMode() {
			t.Error("expected true")
		}
	})

	t.Run("configMapRef set", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "cm"},
		}
		if mesh.IsNoneMode() {
			t.Error("expected false")
		}
	})
}

func TestEnsureMap(t *testing.T) {
	t.Run("creates new map", func(t *testing.T) {
		parent := map[string]any{}
		child := ensureMap(parent, "child")
		child["key"] = "value"
		result := parent["child"].(map[string]any)
		if result["key"] != "value" {
			t.Error("ensureMap did not create writable child map")
		}
	})

	t.Run("returns existing map", func(t *testing.T) {
		existing := map[string]any{"existing": "value"}
		parent := map[string]any{"child": existing}
		child := ensureMap(parent, "child")
		if child["existing"] != "value" {
			t.Error("ensureMap did not return existing map")
		}
	})

	t.Run("replaces non-map value", func(t *testing.T) {
		parent := map[string]any{"child": "not-a-map"}
		child := ensureMap(parent, "child")
		child["key"] = "value"
		result := parent["child"].(map[string]any)
		if result["key"] != "value" {
			t.Error("ensureMap did not replace non-map value")
		}
	})
}

func TestTemplateCache(t *testing.T) {
	t.Run("cache hit within TTL", func(t *testing.T) {
		cache := newTemplateCache()
		tmpl := map[string]any{"kind": "Istio"}
		cache.set("key1", &templateCacheEntry{
			commitSHA: "abc123",
			fetchedAt: time.Now(),
			template:  tmpl,
		})

		entry, ok := cache.get("key1")
		if !ok {
			t.Fatal("expected cache hit")
		}
		if entry.commitSHA != "abc123" {
			t.Errorf("commitSHA = %q, want abc123", entry.commitSHA)
		}
	})

	t.Run("cache miss after TTL", func(t *testing.T) {
		cache := newTemplateCache()
		cache.set("key1", &templateCacheEntry{
			commitSHA: "abc123",
			fetchedAt: time.Now().Add(-10 * time.Minute),
			template:  map[string]any{},
		})

		_, ok := cache.get("key1")
		if ok {
			t.Fatal("expected cache miss after TTL")
		}
	})

	t.Run("cache miss for unknown key", func(t *testing.T) {
		cache := newTemplateCache()
		_, ok := cache.get("nonexistent")
		if ok {
			t.Fatal("expected cache miss")
		}
	})

	t.Run("set evicts expired entries", func(t *testing.T) {
		cache := newTemplateCache()
		cache.set("old", &templateCacheEntry{
			commitSHA: "old-sha",
			fetchedAt: time.Now().Add(-10 * time.Minute),
			template:  map[string]any{},
		})
		cache.set("new", &templateCacheEntry{
			commitSHA: "new-sha",
			fetchedAt: time.Now(),
			template:  map[string]any{},
		})

		cache.mu.Lock()
		_, oldExists := cache.entries["old"]
		_, newExists := cache.entries["new"]
		cache.mu.Unlock()

		if oldExists {
			t.Error("expired entry should have been evicted")
		}
		if !newExists {
			t.Error("fresh entry should still exist")
		}
	})
}

func TestGitCacheKey(t *testing.T) {
	tests := []struct {
		name string
		url  string
		path string
		ref  *meshv1alpha1.GitRef
		want string
	}{
		{
			name: "nil ref defaults to main",
			url:  "https://github.com/org/repo.git",
			path: "istio.yaml",
			ref:  nil,
			want: "https://github.com/org/repo.git|main|istio.yaml",
		},
		{
			name: "branch ref",
			url:  "https://github.com/org/repo.git",
			path: "prod/istio.yaml",
			ref:  &meshv1alpha1.GitRef{Branch: "develop"},
			want: "https://github.com/org/repo.git|branch:develop|prod/istio.yaml",
		},
		{
			name: "tag ref",
			url:  "https://github.com/org/repo.git",
			path: "istio.yaml",
			ref:  &meshv1alpha1.GitRef{Tag: "v1.0"},
			want: "https://github.com/org/repo.git|tag:v1.0|istio.yaml",
		},
		{
			name: "commit ref",
			url:  "https://github.com/org/repo.git",
			path: "istio.yaml",
			ref:  &meshv1alpha1.GitRef{Commit: "abc123def"},
			want: "https://github.com/org/repo.git|commit:abc123def|istio.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gitCacheKey(tc.url, tc.path, tc.ref)
			if got != tc.want {
				t.Errorf("gitCacheKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCopyMap(t *testing.T) {
	t.Run("deep copy preserves nested values", func(t *testing.T) {
		src := map[string]any{
			"spec": map[string]any{
				"values": map[string]any{
					"global": map[string]any{"meshID": "test"},
				},
			},
		}
		dst := copyMap(src)

		nested := dst["spec"].(map[string]any)["values"].(map[string]any)["global"].(map[string]any)
		nested["meshID"] = "modified"

		original := src["spec"].(map[string]any)["values"].(map[string]any)["global"].(map[string]any)
		if original["meshID"] != "test" {
			t.Error("copyMap did not create independent copy — original was mutated")
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		if copyMap(nil) != nil {
			t.Error("copyMap(nil) should return nil")
		}
	})
}

func TestResolveGitRef(t *testing.T) {
	tests := []struct {
		name string
		ref  *meshv1alpha1.GitRef
		want string
	}{
		{name: "nil defaults to main", ref: nil, want: "refs/heads/main"},
		{name: "branch", ref: &meshv1alpha1.GitRef{Branch: "develop"}, want: "refs/heads/develop"},
		{name: "tag", ref: &meshv1alpha1.GitRef{Tag: "v1.0"}, want: "refs/tags/v1.0"},
		{name: "empty ref defaults to main", ref: &meshv1alpha1.GitRef{}, want: "refs/heads/main"},
		{name: "tag takes precedence over branch", ref: &meshv1alpha1.GitRef{Tag: "v1.0", Branch: "main"}, want: "refs/tags/v1.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(resolveGitRef(tc.ref))
			if got != tc.want {
				t.Errorf("resolveGitRef() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitAuthFromSecret(t *testing.T) {
	scheme := newTestScheme()

	t.Run("nil secretRef returns nil auth", func(t *testing.T) {
		r := newTestReconciler(scheme)
		auth, err := r.gitAuthFromSecret(context.Background(), "ns", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if auth != nil {
			t.Error("expected nil auth for nil secretRef")
		}
	})

	t.Run("missing Secret returns error", func(t *testing.T) {
		r := newTestReconciler(scheme)
		_, err := r.gitAuthFromSecret(context.Background(), "ns", &meshv1alpha1.SecretRef{Name: "nonexistent"})
		if err == nil {
			t.Fatal("expected error for missing Secret")
		}
	})

	t.Run("HTTPS username+password", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "ns"},
			Data: map[string][]byte{
				"username": []byte("user"),
				"password": []byte("pass"),
			},
		}
		r := newTestReconciler(scheme, secret)
		auth, err := r.gitAuthFromSecret(context.Background(), "ns", &meshv1alpha1.SecretRef{Name: "creds"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
	})

	t.Run("HTTPS token-only defaults username to git", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "token-creds", Namespace: "ns"},
			Data: map[string][]byte{
				"token": []byte("ghp_abc123"),
			},
		}
		r := newTestReconciler(scheme, secret)
		auth, err := r.gitAuthFromSecret(context.Background(), "ns", &meshv1alpha1.SecretRef{Name: "token-creds"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if auth == nil {
			t.Fatal("expected non-nil auth")
		}
	})

	t.Run("no recognized fields returns error", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "ns"},
			Data:       map[string][]byte{"unrelated": []byte("data")},
		}
		r := newTestReconciler(scheme, secret)
		_, err := r.gitAuthFromSecret(context.Background(), "ns", &meshv1alpha1.SecretRef{Name: "empty"})
		if err == nil {
			t.Fatal("expected error for unrecognized fields")
		}
	})
}

func TestTemplateSourceErrorWrapping(t *testing.T) {
	scheme := newTestScheme()

	t.Run("configMapRef error is wrapped as TemplateSourceError", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "missing"},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveIstioCRTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error")
		}

		var tsErr *TemplateSourceError
		if !errors.As(err, &tsErr) {
			t.Errorf("error should be TemplateSourceError, got %T: %v", err, err)
		}
	})

	t.Run("nil templateSource does not produce TemplateSourceError", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		r := newTestReconciler(scheme)

		_, err := r.resolveIstioCRTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// initTestGitRepo creates a local git repo in a temp directory with a single
// committed file at the given path. Returns the repo directory and commit SHA.
func initTestGitRepo(t *testing.T, filePath, content string) (repoDir, commitSHA string) {
	t.Helper()
	dir := t.TempDir()

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	fullPath := filepath.Join(dir, filePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if _, err := wt.Add(filePath); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}

	hash, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	return dir, hash.String()
}

const validIstioTemplate = `apiVersion: sailoperator.io/v1
kind: Istio
spec:
  values:
    pilot:
      resources:
        requests:
          cpu: 500m
`

func TestResolveGitTemplate(t *testing.T) {
	scheme := newTestScheme()

	t.Run("successful clone and file read", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "istio.yaml", validIstioTemplate)

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "istio.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		tmpl, err := r.resolveGitTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := tmpl["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		pilot := values["pilot"].(map[string]any)
		resources := pilot["resources"].(map[string]any)
		requests := resources["requests"].(map[string]any)
		if requests["cpu"] != "500m" {
			t.Errorf("pilot.resources.requests.cpu = %v, want 500m", requests["cpu"])
		}
	})

	t.Run("file not found at specified path", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "other.yaml", validIstioTemplate)

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "nonexistent.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveGitTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid YAML in repo", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "istio.yaml", "not: valid: yaml: [")

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "istio.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveGitTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})

	t.Run("wrong apiVersion fails validation", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "istio.yaml", "apiVersion: apps/v1\nkind: Deployment\n")

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "istio.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveGitTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("unreachable URL returns error", func(t *testing.T) {
		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  "/nonexistent/path/to/repo",
				Path: "istio.yaml",
			},
		}
		r := newTestReconciler(scheme)

		_, err := r.resolveGitTemplate(context.Background(), mesh)
		if err == nil {
			t.Fatal("expected error for unreachable URL")
		}
	})

	t.Run("commit SHA checkout", func(t *testing.T) {
		dir := t.TempDir()
		repo, _ := git.PlainInit(dir, false)
		wt, _ := repo.Worktree()

		os.WriteFile(filepath.Join(dir, "istio.yaml"), []byte(validIstioTemplate), 0o644)
		wt.Add("istio.yaml")
		firstHash, _ := wt.Commit("first", &git.CommitOptions{
			Author: &object.Signature{Name: "test", Email: "test@test.com"},
		})

		os.WriteFile(filepath.Join(dir, "istio.yaml"), []byte("apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      resources:\n        requests:\n          cpu: 2000m\n"), 0o644)
		wt.Add("istio.yaml")
		wt.Commit("second", &git.CommitOptions{
			Author: &object.Signature{Name: "test", Email: "test@test.com"},
		})

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  dir,
				Path: "istio.yaml",
				Ref:  &meshv1alpha1.GitRef{Commit: firstHash.String()},
			},
		}
		r := newTestReconciler(scheme)

		tmpl, err := r.resolveGitTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spec := tmpl["spec"].(map[string]any)
		values := spec["values"].(map[string]any)
		pilot := values["pilot"].(map[string]any)
		resources := pilot["resources"].(map[string]any)
		requests := resources["requests"].(map[string]any)
		if requests["cpu"] != "500m" {
			t.Errorf("should have first commit content (500m), got %v", requests["cpu"])
		}
	})

	t.Run("nested path in repo", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "config/istio/prod.yaml", validIstioTemplate)

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "config/istio/prod.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		tmpl, err := r.resolveGitTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tmpl["apiVersion"] != istioAPIVersion {
			t.Errorf("apiVersion = %v, want %s", tmpl["apiVersion"], istioAPIVersion)
		}
	})

	t.Run("cache returns same template on second call", func(t *testing.T) {
		repoDir, _ := initTestGitRepo(t, "istio.yaml", validIstioTemplate)

		mesh := newTestMesh("ns", "m", "set", meshv1alpha1.TopologyMultiPrimary, "")
		mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
			Git: &meshv1alpha1.GitTemplateSource{
				URL:  repoDir,
				Path: "istio.yaml",
				Ref:  &meshv1alpha1.GitRef{Branch: "master"},
			},
		}
		r := newTestReconciler(scheme)

		tmpl1, err := r.resolveGitTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}

		tmpl2, err := r.resolveGitTemplate(context.Background(), mesh)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}

		spec1 := tmpl1["spec"].(map[string]any)
		spec2 := tmpl2["spec"].(map[string]any)
		v1 := spec1["values"].(map[string]any)
		v2 := spec2["values"].(map[string]any)
		p1 := v1["pilot"].(map[string]any)
		p2 := v2["pilot"].(map[string]any)
		r1 := p1["resources"].(map[string]any)
		r2 := p2["resources"].(map[string]any)
		req1 := r1["requests"].(map[string]any)
		req2 := r2["requests"].(map[string]any)

		if req1["cpu"] != req2["cpu"] {
			t.Errorf("cached template should match: %v vs %v", req1["cpu"], req2["cpu"])
		}
	})
}
