//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

var (
	hubClient    client.Client
	spokeClients map[string]client.Client
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Test Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	SetDefaultEventuallyTimeout(30 * time.Second)
	SetDefaultEventuallyPollingInterval(250 * time.Millisecond)

	util.MustAddToScheme(
		meshv1alpha1.Install,
		clusterv1.Install,
		clusterv1beta2.Install,
		workv1.Install,
		operatorsv1.AddToScheme,
		operatorsv1alpha1.AddToScheme,
		msav1beta1.AddToScheme,
		addonv1beta1.Install,
	)

	hubKubeconfig := env("HUB_KUBECONFIG", ".kube/hub.config")
	cluster1Kubeconfig := env("CLUSTER1_KUBECONFIG", ".kube/cluster1.config")
	cluster2Kubeconfig := env("CLUSTER2_KUBECONFIG", ".kube/cluster2.config")

	hubClient = clientFrom(hubKubeconfig)
	spokeClients = map[string]client.Client{
		"cluster1": clientFrom(cluster1Kubeconfig),
		"cluster2": clientFrom(cluster2Kubeconfig),
	}

	Step("Verifying cluster connectivity")
	verifyConnection(ctx, hubClient, "hub")
	for name, c := range spokeClients {
		verifyConnection(ctx, c, name)
	}

	Step("Checking for existing resources that can interfere with our testing")
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	Expect(hubClient.List(ctx, meshList)).To(Succeed())
	Expect(meshList.Items).To(BeEmpty(),
		"existing MultiClusterMesh resources found; run 'make dev-clean-meshes' to clean up")
})

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func clientFrom(kubeconfig string) client.Client {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig from %s", kubeconfig)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create client from %s", kubeconfig)
	return c
}

func Step(format string, args ...any) {
	By(fmt.Sprintf(format, args...))
}

func verifyConnection(ctx context.Context, c client.Client, name string) {
	nsList := &corev1.NamespaceList{}
	Expect(c.List(ctx, nsList)).To(Succeed(),
		"failed to connect to %s cluster", name)
	GinkgoWriter.Printf("Connected to %s cluster (%d namespaces)\n", name, len(nsList.Items))
}
