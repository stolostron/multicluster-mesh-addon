package mesh

import (
	"context"
	"encoding/json"
	"fmt"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

// ensureControlPlane creates or updates the per-mesh control plane ManifestWork for a cluster.
// The ManifestWork contains the Istio CR, control plane namespace, and RBAC resources for
// cross-cluster discovery. For PrimaryRemote remotes, the Istio CR includes remotePilotAddress
// read from the primary cluster's gateway ManifestWork feedback.
func (r *Reconciler) ensureControlPlane(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster, template map[string]any) error {
	work, err := r.buildControlPlaneManifestWork(ctx, mesh, clusters, cluster, template)
	if err != nil {
		return fmt.Errorf("failed to build control plane ManifestWork for cluster %s: %w", cluster.Name, err)
	}

	applied, err := r.workApplier.Apply(ctx, work)
	if err != nil {
		return fmt.Errorf("failed to apply control plane ManifestWork on cluster %s: %w", cluster.Name, err)
	}

	klog.V(4).Infof("Applied control plane ManifestWork %s/%s", applied.Namespace, applied.Name)
	return nil
}

// buildControlPlaneManifestWork constructs the ManifestWork containing the Istio CR (with
// FeedbackRules and CEL ConditionRules for readiness gating), the control plane namespace
// with network topology label, and ClusterRole/ClusterRoleBinding for MSA-based discovery.
func (r *Reconciler) buildControlPlaneManifestWork(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster, template map[string]any) (*workv1.ManifestWork, error) {
	cpNamespace := mesh.GetControlPlaneNamespace()
	meshID := getMeshID(mesh)
	clusterName := cluster.Name
	istioCRName := getIstioCRName(mesh)

	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: cpNamespace,
			Labels: map[string]string{
				"topology.istio.io/network": getNetworkID(clusterName),
			},
		},
	}

	istioCR, err := r.buildIstioCR(ctx, mesh, clusters, cluster, template)
	if err != nil {
		return nil, err
	}

	istioCRJSON, err := json.Marshal(istioCR)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Istio CR: %w", err)
	}

	clusterRoleName := fmt.Sprintf("multicluster-mesh-istio-reader-%s", meshID)

	clusterRole := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"endpoints", "namespaces", "nodes", "pods", "services"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"discovery.k8s.io"},
				Resources: []string{"endpointslices"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"extensions.istio.io", "networking.istio.io", "security.istio.io", "telemetry.istio.io"},
				Resources: []string{"*"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      getMSAName(mesh),
			Namespace: MSASpokeNamespace,
		}},
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getCPManifestWorkName(mesh),
			Namespace: clusterName,
			Labels:    meshOwnedLabels(mesh, clusterName),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{RawExtension: runtime.RawExtension{Object: ns}},
					{RawExtension: runtime.RawExtension{Raw: istioCRJSON}},
					{RawExtension: runtime.RawExtension{Object: clusterRole}},
					{RawExtension: runtime.RawExtension{Object: clusterRoleBinding}},
				},
			},
			ManifestConfigs: []workv1.ManifestConfigOption{{
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:    "sailoperator.io",
					Resource: "istios",
					Name:     istioCRName,
				},
				FeedbackRules: []workv1.FeedbackRule{{
					Type: workv1.JSONPathsType,
					JsonPaths: []workv1.JsonPath{
						{Name: "readyStatus", Path: `.status.conditions[?(@.type=="Ready")].status`},
					},
				}},
				ConditionRules: []workv1.ConditionRule{{
					Condition: "ControlPlaneReady",
					Type:      workv1.CelConditionExpressionsType,
					CelExpressions: []string{
						`has(object.status) && has(object.status.conditions) && object.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')`,
					},
					MessageExpression: `result ? "Istio control plane is ready" : "Waiting for Istio control plane"`,
				}},
			}},
		},
	}, nil
}

// buildIstioCR applies controller-managed fields (meshID, network, trustDomain, etc.)
// to a deep copy of the pre-resolved template. Each cluster gets its own copy since
// applyControllerManagedFields sets cluster-specific values (clusterName, network, etc.).
func (r *Reconciler) buildIstioCR(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster, template map[string]any) (*unstructured.Unstructured, error) {
	return r.applyControllerManagedFields(ctx, copyMap(template), mesh, clusters, cluster)
}

// getGatewayAddressForCluster reads the gateway ManifestWork from the hub and
// extracts the LB address from its feedback values.
func (r *Reconciler) getGatewayAddressForCluster(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) (string, error) {
	work := &workv1.ManifestWork{}
	if err := r.Get(ctx, key.Of(getGWManifestWorkName(mesh), cluster.Name), work); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get gateway ManifestWork for cluster %s: %w", cluster.Name, err)
	}

	for _, manifest := range work.Status.ResourceStatus.Manifests {
		if manifest.ResourceMeta.Kind == "Service" {
			return getGatewayAddress(manifest.StatusFeedbacks.Values), nil
		}
	}

	return "", nil
}

// isControlPlaneReady checks the CP ManifestWork's manifest conditions for ControlPlaneReady=True.
func (r *Reconciler) isControlPlaneReady(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) bool {
	work := &workv1.ManifestWork{}
	if err := r.Get(ctx, key.Of(getCPManifestWorkName(mesh), cluster.Name), work); err != nil {
		klog.V(4).Infof("Failed to get CP ManifestWork for cluster %s: %v", cluster.Name, err)
		return false
	}

	for _, manifest := range work.Status.ResourceStatus.Manifests {
		if meta.IsStatusConditionTrue(manifest.Conditions, "ControlPlaneReady") {
			return true
		}
	}

	return false
}

// isOperatorInstalled checks whether the operator is installed on the given cluster
// by looking for the installedCSV feedback on the operator ManifestWork.
func (r *Reconciler) isOperatorInstalled(ctx context.Context, cluster *clusterv1.ManagedCluster) bool {
	work := &workv1.ManifestWork{}
	if err := r.Get(ctx, key.Of(OperatorManifestWorkName, cluster.Name), work); err != nil {
		klog.V(4).Infof("Failed to get operator ManifestWork for cluster %s: %v", cluster.Name, err)
		return false
	}

	return getManifestWorkFeedback(work) != nil
}
