package mesh

import (
	"context"
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func gwTestMesh(namespace, name, clusterSet string, topo meshv1alpha1.TopologyType, primary string) *meshv1alpha1.MultiClusterMesh {
	return &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			ClusterSet: clusterSet,
			Topology: meshv1alpha1.TopologyConfig{
				Type:           topo,
				PrimaryCluster: primary,
			},
		},
	}
}

func unmarshalTypedDeployment(t *testing.T, manifest workv1.Manifest) *appsv1.Deployment {
	t.Helper()
	if manifest.RawExtension.Object != nil {
		d, ok := manifest.RawExtension.Object.(*appsv1.Deployment)
		if ok {
			return d
		}
	}
	d := &appsv1.Deployment{}
	if err := json.Unmarshal(manifest.RawExtension.Raw, d); err != nil {
		t.Fatalf("failed to unmarshal Deployment: %v", err)
	}
	return d
}

func unmarshalTypedService(t *testing.T, manifest workv1.Manifest) *corev1.Service {
	t.Helper()
	if manifest.RawExtension.Object != nil {
		s, ok := manifest.RawExtension.Object.(*corev1.Service)
		if ok {
			return s
		}
	}
	s := &corev1.Service{}
	if err := json.Unmarshal(manifest.RawExtension.Raw, s); err != nil {
		t.Fatalf("failed to unmarshal Service: %v", err)
	}
	return s
}

func unmarshalToMap(t *testing.T, manifest workv1.Manifest) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(manifest.RawExtension.Raw, &obj); err != nil {
		t.Fatalf("failed to unmarshal unstructured: %v", err)
	}
	return obj
}

func ptrString(s string) *string { return &s }

