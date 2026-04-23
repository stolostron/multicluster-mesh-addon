//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Test Suite")
}

// mustAddToScheme registers a scheme and fails the test if it errors
func mustAddToScheme(fn func(*runtime.Scheme) error, s *runtime.Scheme) {
	Expect(fn(s)).To(Succeed())
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	// Set global timeout & polling defaults
	SetDefaultEventuallyTimeout(10 * time.Second)
	SetDefaultEventuallyPollingInterval(250 * time.Millisecond)
	SetDefaultConsistentlyDuration(2 * time.Second)
	SetDefaultConsistentlyPollingInterval(250 * time.Millisecond)
	meshcontroller.MissingClaimRequeueDelay = 1 * time.Second

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd"),                      // Custom MultiClusterMesh CRD
			filepath.Join("..", "..", "test", "integration", "crds", "ocm"), // OCM CRDs
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Register schemes
	mustAddToScheme(meshv1alpha1.Install, scheme.Scheme)
	mustAddToScheme(clusterv1.Install, scheme.Scheme)
	mustAddToScheme(clusterv1beta2.Install, scheme.Scheme)
	mustAddToScheme(workv1.Install, scheme.Scheme)
	mustAddToScheme(operatorsv1.AddToScheme, scheme.Scheme)
	mustAddToScheme(operatorsv1alpha1.AddToScheme, scheme.Scheme)

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	ctx, cancel = context.WithCancel(context.Background())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics in tests
		},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(meshcontroller.RegisterController(mgr)).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	if err != nil {
		// The kube-apiserver may timeout during shutdown when cleaning up test resources with finalizers.
		// This is expected in test environments and doesn't indicate a real problem since processes are
		// force-killed anyway. Log the error but don't fail the test suite.
		GinkgoLogr.Error(err, "Error stopping test environment (this is usually harmless)")
	}
})
