package mesh

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

func (r *Reconciler) ensureGateway(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster) error {
	work := r.buildGatewayManifestWork(mesh, clusters, cluster)

	applied, err := r.workApplier.Apply(ctx, work)
	if err != nil {
		return fmt.Errorf("failed to apply gateway ManifestWork on cluster %s: %w", cluster.Name, err)
	}

	klog.V(4).Infof("Applied gateway ManifestWork %s/%s", applied.Namespace, applied.Name)
	return nil
}

// buildGatewayManifestWork constructs the ManifestWork for the east-west gateway on a cluster.
// Contains the gateway ServiceAccount, Deployment (SSA strategy to coexist with Istio's webhook),
// LoadBalancer Service (with LB address feedback), cross-network Gateway, and for PrimaryRemote
// primaries the istiod Gateway + VirtualService that expose istiod to remote clusters.
func (r *Reconciler) buildGatewayManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster) *workv1.ManifestWork {
	cpNamespace := mesh.GetControlPlaneNamespace()
	clusterName := cluster.Name
	revisionLabel := getIstioCRName(mesh)
	networkID := getNetworkID(clusterName)

	var replicas int32 = 1

	gwLabels := map[string]string{
		"app":   "istio-eastwestgateway",
		"istio": "eastwestgateway",
	}

	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-eastwestgateway",
			Namespace: cpNamespace,
		},
	}

	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    gwLabels,
			Name:      "istio-eastwestgateway",
			Namespace: cpNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: gwLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"inject.istio.io/templates": "gateway",
					},
					Labels: map[string]string{
						"app":                       "istio-eastwestgateway",
						"istio":                     "eastwestgateway",
						"istio.io/rev":              revisionLabel,
						"topology.istio.io/network": networkID,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Env: []corev1.EnvVar{{
							Name:  "ISTIO_META_REQUESTED_NETWORK_VIEW",
							Value: networkID,
						}},
						Image: "auto",
						Name:  "istio-proxy",
					}},
					ServiceAccountName: "istio-eastwestgateway",
				},
			},
		},
	}

	svcType := corev1.ServiceTypeLoadBalancer
	if mesh.Spec.Gateway.ServiceType == meshv1alpha1.GatewayServiceTypeNodePort {
		svcType = corev1.ServiceTypeNodePort
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    gwLabels,
			Name:      "istio-eastwestgateway",
			Namespace: cpNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "status-port", Port: 15021, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(15021)},
				{Name: "tls", Port: 15443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(15443)},
				{Name: "tls-istiod", Port: 15012, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(15012)},
				{Name: "tls-webhook", Port: 15017, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(15017)},
			},
			Selector: gwLabels,
			Type:     svcType,
		},
	}

	crossNetworkGW := buildCrossNetworkGateway(cpNamespace)

	manifests := []workv1.Manifest{
		{RawExtension: runtime.RawExtension{Object: sa}},
		{RawExtension: runtime.RawExtension{Object: deploy}},
		{RawExtension: runtime.RawExtension{Object: svc}},
		mustMarshalUnstructured(crossNetworkGW),
	}

	if mesh.Spec.Topology.Type == meshv1alpha1.TopologyPrimaryRemote {
		if primary := getPrimaryCluster(mesh, clusters); primary != nil && primary.Name == clusterName {
			manifests = append(manifests,
				mustMarshalUnstructured(buildIstiodGateway(cpNamespace)),
				mustMarshalUnstructured(buildIstiodVirtualService(cpNamespace)),
			)
		}
	}

	manifestConfigs := []workv1.ManifestConfigOption{
		{
			ResourceIdentifier: workv1.ResourceIdentifier{
				Group:     "apps",
				Name:      "istio-eastwestgateway",
				Namespace: cpNamespace,
				Resource:  "deployments",
			},
			UpdateStrategy: &workv1.UpdateStrategy{
				Type: workv1.UpdateStrategyTypeServerSideApply,
				ServerSideApply: &workv1.ServerSideApplyConfig{
					FieldManager: "work-agent-mesh-addon",
					Force:        true,
				},
			},
		},
		{
			ResourceIdentifier: workv1.ResourceIdentifier{
				Group:     "",
				Name:      "istio-eastwestgateway",
				Namespace: cpNamespace,
				Resource:  "services",
			},
			FeedbackRules: []workv1.FeedbackRule{{
				Type: workv1.JSONPathsType,
				JsonPaths: []workv1.JsonPath{
					{Name: "lbIP", Path: ".status.loadBalancer.ingress[0].ip"},
					{Name: "lbHostname", Path: ".status.loadBalancer.ingress[0].hostname"},
				},
			}},
		},
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    meshOwnedLabels(mesh, clusterName),
			Name:      getGWManifestWorkName(mesh),
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			ManifestConfigs: manifestConfigs,
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

func buildCrossNetworkGateway(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				"name":      "cross-network-gateway",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"istio": "eastwestgateway",
				},
				"servers": []any{
					map[string]any{
						"hosts": []any{"*.local"},
						"port": map[string]any{
							"name":     "tls",
							"number":   int64(15443),
							"protocol": "TLS",
						},
						"tls": map[string]any{
							"mode": "AUTO_PASSTHROUGH",
						},
					},
				},
			},
		},
	}
}

