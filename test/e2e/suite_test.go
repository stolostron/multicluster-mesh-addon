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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

const (
	testOperatorName      = "sailoperator"
	testOperatorNamespace = "sail-operator"
	testCatalogSource     = "operatorhubio-catalog"
	testCatalogNamespace  = "olm"
)

var (
	clusters = []string{"cluster1", "cluster2"}

	hubClient    client.Client
	spokeClients map[string]client.Client
	// spokeConfigs holds the raw REST configs needed for pod exec, which client.Client does not support.
	spokeConfigs map[string]*rest.Config
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
	spokeClients = make(map[string]client.Client)
	spokeConfigs = make(map[string]*rest.Config)
	for name, kc := range map[string]string{
		"cluster1": cluster1Kubeconfig,
		"cluster2": cluster2Kubeconfig,
	} {
		cfg, c := clientAndConfigFrom(kc)
		spokeClients[name] = c
		spokeConfigs[name] = cfg
	}

	Step("Verifying cluster connectivity")
	verifyConnection(ctx, hubClient, "hub")
	for name, c := range spokeClients {
		verifyConnection(ctx, c, name)
	}

	Step("Checking for existing resources that can interfere with our testing")
	checkNoExistingResources(ctx, hubClient)
})

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func clientFrom(kubeconfig string) client.Client {
	_, c := clientAndConfigFrom(kubeconfig)
	return c
}

func clientAndConfigFrom(kubeconfig string) (*rest.Config, client.Client) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig from %s", kubeconfig)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create client from %s", kubeconfig)
	return cfg, c
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

func checkNoExistingResources(ctx context.Context, c client.Client) {
	mwList := &workv1.ManifestWorkList{}
	err := c.List(ctx, mwList, client.MatchingLabels{meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue})
	Expect(err).NotTo(HaveOccurred())
	Expect(mwList.Items).To(BeEmpty(),
		"existing ManifestWorks found; run 'make dev-clean-meshes' to clean up")

	msaList := &msav1beta1.ManagedServiceAccountList{}
	err = c.List(ctx, msaList, client.MatchingLabels{meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue})
	Expect(err).NotTo(HaveOccurred())
	Expect(msaList.Items).To(BeEmpty(),
		"existing ManagedServiceAccounts found; run 'make dev-clean-meshes' to clean up")
}