func TestBuildGatewayManifestWork_MultiPrimary(t *testing.T) {
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")
	clusters := []clusterv1.ManagedCluster{*cluster}

	r := &Reconciler{}
	work := r.buildGatewayManifestWork(mesh, clusters, cluster)

	if got := len(work.Spec.Workload.Manifests); got != 4 {
		t.Fatalf("expected 4 manifests, got %d", got)
	}

	wantLabels := meshOwnedLabels(mesh, cluster.Name)
	for k, v := range wantLabels {
		if work.Labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, work.Labels[k], v)
		}
	}

	if work.Name != getGWManifestWorkName(mesh) {
		t.Errorf("name = %q, want %q", work.Name, getGWManifestWorkName(mesh))
	}
	if work.Namespace != cluster.Name {
		t.Errorf("namespace = %q, want %q", work.Namespace, cluster.Name)
	}

	// manifest[0]: ServiceAccount
	if work.Spec.Workload.Manifests[0].RawExtension.Object == nil {
		t.Fatal("expected ServiceAccount object, got nil")
	}

	// manifest[1]: Deployment
	deploy := unmarshalTypedDeployment(t, work.Spec.Workload.Manifests[1])
	revisionLabel := getIstioCRName(mesh)
	if got := deploy.Spec.Template.Labels["istio.io/rev"]; got != revisionLabel {
		t.Errorf("revision label = %q, want %q", got, revisionLabel)
	}

	networkID := getNetworkID(cluster.Name)
	envVars := deploy.Spec.Template.Spec.Containers[0].Env
	if len(envVars) == 0 || envVars[0].Name != "ISTIO_META_REQUESTED_NETWORK_VIEW" || envVars[0].Value != networkID {
		t.Errorf("expected ISTIO_META_REQUESTED_NETWORK_VIEW=%s, got %+v", networkID, envVars)
	}
	if deploy.Spec.Template.Spec.Containers[0].Image != "auto" {
		t.Errorf("image = %q, want %q", deploy.Spec.Template.Spec.Containers[0].Image, "auto")
	}

	// manifest[2]: Service
	svc := unmarshalTypedService(t, work.Spec.Workload.Manifests[2])
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("service type = %v, want LoadBalancer", svc.Spec.Type)
	}
	wantPorts := []int32{15021, 15443, 15012, 15017}
	if len(svc.Spec.Ports) != len(wantPorts) {
		t.Fatalf("expected %d ports, got %d", len(wantPorts), len(svc.Spec.Ports))
	}
	for i, p := range svc.Spec.Ports {
		if p.Port != wantPorts[i] {
			t.Errorf("port[%d] = %d, want %d", i, p.Port, wantPorts[i])
		}
	}

	// ManifestConfigs: SSA on Deployment
	var foundSSA bool
	for _, mc := range work.Spec.ManifestConfigs {
		if mc.ResourceIdentifier.Resource == "deployments" {
			foundSSA = true
			if mc.UpdateStrategy == nil || mc.UpdateStrategy.Type != workv1.UpdateStrategyTypeServerSideApply {
				t.Error("expected SSA UpdateStrategy on deployment")
			}
			if mc.UpdateStrategy.ServerSideApply == nil || mc.UpdateStrategy.ServerSideApply.FieldManager != "work-agent-mesh-addon" {
				t.Error("expected field manager work-agent-mesh-addon")
			}
		}
	}
	if !foundSSA {
		t.Error("no ManifestConfigOption found for deployments")
	}

	// ManifestConfigs: FeedbackRules on Service
	var foundFeedback bool
	for _, mc := range work.Spec.ManifestConfigs {
		if mc.ResourceIdentifier.Resource == "services" {
			foundFeedback = true
			if len(mc.FeedbackRules) == 0 {
				t.Fatal("expected FeedbackRules on service")
			}
			names := map[string]bool{}
			for _, jp := range mc.FeedbackRules[0].JsonPaths {
				names[jp.Name] = true
			}
			if !names["lbIP"] || !names["lbHostname"] {
				t.Errorf("expected lbIP and lbHostname feedback, got %v", names)
			}
		}
	}
	if !foundFeedback {
		t.Error("no ManifestConfigOption found for services")
	}

	// No istiod Gateway or VirtualService in MultiPrimary
	for i := 3; i < len(work.Spec.Workload.Manifests); i++ {
		obj := unmarshalToMap(t, work.Spec.Workload.Manifests[i])
		kind, _ := obj["kind"].(string)
		if kind == "VirtualService" {
			t.Error("MultiPrimary should not include istiod VirtualService")
		}
		meta, _ := obj["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		if kind == "Gateway" && name == "istiod-gateway" {
			t.Error("MultiPrimary should not include istiod Gateway")
		}
	}
}

func TestBuildGatewayManifestWork_PrimaryRemotePrimary(t *testing.T) {
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyPrimaryRemote, "cluster-primary")
	primary := newTestCluster("cluster-primary")
	remote := newTestCluster("cluster-remote")
	clusters := []clusterv1.ManagedCluster{*primary, *remote}

	r := &Reconciler{}
	work := r.buildGatewayManifestWork(mesh, clusters, primary)

	if got := len(work.Spec.Workload.Manifests); got != 6 {
		t.Fatalf("expected 6 manifests for primary in PrimaryRemote, got %d", got)
	}

	// manifest[4]: istiod Gateway
	gwObj := unmarshalToMap(t, work.Spec.Workload.Manifests[4])
	if gwObj["kind"] != "Gateway" {
		t.Errorf("manifest[4] kind = %v, want Gateway", gwObj["kind"])
	}
	gwMeta, _ := gwObj["metadata"].(map[string]any)
	if gwMeta["name"] != "istiod-gateway" {
		t.Errorf("manifest[4] name = %v, want istiod-gateway", gwMeta["name"])
	}

	// manifest[5]: istiod VirtualService with correct host
	vsObj := unmarshalToMap(t, work.Spec.Workload.Manifests[5])
	if vsObj["kind"] != "VirtualService" {
		t.Errorf("manifest[5] kind = %v, want VirtualService", vsObj["kind"])
	}
	vsSpec, _ := vsObj["spec"].(map[string]any)
	tlsRoutes, _ := vsSpec["tls"].([]any)
	if len(tlsRoutes) != 2 {
		t.Fatalf("expected 2 TLS routes, got %d", len(tlsRoutes))
	}
	expectedHost := "istiod." + mesh.GetControlPlaneNamespace() + ".svc.cluster.local"
	firstRoute := tlsRoutes[0].(map[string]any)
	routes, _ := firstRoute["route"].([]any)
	dest := routes[0].(map[string]any)["destination"].(map[string]any)
	if dest["host"] != expectedHost {
		t.Errorf("VS route host = %v, want %v", dest["host"], expectedHost)
	}
}

