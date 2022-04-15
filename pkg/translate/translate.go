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
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	maistrav1 "maistra.io/api/core/v1"
	maistrav2 "maistra.io/api/core/v2"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	utils "github.com/stolostron/multicluster-mesh-addon/pkg/utils"
)

var ossmProfileComponentsMap, istioProfileComponentsMap map[string][]string

func init() {
	ossmProfileComponentsMap = map[string][]string{
		"default": []string{"grafana", "istio-discovery", "istio-egress", "istio-ingress", "kiali", "mesh-config", "prometheus", "telemetry-common", "tracing"},
	}
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
			MeshProvider: meshv1alpha1.MeshProviderCommunityIstio,
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

// TranslateOSSMToLogicMesh translate the physical openshift service mesh to the logical mesh
func TranslateOSSMToLogicMesh(smcp *maistrav2.ServiceMeshControlPlane, smmr *maistrav1.ServiceMeshMemberRoll, cluster string) (*meshv1alpha1.Mesh, error) {
	meshMember := []string{}
	if smmr != nil {
		meshMember = smmr.Spec.Members
	}

	trustDomain := "cluster.local"
	accessLogFile, accessLogFormat, accessLogEncoding := "", "", ""
	if smcp.Spec.Security != nil && smcp.Spec.Security.Trust != nil && smcp.Spec.Security.Trust.Domain != "" {
		trustDomain = smcp.Spec.Security.Trust.Domain
	}

	if smcp.Spec.Proxy != nil && smcp.Spec.Proxy.AccessLogging != nil && smcp.Spec.Proxy.AccessLogging.File != nil {
		accessLogFile = smcp.Spec.Proxy.AccessLogging.File.Name
		accessLogFormat = smcp.Spec.Proxy.AccessLogging.File.Format
		accessLogEncoding = smcp.Spec.Proxy.AccessLogging.File.Encoding
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

	allComponents := make([]string, 0, 4)
	for _, v := range smcp.Status.Readiness.Components {
		if len(v) == 1 && v[0] == "" {
			continue
		}
		allComponents = append(allComponents, v...)
	}

	meshName := cluster + "-" + smcp.GetNamespace() + "-" + smcp.GetName()
	mesh := &meshv1alpha1.Mesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meshName,
			Namespace: cluster,
			Labels:    map[string]string{constants.LabelKeyForDiscoveriedMesh: "true"},
		},
		Spec: meshv1alpha1.MeshSpec{
			MeshProvider: meshv1alpha1.MeshProviderOpenshift,
			Cluster:      cluster,
			ControlPlane: &meshv1alpha1.MeshControlPlane{
				Namespace:  smcp.GetNamespace(),
				Profiles:   smcp.Spec.Profiles,
				Version:    smcp.Spec.Version,
				Components: allComponents,
			},
			MeshConfig:     meshConfig,
			MeshMemberRoll: meshMember,
		},
		Status: meshv1alpha1.MeshStatus{
			Readiness: smcp.Status.Readiness,
		},
	}

	return mesh, nil
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
					{Name: "istio-ingressgateway", Enabled: enabledPbVal},
				}
				ingressGatewaysEnabled = true
				gatewaysEnabled = true
			case "istio-egress":
				gatewaysIOP.Spec.Components.EgressGateways = []*iopspecv1alpha1.GatewaySpec{
					{Name: "istio-egressgateway", Enabled: enabledPbVal},
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

// TranslateToPhysicalOSSM translate the logical mesh to the physical mesh
func TranslateToPhysicalOSSM(mesh *meshv1alpha1.Mesh) (*maistrav2.ServiceMeshControlPlane, *maistrav1.ServiceMeshMemberRoll, error) {
	if mesh.Spec.Cluster == "" {
		return nil, nil, fmt.Errorf("cluster field in mesh object is empty")
	}
	if mesh.Spec.ControlPlane == nil {
		return nil, nil, fmt.Errorf("controlPlane field in mesh object is empty")
	}
	if mesh.Spec.ControlPlane.Namespace == "" {
		return nil, nil, fmt.Errorf("controlPlane namespace field in mesh object is empty")
	}
	smcpName := mesh.GetName()
	isDiscoveriedMesh, ok := mesh.GetLabels()[constants.LabelKeyForDiscoveriedMesh]
	if ok && isDiscoveriedMesh == "true" {
		smcpName = strings.Replace(smcpName, mesh.Spec.Cluster+"-"+mesh.Spec.ControlPlane.Namespace+"-", "", 1)
	}
	namespace := mesh.Spec.ControlPlane.Namespace
	version := "v2.1" // mesh federation is support by OSSM >= v2.1
	if mesh.Spec.ControlPlane.Version != "" {
		version = mesh.Spec.ControlPlane.Version
	}
	profiles := []string{"default"}
	if mesh.Spec.ControlPlane.Profiles != nil {
		profiles = mesh.Spec.ControlPlane.Profiles
	}

	addonEnabled := false
	gatewayEnabled := false
	var tracingConfig *maistrav2.TracingConfig
	var prometheusAddonConfig *maistrav2.PrometheusAddonConfig
	var stackdriverAddonConfig *maistrav2.StackdriverAddonConfig
	var jaegerAddonConfig *maistrav2.JaegerAddonConfig
	var grafanaAddonConfig *maistrav2.GrafanaAddonConfig
	var kialiAddonConfig *maistrav2.KialiAddonConfig
	var threeScaleAddonConfig *maistrav2.ThreeScaleAddonConfig
	var ingressGatewayConfig *maistrav2.ClusterIngressGatewayConfig
	var egressGatewayConfig *maistrav2.EgressGatewayConfig
	// var additionalIngressGatewayConfig map[string]*maistrav2.IngressGatewayConfig
	// var additionalEgressGatewayConfig map[string]*maistrav2.EgressGatewayConfig

	if mesh.Spec.ControlPlane.Components != nil {
		for _, c := range mesh.Spec.ControlPlane.Components {
			for _, p := range profiles {
				if utils.SliceContainsString(ossmProfileComponentsMap[p], c) {
					continue
				}
				switch c {
				case "tracing":
					tracingConfig = &maistrav2.TracingConfig{
						Type: maistrav2.TracerTypeJaeger,
					}
				case "prometheus":
					prometheusAddonConfig = &maistrav2.PrometheusAddonConfig{
						Enablement: maistrav2.Enablement{
							Enabled: &[]bool{true}[0],
						},
					}
					addonEnabled = true
				case "grafana":
					grafanaAddonConfig = &maistrav2.GrafanaAddonConfig{
						Enablement: maistrav2.Enablement{
							Enabled: &[]bool{true}[0],
						},
					}
					addonEnabled = true
				case "kiali":
					kialiAddonConfig = &maistrav2.KialiAddonConfig{
						Enablement: maistrav2.Enablement{
							Enabled: &[]bool{true}[0],
						},
					}
					addonEnabled = true
				case "3scale":
					threeScaleAddonConfig = &maistrav2.ThreeScaleAddonConfig{
						Enablement: maistrav2.Enablement{
							Enabled: &[]bool{true}[0],
						},
					}
					addonEnabled = true
				case "istio-ingress":
					ingressGatewayConfig = &maistrav2.ClusterIngressGatewayConfig{
						IngressGatewayConfig: maistrav2.IngressGatewayConfig{
							GatewayConfig: maistrav2.GatewayConfig{
								Enablement: maistrav2.Enablement{
									Enabled: &[]bool{true}[0],
								},
							},
						},
					}
					gatewayEnabled = true
				case "istio-egress":
					egressGatewayConfig = &maistrav2.EgressGatewayConfig{
						GatewayConfig: maistrav2.GatewayConfig{
							Enablement: maistrav2.Enablement{
								Enabled: &[]bool{true}[0],
							},
						},
					}
					gatewayEnabled = true
				}
			}
		}
	}

	// default smcp
	smcp := &maistrav2.ServiceMeshControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      smcpName,
			Namespace: namespace,
		},
		Spec: maistrav2.ControlPlaneSpec{
			Version:  version,
			Profiles: profiles,
		},
	}

	// set addons
	if addonEnabled {
		addonsConfig := &maistrav2.AddonsConfig{}
		if prometheusAddonConfig != nil {
			addonsConfig.Prometheus = prometheusAddonConfig
		}
		if stackdriverAddonConfig != nil {
			addonsConfig.Stackdriver = stackdriverAddonConfig
		}
		if jaegerAddonConfig != nil {
			addonsConfig.Jaeger = jaegerAddonConfig
		}
		if grafanaAddonConfig != nil {
			addonsConfig.Grafana = grafanaAddonConfig
		}
		if kialiAddonConfig != nil {
			addonsConfig.Kiali = kialiAddonConfig
		}
		if threeScaleAddonConfig != nil {
			addonsConfig.ThreeScale = threeScaleAddonConfig
		}
		smcp.Spec.Addons = addonsConfig
	}

	// set gateways
	if gatewayEnabled {
		gatewaysConfig := &maistrav2.GatewaysConfig{
			Enablement: maistrav2.Enablement{
				Enabled: &[]bool{true}[0],
			},
		}
		if ingressGatewayConfig != nil {
			gatewaysConfig.ClusterIngress = ingressGatewayConfig
		}
		if egressGatewayConfig != nil {
			gatewaysConfig.ClusterEgress = egressGatewayConfig
		}
		smcp.Spec.Gateways = gatewaysConfig
	}

	if tracingConfig != nil {
		smcp.Spec.Tracing = tracingConfig
	}

	if mesh.Spec.MeshConfig != nil {
		if mesh.Spec.MeshConfig.TrustDomain != "" {
			smcp.Spec.Security = &maistrav2.SecurityConfig{
				Trust: &maistrav2.TrustConfig{
					Domain: mesh.Spec.MeshConfig.TrustDomain,
				},
			}
		}
		if mesh.Spec.MeshConfig.ProxyConfig != nil && mesh.Spec.MeshConfig.ProxyConfig.AccessLogging != nil {
			smcp.Spec.Proxy = &maistrav2.ProxyConfig{
				AccessLogging: &maistrav2.ProxyAccessLoggingConfig{
					File: &maistrav2.ProxyFileAccessLogConfig{},
				},
			}
			if mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.File != "" {
				smcp.Spec.Proxy.AccessLogging.File.Name = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.File
				smcp.Spec.Proxy.AccessLogging.File.Format = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Format
				smcp.Spec.Proxy.AccessLogging.File.Encoding = mesh.Spec.MeshConfig.ProxyConfig.AccessLogging.Encoding
			}
		}
	}

	if !gatewayEnabled && len(mesh.Spec.ControlPlane.Peers) > 0 {
		smcp.Spec.Gateways = &maistrav2.GatewaysConfig{
			Enablement: maistrav2.Enablement{
				Enabled: &[]bool{true}[0],
			},
			IngressGateways: map[string]*maistrav2.IngressGatewayConfig{},
			EgressGateways:  map[string]*maistrav2.EgressGatewayConfig{},
		}
	}

	for _, peer := range mesh.Spec.ControlPlane.Peers {
		peerName := peer.Name
		if peerName != "" {
			smcp.Spec.Gateways.EgressGateways[peerName+"-egress"] = &maistrav2.EgressGatewayConfig{
				RequestedNetworkView: []string{"network-" + peerName},
				GatewayConfig: maistrav2.GatewayConfig{
					Enablement: maistrav2.Enablement{
						Enabled: &[]bool{true}[0],
					},
					RouterMode: maistrav2.RouterModeTypeSNIDNAT,
					Service: maistrav2.GatewayServiceConfig{
						Metadata: &maistrav2.MetadataConfig{
							Labels: map[string]string{
								constants.FederationEgressServiceLabelKey: peerName,
							},
						},
						ServiceSpec: corev1.ServiceSpec{
							Type: corev1.ServiceTypeClusterIP,
							Ports: []corev1.ServicePort{
								corev1.ServicePort{
									Name:       "tls",
									Port:       15443,
									TargetPort: intstr.FromInt(15443),
								},
								corev1.ServicePort{
									Name:       "http-discovery",
									Port:       8188,
									TargetPort: intstr.FromInt(8188),
								},
							},
						},
					},
					Runtime: &maistrav2.ComponentRuntimeConfig{
						Deployment: &maistrav2.DeploymentRuntimeConfig{
							AutoScaling: &maistrav2.AutoScalerConfig{
								Enablement: maistrav2.Enablement{
									Enabled: &[]bool{false}[0],
								},
							},
						},
						Container: &maistrav2.ContainerConfig{
							CommonContainerConfig: maistrav2.CommonContainerConfig{
								Resources: &corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("10m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
						},
					},
				},
			}

			smcp.Spec.Gateways.IngressGateways[peerName+"-ingress"] = &maistrav2.IngressGatewayConfig{
				GatewayConfig: maistrav2.GatewayConfig{
					Enablement: maistrav2.Enablement{
						Enabled: &[]bool{true}[0],
					},
					RouterMode: maistrav2.RouterModeTypeSNIDNAT,
					Service: maistrav2.GatewayServiceConfig{
						Metadata: &maistrav2.MetadataConfig{
							Labels: map[string]string{
								constants.FederationIngressServiceLabelKey: peerName,
							},
							Annotations: map[string]string{
								"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
							},
						},
						ServiceSpec: corev1.ServiceSpec{
							Type: corev1.ServiceTypeLoadBalancer,
							Ports: []corev1.ServicePort{
								corev1.ServicePort{
									Name:       "tls",
									Port:       15443,
									TargetPort: intstr.FromInt(15443),
								},
								corev1.ServicePort{
									Name:       "https-discovery",
									Port:       8188,
									TargetPort: intstr.FromInt(8188),
								},
							},
						},
					},
					Runtime: &maistrav2.ComponentRuntimeConfig{
						Deployment: &maistrav2.DeploymentRuntimeConfig{
							AutoScaling: &maistrav2.AutoScalerConfig{
								Enablement: maistrav2.Enablement{
									Enabled: &[]bool{false}[0],
								},
							},
						},
						Container: &maistrav2.ContainerConfig{
							CommonContainerConfig: maistrav2.CommonContainerConfig{
								Resources: &corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("10m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
						},
					},
				},
			}
		}
	}

	var smmr *maistrav1.ServiceMeshMemberRoll
	if len(mesh.Spec.MeshMemberRoll) > 0 {
		smmr = &maistrav1.ServiceMeshMemberRoll{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: namespace,
			},
			Spec: maistrav1.ServiceMeshMemberRollSpec{
				Members: mesh.Spec.MeshMemberRoll,
			},
		}
	}

	return smcp, smmr, nil
}
