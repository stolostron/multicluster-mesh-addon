package translate

import (
	"fmt"
	"strings"

	"github.com/gogo/protobuf/types"
	istioapimeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapinetworkv1alpha3 "istio.io/api/networking/v1alpha3"
	iopspecv1alpha1 "istio.io/api/operator/v1alpha1"
	istionetworkv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	iopname "istio.io/istio/operator/pkg/name"
	ioptranslate "istio.io/istio/operator/pkg/translate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
)

var istioProfileComponentsMap map[string][]string

func init() {
	istioProfileComponentsMap = map[string][]string{
		"empty":     []string{},
		"minimal":   []string{"base", "istiod"},
		"default":   []string{"base", "istiod", "istio-ingress"},
		"demo":      []string{"base", "istiod", "istio-egress", "istio-ingress"},
		"openshift": []string{"base", "istiod", "istio-ingress", "cni"},
		"external":  []string{"istiod-remote"},
	}
}

// TranslateIstioToLogicMesh translate the physical istio service mesh to the logical mesh
func TranslateIstioToLogicMesh(iop *iopv1alpha1.IstioOperator, memberNamespaces []string, cluster string) (*meshv1alpha1.Mesh, error) {
	trustDomain := "cluster.local"
	accessLogFile, accessLogFormat, accessLogEncoding := "", "", ""
	if iop.Spec.MeshConfig != nil {
		trustDomainVal, ok := iop.Spec.MeshConfig["trustDomain"]
		if ok && trustDomainVal != nil && trustDomainVal.(string) != "" {
			trustDomain = trustDomainVal.(string)
		}
		accessLogFileVal, ok := iop.Spec.MeshConfig["accessLogFile"]
		if ok && accessLogFileVal != nil && accessLogFileVal.(string) != "" {
			accessLogFile = accessLogFileVal.(string)
		}
		accessLogFormatVal, ok := iop.Spec.MeshConfig["accessLogFormat"]
		if ok && accessLogFormatVal != nil && accessLogFormatVal.(string) != "" {
			accessLogFormat = accessLogFormatVal.(string)
		}
		accessLogEncodingVal, ok := iop.Spec.MeshConfig["accessLogEncoding"]
		if ok && accessLogEncodingVal != nil && accessLogEncodingVal.(string) != "" {
			accessLogEncoding = accessLogEncodingVal.(string)
		}
	}

	meshConfig := &meshv1alpha1.MeshConfig{
		TrustDomain: trustDomain,
	}
	if accessLogFile != "" || accessLogFormat != "" || accessLogEncoding != "" {
		meshConfig.ProxyConfig = &meshv1alpha1.ProxyConfig{
			AccessLogging: &meshv1alpha1.AccessLogging{},
		}
		if accessLogFile != "" {
			meshConfig.ProxyConfig.AccessLogging.File = accessLogFile
		}
		if accessLogFormat != "" {
			meshConfig.ProxyConfig.AccessLogging.Format = accessLogFormat
		}
		if accessLogEncoding != "" {
			meshConfig.ProxyConfig.AccessLogging.Encoding = accessLogEncoding
		}
	}

	profile, controlPlaneNamespace, enabledComponents, err := getProfileNSAndEnabledComponents(iop)
	if err != nil {
		return nil, err
	}

	tag, ok := iop.Spec.Tag.(string)
	if !ok {
		return nil, fmt.Errorf("invalid tag for IstioOperatorSpec: %v", iop.Spec.Tag)
	}

	meshName := cluster + "-" + iop.GetNamespace() + "-" + iop.GetName()
	mesh := &meshv1alpha1.Mesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meshName,
			Namespace: cluster,
			Labels:    map[string]string{constants.LabelKeyForDiscoveriedMesh: "true"},
		},
		Spec: meshv1alpha1.MeshSpec{
			MeshProvider: meshv1alpha1.MeshProviderUpstreamIstio,
			Cluster:      cluster,
			ControlPlane: &meshv1alpha1.MeshControlPlane{
				Namespace:  controlPlaneNamespace,
				Profiles:   []string{profile},
				Version:    tag,
				Revision:   iop.Spec.Revision,
				Components: enabledComponents,
			},
			MeshConfig:     meshConfig,
			MeshMemberRoll: memberNamespaces,
		},
		// Status: meshv1alpha1.MeshStatus{
		// 	Readiness: iop.Status.Readiness,
		// },
	}

	return mesh, nil
}

