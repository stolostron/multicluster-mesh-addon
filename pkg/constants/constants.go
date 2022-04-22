package constants

// mesh addon constants
const (
	DefaultMeshAddonImage               = "quay.io/morvencao/multicluster-mesh-addon:latest"
	MeshAddonName                       = "multicluster-mesh"
	MeshAgentNamespace                  = "open-cluster-management-agent-addon"
	LabelKeyForDiscoveriedMesh          = "mesh.open-cluster-management.io/discovery"
	AnnotationKeyForMeshFederationOwner = "mesh.open-cluster-management.io/federation-owner"
)

// mesh federation configuration constants
const (
	FederationConfigMapMeshPeerCALabelKey          = "root-cert.pem"
	FederationConfigMapMeshPeerEndpointLabelKey    = "mesh-peer-endpoint"
	FederationConfigMapMeshPeerTrustDomainLabelKey = "mesh-peer-trustdomain"
	FederationConfigMapMeshPeerNamespaceLabelKey   = "mesh-peer-namespace"
	FederationConfigMapMeshNamespaceLabelKey       = "mesh-namespace"
	FederationResourcesLabelKey                    = "mesh.open-cluster.io/federation"
)
