package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultNamespace         = "default"
	defaultIstioName         = "default"
	defaultIstioCNINamespace = "istio-cni"
	defaultIstioCNIName      = "default"
	defaultGatewayName       = "eastwestgateway"
	defaultRemediation       = "enforce"
	defaultRemediationInform = "inform"
	defaultPruneBehavior     = "DeleteIfCreated"
	configurationPolicyAPI   = "policy.open-cluster-management.io/v1"
	meshAPI                  = "mesh.open-cluster-management.io/v1alpha1"
	managedClusterNetwork    = "topology.istio.io/network"
)

type policyScaffoldOptions struct {
	namespace           string
	placementName       string
	policyName          string
	remediationAction   string
	pruneObjectBehavior string
}

type scaffoldOptions struct {
	policyScaffoldOptions
	meshName          string
	istioVersion      string
	istioName         string
	gatewayName       string
	dependsOnIstioCNI string
}

type istioCNIOptions struct {
	policyScaffoldOptions
	cniName      string
	cniNamespace string
}

type policyTemplateOptions struct {
	name                string
	objectTemplates     []any
	remediationAction   string
	pruneObjectBehavior string
	dependsOn           string
}

func newScaffoldCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "scaffold",
		Short: "Generate ACM policy bundles for mesh resources",
	}
	command.AddCommand(newIstioScaffoldCommand(), newIstioCNIScaffoldCommand())

	return command
}

func newIstioScaffoldCommand() *cobra.Command {
	options := scaffoldOptions{
		policyScaffoldOptions: defaultPolicyScaffoldOptions(),
		istioName:             defaultIstioName,
		gatewayName:           defaultGatewayName,
	}

	command := &cobra.Command{
		Use:   "istio MESH_NAME",
		Short: "Generate a policy for Istio and its east-west Gateway",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			options.meshName = args[0]
			manifest, err := renderScaffold(options)
			if err != nil {
				return err
			}

			_, err = command.OutOrStdout().Write(manifest)
			return err
		},
	}

	addPolicyScaffoldFlags(command, &options.policyScaffoldOptions)
	command.Flags().StringVar(&options.istioVersion, "istio-version", "", "Istio version supported by the installed Sail or OSSM operator (required)")
	command.Flags().StringVar(&options.istioName, "istio-name", defaultIstioName, "Name of the generated cluster-scoped Istio resource")
	command.Flags().StringVar(&options.gatewayName, "gateway-name", defaultGatewayName, "Name of the generated east-west Gateway resource")
	command.Flags().StringVar(&options.dependsOnIstioCNI, "depends-on-istiocni", "", "IstioCNI Policy name from scaffold istiocni; wait for its Ready condition before applying Istio")
	markRequired(command, "istio-version")

	return command
}

func newIstioCNIScaffoldCommand() *cobra.Command {
	options := istioCNIOptions{
		policyScaffoldOptions: defaultPolicyScaffoldOptions(),
		cniName:               defaultIstioCNIName,
		cniNamespace:          defaultIstioCNINamespace,
	}

	command := &cobra.Command{
		Use:   "istiocni",
		Short: "Generate a policy for the IstioCNI resource",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manifest, err := renderIstioCNIScaffold(options)
			if err != nil {
				return err
			}

			_, err = command.OutOrStdout().Write(manifest)
			return err
		},
	}

	addPolicyScaffoldFlags(command, &options.policyScaffoldOptions)
	command.Flags().StringVar(&options.cniName, "cni-name", defaultIstioCNIName, "Name of the generated IstioCNI resource")
	command.Flags().StringVar(&options.cniNamespace, "cni-namespace", defaultIstioCNINamespace, "Namespace used by the IstioCNI resource")

	return command
}

func defaultPolicyScaffoldOptions() policyScaffoldOptions {
	return policyScaffoldOptions{
		namespace:           defaultNamespace,
		remediationAction:   defaultRemediation,
		pruneObjectBehavior: defaultPruneBehavior,
	}
}

