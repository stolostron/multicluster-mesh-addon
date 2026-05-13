package util

import (
	"context"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

// CreateMultiClusterMesh creates a MultiClusterMesh resource with optional operator configuration.
func CreateMultiClusterMesh(ctx context.Context, k8sClient client.Client, name, namespace, clusterSet string, operatorConfig meshv1alpha1.OperatorConfig) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			ClusterSet: clusterSet,
			Operator:   operatorConfig,
		},
	}
	Expect(k8sClient.Create(ctx, mesh)).To(Succeed())
}
