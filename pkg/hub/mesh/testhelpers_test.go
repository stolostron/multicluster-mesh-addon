package mesh

import (
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = clusterv1.Install(s)
	_ = clusterv1beta2.Install(s)
	_ = meshv1alpha1.Install(s)
	_ = workv1.Install(s)
	return s
}

func newTestMesh(namespace, name, clusterSet string, topology meshv1alpha1.TopologyType, primaryCluster string) *meshv1alpha1.MultiClusterMesh {
	return &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: meshv1alpha1.MultiClusterMeshSpec{
			ClusterSet: clusterSet,
			Topology: meshv1alpha1.TopologyConfig{
				Type:           topology,
				PrimaryCluster: primaryCluster,
			},
		},
	}
}

func newTestCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func newTestReconciler(scheme *runtime.Scheme, objs ...client.Object) *Reconciler {
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...).WithStatusSubresource(objs...)
	}
	return &Reconciler{Client: builder.Build(), Scheme: scheme, templateCache: newTemplateCache()}
}

func newTestClusterWithURL(name, clusterSet, apiServerURL string) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{ClusterSetLabel: clusterSet},
		},
	}
	if apiServerURL != "" {
		c.Spec.ManagedClusterClientConfigs = []clusterv1.ClientConfig{
			{URL: apiServerURL},
		}
	}
	return c
}
