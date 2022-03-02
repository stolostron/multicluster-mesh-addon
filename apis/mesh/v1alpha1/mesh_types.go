package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	maistrav2 "maistra.io/api/core/v2"
)

// MeshSpec defines the desired state of physical service mesh in a managed cluster
type MeshSpec struct {
	MeshProvider   MeshProvider      `json:"meshProvider,omitempty"`
	Cluster        string            `json:"cluster,omitempty"`
	ControlPlane   *MeshControlPlane `json:"controlPlane,omitempty"`
	MeshMemberRoll []string          `json:"meshMemberRoll,omitempty"`
	TrustDomain    string            `json:"trustDomain,omitempty"`
}

type MeshProvider string

const (
	MeshProviderOpenshift      MeshProvider = "Openshift Service Mesh"
	MeshProviderCommunityIstio MeshProvider = "Community Istio"
	// more providers come later
)

// MeshControlPlane defines the mesh control plane
type MeshControlPlane struct {
	Namespace  string   `json:"namespace,omitempty"`
	Version    string   `json:"version,omitempty"`
	Profiles   []string `json:"profiles,omitempty"`
	Revision   string   `json:"revision,omitempty"`
	Components []string `json:"components,omitempty"`
	Peers      []Peer   `json:"peers,omitempty"`
}

// Peer defines mesh peer
type Peer struct {
	Name    string `json:"name,omitempty"`
	Cluster string `json:"cluster,omitempty"`
}

// MeshStatus defines the observed state of Mesh
type MeshStatus struct {
	Readiness maistrav2.ReadinessStatus `json:"readiness"`
}

// +genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="CLUSTER",type="string",JSONPath=".spec.cluster",description="Cluster of the mesh"
//+kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".spec.controlPlane.version",description="Version of the mesh"
//+kubebuilder:printcolumn:name="PEERS",type="string",JSONPath=".spec.controlPlane.peers[*].name"
//+kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Mesh is the Schema for the meshes API
type Mesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshSpec   `json:"spec,omitempty"`
	Status MeshStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MeshList contains a list of Mesh
type MeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Mesh `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Mesh{}, &MeshList{})
}