func addPolicyScaffoldFlags(command *cobra.Command, options *policyScaffoldOptions) {
	command.Flags().StringVarP(&options.namespace, "namespace", "n", defaultNamespace, "Namespace of the hub Policy and PlacementBinding")
	command.Flags().StringVar(&options.placementName, "placement", "", "Existing Placement name that selects the target clusters (required)")
	command.Flags().StringVar(&options.policyName, "policy-name", "", "Name for the generated hub Policy")
	command.Flags().StringVar(&options.remediationAction, "remediation-action", defaultRemediation, "Policy action: enforce or inform")
	command.Flags().StringVar(&options.pruneObjectBehavior, "prune-object-behavior", defaultPruneBehavior, "Cleanup behavior: DeleteIfCreated or None")
	markRequired(command, "placement")
}

func markRequired(command *cobra.Command, names ...string) {
	for _, name := range names {
		if err := command.MarkFlagRequired(name); err != nil {
			panic(err)
		}
	}
}

func renderScaffold(options scaffoldOptions) ([]byte, error) {
	if options.policyName == "" {
		options.policyName = defaultPolicyName("istio-installation", options.namespace, options.meshName)
	}
	if err := validateScaffoldOptions(options); err != nil {
		return nil, err
	}

	dependencies := ""
	if options.dependsOnIstioCNI != "" {
		dependencies = options.dependsOnIstioCNI + "-ready"
	}
	templates := []policyTemplateOptions{{
		name:                options.policyName,
		objectTemplates:     istioObjects(options),
		remediationAction:   options.remediationAction,
		pruneObjectBehavior: options.pruneObjectBehavior,
		dependsOn:           dependencies,
	}}

	return renderPolicyBundle(options.policyScaffoldOptions, templates, options.remediationAction)
}

func renderIstioCNIScaffold(options istioCNIOptions) ([]byte, error) {
	if options.policyName == "" {
		subject := options.cniNamespace + "-" + options.cniName
		options.policyName = defaultPolicyName("istiocni-installation", options.namespace, subject)
	}
	if err := validateIstioCNIOptions(options); err != nil {
		return nil, err
	}

	templates := []policyTemplateOptions{
		{
			name:                options.policyName,
			objectTemplates:     istioCNIObjects(options),
			remediationAction:   options.remediationAction,
			pruneObjectBehavior: options.pruneObjectBehavior,
		},
		{
			name:              options.policyName + "-ready",
			objectTemplates:   istioCNIReadiness(options),
			remediationAction: defaultRemediationInform,
		},
	}

	return renderPolicyBundle(options.policyScaffoldOptions, templates, "")
}

func renderPolicyBundle(options policyScaffoldOptions, templates []policyTemplateOptions, rootRemediationAction string) ([]byte, error) {
	policyTemplates := make([]any, 0, len(templates))
	for _, template := range templates {
		configurationPolicySpec := map[string]any{
			"remediationAction": template.remediationAction,
			"severity":          "medium",
			"object-templates":  template.objectTemplates,
		}
		if template.pruneObjectBehavior != "" {
			configurationPolicySpec["pruneObjectBehavior"] = template.pruneObjectBehavior
		}
		configurationPolicy := map[string]any{
			"apiVersion": configurationPolicyAPI,
			"kind":       "ConfigurationPolicy",
			"metadata":   map[string]any{"name": template.name},
			"spec":       configurationPolicySpec,
		}
		policyTemplate := map[string]any{"objectDefinition": configurationPolicy}
		if template.dependsOn != "" {
			policyTemplate["extraDependencies"] = []any{
				map[string]any{
					"apiVersion": configurationPolicyAPI,
					"kind":       "ConfigurationPolicy",
					"name":       template.dependsOn,
					"compliance": "Compliant",
				},
			}
		}
		policyTemplates = append(policyTemplates, policyTemplate)
	}

	policySpec := map[string]any{
		"disabled":         false,
		"policy-templates": policyTemplates,
	}
	if rootRemediationAction != "" {
		policySpec["remediationAction"] = rootRemediationAction
	}
	policy := map[string]any{
		"apiVersion": configurationPolicyAPI,
		"kind":       "Policy",
		"metadata": map[string]any{
			"name":      options.policyName,
			"namespace": options.namespace,
		},
		"spec": policySpec,
	}
	binding := map[string]any{
		"apiVersion": "policy.open-cluster-management.io/v1",
		"kind":       "PlacementBinding",
		"metadata": map[string]any{
			"name":      "binding-" + options.policyName,
			"namespace": options.namespace,
		},
		"placementRef": map[string]any{
			"apiGroup": "cluster.open-cluster-management.io",
			"kind":     "Placement",
			"name":     options.placementName,
		},
		"subjects": []any{
			map[string]any{
				"apiGroup": "policy.open-cluster-management.io",
				"kind":     "Policy",
				"name":     options.policyName,
			},
		},
	}

	return encodeDocuments(policy, binding)
}

