package main

import (
	"bytes"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestRenderScaffold(t *testing.T) {
	manifest, err := renderScaffold(validScaffoldOptions())
	if err != nil {
		t.Fatalf("render scaffold: %v", err)
	}

	documents := decodeDocuments(t, manifest)
	policy := documents[0]
	if policy["kind"] != "Policy" {
		t.Fatalf("first document kind = %v, want Policy", policy["kind"])
	}
	metadata := policy["metadata"].(map[string]any)
	if metadata["namespace"] != "mesh-system" {
		t.Fatalf("policy namespace = %v, want mesh-system", metadata["namespace"])
	}
	policyName := metadata["name"].(string)
	if policyName != defaultPolicyName("istio-installation", "mesh-system", "basic-mesh") {
		t.Fatalf("policy name = %q, want deterministic default", policyName)
	}

	policySpec := policy["spec"].(map[string]any)
	if policySpec["remediationAction"] != "enforce" {
		t.Fatalf("remediationAction = %v, want enforce", policySpec["remediationAction"])
	}
	policyTemplates := policySpec["policy-templates"].([]any)
	policyTemplate := policyTemplates[0].(map[string]any)
	configurationPolicy := policyTemplate["objectDefinition"].(map[string]any)
	if configurationPolicy["apiVersion"] != configurationPolicyAPI || configurationPolicy["kind"] != "ConfigurationPolicy" {
		t.Fatalf("unexpected embedded resource: %v %v", configurationPolicy["apiVersion"], configurationPolicy["kind"])
	}

	configurationSpec := configurationPolicy["spec"].(map[string]any)
	if configurationSpec["pruneObjectBehavior"] != "DeleteIfCreated" {
		t.Fatalf("pruneObjectBehavior = %v, want DeleteIfCreated", configurationSpec["pruneObjectBehavior"])
	}
	if _, ok := configurationSpec["object-templates-raw"]; ok {
		t.Fatal("ConfigurationPolicy used object-templates-raw, want structured object-templates")
	}
	objectTemplates := configurationSpec["object-templates"].([]any)
	if len(objectTemplates) != 2 {
		t.Fatalf("got %d object templates, want Istio and Gateway", len(objectTemplates))
	}
	istio := objectTemplates[0].(map[string]any)["objectDefinition"].(map[string]any)
	istioSpec := istio["spec"].(map[string]any)
	expectedNamespace := `{{hub (lookup "mesh.open-cluster-management.io/v1alpha1" "MultiClusterMesh" "mesh-system" "basic-mesh").status.controlPlaneNamespace hub}}`
	expectedNetwork := `{{hub index .ManagedClusterLabels "topology.istio.io/network" | default .ManagedClusterName hub}}`
	if istioSpec["namespace"] != expectedNamespace || istioSpec["version"] != "v1.30.4" {
		t.Fatalf("unexpected Istio spec: %v", istioSpec)
	}
	values := istioSpec["values"].(map[string]any)
	meshConfig := values["meshConfig"].(map[string]any)
	global := values["global"].(map[string]any)
	if meshConfig["trustDomain"] != "basic-mesh" || global["meshID"] != "basic-mesh" {
		t.Fatalf("mesh name anchor did not resolve in YAML: meshConfig=%v global=%v", meshConfig, global)
	}
	if global["network"] != expectedNetwork || global["multiCluster"].(map[string]any)["clusterName"] != "{{hub .ManagedClusterName hub}}" {
		t.Fatalf("unexpected global mesh values: %v", global)
	}
	gateway := objectTemplates[1].(map[string]any)["objectDefinition"].(map[string]any)
	gatewayMetadata := gateway["metadata"].(map[string]any)
	if gatewayMetadata["namespace"] != expectedNamespace || gatewayMetadata["labels"].(map[string]any)[managedClusterNetwork] != expectedNetwork {
		t.Fatalf("namespace and network aliases did not resolve in YAML: %v", gatewayMetadata)
	}
	for _, anchor := range []string{"&controlPlaneNamespace", "*controlPlaneNamespace", "&meshName", "*meshName", "&network", "*network"} {
		if !strings.Contains(string(manifest), anchor) {
			t.Errorf("generated manifest does not contain YAML anchor syntax %q", anchor)
		}
	}

	binding := documents[1]
	if binding["kind"] != "PlacementBinding" {
		t.Fatalf("second document kind = %v, want PlacementBinding", binding["kind"])
	}
	bindingMetadata := binding["metadata"].(map[string]any)
	if bindingMetadata["namespace"] != "mesh-system" {
		t.Fatalf("binding namespace = %v, want mesh-system", bindingMetadata["namespace"])
	}
	placementRef := binding["placementRef"].(map[string]any)
	if placementRef["name"] != "mesh-placement" {
		t.Fatalf("PlacementBinding references %v, want mesh-placement", placementRef["name"])
	}
	if len(binding["subjects"].([]any)) != 1 {
		t.Fatalf("PlacementBinding subjects = %v, want one Policy", binding["subjects"])
	}
}

func TestRenderScaffoldDependsOnIstioCNI(t *testing.T) {
	options := validScaffoldOptions()
	options.dependsOnIstioCNI = "istiocni-installation-basic-mesh"
	manifest, err := renderScaffold(options)
	if err != nil {
		t.Fatalf("render scaffold: %v", err)
	}

	policy := decodeDocuments(t, manifest)[0]
	policyTemplate := policy["spec"].(map[string]any)["policy-templates"].([]any)[0].(map[string]any)
	dependencies := policyTemplate["extraDependencies"].([]any)
	if len(dependencies) != 1 {
		t.Fatalf("got %d extra dependencies, want one", len(dependencies))
	}
	dependency := dependencies[0].(map[string]any)
	if dependency["apiVersion"] != configurationPolicyAPI || dependency["kind"] != "ConfigurationPolicy" {
		t.Fatalf("unexpected dependency resource: %v %v", dependency["apiVersion"], dependency["kind"])
	}
	if dependency["name"] != options.dependsOnIstioCNI+"-ready" || dependency["compliance"] != "Compliant" {
		t.Fatalf("unexpected dependency: %v", dependency)
	}
}

func TestRenderIstioCNIScaffold(t *testing.T) {
	manifest, err := renderIstioCNIScaffold(istioCNIOptions{
		policyScaffoldOptions: policyScaffoldOptions{
			namespace:           "mesh-system",
			placementName:       "mesh-placement",
			remediationAction:   defaultRemediation,
			pruneObjectBehavior: defaultPruneBehavior,
		},
		cniName:      defaultIstioCNIName,
		cniNamespace: defaultIstioCNINamespace,
	})
	if err != nil {
		t.Fatalf("render IstioCNI scaffold: %v", err)
	}

	documents := decodeDocuments(t, manifest)
	policy := documents[0]
	policyName := policy["metadata"].(map[string]any)["name"].(string)
	if policyName != defaultPolicyName("istiocni-installation", "mesh-system", "istio-cni-default") {
		t.Fatalf("policy name = %q, want deterministic CNI policy name", policyName)
	}
	policyTemplates := policy["spec"].(map[string]any)["policy-templates"].([]any)
	if len(policyTemplates) != 2 {
		t.Fatalf("got %d policy templates, want CNI install and readiness policies", len(policyTemplates))
	}
	configurationPolicy := policyTemplates[0].(map[string]any)["objectDefinition"].(map[string]any)
	configurationSpec := configurationPolicy["spec"].(map[string]any)
	if _, ok := configurationSpec["object-templates-raw"]; ok {
		t.Fatal("IstioCNI ConfigurationPolicy used object-templates-raw, want structured object-templates")
	}
	objects := configurationSpec["object-templates"].([]any)
	if len(objects) != 2 {
		t.Fatalf("got %d IstioCNI objects, want Namespace and IstioCNI", len(objects))
	}
	namespace := objects[0].(map[string]any)["objectDefinition"].(map[string]any)
	istiocni := objects[1].(map[string]any)["objectDefinition"].(map[string]any)
	if namespace["kind"] != "Namespace" || namespace["metadata"].(map[string]any)["name"] != "istio-cni" {
		t.Fatalf("unexpected CNI Namespace object: %v", namespace)
	}
	if istiocni["kind"] != "IstioCNI" || istiocni["metadata"].(map[string]any)["name"] != "default" || istiocni["spec"].(map[string]any)["namespace"] != "istio-cni" {
		t.Fatalf("unexpected IstioCNI object: %v", istiocni)
	}

	readinessPolicy := policyTemplates[1].(map[string]any)["objectDefinition"].(map[string]any)
	if readinessPolicy["metadata"].(map[string]any)["name"] != policyName+"-ready" {
		t.Fatalf("readiness ConfigurationPolicy name = %v, want %s-ready", readinessPolicy["metadata"].(map[string]any)["name"], policyName)
	}
	readinessSpec := readinessPolicy["spec"].(map[string]any)
	if readinessSpec["remediationAction"] != "inform" {
		t.Fatalf("readiness remediationAction = %v, want inform", readinessSpec["remediationAction"])
	}
	readinessObjects := readinessSpec["object-templates"].([]any)
	condition := readinessObjects[0].(map[string]any)["objectDefinition"].(map[string]any)["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any)
	if condition["type"] != "Ready" || condition["status"] != "True" {
		t.Fatalf("readiness policy does not check Ready=True: %v", condition)
	}
}

func TestRenderScaffoldRejectsInvalidOptions(t *testing.T) {
	valid := validScaffoldOptions()
	tests := []struct {
		name   string
		update func(*scaffoldOptions)
	}{
		{name: "missing mesh", update: func(options *scaffoldOptions) { options.meshName = "" }},
		{name: "invalid namespace", update: func(options *scaffoldOptions) { options.namespace = "bad namespace" }},
		{name: "missing Placement", update: func(options *scaffoldOptions) { options.placementName = "" }},
		{name: "missing version", update: func(options *scaffoldOptions) { options.istioVersion = " " }},
		{name: "invalid remediation action", update: func(options *scaffoldOptions) { options.remediationAction = "pause" }},
		{name: "invalid prune behavior", update: func(options *scaffoldOptions) { options.pruneObjectBehavior = "DeleteAll" }},
		{name: "invalid dependency name", update: func(options *scaffoldOptions) { options.dependsOnIstioCNI = "bad name" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.update(&options)
			if _, err := renderScaffold(options); err == nil {
				t.Fatal("renderScaffold succeeded, want validation error")
			}
		})
	}
}

func TestRenderScaffoldTargetsNamedIstioRevision(t *testing.T) {
	options := validScaffoldOptions()
	options.istioName = "mesh-a"
	manifest, err := renderScaffold(options)
	if err != nil {
		t.Fatalf("render scaffold: %v", err)
	}

	objects := decodeDocuments(t, manifest)[0]["spec"].(map[string]any)["policy-templates"].([]any)[0].(map[string]any)["objectDefinition"].(map[string]any)["spec"].(map[string]any)["object-templates"].([]any)
	gateway := objects[1].(map[string]any)["objectDefinition"].(map[string]any)
	labels := gateway["metadata"].(map[string]any)["labels"].(map[string]any)
	if labels["istio.io/rev"] != "mesh-a" {
		t.Fatalf("gateway labels do not target Istio revision: %v", labels)
	}
}

func TestScaffoldCommandValidatesRequiredFlagsAndEmptyValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing Istio flags",
			args: []string{"scaffold", "istio", "mesh"},
			want: `required flag(s) "istio-version", "placement" not set`,
		},
		{
			name: "explicit empty required flag",
			args: []string{"scaffold", "istio", "mesh", "--placement=", "--istio-version=v1.30.4"},
			want: "placement name is required",
		},
		{
			name: "missing IstioCNI Placement",
			args: []string{"scaffold", "istiocni"},
			want: `required flag(s) "placement" not set`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := newCommand()
			command.SetArgs(test.args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestDefaultPolicyNameIsScopedAndBounded(t *testing.T) {
	first := defaultPolicyName("istio-installation", "mesh-system", "example-mesh")
	if len(first) > 63 {
		t.Fatalf("default policy name length = %d, want at most 63", len(first))
	}
	if first == defaultPolicyName("istio-installation", "other-namespace", "example-mesh") {
		t.Fatal("policy names for different namespaces should differ")
	}
	if first == defaultPolicyName("istio-installation", "mesh-system", "other-mesh") {
		t.Fatal("policy names for different mesh names should differ")
	}
}

func validScaffoldOptions() scaffoldOptions {
	return scaffoldOptions{
		policyScaffoldOptions: policyScaffoldOptions{
			namespace:           "mesh-system",
			placementName:       "mesh-placement",
			remediationAction:   defaultRemediation,
			pruneObjectBehavior: defaultPruneBehavior,
		},
		meshName:     "basic-mesh",
		istioVersion: "v1.30.4",
		istioName:    defaultIstioName,
		gatewayName:  defaultGatewayName,
	}
}

func decodeDocuments(t *testing.T, manifest []byte) []map[string]any {
	t.Helper()

	documents := strings.Split(strings.TrimSpace(string(manifest)), "\n---\n")
	decoded := make([]map[string]any, len(documents))
	for i, document := range documents {
		if err := yaml.Unmarshal([]byte(document), &decoded[i]); err != nil {
			t.Fatalf("decode YAML document %d: %v", i, err)
		}
	}
	return decoded
}