func buildIstiodGateway(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]any{
				"name":      "istiod-gateway",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"istio": "eastwestgateway",
				},
				"servers": []any{
					map[string]any{
						"hosts": []any{"*"},
						"port": map[string]any{
							"name":     "tls-istiod",
							"number":   int64(15012),
							"protocol": "TLS",
						},
						"tls": map[string]any{
							"mode": "PASSTHROUGH",
						},
					},
					map[string]any{
						"hosts": []any{"*"},
						"port": map[string]any{
							"name":     "tls-istiodwebhook",
							"number":   int64(15017),
							"protocol": "TLS",
						},
						"tls": map[string]any{
							"mode": "PASSTHROUGH",
						},
					},
				},
			},
		},
	}
}

func buildIstiodVirtualService(namespace string) *unstructured.Unstructured {
	istiodHost := fmt.Sprintf("istiod.%s.svc.cluster.local", namespace)
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1",
			"kind":       "VirtualService",
			"metadata": map[string]any{
				"name":      "istiod-vs",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"gateways": []any{"istiod-gateway"},
				"hosts":    []any{"*"},
				"tls": []any{
					map[string]any{
						"match": []any{
							map[string]any{
								"port":     int64(15012),
								"sniHosts": []any{"*"},
							},
						},
						"route": []any{
							map[string]any{
								"destination": map[string]any{
									"host": istiodHost,
									"port": map[string]any{
										"number": int64(15012),
									},
								},
							},
						},
					},
					map[string]any{
						"match": []any{
							map[string]any{
								"port":     int64(15017),
								"sniHosts": []any{"*"},
							},
						},
						"route": []any{
							map[string]any{
								"destination": map[string]any{
									"host": istiodHost,
									"port": map[string]any{
										"number": int64(443),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func mustMarshalUnstructured(obj *unstructured.Unstructured) workv1.Manifest {
	raw, err := json.Marshal(obj)
	if err != nil {
		klog.Fatalf("Failed to marshal unstructured object %s/%s: %v", obj.GetKind(), obj.GetName(), err)
	}
	return workv1.Manifest{
		RawExtension: runtime.RawExtension{Raw: raw},
	}
}

func (r *Reconciler) isGatewayReady(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) bool {
	if mesh.Spec.Gateway.ServiceType == meshv1alpha1.GatewayServiceTypeNodePort {
		return r.isGatewayDeploymentReady(ctx, mesh, cluster)
	}
	addr, err := r.getGatewayAddressForCluster(ctx, mesh, cluster)
	if err != nil {
		klog.V(4).Infof("Failed to get gateway address for cluster %s: %v", cluster.Name, err)
		return false
	}
	return addr != ""
}

// isGatewayDeploymentReady checks the gateway ManifestWork for deployment availability
// via the Applied condition. Used for NodePort where there's no LB address to wait for.
func (r *Reconciler) isGatewayDeploymentReady(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) bool {
	work := &workv1.ManifestWork{}
	if err := r.Get(ctx, key.Of(getGWManifestWorkName(mesh), cluster.Name), work); err != nil {
		return false
	}
	for _, cond := range work.Status.Conditions {
		if cond.Type == "Applied" && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Reconciler) areAllGatewaysReady(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) bool {
	for i := range clusters {
		if !r.isGatewayReady(ctx, mesh, &clusters[i]) {
			return false
		}
	}
	return true
}