func istioObjects(options scaffoldOptions) []any {
	controlPlaneNamespace := anchoredString("controlPlaneNamespace", fmt.Sprintf(
		`{{hub (lookup %q "MultiClusterMesh" %q %q).status.controlPlaneNamespace hub}}`,
		meshAPI,
		options.namespace,
		options.meshName,
	), yamlv3.SingleQuotedStyle)
	meshName := anchoredString("meshName", options.meshName, yamlv3.DoubleQuotedStyle)
	network := anchoredString("network", fmt.Sprintf(
		`{{hub index .ManagedClusterLabels %q | default .ManagedClusterName hub}}`,
		managedClusterNetwork,
	), yamlv3.SingleQuotedStyle)
	gatewayLabels := map[string]any{managedClusterNetwork: templateAlias(network)}
	if options.istioName != defaultIstioName {
		gatewayLabels["istio.io/rev"] = options.istioName
	}

	return []any{
		map[string]any{
			"complianceType": "musthave",
			"objectDefinition": map[string]any{
				"apiVersion": "sailoperator.io/v1",
				"kind":       "Istio",
				"metadata":   map[string]any{"name": options.istioName},
				"spec": map[string]any{
					"version":        options.istioVersion,
					"namespace":      controlPlaneNamespace,
					"updateStrategy": map[string]any{"type": "InPlace"},
					"values": map[string]any{
						"meshConfig": map[string]any{"trustDomain": templateAlias(meshName)},
						"global": map[string]any{
							"meshID":       meshName,
							"multiCluster": map[string]any{"clusterName": "{{hub .ManagedClusterName hub}}"},
							"network":      network,
						},
					},
				},
			},
		},
		map[string]any{
			"complianceType": "musthave",
			"objectDefinition": map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "Gateway",
				"metadata": map[string]any{
					"name":      options.gatewayName,
					"namespace": templateAlias(controlPlaneNamespace),
					"labels":    gatewayLabels,
				},
				"spec": map[string]any{
					"gatewayClassName": "istio",
					"listeners": []any{
						map[string]any{
							"name":     "cross-network",
							"hostname": "*.local",
							"port":     15443,
							"protocol": "TLS",
							"tls":      map[string]any{"mode": "Passthrough"},
						},
					},
				},
			},
		},
	}
}

func istioCNIObjects(options istioCNIOptions) []any {
	return []any{
		map[string]any{
			"complianceType": "musthave",
			"objectDefinition": map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata":   map[string]any{"name": options.cniNamespace},
			},
		},
		map[string]any{
			"complianceType": "musthave",
			"objectDefinition": map[string]any{
				"apiVersion": "sailoperator.io/v1",
				"kind":       "IstioCNI",
				"metadata":   map[string]any{"name": options.cniName},
				"spec":       map[string]any{"namespace": options.cniNamespace},
			},
		},
	}
}

func istioCNIReadiness(options istioCNIOptions) []any {
	return []any{
		map[string]any{
			"complianceType": "musthave",
			"objectDefinition": map[string]any{
				"apiVersion": "sailoperator.io/v1",
				"kind":       "IstioCNI",
				"metadata":   map[string]any{"name": options.cniName},
				"status": map[string]any{
					"conditions": []any{
						map[string]any{"type": "Ready", "status": "True"},
					},
				},
			},
		},
	}
}

func anchoredString(name, value string, style yamlv3.Style) *yamlv3.Node {
	return &yamlv3.Node{
		Kind:   yamlv3.ScalarNode,
		Tag:    "!!str",
		Style:  style,
		Value:  value,
		Anchor: name,
	}
}

func templateAlias(anchor *yamlv3.Node) *yamlv3.Node {
	return &yamlv3.Node{Kind: yamlv3.AliasNode, Value: anchor.Anchor, Alias: anchor}
}

