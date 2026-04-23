package v1alpha1

import (
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status

// MultiClusterMesh represents a multi-cluster service mesh configuration
type MultiClusterMesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultiClusterMeshSpec   `json:"spec,omitempty"`
	Status MultiClusterMeshStatus `json:"status,omitempty"`
}

// MultiClusterMeshSpec defines the desired state of a multi-cluster mesh
type MultiClusterMeshSpec struct {
	// ClusterSet references the ACM ManagedClusterSet that defines cluster membership
	// +required
	ClusterSet string `json:"clusterSet"`

	// ControlPlane defines the target configuration for the mesh control plane
	// +optional
	ControlPlane ControlPlaneConfig `json:"controlPlane,omitempty"`

	// Operator defines the Sail Operator installation configuration
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

// OperatorConfig defines the Sail Operator installation settings
type OperatorConfig struct {
	// Namespace is the namespace where the Sail Operator will be installed
	// Defaults to "openshift-operators" on OpenShift, "sail-operator" on vanilla Kubernetes
	// +optional
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

// IssuerReference references a cert-manager Issuer
type IssuerReference struct {
	// Name of the Issuer
	// +required
	Name string `json:"name"`
}

// DiscoveryConfig defines endpoint discovery token configuration
type DiscoveryConfig struct {
	// TokenValidity defines how long discovery tokens are valid
	// Supports hours (h), days (d), weeks (w), or months (m)
	// +optional
	// +kubebuilder:default="1m"
	TokenValidity string `json:"tokenValidity,omitempty"`
}

// MultiClusterMeshStatus defines the observed state of MultiClusterMesh
type MultiClusterMeshStatus struct {
	// Conditions represent the latest available observations of the mesh state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClusterStatus tracks the status of each cluster in the mesh
	// +optional
	ClusterStatus []ClusterMeshStatus `json:"clusterStatus,omitempty"`
}

// ClusterMeshStatus tracks the mesh status for a specific cluster
type ClusterMeshStatus struct {
	// ClusterName is the name of the managed cluster
	ClusterName string `json:"clusterName"`

	// OperatorReady indicates if the Sail Operator is installed and ready
	OperatorReady bool `json:"operatorReady"`

	// TrustEstablished indicates if certificates have been distributed
	TrustEstablished bool `json:"trustEstablished"`

	// DiscoveryConfigured indicates if discovery secrets are in place
	DiscoveryConfigured bool `json:"discoveryConfigured"`

	// Conditions specific to this cluster
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
