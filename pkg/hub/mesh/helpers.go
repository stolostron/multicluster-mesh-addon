package mesh

import (
	"fmt"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

const (
	IstioCNINamespace        = "istio-cni"
	IstioCNIManifestWorkName = "multicluster-mesh-istiocni"

	ManifestWorkCPPrefix = "multicluster-mesh-cp-"
	ManifestWorkGWPrefix = "multicluster-mesh-gw-"
	ManifestWorkRSPrefix = "multicluster-mesh-rs-"

	// MSASpokeNamespace is the namespace where ManagedServiceAccount agent creates
	// service accounts on spoke clusters.
	MSASpokeNamespace = "open-cluster-management-agent-addon"

	// TokenHashAnnotation is set on remote secret ManifestWorks to detect MSA token rotation.
	TokenHashAnnotation = "mesh.open-cluster-management.io/token-hash"

	// MSASecretLabel identifies Secrets synced by the ManagedServiceAccount addon.
	MSASecretLabel = "authentication.open-cluster-management.io/is-managed-serviceaccount"
)

func getNetworkID(clusterName string) string {
	return "network-" + clusterName
}

func getMeshID(mesh *meshv1alpha1.MultiClusterMesh) string {
	return mesh.Namespace + "-" + mesh.Name
}

func getIstioCRName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return getMeshID(mesh) + "-cp"
}

func getMSAName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return fmt.Sprintf("%s-istio-reader-%s", mesh.Namespace, mesh.Name)
}

func getCPManifestWorkName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return ManifestWorkCPPrefix + getMeshID(mesh)
}

func getGWManifestWorkName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return ManifestWorkGWPrefix + getMeshID(mesh)
}

func getRSManifestWorkName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return ManifestWorkRSPrefix + getMeshID(mesh)
}

// getGatewayAddress extracts the east-west gateway LB address from ManifestWork
// feedback values. Prefers IP over hostname (handles AWS-style hostname-only LBs).
func getGatewayAddress(values []workv1.FeedbackValue) string {
	for _, v := range values {
		if v.Name == "lbIP" && v.Value.String != nil && *v.Value.String != "" {
			return *v.Value.String
		}
	}
	for _, v := range values {
		if v.Name == "lbHostname" && v.Value.String != nil && *v.Value.String != "" {
			return *v.Value.String
		}
	}
	return ""
}

// isOpenShift returns true if the cluster is an OpenShift variant based on its product claim.
func isOpenShift(cluster *clusterv1.ManagedCluster) bool {
	product := ""
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == "product.open-cluster-management.io" {
			product = claim.Value
			break
		}
	}
	switch product {
	case "OpenShift", "ROSA", "ARO", "ROKS", "OpenShiftDedicated":
		return true
	}
	return false
}

// getPrimaryCluster returns the primary cluster for PrimaryRemote topology.
// Falls back to first cluster alphabetically if not specified.
func getPrimaryCluster(mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) *clusterv1.ManagedCluster {
	target := mesh.Spec.Topology.PrimaryCluster
	for i := range clusters {
		if target != "" && clusters[i].Name == target {
			return &clusters[i]
		}
	}
	if len(clusters) > 0 {
		return &clusters[0]
	}
	return nil
}

// getRemoteClusters returns all clusters except the primary for PrimaryRemote topology.
func getRemoteClusters(mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) []*clusterv1.ManagedCluster {
	primary := getPrimaryCluster(mesh, clusters)
	if primary == nil {
		return nil
	}
	var remotes []*clusterv1.ManagedCluster
	for i := range clusters {
		if clusters[i].Name != primary.Name {
			remotes = append(remotes, &clusters[i])
		}
	}
	return remotes
}