func validateScaffoldOptions(options scaffoldOptions) error {
	if err := validatePolicyScaffoldOptions(options.policyScaffoldOptions); err != nil {
		return err
	}
	for _, value := range []struct {
		name      string
		value     string
		validator func(string) []string
	}{
		{name: "mesh name", value: options.meshName, validator: validation.IsDNS1123Subdomain},
		{name: "Istio name", value: options.istioName, validator: validation.IsDNS1123Subdomain},
		{name: "gateway name", value: options.gatewayName, validator: validation.IsDNS1123Subdomain},
	} {
		if err := validateDNSName(value.name, value.value, value.validator); err != nil {
			return err
		}
	}
	if strings.TrimSpace(options.istioVersion) == "" {
		return errors.New("istio version is required")
	}
	if strings.ContainsAny(options.istioVersion, " \t\r\n") {
		return errors.New("istio version must not contain whitespace")
	}
	if options.dependsOnIstioCNI != "" {
		if err := validateDNSName("IstioCNI ConfigurationPolicy name", options.dependsOnIstioCNI, validation.IsDNS1123Subdomain); err != nil {
			return err
		}
		if len(options.dependsOnIstioCNI)+len("-ready") > 253 {
			return errors.New("IstioCNI policy name is too long to create its readiness ConfigurationPolicy name")
		}
	}

	return nil
}

func validateIstioCNIOptions(options istioCNIOptions) error {
	if err := validatePolicyScaffoldOptions(options.policyScaffoldOptions); err != nil {
		return err
	}
	if len(options.policyName)+len("-ready") > 253 {
		return errors.New("policy name is too long to create its readiness ConfigurationPolicy name")
	}
	if err := validateDNSName("CNI name", options.cniName, validation.IsDNS1123Subdomain); err != nil {
		return err
	}
	return validateDNSName("CNI namespace", options.cniNamespace, validation.IsDNS1123Label)
}

func validatePolicyScaffoldOptions(options policyScaffoldOptions) error {
	if err := validateDNSName("namespace", options.namespace, validation.IsDNS1123Label); err != nil {
		return err
	}
	if err := validateDNSName("placement name", options.placementName, validation.IsDNS1123Subdomain); err != nil {
		return err
	}
	if options.policyName != "" {
		if err := validateDNSName("policy name", options.policyName, validation.IsDNS1123Subdomain); err != nil {
			return err
		}
		if len("binding-")+len(options.policyName) > 253 {
			return errors.New("policy name is too long to create a PlacementBinding name")
		}
	}
	if options.remediationAction != "enforce" && options.remediationAction != "inform" {
		return fmt.Errorf("invalid remediation action %q: use enforce or inform", options.remediationAction)
	}
	if options.pruneObjectBehavior != "DeleteIfCreated" && options.pruneObjectBehavior != "None" {
		return fmt.Errorf("invalid prune object behavior %q: use DeleteIfCreated or None", options.pruneObjectBehavior)
	}

	return nil
}

func validateDNSName(name, value string, validator func(string) []string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if problems := validator(value); len(problems) > 0 {
		return fmt.Errorf("invalid %s %q: %s", name, value, strings.Join(problems, "; "))
	}
	return nil
}

func defaultPolicyName(prefix, namespace, subject string) string {
	stem := strings.NewReplacer(".", "-", "_", "-").Replace(strings.ToLower(subject))
	stem = strings.Trim(stem, "-")
	maxStemLength := 63 - len(prefix) - len("-") - len("-") - 10
	if len(stem) > maxStemLength {
		stem = strings.TrimRight(stem[:maxStemLength], "-")
	}
	hash := sha256.Sum256([]byte(namespace + "/" + subject))
	return fmt.Sprintf("%s-%s-%s", prefix, stem, hex.EncodeToString(hash[:])[:10])
}

func encodeDocuments(objects ...map[string]any) ([]byte, error) {
	var output bytes.Buffer
	encoder := yamlv3.NewEncoder(&output)
	encoder.SetIndent(2)
	for _, object := range objects {
		if err := encoder.Encode(object); err != nil {
			return nil, fmt.Errorf("failed to marshal generated resource: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish generated YAML: %w", err)
	}

	return output.Bytes(), nil
}
