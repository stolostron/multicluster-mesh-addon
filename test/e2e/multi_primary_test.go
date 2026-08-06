//go:build e2e_multicluster

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

var (
	testdataDir string
	samplesDir  string
)

func init() {
	_, thisFile, _, _ := runtime.Caller(0)
	testdataDir = filepath.Join(filepath.Dir(thisFile), "testdata")
	samplesDir = filepath.Join(filepath.Dir(thisFile), "..", "..", "samples")
}

var _ = Describe("Multi-primary data plane", Ordered, Serial, func() {
	const (
		meshName    = "multi-primary-mesh"
		clusterSet  = "mesh-cluster-set"
		cpNamespace = "istio-system"
		sampleNS    = "sample"
	)

	var (
		mesh     *meshv1alpha1.MultiClusterMesh
		meshNS   string
		networks = map[string]string{
			"cluster1": "network1",
			"cluster2": "network2",
		}
	)

	BeforeAll(func(ctx SpecContext) {
		meshNS = util.UniqueName("mp-test-ns")
		util.CreateNamespace(ctx, hubClient, meshNS)

		Step("Setting up cert-manager trust chain in %s", meshNS)
		hubClient.ApplyFile(ctx, filepath.Join(samplesDir, "cert-manager-issuer.yaml"), nil, meshNS)

		Step("Labeling ManagedClusters with network identity")
		for cluster := range spokeClients {
			mc := &clusterv1.ManagedCluster{}
			Expect(hubClient.Get(ctx, key.Of(cluster), mc)).To(Succeed())
			metav1.SetMetaDataLabel(&mc.ObjectMeta, meshcontroller.IstioNetworkLabel, networks[cluster])
			Expect(hubClient.Update(ctx, mc)).To(Succeed())
		}

		Step("Creating MultiClusterMesh CR")
		mesh = util.CreateMultiClusterMesh(ctx, hubClient, meshName, meshNS, clusterSet,
			meshv1alpha1.MultiClusterMeshSpec{
				Operator: meshv1alpha1.OperatorConfig{
					Name:            testOperatorName,
					Namespace:       testOperatorNamespace,
					Source:          testCatalogSource,
					SourceNamespace: testCatalogNamespace,
				},
				Security: meshv1alpha1.SecurityConfig{
					Trust: meshv1alpha1.TrustConfig{
						CertManager: meshv1alpha1.CertManagerConfig{
							IssuerRef: meshv1alpha1.IssuerReference{Name: "mesh-root-ca"},
						},
					},
				},
			})

		Step("Waiting for the mesh to become ready")
		Eventually(func(g Gomega) {
			g.Expect(hubClient.Get(ctx, key.For(mesh), mesh)).To(Succeed())
			g.Expect(meta.IsStatusConditionTrue(mesh.Status.Conditions, meshv1alpha1.ConditionReady)).To(BeTrue())
		}).Should(Succeed())

		Step("Verifying control plane namespace exists with network labels")
		for _, spokeClient := range spokeClients {
			ns := &corev1.Namespace{}
			Expect(spokeClient.Get(ctx, key.Of(cpNamespace), ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKey("topology.istio.io/network"))
		}
	}, NodeTimeout(5*time.Minute))

	AfterAll(func(ctx SpecContext) {
		Step("Cleaning up spoke resources")
		for _, spokeClient := range spokeClients {
			_ = client.IgnoreNotFound(spokeClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: sampleNS},
			}))
			spokeClient.Cleanup(ctx)
		}

		Step("Removing network labels from ManagedClusters")
		for cluster := range spokeClients {
			mc := &clusterv1.ManagedCluster{}
			if err := hubClient.Get(ctx, key.Of(cluster), mc); err != nil {
				continue
			}
			delete(mc.Labels, meshcontroller.IstioNetworkLabel)
			_ = hubClient.Update(ctx, mc)
		}

		Step("Deleting test mesh %s", meshName)
		if mesh != nil {
			_ = client.IgnoreNotFound(hubClient.Delete(ctx, mesh))
		}

		Step("Cleaning up hub resources")
		hubClient.Cleanup(ctx)
		_ = client.IgnoreNotFound(hubClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: meshNS},
		}))
	})

	When("Istio multi-cluster is deployed", func() {
		BeforeAll(func(ctx SpecContext) {
			for cluster, spokeClient := range spokeClients {
				Step("Ensuring istio-cni namespace on %s", cluster)
				util.CreateNamespace(ctx, spokeClient, "istio-cni")

				Step("Applying IstioCNI CR on %s", cluster)
				spokeClient.ApplyFile(ctx, filepath.Join(testdataDir, "istiocni-cr.yaml"), nil)

				Step("Applying Istio CR on %s", cluster)
				spokeClient.ApplyFile(ctx, filepath.Join(testdataDir, "istio-cr.yaml"), map[string]string{
					"CPNamespace": cpNamespace,
					"ClusterName": cluster,
					"Network":     networks[cluster],
				})
			}

			for cluster, spokeClient := range spokeClients {
				Step("Waiting for istiod to be ready on %s", cluster)
				waitForDeploymentReady(ctx, spokeClient, "istiod", cpNamespace, 5*time.Minute)
				Success("istiod is ready on %s", cluster)
			}

			// TODO(endpoint-discovery): Remove once the controller distributes remote secrets.
			Step("Creating remote secrets for cross-cluster discovery")
			util.CreateAndDistributeRemoteSecrets(ctx, hubClient, spokeClients, clusters, cpNamespace)

			for cluster, spokeClient := range spokeClients {
				Step("Applying east-west Gateway API resource on %s", cluster)
				spokeClient.ApplyFile(ctx, filepath.Join(testdataDir, "eastwest-gateway.yaml"), map[string]string{
					"CPNamespace": cpNamespace,
					"Network":     networks[cluster],
				})
			}

			for cluster, spokeClient := range spokeClients {
				Step("Waiting for east-west gateway deployment to be ready on %s", cluster)
				waitForDeploymentReady(ctx, spokeClient, "eastwestgateway-istio", cpNamespace, 3*time.Minute)

				Step("Waiting for LoadBalancer IP on %s", cluster)
				ip := waitForLoadBalancerIP(ctx, spokeClient, "eastwestgateway-istio", cpNamespace, 3*time.Minute)
				Success("East-west gateway on %s has IP: %s", cluster, ip)
			}
		}, NodeTimeout(10*time.Minute))

		// REVISIT: Currently, the controller does not support secret distribution.
		// See PIt specs in mesh_lifecycle_test.go for what the controller will eventually do.
		It("should distribute remote secrets to spoke clusters", func(ctx SpecContext) {
			for source := range spokeClients {
				for target, targetClient := range spokeClients {
					if source == target {
						continue
					}
					secret := &corev1.Secret{}
					Expect(targetClient.Get(ctx, key.Of("istio-remote-secret-"+source, cpNamespace), secret)).
						To(Succeed(), "remote secret for %s not found on %s", source, target)
					Expect(secret.Labels).To(HaveKeyWithValue("istio/multiCluster", "true"))
					Expect(secret.Annotations).To(HaveKeyWithValue("networking.istio.io/cluster", source))
				}
			}
		})

		It("should have cross-cluster data plane traffic working", func(ctx SpecContext) {
			Step("Creating sample namespace with istio-injection on both clusters")
			for _, spokeClient := range spokeClients {
				util.CreateNamespace(ctx, spokeClient, sampleNS, map[string]string{
					"istio-injection": "enabled",
				})
			}

			Step("Deploying helloworld Service on both clusters")
			for _, spokeClient := range spokeClients {
				spokeClient.ApplyFile(ctx, filepath.Join(testdataDir, "helloworld-service.yaml"), nil, sampleNS)
			}

			Step("Deploying helloworld-v1 on cluster1")
			spokeClients["cluster1"].ApplyFile(ctx, filepath.Join(testdataDir, "helloworld.yaml"),
				map[string]string{"Version": "v1"}, sampleNS)

			Step("Deploying helloworld-v2 on cluster2")
			spokeClients["cluster2"].ApplyFile(ctx, filepath.Join(testdataDir, "helloworld.yaml"),
				map[string]string{"Version": "v2"}, sampleNS)

			Step("Deploying curl on both clusters")
			for _, spokeClient := range spokeClients {
				spokeClient.ApplyFile(ctx, filepath.Join(testdataDir, "curl.yaml"), nil, sampleNS)
			}

			Step("Waiting for helloworld-v1 to be ready on cluster1")
			waitForDeploymentReady(ctx, spokeClients["cluster1"], "helloworld-v1", sampleNS)

			Step("Waiting for helloworld-v2 to be ready on cluster2")
			waitForDeploymentReady(ctx, spokeClients["cluster2"], "helloworld-v2", sampleNS)

			Step("Waiting for curl to be ready on both clusters")
			for _, spokeClient := range spokeClients {
				waitForDeploymentReady(ctx, spokeClient, "curl", sampleNS)
			}

			Step("Verifying cross-cluster traffic from each cluster")
			for cluster, spokeClient := range spokeClients {
				var sawV1, sawV2 bool
				Eventually(func(g Gomega) {
					output, err := spokeClient.Exec(ctx,
						sampleNS, "deployment/curl", "curl",
						[]string{"curl", "-sS", "--fail", fmt.Sprintf("helloworld.%s:5000/hello", sampleNS)})
					if err == nil {
						sawV1 = sawV1 || strings.Contains(output, "v1")
						sawV2 = sawV2 || strings.Contains(output, "v2")
					}
					g.Expect(sawV1).To(BeTrue(), "never saw v1 from %s", cluster)
					g.Expect(sawV2).To(BeTrue(), "never saw v2 from %s", cluster)
				}).WithTimeout(3 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
				Success("Cross-cluster traffic verified from %s", cluster)
			}
		}, SpecTimeout(8*time.Minute))
	})
})