func TestBuildGatewayManifestWork_PrimaryRemoteRemote(t *testing.T) {
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyPrimaryRemote, "cluster-primary")
	primary := newTestCluster("cluster-primary")
	remote := newTestCluster("cluster-remote")
	clusters := []clusterv1.ManagedCluster{*primary, *remote}

	r := &Reconciler{}
	work := r.buildGatewayManifestWork(mesh, clusters, remote)

	if got := len(work.Spec.Workload.Manifests); got != 4 {
		t.Fatalf("expected 4 manifests for remote in PrimaryRemote (no istiod GW/VS), got %d", got)
	}
}

func TestBuildCrossNetworkGateway(t *testing.T) {
	gw := buildCrossNetworkGateway("istio-system")

	spec, _ := gw.Object["spec"].(map[string]any)
	servers, _ := spec["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}

	server := servers[0].(map[string]any)

	hosts, _ := server["hosts"].([]any)
	if len(hosts) != 1 || hosts[0] != "*.local" {
		t.Errorf("hosts = %v, want [*.local]", hosts)
	}

	port, _ := server["port"].(map[string]any)
	if port["number"] != int64(15443) {
		t.Errorf("port number = %v, want 15443", port["number"])
	}
	if port["protocol"] != "TLS" {
		t.Errorf("port protocol = %v, want TLS", port["protocol"])
	}

	tls, _ := server["tls"].(map[string]any)
	if tls["mode"] != "AUTO_PASSTHROUGH" {
		t.Errorf("tls mode = %v, want AUTO_PASSTHROUGH", tls["mode"])
	}
}

func TestBuildIstiodGateway(t *testing.T) {
	gw := buildIstiodGateway("istio-system")

	spec, _ := gw.Object["spec"].(map[string]any)
	servers, _ := spec["servers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}

	wantPorts := []int64{15012, 15017}
	for i, s := range servers {
		srv := s.(map[string]any)
		port := srv["port"].(map[string]any)
		if port["number"] != wantPorts[i] {
			t.Errorf("server[%d] port = %v, want %d", i, port["number"], wantPorts[i])
		}

		tls := srv["tls"].(map[string]any)
		if tls["mode"] != "PASSTHROUGH" {
			t.Errorf("server[%d] tls mode = %v, want PASSTHROUGH", i, tls["mode"])
		}
	}
}

func TestBuildIstiodVirtualService(t *testing.T) {
	vs := buildIstiodVirtualService("istio-system")
	expectedHost := "istiod.istio-system.svc.cluster.local"

	spec, _ := vs.Object["spec"].(map[string]any)
	tlsRoutes, _ := spec["tls"].([]any)
	if len(tlsRoutes) != 2 {
		t.Fatalf("expected 2 TLS routes, got %d", len(tlsRoutes))
	}

	// Route 0: port 15012 -> istiod:15012
	route0 := tlsRoutes[0].(map[string]any)
	match0 := route0["match"].([]any)[0].(map[string]any)
	if match0["port"] != int64(15012) {
		t.Errorf("route[0] match port = %v, want 15012", match0["port"])
	}
	dest0 := route0["route"].([]any)[0].(map[string]any)["destination"].(map[string]any)
	if dest0["host"] != expectedHost {
		t.Errorf("route[0] host = %v, want %v", dest0["host"], expectedHost)
	}
	destPort0 := dest0["port"].(map[string]any)
	if destPort0["number"] != int64(15012) {
		t.Errorf("route[0] dest port = %v, want 15012", destPort0["number"])
	}

	// Route 1: port 15017 -> istiod:443
	route1 := tlsRoutes[1].(map[string]any)
	match1 := route1["match"].([]any)[0].(map[string]any)
	if match1["port"] != int64(15017) {
		t.Errorf("route[1] match port = %v, want 15017", match1["port"])
	}
	dest1 := route1["route"].([]any)[0].(map[string]any)["destination"].(map[string]any)
	if dest1["host"] != expectedHost {
		t.Errorf("route[1] host = %v, want %v", dest1["host"], expectedHost)
	}
	destPort1 := dest1["port"].(map[string]any)
	if destPort1["number"] != int64(443) {
		t.Errorf("route[1] dest port = %v, want 443", destPort1["number"])
	}
}

func TestIsGatewayReady(t *testing.T) {
	scheme := newTestScheme()
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")

	tests := []struct {
		name     string
		feedback []workv1.FeedbackValue
		expected bool
	}{
		{
			name: "ready with IP",
			feedback: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: ptrString("10.0.0.1")}},
			},
			expected: true,
		},
		{
			name: "ready with hostname",
			feedback: []workv1.FeedbackValue{
				{Name: "lbHostname", Value: workv1.FieldValue{String: ptrString("a.elb.amazonaws.com")}},
			},
			expected: true,
		},
		{
			name:     "not ready with empty feedback",
			feedback: []workv1.FeedbackValue{},
			expected: false,
		},
		{
			name: "not ready with empty strings",
			feedback: []workv1.FeedbackValue{
				{Name: "lbIP", Value: workv1.FieldValue{String: ptrString("")}},
				{Name: "lbHostname", Value: workv1.FieldValue{String: ptrString("")}},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gwWork := &workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getGWManifestWorkName(mesh),
					Namespace: cluster.Name,
				},
				Status: workv1.ManifestWorkStatus{
					ResourceStatus: workv1.ManifestResourceStatus{
						Manifests: []workv1.ManifestCondition{{
							ResourceMeta: workv1.ManifestResourceMeta{Kind: "Service"},
							StatusFeedbacks: workv1.StatusFeedbackResult{
								Values: tc.feedback,
							},
						}},
					},
				},
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(gwWork).
				WithStatusSubresource(gwWork).
				Build()

			r := &Reconciler{Client: c, Scheme: scheme}
			got := r.isGatewayReady(context.Background(), mesh, cluster)
			if got != tc.expected {
				t.Errorf("isGatewayReady() = %v, want %v", got, tc.expected)
			}
		})
	}

	t.Run("not ready when ManifestWork missing", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Reconciler{Client: c, Scheme: scheme}
		if r.isGatewayReady(context.Background(), mesh, cluster) {
			t.Error("expected false when ManifestWork does not exist")
		}
	})
}

