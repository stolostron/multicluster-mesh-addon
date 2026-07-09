package mesh

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func TestGetNetworkID(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		want        string
	}{
		{name: "simple name", clusterName: "cluster1", want: "network-cluster1"},
		{name: "empty name", clusterName: "", want: "network-"},
		{name: "name with dashes", clusterName: "my-cluster-01", want: "network-my-cluster-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getNetworkID(tc.clusterName); got != tc.want {
				t.Errorf("getNetworkID(%q) = %q, want %q", tc.clusterName, got, tc.want)
			}
		})
	}
}

func TestGetMeshID(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		meshName  string
		want      string
	}{
		{name: "basic", namespace: "ns1", meshName: "mesh1", want: "ns1-mesh1"},
		{name: "empty namespace", namespace: "", meshName: "mesh1", want: "-mesh1"},
		{name: "empty name", namespace: "ns1", meshName: "", want: "ns1-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := &meshv1alpha1.MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{Namespace: tc.namespace, Name: tc.meshName},
			}
			if got := getMeshID(mesh); got != tc.want {
				t.Errorf("getMeshID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetIstioCRName(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "main"},
	}
	want := "prod-main-cp"
	if got := getIstioCRName(mesh); got != want {
		t.Errorf("getIstioCRName() = %q, want %q", got, want)
	}
}

func TestGetMSAName(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "main"},
	}
	want := "prod-istio-reader-main"
	if got := getMSAName(mesh); got != want {
		t.Errorf("getMSAName() = %q, want %q", got, want)
	}
}

func TestManifestWorkNames(t *testing.T) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "mesh"},
	}

	tests := []struct {
		name string
		fn   func(*meshv1alpha1.MultiClusterMesh) string
		want string
	}{
		{name: "control plane", fn: getCPManifestWorkName, want: "multicluster-mesh-cp-ns-mesh"},
		{name: "gateway", fn: getGWManifestWorkName, want: "multicluster-mesh-gw-ns-mesh"},
		{name: "remote secret", fn: getRSManifestWorkName, want: "multicluster-mesh-rs-ns-mesh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(mesh); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetGatewayAddress(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name   string
		values []workv1.FeedbackValue
		want   string
	}{
		{name: "nil values", values: nil, want: ""},
		{name: "empty values", values: []workv1.FeedbackValue{}, want: ""},
		{
			name: "IP only",
			values: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: strPtr("10.0.0.1")}},
			},
			want: "10.0.0.1",
		},
		{
			name: "hostname only",
			values: []workv1.FeedbackValue{
				{Name: "lbHostname", Value: workv1.FieldValue{String: strPtr("lb.example.com")}},
			},
			want: "lb.example.com",
		},
		{
			name: "IP preferred over hostname",
			values: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: strPtr("10.0.0.1")}},
				{Name: "lbHostname", Value: workv1.FieldValue{String: strPtr("lb.example.com")}},
			},
			want: "10.0.0.1",
		},
		{
			name: "empty IP falls back to hostname",
			values: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: strPtr("")}},
				{Name: "lbHostname", Value: workv1.FieldValue{String: strPtr("lb.example.com")}},
			},
			want: "lb.example.com",
		},
		{
			name: "nil IP pointer falls back to hostname",
			values: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: nil}},
				{Name: "lbHostname", Value: workv1.FieldValue{String: strPtr("lb.example.com")}},
			},
			want: "lb.example.com",
		},
		{
			name: "both nil pointers",
			values: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: nil}},
				{Name: "lbHostname", Value: workv1.FieldValue{String: nil}},
			},
			want: "",
		},
		{
			name: "unrelated feedback values only",
			values: []workv1.FeedbackValue{
				{Name: "other", Value: workv1.FieldValue{String: strPtr("ignored")}},
			},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getGatewayAddress(tc.values); got != tc.want {
				t.Errorf("getGatewayAddress() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsOpenShift(t *testing.T) {
	clusterWithProduct := func(product string) *clusterv1.ManagedCluster {
		c := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
		if product != "" {
			c.Status.ClusterClaims = []clusterv1.ManagedClusterClaim{
				{Name: "product.open-cluster-management.io", Value: product},
			}
		}
		return c
	}

	tests := []struct {
		name    string
		product string
		want    bool
	}{
		{name: "OpenShift", product: "OpenShift", want: true},
		{name: "ROSA", product: "ROSA", want: true},
		{name: "ARO", product: "ARO", want: true},
		{name: "ROKS", product: "ROKS", want: true},
		{name: "OpenShiftDedicated", product: "OpenShiftDedicated", want: true},
		{name: "Kind", product: "Kind", want: false},
		{name: "empty string", product: "", want: false},
		{name: "other product", product: "EKS", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOpenShift(clusterWithProduct(tc.product)); got != tc.want {
				t.Errorf("isOpenShift(%q) = %v, want %v", tc.product, got, tc.want)
			}
		})
	}
}

func TestGetPrimaryCluster(t *testing.T) {
	makeClusters := func(names ...string) []clusterv1.ManagedCluster {
		clusters := make([]clusterv1.ManagedCluster, len(names))
		for i, n := range names {
			clusters[i] = clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: n}}
		}
		return clusters
	}

	tests := []struct {
		name           string
		primaryCluster string
		clusters       []clusterv1.ManagedCluster
		wantName       string
		wantNil        bool
	}{
		{
			name:           "explicit primary found",
			primaryCluster: "cluster-b",
			clusters:       makeClusters("cluster-a", "cluster-b", "cluster-c"),
			wantName:       "cluster-b",
		},
		{
			name:           "explicit primary not found falls back to first",
			primaryCluster: "nonexistent",
			clusters:       makeClusters("cluster-a", "cluster-b"),
			wantName:       "cluster-a",
		},
		{
			name:           "empty primary returns first cluster",
			primaryCluster: "",
			clusters:       makeClusters("cluster-a", "cluster-b"),
			wantName:       "cluster-a",
		},
		{
			name:           "no clusters returns nil",
			primaryCluster: "",
			clusters:       nil,
			wantNil:        true,
		},
		{
			name:           "single cluster",
			primaryCluster: "",
			clusters:       makeClusters("only"),
			wantName:       "only",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := &meshv1alpha1.MultiClusterMesh{
				Spec: meshv1alpha1.MultiClusterMeshSpec{
					Topology: meshv1alpha1.TopologyConfig{
						PrimaryCluster: tc.primaryCluster,
					},
				},
			}
			got := getPrimaryCluster(mesh, tc.clusters)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %q", got.Name)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil, got nil")
			}
			if got.Name != tc.wantName {
				t.Errorf("got %q, want %q", got.Name, tc.wantName)
			}
		})
	}
}

