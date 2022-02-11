package constants

const (
	DefaultMeshAddonImage          = "quay.io/morvencao/multicluster-mesh-addon:latest"
	MeshAddonName                  = "multicluster-mesh"
	MeshAgentInstallationNamespace = "open-cluster-management-agent-addon"
	LabelKeyForDiscoveriedMesh     = "mesh.open-cluster-management.io/discovery"
)

const (
	IstioCAConfigmapName  = "istio-ca-root-cert"
	IstioCAConfigmapKey   = "root-cert.pem"
	IstioCAConfigmapLabel = "istio.io/config"
)

const (
	FederationServiceLabelKey = "federation.maistra.io/ingress-for"
)

const (
	FederationConfigMapMeshPeerEndpointLabelKey    = "mesh-peer-endpoint"
	FederationConfigMapMeshPeerCALabelKey          = "root-cert.pem"
	FederationConfigMapMeshPeerTrustDomainLabelKey = "mesh-peer-trustdomain"
	FederationConfigMapMeshPeerNamespaceLabelKey   = "mesh-peer-namespace"
	FederationConfigMapMeshNamespaceLabelKey       = "mesh-namespace"
	FederationResourcesLabelKey                    = "mesh.open-cluster.io/federation"
)
