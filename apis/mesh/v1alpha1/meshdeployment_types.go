package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MeshDeploymentSpec defines the desired state of MeshDeployment
type MeshDeploymentSpec struct {
	MeshProvider   MeshProvider      `json:"meshProvider,omitempty"`
	Clusters       []string          `json:"clusters,omitempty"`
	ControlPlane   *MeshControlPlane `json:"controlPlane,omitempty"`
	MeshConfig     *MeshConfig       `json:"meshConfig,omitempty"`
	MeshMemberRoll []string          `json:"meshMemberRoll,omitempty"`
}

// MeshDeploymentStatus defines the observed state of MeshDeployment
type MeshDeploymentStatus struct {
}

// +genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="CLUSTERS",type="string",JSONPath=".spec.clusters",description="Target clusters of the mesh deployment"
//+kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".spec.controlPlane.version",description="Version of the mesh"
//+kubebuilder:printcolumn:name="PROVIDER",type="string",JSONPath=".spec.meshProvider",description="Provider of the mesh"
//+kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// MeshDeployment is the Schema for the meshdeployments API
type MeshDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshDeploymentSpec   `json:"spec,omitempty"`
	Status MeshDeploymentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MeshDeploymentList contains a list of MeshDeployment
type MeshDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MeshDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MeshDeployment{}, &MeshDeploymentList{})
}
