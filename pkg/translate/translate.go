package translate

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	maistrav1 "maistra.io/api/core/v1"
	maistrav2 "maistra.io/api/core/v2"

	meshv1alpha1 "github.com/morvencao/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/morvencao/multicluster-mesh-addon/pkg/constants"
	utils "github.com/morvencao/multicluster-mesh-addon/pkg/utils"
)

var profileComponentsMap map[string][]string

func init() {
	profileComponentsMap = make(map[string][]string)
	profileComponentsMap["default"] = []string{"grafana", "istio-discovery", "istio-egress", "istio-ingress", "kiali", "mesh-config", "prometheus", "telemetry-common", "tracing"}
}

func TranslateToLogicMesh(smcp *maistrav2.ServiceMeshControlPlane, smmr *maistrav1.ServiceMeshMemberRoll, cluster string) (*meshv1alpha1.Mesh, error) {
	meshMember := []string{}
	if smmr != nil {
		meshMember = smmr.Spec.Members
	}

	trustDomain := "cluster.local"
	if smcp.Spec.Security != nil && smcp.Spec.Security.Trust != nil && smcp.Spec.Security.Trust.Domain != "" {
		trustDomain = smcp.Spec.Security.Trust.Domain
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
			MeshMemberRoll: meshMember,
			TrustDomain:    trustDomain,
		},
		Status: meshv1alpha1.MeshStatus{
			Readiness: smcp.Status.Readiness,
		},
	}

	return mesh, nil
}

func TranslateToPhysicalMesh(mesh *meshv1alpha1.Mesh) (*maistrav2.ServiceMeshControlPlane, *maistrav1.ServiceMeshMemberRoll, error) {
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
		for _, c := range profiles {
			for _, p := range profiles {
				if utils.SliceContainsString(profileComponentsMap[p], c) {
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

	// set trust domain
	if mesh.Spec.TrustDomain != "" {
		smcp.Spec.Security = &maistrav2.SecurityConfig{
			Trust: &maistrav2.TrustConfig{
				Domain: mesh.Spec.TrustDomain,
			},
		}
	}

	if !gatewayEnabled && len(mesh.Spec.ControlPlane.FederationGateways) > 0 {
		smcp.Spec.Gateways = &maistrav2.GatewaysConfig{
			Enablement: maistrav2.Enablement{
				Enabled: &[]bool{true}[0],
			},
			IngressGateways: map[string]*maistrav2.IngressGatewayConfig{},
			EgressGateways:  map[string]*maistrav2.EgressGatewayConfig{},
		}
	}

	for _, federatedGw := range mesh.Spec.ControlPlane.FederationGateways {
		meshPeer := federatedGw.MeshPeer
		if meshPeer != "" {
			smcp.Spec.Gateways.EgressGateways[meshPeer+"-egress"] = &maistrav2.EgressGatewayConfig{
				RequestedNetworkView: []string{"network-" + meshPeer},
				GatewayConfig: maistrav2.GatewayConfig{
					Enablement: maistrav2.Enablement{
						Enabled: &[]bool{true}[0],
					},
					RouterMode: maistrav2.RouterModeTypeSNIDNAT,
					Service: maistrav2.GatewayServiceConfig{
						Metadata: &maistrav2.MetadataConfig{
							Labels: map[string]string{
								"federation.maistra.io/egress-for": meshPeer,
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

			smcp.Spec.Gateways.IngressGateways[meshPeer+"-ingress"] = &maistrav2.IngressGatewayConfig{
				GatewayConfig: maistrav2.GatewayConfig{
					Enablement: maistrav2.Enablement{
						Enabled: &[]bool{true}[0],
					},
					RouterMode: maistrav2.RouterModeTypeSNIDNAT,
					Service: maistrav2.GatewayServiceConfig{
						Metadata: &maistrav2.MetadataConfig{
							Labels: map[string]string{
								"federation.maistra.io/ingress-for": meshPeer,
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
