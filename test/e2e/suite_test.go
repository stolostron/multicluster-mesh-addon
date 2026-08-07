//go:build e2e || e2e_multicluster

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
	clusters = []string{"cluster1", "cluster2"}

	hubClient    *util.E2EClient
	spokeClients map[string]*util.E2EClient

	testOperatorName      string
	testOperatorNamespace string
	testCatalogSource     string
	testCatalogNamespace  string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Test Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	SetDefaultEventuallyTimeout(2 * time.Minute)
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

	hubClient = util.NewE2EClient(clientFrom(hubKubeconfig), hubKubeconfig)
	spokeClients = make(map[string]*util.E2EClient)
	for name, kc := range map[string]string{
		"cluster1": cluster1Kubeconfig,
		"cluster2": cluster2Kubeconfig,
	} {
		spokeClients[name] = util.NewE2EClient(clientFrom(kc), kc)
	}

	Step("Detecting platform (kind vs OCP)")
	detectPlatform(ctx, hubClient, "cluster1")

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

func Success(format string, args ...any) {
	GinkgoWriter.Println("* " + fmt.Sprintf(format, args...))
}

func detectPlatform(ctx context.Context, hubClient client.Client, clusterName string) {
	cfg, err := util.DetectPlatform(ctx, hubClient, clusterName)
	Expect(err).NotTo(HaveOccurred(), "failed to detect platform")
	GinkgoWriter.Printf("Detected platform: catalog=%s/%s, operator=%s/%s\n",
		cfg.CatalogNamespace, cfg.CatalogSource, cfg.OperatorNamespace, cfg.OperatorName)
	testOperatorName = cfg.OperatorName
	testOperatorNamespace = cfg.OperatorNamespace
	testCatalogSource = cfg.CatalogSource
	testCatalogNamespace = cfg.CatalogNamespace
}

func verifyConnection(ctx context.Context, c client.Client, name string) {
	nsList := &corev1.NamespaceList{}
	Expect(c.List(ctx, nsList)).To(Succeed(),
		"failed to connect to %s cluster", name)
	GinkgoWriter.Printf("Connected to %s cluster (%d namespaces)\n", name, len(nsList.Items))
}