func TestAreAllGatewaysReady(t *testing.T) {
	scheme := newTestScheme()
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")

	clusters := []clusterv1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
	}

	makeGWWork := func(clusterName string, ip string) *workv1.ManifestWork {
		return &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getGWManifestWorkName(mesh),
				Namespace: clusterName,
			},
			Status: workv1.ManifestWorkStatus{
				ResourceStatus: workv1.ManifestResourceStatus{
					Manifests: []workv1.ManifestCondition{{
						ResourceMeta: workv1.ManifestResourceMeta{Kind: "Service"},
						StatusFeedbacks: workv1.StatusFeedbackResult{
							Values: []workv1.FeedbackValue{
								{Name: "lbIP", Value: workv1.FieldValue{String: ptrString(ip)}},
							},
						},
					}},
				},
			},
		}
	}

	t.Run("all ready", func(t *testing.T) {
		workA := makeGWWork("cluster-a", "10.0.0.1")
		workB := makeGWWork("cluster-b", "10.0.0.2")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(workA, workB).
			WithStatusSubresource(workA, workB).
			Build()

		r := &Reconciler{Client: c, Scheme: scheme}
		if !r.areAllGatewaysReady(context.Background(), mesh, clusters) {
			t.Error("expected true when all gateways have addresses")
		}
	})

	t.Run("one not ready", func(t *testing.T) {
		workA := makeGWWork("cluster-a", "10.0.0.1")
		workB := makeGWWork("cluster-b", "")

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(workA, workB).
			WithStatusSubresource(workA, workB).
			Build()

		r := &Reconciler{Client: c, Scheme: scheme}
		if r.areAllGatewaysReady(context.Background(), mesh, clusters) {
			t.Error("expected false when one gateway has no address")
		}
	})

	t.Run("none ready", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Reconciler{Client: c, Scheme: scheme}
		if r.areAllGatewaysReady(context.Background(), mesh, clusters) {
			t.Error("expected false when no ManifestWorks exist")
		}
	})
}