func getProfileNSAndEnabledComponents(iop *iopv1alpha1.IstioOperator) (string, string, []string, error) {
	var enabledComponents []string
	if iop.Spec.Components != nil {
		for _, c := range iopname.AllCoreComponentNames {
			enabled, err := ioptranslate.IsComponentEnabledInSpec(c, iop.Spec)
			if err != nil {
				return "", "", nil, fmt.Errorf("failed to check if component: %s is enabled or not: %v", string(c), err)
			}
			if enabled {
				enabledComponents = append(enabledComponents, iopname.UserFacingComponentName(c))
			}
		}
		for _, c := range iop.Spec.Components.IngressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, iopname.UserFacingComponentName(iopname.IngressComponentName))
				break
			}
		}
		for _, c := range iop.Spec.Components.EgressGateways {
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, iopname.UserFacingComponentName(iopname.EgressComponentName))
				break
			}
		}
	}

	if configuredNamespace := iopv1alpha1.Namespace(iop.Spec); configuredNamespace != "" {
		return iop.Spec.Profile, configuredNamespace, enabledComponents, nil
	}
	return iop.Spec.Profile, iopname.IstioDefaultNamespace, enabledComponents, nil
}

// TranslateToPhysicalIstio translate the logical mesh to the physical mesh
func TranslateToPhysicalIstio(mesh *meshv1alpha1.Mesh) (*iopv1alpha1.IstioOperator, *iopv1alpha1.IstioOperator, *istionetworkv1alpha3.Gateway, error) {
	if mesh.Spec.Cluster == "" {
		return nil, nil, nil, fmt.Errorf("cluster field in mesh object is empty")
	}
	if mesh.Spec.ControlPlane == nil {
		return nil, nil, nil, fmt.Errorf("controlPlane field in mesh object is empty")
	}
	if mesh.Spec.ControlPlane.Namespace == "" {
		return nil, nil, nil, fmt.Errorf("controlPlane namespace field in mesh object is empty")
	}
	iopName := mesh.GetName()
	isDiscoveriedMesh, ok := mesh.GetLabels()[constants.LabelKeyForDiscoveriedMesh]
	if ok && isDiscoveriedMesh == "true" {
		iopName = strings.Replace(iopName, mesh.Spec.Cluster+"-"+mesh.Spec.ControlPlane.Namespace+"-", "", 1)
	}
	namespace := mesh.Spec.ControlPlane.Namespace
	profile := "default"
	if len(mesh.Spec.ControlPlane.Profiles) > 0 {
		profile = mesh.Spec.ControlPlane.Profiles[0]
	}

	disabledPbVal := &iopspecv1alpha1.BoolValueForPB{BoolValue: types.BoolValue{Value: false}}
	enabledPbVal := &iopspecv1alpha1.BoolValueForPB{BoolValue: types.BoolValue{Value: true}}
	ingressGatewaysEnabled, gatewaysEnabled := false, false

	// default iop for control plane
	controlPlaneIOP := &iopv1alpha1.IstioOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IstioOperator",
			APIVersion: "install.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      iopName,
			Namespace: namespace,
		},
		Spec: &iopspecv1alpha1.IstioOperatorSpec{
			Tag:       mesh.Spec.ControlPlane.Version,
			Revision:  mesh.Spec.ControlPlane.Revision,
			Profile:   profile,
			Namespace: namespace,
			Components: &iopspecv1alpha1.IstioComponentSetSpec{
				IngressGateways: []*iopspecv1alpha1.GatewaySpec{
					{Name: "istio-ingressgateway", Enabled: disabledPbVal},
				},
				EgressGateways: []*iopspecv1alpha1.GatewaySpec{
					{Name: "istio-egressgateway", Enabled: disabledPbVal},
				},
			},
			Values: map[string]interface{}{
				"global": map[string]interface{}{
					"istioNamespace": namespace,
				},
			},
		},
	}

	// default iop for gateways
	gatewaysIOP := &iopv1alpha1.IstioOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IstioOperator",
			APIVersion: "install.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      iopName + "-gateways",
			Namespace: namespace,
		},
		Spec: &iopspecv1alpha1.IstioOperatorSpec{
			Tag:        mesh.Spec.ControlPlane.Version,
			Revision:   mesh.Spec.ControlPlane.Revision,
			Profile:    "empty",
			Namespace:  namespace,
			Components: &iopspecv1alpha1.IstioComponentSetSpec{},
			Values: map[string]interface{}{
				"global": map[string]interface{}{
					"istioNamespace": namespace,
				},
				"gateways": map[string]interface{}{
					"istio-ingressgateway": map[string]interface{}{
						"injectionTemplate": "gateway",
					},
				},
			},
		},
	}

	if mesh.Spec.MeshConfig != nil {
		controlPlaneIOP.Spec.MeshConfig = map[string]interface{}{}
		if mesh.Spec.MeshConfig.TrustDomain != "" {
			controlPlaneIOP.Spec.MeshConfig["trustDomain"] = mesh.Spec.MeshConfig.TrustDomain
		}
		if mesh.Spec.MeshConfig.ProxyConfig != nil && mesh.Spec.MeshConfig.ProxyConfig.AccessLogging != nil {
			if mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.File != "" {
				controlPlaneIOP.Spec.MeshConfig["accessLogFile"] = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.File
			}
			if mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Format != "" {
				controlPlaneIOP.Spec.MeshConfig["accessLogFormat"] = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Format
			}
			if mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Encoding != "" {
				controlPlaneIOP.Spec.MeshConfig["accessLogEncoding"] = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Encoding
			}
		}
	}

	// addonEnabled := false
	if mesh.Spec.ControlPlane.Components != nil {
		for _, c := range mesh.Spec.ControlPlane.Components {
			// if utils.SliceContainsString(istioProfileComponentsMap[profile], c) {
			// 	continue
			// }
			switch c {
			case "base":
				controlPlaneIOP.Spec.Components.Base = &iopspecv1alpha1.BaseComponentSpec{Enabled: enabledPbVal}
			case "istiod":
				controlPlaneIOP.Spec.Components.Pilot = &iopspecv1alpha1.ComponentSpec{Enabled: enabledPbVal}
			case "istiod-remote":
				controlPlaneIOP.Spec.Components.IstiodRemote = &iopspecv1alpha1.ComponentSpec{Enabled: enabledPbVal}
			case "cni":
				controlPlaneIOP.Spec.Components.Cni = &iopspecv1alpha1.ComponentSpec{Enabled: enabledPbVal}
			case "istio-ingress":
				gatewaysIOP.Spec.Components.IngressGateways = []*iopspecv1alpha1.GatewaySpec{
					{
						Name:    "istio-ingressgateway",
						Enabled: enabledPbVal,
						K8S: &iopspecv1alpha1.KubernetesResourcesSpec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
				}
				ingressGatewaysEnabled = true
				gatewaysEnabled = true
			case "istio-egress":
				gatewaysIOP.Spec.Components.EgressGateways = []*iopspecv1alpha1.GatewaySpec{
					{
						Name:    "istio-egressgateway",
						Enabled: enabledPbVal,
						K8S: &iopspecv1alpha1.KubernetesResourcesSpec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
				}
				gatewaysEnabled = true
			}
		}
	}

	var eastwestgw *istionetworkv1alpha3.Gateway
	if len(mesh.Spec.ControlPlane.Peers) > 0 {
		defaultConfig := &istioapimeshv1alpha1.ProxyConfig{
			ProxyMetadata: map[string]string{
				"ISTIO_META_DNS_CAPTURE":       "true",
				"ISTIO_META_DNS_AUTO_ALLOCATE": "true",
			},
		}
		outboundTrafficPolicy := &istioapimeshv1alpha1.MeshConfig_OutboundTrafficPolicy{
			Mode: istioapimeshv1alpha1.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY,
		}
		if len(controlPlaneIOP.Spec.MeshConfig) > 0 {
			controlPlaneIOP.Spec.MeshConfig["defaultConfig"] = defaultConfig
			controlPlaneIOP.Spec.MeshConfig["outboundTrafficPolicy"] = outboundTrafficPolicy
		} else {
			controlPlaneIOP.Spec.MeshConfig = map[string]interface{}{
				"defaultConfig":         defaultConfig,
				"outboundTrafficPolicy": outboundTrafficPolicy,
			}
		}
		controlPlaneIOP.Spec.Components.Pilot = &iopspecv1alpha1.ComponentSpec{
			Enabled: enabledPbVal,
			K8S: &iopspecv1alpha1.KubernetesResourcesSpec{
				Env: []*iopspecv1alpha1.EnvVar{
					{
						Name:  "PILOT_SKIP_VALIDATE_TRUST_DOMAIN",
						Value: "true",
					},
				},
			},
		}

		globalValues := controlPlaneIOP.Spec.Values["global"].(map[string]interface{})
		if len(globalValues) > 0 {
			globalValues["network"] = iopName
			globalValues["multiCluster"] = map[string]interface{}{"clusterName": iopName}
		} else {
			globalValues = map[string]interface{}{
				"network":      iopName,
				"multiCluster": map[string]interface{}{"clusterName": iopName},
			}
		}
		controlPlaneIOP.Spec.Values["global"] = globalValues

		eastwestgateway := &iopspecv1alpha1.GatewaySpec{
			Name:    "istio-eastwestgateway",
			Enabled: enabledPbVal,
			Label: map[string]string{
				"istio": "eastwestgateway",
				"app":   "istio-eastwestgateway",
			},
			K8S: &iopspecv1alpha1.KubernetesResourcesSpec{
				ImagePullPolicy: "IfNotPresent",
				Service: &iopspecv1alpha1.ServiceSpec{
					Type: "LoadBalancer",
					Ports: []*iopspecv1alpha1.ServicePort{
						{
							Name:       "status-port",
							Port:       15021,
							TargetPort: &iopspecv1alpha1.IntOrStringForPB{IntOrString: intstr.FromInt(15021)},
						},
						{
							Name:       "http2",
							Port:       80,
							TargetPort: &iopspecv1alpha1.IntOrStringForPB{IntOrString: intstr.FromInt(8080)},
						},
						{
							Name:       "https",
							Port:       443,
							TargetPort: &iopspecv1alpha1.IntOrStringForPB{IntOrString: intstr.FromInt(8443)},
						},
						{
							Name:       "tls",
							Port:       15443,
							TargetPort: &iopspecv1alpha1.IntOrStringForPB{IntOrString: intstr.FromInt(15443)},
						},
					},
				},
			},
		}

		if !ingressGatewaysEnabled {
			gatewaysIOP.Spec.Components.IngressGateways = []*iopspecv1alpha1.GatewaySpec{eastwestgateway}
		} else {
			gatewaysIOP.Spec.Components.IngressGateways = append(gatewaysIOP.Spec.Components.IngressGateways, eastwestgateway)
		}
		gatewaysEnabled = true
		eastwestgw = &istionetworkv1alpha3.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cross-network-gateway",
				Namespace: namespace,
			},
			Spec: istioapinetworkv1alpha3.Gateway{
				Selector: map[string]string{
					"istio": "eastwestgateway",
					"app":   "istio-eastwestgateway",
				},
				Servers: []*istioapinetworkv1alpha3.Server{
					{
						Hosts: []string{"*.global"},
						Port:  &istioapinetworkv1alpha3.Port{Name: "tls", Number: 15443, Protocol: "TLS"},
						Tls:   &istioapinetworkv1alpha3.ServerTLSSettings{Mode: istioapinetworkv1alpha3.ServerTLSSettings_AUTO_PASSTHROUGH},
					},
				},
			},
		}
	}

	if !gatewaysEnabled {
		return controlPlaneIOP, nil, nil, nil
	}

	return controlPlaneIOP, gatewaysIOP, eastwestgw, nil
}