func TestGetRemoteClusters(t *testing.T) {
	makeClusters := func(names ...string) []clusterv1.ManagedCluster {
		clusters := make([]clusterv1.ManagedCluster, len(names))
		for i, n := range names {
			clusters[i] = clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: n}}
		}
		return clusters
	}

	tests := []struct {
		name           string
		primaryCluster string
		clusters       []clusterv1.ManagedCluster
		wantNames      []string
	}{
		{
			name:           "excludes explicit primary",
			primaryCluster: "cluster-b",
			clusters:       makeClusters("cluster-a", "cluster-b", "cluster-c"),
			wantNames:      []string{"cluster-a", "cluster-c"},
		},
		{
			name:           "excludes first when no primary set",
			primaryCluster: "",
			clusters:       makeClusters("cluster-a", "cluster-b"),
			wantNames:      []string{"cluster-b"},
		},
		{
			name:           "single cluster returns empty remotes",
			primaryCluster: "",
			clusters:       makeClusters("only"),
			wantNames:      nil,
		},
		{
			name:           "no clusters returns nil",
			primaryCluster: "",
			clusters:       nil,
			wantNames:      nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := &meshv1alpha1.MultiClusterMesh{
				Spec: meshv1alpha1.MultiClusterMeshSpec{
					Topology: meshv1alpha1.TopologyConfig{
						PrimaryCluster: tc.primaryCluster,
					},
				},
			}
			got := getRemoteClusters(mesh, tc.clusters)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d remotes, want %d", len(got), len(tc.wantNames))
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("remote[%d] = %q, want %q", i, got[i].Name, want)
				}
			}
		})
	}
}
