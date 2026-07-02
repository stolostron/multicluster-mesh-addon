package v1alpha1

import (
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="metadata.name must not exceed 63 characters"

// MultiClusterMesh represents a multi-cluster service mesh configuration
type MultiClusterMesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultiClusterMeshSpec   `json:"spec,omitempty"`
	Status MultiClusterMeshStatus `json:"status,omitempty"`
}

// GetTrustDomain returns the trust domain for this mesh, derived from the mesh name.
func (m *MultiClusterMesh) GetTrustDomain() string {
	return m.Name
}

// GetControlPlaneNamespace returns the control plane namespace, defaulting to "istio-system".
func (m *MultiClusterMesh) GetControlPlaneNamespace() string {
	if m.Spec.ControlPlane.Namespace == "" {
		return "istio-system"
	}
	return m.Spec.ControlPlane.Namespace
}

// SetReadyCondition sets the mesh-level Ready condition.
func (m *MultiClusterMesh) SetReadyCondition(status metav1.ConditionStatus, reason string, messageFmt string, args ...any) {
	meta.SetStatusCondition(&m.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Reason:             reason,
		ObservedGeneration: m.Generation,
		Message:            fmt.Sprintf(messageFmt, args...),
	})
}

// SetClusterCondition sets a per-cluster condition, creating the cluster status entry if needed.
func (m *MultiClusterMesh) SetClusterCondition(clusterName string, conditionType string, status metav1.ConditionStatus, reason string, messageFmt string, args ...any) {
	cs := m.getOrCreateClusterStatus(clusterName)
	meta.SetStatusCondition(&cs.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		ObservedGeneration: m.Generation,
		Message:            fmt.Sprintf(messageFmt, args...),
	})
}

func (m *MultiClusterMesh) getOrCreateClusterStatus(clusterName string) *ClusterMeshStatus {
	// Index-based iteration to return a pointer into the slice, not a copy.
	for i := range m.Status.ClusterStatus {
		if m.Status.ClusterStatus[i].ClusterName == clusterName {
			return &m.Status.ClusterStatus[i]
		}
	}
	m.Status.ClusterStatus = append(m.Status.ClusterStatus, ClusterMeshStatus{ClusterName: clusterName})
	return &m.Status.ClusterStatus[len(m.Status.ClusterStatus)-1]
}

// MultiClusterMeshSpec defines the desired state of a multi-cluster mesh
type MultiClusterMeshSpec struct {
	// ClusterSet references the ACM ManagedClusterSet that defines cluster membership
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.clusterSet is immutable"
	ClusterSet string `json:"clusterSet"`

	// ControlPlane defines the target configuration for the mesh control plane
	// +optional
	ControlPlane ControlPlaneConfig `json:"controlPlane,omitempty"`

	// Operator defines the Service Mesh Operator installation configuration
	// +optional
	Operator OperatorConfig `json:"operator,omitempty"`

	// Security defines the trust and discovery configuration
	// +optional
	Security SecurityConfig `json:"security,omitempty"`
}

// ControlPlaneConfig defines where the mesh control plane will be installed
type ControlPlaneConfig struct {
	// Namespace is the namespace where Istio will be installed on each cluster
	// +optional
	// +kubebuilder:default="istio-system"
	Namespace string `json:"namespace,omitempty"`
}

// OperatorConfig defines the Service Mesh Operator installation settings
type OperatorConfig struct {
	// Namespace is the namespace where the Service Mesh Operator will be installed.
	// This namespace may be deleted when the mesh is removed, so avoid
	// using a namespace that contains other resources.
	// +optional
	// +kubebuilder:default="multicluster-mesh-operator"
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('openshift-')",message="namespace must not use the reserved 'openshift-' prefix"
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('kube-')",message="namespace must not use the reserved 'kube-' prefix"
	// +kubebuilder:validation:XValidation:rule="self != 'default'",message="namespace must not be 'default'"
	Namespace string `json:"namespace,omitempty"`

	// Channel is the OLM subscription channel (e.g., "stable", "1.23")
	// +optional
	// +kubebuilder:default="stable"
	Channel string `json:"channel,omitempty"`

	// Source is the CatalogSource name
	// Defaults to "redhat-operators" on OpenShift, "operatorhubio-catalog" on vanilla Kubernetes
	// +optional
	Source string `json:"source,omitempty"`

	// SourceNamespace is the namespace of the CatalogSource
	// Defaults to "openshift-marketplace" on OpenShift, "olm" on vanilla Kubernetes
	// +optional
	SourceNamespace string `json:"sourceNamespace,omitempty"`

	// StartingCSV is the specific operator version to install
	// Useful for testing or pinning to a specific version
	// +optional
	StartingCSV string `json:"startingCSV,omitempty"`

	// InstallPlanApproval is the approval strategy (Automatic or Manual)
	// +optional
	// +kubebuilder:default="Automatic"
	// +kubebuilder:validation:Enum=Automatic;Manual
	InstallPlanApproval operatorsv1alpha1.Approval `json:"installPlanApproval,omitempty"`
}