func waitForDeploymentReady(ctx context.Context, k8sClient client.Client, name, namespace string, timeout ...time.Duration) {
	e := Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(k8sClient.Get(ctx, key.Of(name, namespace), deploy)).To(Succeed())
		g.Expect(deploy.Status.ObservedGeneration).To(Equal(deploy.Generation),
			"deployment %s/%s status not yet observed for current generation", namespace, name)
		g.Expect(deploy.Status.Conditions).To(ContainElement(And(
			HaveField("Type", appsv1.DeploymentAvailable),
			HaveField("Status", corev1.ConditionTrue),
		)), "deployment %s/%s is not Available", namespace, name)
	}).WithPolling(2 * time.Second)
	if len(timeout) > 0 {
		e = e.WithTimeout(timeout[0])
	}
	e.Should(Succeed())
}

func waitForLoadBalancerIP(ctx context.Context, k8sClient client.Client, name, namespace string, timeout ...time.Duration) string {
	var address string
	e := Eventually(func(g Gomega) {
		svc := &corev1.Service{}
		g.Expect(k8sClient.Get(ctx, key.Of(name, namespace), svc)).To(Succeed())
		g.Expect(svc.Status.LoadBalancer.Ingress).NotTo(BeEmpty(),
			"service %s/%s has no LoadBalancer ingress", namespace, name)
		address = svc.Status.LoadBalancer.Ingress[0].IP
		if address == "" {
			address = svc.Status.LoadBalancer.Ingress[0].Hostname
		}
		g.Expect(address).NotTo(BeEmpty(), "service %s/%s has no IP or hostname", namespace, name)
	}).WithPolling(2 * time.Second)
	if len(timeout) > 0 {
		e = e.WithTimeout(timeout[0])
	}
	e.Should(Succeed())
	return address
}