func TestBuildGatewayManifestWork_NodePort(t *testing.T) {
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	mesh.Spec.Gateway.ServiceType = meshv1alpha1.GatewayServiceTypeNodePort
	cluster := newTestCluster("cluster-a")
	clusters := []clusterv1.ManagedCluster{*cluster}

	r := &Reconciler{}
	work := r.buildGatewayManifestWork(mesh, clusters, cluster)

	svc := unmarshalTypedService(t, work.Spec.Workload.Manifests[2])
	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("expected ServiceType=NodePort, got %s", svc.Spec.Type)
	}
}

func TestBuildGatewayManifestWork_LoadBalancerDefault(t *testing.T) {
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	cluster := newTestCluster("cluster-a")
	clusters := []clusterv1.ManagedCluster{*cluster}

	r := &Reconciler{}
	work := r.buildGatewayManifestWork(mesh, clusters, cluster)

	svc := unmarshalTypedService(t, work.Spec.Workload.Manifests[2])
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("expected ServiceType=LoadBalancer (default), got %s", svc.Spec.Type)
	}
}

func TestIsGatewayReady_NodePort(t *testing.T) {
	scheme := newTestScheme()
	mesh := gwTestMesh("test-ns", "my-mesh", "test-set", meshv1alpha1.TopologyMultiPrimary, "")
	mesh.Spec.Gateway.ServiceType = meshv1alpha1.GatewayServiceTypeNodePort
	cluster := newTestCluster("cluster-a")

	t.Run("ready when ManifestWork Applied condition is True", func(t *testing.T) {
		work := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getGWManifestWorkName(mesh),
				Namespace: "cluster-a",
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{{
					Type:   "Applied",
					Status: metav1.ConditionTrue,
				}},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(work).
			WithStatusSubresource(work).
			Build()
		r := &Reconciler{Client: c, Scheme: scheme}

		if !r.isGatewayReady(context.Background(), mesh, cluster) {
			t.Error("expected gateway ready for NodePort with Applied=True")
		}
	})

	t.Run("not ready when ManifestWork has no Applied condition", func(t *testing.T) {
		work := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getGWManifestWorkName(mesh),
				Namespace: "cluster-a",
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(work).
			WithStatusSubresource(work).
			Build()
		r := &Reconciler{Client: c, Scheme: scheme}

		if r.isGatewayReady(context.Background(), mesh, cluster) {
			t.Error("expected gateway not ready for NodePort without Applied condition")
		}
	})

	t.Run("not ready when ManifestWork does not exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Reconciler{Client: c, Scheme: scheme}

		if r.isGatewayReady(context.Background(), mesh, cluster) {
			t.Error("expected gateway not ready when ManifestWork missing")
		}
	})
}
