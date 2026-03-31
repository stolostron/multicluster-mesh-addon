package util

import (
	"context"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateManagedClusterSet creates a ManagedClusterSet.
func CreateManagedClusterSet(ctx context.Context, k8sClient client.Client, name string) {
	Expect(k8sClient.Create(ctx, &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			},
		},
	})).To(Succeed())
}

// CreateManagedCluster creates a ManagedCluster without any product claim.
// Also creates the cluster namespace (required for ManifestWorks).
func CreateManagedCluster(ctx context.Context, k8sClient client.Client, name, clusterSet string) {
	CreateNamespace(ctx, k8sClient, name)
	Expect(k8sClient.Create(ctx, &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"cluster.open-cluster-management.io/clusterset": clusterSet,
			},
		},
	})).To(Succeed())
}

// SetProductClaim sets the claim on an existing ManagedCluster to the given claim.
func SetProductClaim(ctx context.Context, k8sClient client.Client, clusterName, productClaim string) {
	cluster := &clusterv1.ManagedCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName}, cluster)).To(Succeed())
	cluster.Status.ClusterClaims = []clusterv1.ManagedClusterClaim{
		{Name: "product.open-cluster-management.io", Value: productClaim},
	}
	Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
}

// CreateK8sManagedCluster creates a vanilla Kubernetes ManagedCluster with product claim "Other".
func CreateK8sManagedCluster(ctx context.Context, k8sClient client.Client, name, clusterSet string) {
	CreateManagedCluster(ctx, k8sClient, name, clusterSet)
	SetProductClaim(ctx, k8sClient, name, "Other")
}

// CreateOCPManagedCluster creates an OpenShift ManagedCluster with the specified product claim.
func CreateOCPManagedCluster(ctx context.Context, k8sClient client.Client, name, clusterSet, product string) {
	CreateManagedCluster(ctx, k8sClient, name, clusterSet)
	SetProductClaim(ctx, k8sClient, name, product)
}
