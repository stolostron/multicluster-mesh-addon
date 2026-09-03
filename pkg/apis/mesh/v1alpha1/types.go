package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="metadata.name must not exceed 63 characters"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Placement",type="string",JSONPath=`.spec.placementRef.name`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Message",type="string",priority=1,JSONPath=`.status.conditions[?(@.type=="Ready")].message`

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

// HasTrustConfigured returns true if trust distribution is configured.
func (m *MultiClusterMesh) HasTrustConfigured() bool {
	return m.Spec.Security.Trust.CertManager.IssuerRef.Name != ""
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

// RemoveClusterCondition removes a condition from a cluster's status, if present.
func (m *MultiClusterMesh) RemoveClusterCondition(clusterName string, conditionType string) {
	for i := range m.Status.ClusterStatus {
		if m.Status.ClusterStatus[i].ClusterName == clusterName {
			meta.RemoveStatusCondition(&m.Status.ClusterStatus[i].Conditions, conditionType)
			return
		}
	}
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
	// PlacementRef references a Placement resource in the same namespace
	// that determines which clusters are members of this mesh.
	// The controller reads PlacementDecisions to resolve cluster membership.
	// +required
	PlacementRef PlacementReference `json:"placementRef"`

	// ControlPlane defines the target configuration for the mesh control plane
	// +optional
	ControlPlane ControlPlaneConfig `json:"controlPlane,omitempty"`

	// Security defines the trust and discovery configuration
	// +optional
	Security SecurityConfig `json:"security,omitempty"`
}

// PlacementReference references a Placement in the same namespace as the mesh.
type PlacementReference struct {
	// Name of the Placement resource
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ControlPlaneConfig defines where the mesh control plane will be installed
type ControlPlaneConfig struct {
	// Namespace is the namespace where Istio will be installed on each cluster.
	// This namespace is created and deleted by the addon as part of the mesh lifecycle.
	// +optional
	// +kubebuilder:default="istio-system"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.controlPlane.namespace is immutable"
	Namespace string `json:"namespace,omitempty"`
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
	// ConditionReady indicates whether mesh resources have been reconciled
	ConditionReady = "Ready"

	// ConditionTrustDistributed indicates whether trust has been distributed to a cluster
	ConditionTrustDistributed = "TrustDistributed"

	// ConditionDiscoveryConfigured indicates whether endpoint discovery is configured on a cluster
	ConditionDiscoveryConfigured = "DiscoveryConfigured"

	// ReasonAllClustersReady indicates mesh resources have been reconciled for all clusters
	ReasonAllClustersReady = "AllClustersReady"

	// ReasonClustersNotReady indicates that one or more clusters are not ready
	ReasonClustersNotReady = "ClustersNotReady"

	// ReasonDistributionPending indicates trust distribution is in progress
	ReasonDistributionPending = "DistributionPending"

	// ReasonConfigured indicates endpoint discovery is fully configured
	ReasonConfigured = "Configured"

	// ReasonConfigurationPending indicates endpoint discovery configuration is in progress
	ReasonConfigurationPending = "ConfigurationPending"

	// ReasonNoAPIEndpoint indicates the cluster has no API endpoint configured
	ReasonNoAPIEndpoint = "NoAPIEndpoint"

	// ReasonDistributed indicates trust has been distributed to the cluster
	ReasonDistributed = "Distributed"

	// ReasonReconcileError indicates an error occurred during reconciliation
	ReasonReconcileError = "ReconcileError"

	// ReasonNamespaceConflict indicates a conflict with an older mesh's control plane namespace
	ReasonNamespaceConflict = "NamespaceConflict"

	// ReasonPlacementNotFound indicates the referenced Placement does not exist
	ReasonPlacementNotFound = "PlacementNotFound"

	// ReasonNoClustersSelected indicates the Placement selected zero clusters
	ReasonNoClustersSelected = "NoClustersSelected"
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
