package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MeshFederationSpec defines the desired state of MeshFederation of a central view
type MeshFederationSpec struct {
	MeshPeers   []MeshPeer   `json:"meshPeers,omitempty"`
	TrustConfig *TrustConfig `json:"trustConfig,omitempty"`
}

// MeshPeer defines mesh peers
type MeshPeer struct {
	Peers []Peer `json:"peers,omitempty"`
	// additional setting for peers...
}

type TrustType string

const (
	TrustTypeLimited  TrustType = "Limited"  // limited trust gated at gateways, used by OSSM
	TrustTypeComplete TrustType = "Complete" // complete trust by shared CA, used by upstream istio
)

// TrustConfig defines the trust configuratin for mesh peers
type TrustConfig struct {
	TrustType TrustType `json:"trustType,omitempty"`
}

// MeshFederationStatus defines the observed state of MeshFederation
type MeshFederationStatus struct {
}

// +genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MeshFederation is the Schema for the meshfederations API
type MeshFederation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshFederationSpec   `json:"spec,omitempty"`
	Status MeshFederationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MeshFederationList contains a list of MeshFederation
type MeshFederationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MeshFederation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MeshFederation{}, &MeshFederationList{})
}