// SecurityConfig defines trust and discovery configuration
type SecurityConfig struct {
	// Trust defines the mTLS trust configuration
	// +optional
	Trust TrustConfig `json:"trust,omitempty"`

	// Discovery defines the endpoint discovery configuration
	// +optional
	Discovery DiscoveryConfig `json:"discovery,omitempty"`
}

// TrustConfig defines the cert-manager integration for mTLS
type TrustConfig struct {
	// CertManager defines the cert-manager issuer reference
	// +optional
	CertManager CertManagerConfig `json:"certManager,omitempty"`
}

// CertManagerConfig references a cert-manager issuer
type CertManagerConfig struct {
	// IssuerRef references the cert-manager Issuer to use as Root CA
	// +required
	IssuerRef IssuerReference `json:"issuerRef"`
}

// IssuerReference references a cert-manager Issuer or ClusterIssuer
type IssuerReference struct {
	// Name of the Issuer or ClusterIssuer
	// +required
	Name string `json:"name"`

	// Kind of the issuer (Issuer or ClusterIssuer)
	// +optional
	// +kubebuilder:default="Issuer"
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	Kind string `json:"kind,omitempty"`
}

// DiscoveryConfig defines endpoint discovery token configuration
type DiscoveryConfig struct {
	// TokenValidity defines how long discovery tokens are valid
	// Supports hours (h), minutes (m), seconds (s). If unset, defaults to 360h
	// +optional
	// +kubebuilder:default="360h"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('10m')", message="TokenValidity must be at least 10 minutes"
	TokenValidity *metav1.Duration `json:"tokenValidity,omitempty"`
}

const (
	// ConditionReady indicates whether the mesh is fully operational
	ConditionReady = "Ready"

	// ConditionOperatorInstalled indicates whether the operator is installed on a cluster
	ConditionOperatorInstalled = "OperatorInstalled"

	// ReasonAllClustersReady indicates all clusters have confirmed operator installation
	ReasonAllClustersReady = "AllClustersReady"

	// ReasonClustersNotReady indicates that not all clusters have confirmed operator installation
	ReasonClustersNotReady = "ClustersNotReady"

	// ReasonInstallationPending indicates the operator installation has been requested
	ReasonInstallationPending = "InstallationPending"

	// ReasonOperatorInstalled indicates the operator CSV has been successfully installed
	ReasonOperatorInstalled = "Installed"

	// ReasonMissingProductClaim indicates the cluster is missing its product claim
	ReasonMissingProductClaim = "MissingProductClaim"

	// ReasonReconcileError indicates an error occurred during reconciliation
	ReasonReconcileError = "ReconcileError"

	// ReasonOperatorConfigConflict indicates a conflict with an older mesh's operator config
	ReasonOperatorConfigConflict = "OperatorConfigConflict"

	// ReasonNamespaceConflict indicates a conflict with an older mesh's control plane namespace
	ReasonNamespaceConflict = "NamespaceConflict"
)

// MultiClusterMeshStatus defines the observed state of MultiClusterMesh
type MultiClusterMeshStatus struct {
	// Conditions represent the latest available observations of the mesh state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClusterStatus tracks the status of each cluster in the mesh
	// +listType=map
	// +listMapKey=clusterName
	// +optional
	ClusterStatus []ClusterMeshStatus `json:"clusterStatus,omitempty"`
}

// ClusterMeshStatus tracks the mesh status for a specific cluster
type ClusterMeshStatus struct {
	// ClusterName is the name of the managed cluster
	// +required
	ClusterName string `json:"clusterName"`

	// Conditions represent the latest available observations of this cluster's state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MultiClusterMeshList contains a list of MultiClusterMesh
type MultiClusterMeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultiClusterMesh `json:"items"`
}
