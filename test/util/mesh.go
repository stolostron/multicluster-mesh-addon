package util

import (
	"context"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

// CreateMultiClusterMesh creates a MultiClusterMesh resource.
// An optional MeshSpec can be passed to override fields beyond clusterSet.
func CreateMultiClusterMesh(ctx context.Context, k8sClient client.Client, name, namespace, clusterSet string, spec ...meshv1alpha1.MultiClusterMeshSpec) *meshv1alpha1.MultiClusterMesh {
	var meshSpec meshv1alpha1.MultiClusterMeshSpec
	if len(spec) > 0 {
		meshSpec = spec[0]
	}
	meshSpec.ClusterSet = clusterSet

	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: meshSpec,
	}
	Expect(k8sClient.Create(ctx, mesh)).To(Succeed())
	return mesh
}

// CertManagerSpec returns a MeshSpec with cert-manager issuer configuration.
func CertManagerSpec(issuerName string) meshv1alpha1.MultiClusterMeshSpec {
	return meshv1alpha1.MultiClusterMeshSpec{
		Security: meshv1alpha1.SecurityConfig{
			Trust: meshv1alpha1.TrustConfig{
				CertManager: meshv1alpha1.CertManagerConfig{
					IssuerRef: meshv1alpha1.IssuerReference{Name: issuerName},
				},
			},
		},
	}
}
