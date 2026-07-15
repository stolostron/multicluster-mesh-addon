//go:build e2e_multicluster

package e2e

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
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

var _ = Describe("Multi-primary multi-network mesh", Ordered, Serial, func() {
	const (
		meshName    = "multi-primary-mesh"
		clusterSet  = "mesh-cluster-set"
		cpNamespace = "istio-system"
		meshID      = "multi-primary-mesh"
		trustDomain = "cluster.local"
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
		util.LoadAndApplyYAMLInNamespace(ctx, hubClient, filepath.Join(samplesDir, "cert-manager-issuer.yaml"), meshNS, nil)
	})

	AfterAll(func(ctx SpecContext) {
		Step("Cleaning up sample namespace on spoke clusters")
		for _, spokeClient := range spokeClients {
			_ = client.IgnoreNotFound(spokeClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: sampleNS},
			}))
		}

		Step("Cleaning up Istio resources on spoke clusters")
		for cluster, spokeClient := range spokeClients {
			util.DeleteYAMLResources(ctx, spokeClient, filepath.Join(testdataDir, "eastwest-gateway.yaml"), map[string]string{
				"CPNamespace": cpNamespace,
				"Network":     networks[cluster],
			})
			util.DeleteYAMLResources(ctx, spokeClient, filepath.Join(testdataDir, "istio-cr.yaml"), map[string]string{
				"CRName":      "default",
				"CPNamespace": cpNamespace,
				"TrustDomain": trustDomain,
				"MeshID":      meshID,
				"ClusterName": cluster,
				"Network":     networks[cluster],
			})
			util.DeleteYAMLResources(ctx, spokeClient, filepath.Join(testdataDir, "istiocni-cr.yaml"), nil)
		}

		Step("Deleting test mesh %s", meshName)
		if mesh != nil {
			_ = client.IgnoreNotFound(hubClient.Delete(ctx, mesh))
		}

		Step("Waiting for addon resources to be cleaned up")
		Eventually(func(g Gomega) {
			mwList := &workv1.ManifestWorkList{}
			err := hubClient.List(ctx, mwList, client.MatchingLabels{meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(mwList.Items).To(BeEmpty(), "ManifestWorks still exist")
		}).WithTimeout(2 * time.Minute).Should(Succeed())

		Step("Cleaning up cert-manager trust chain in %s", meshNS)
		util.DeleteYAMLResourcesInNamespace(ctx, hubClient, filepath.Join(samplesDir, "cert-manager-issuer.yaml"), meshNS, nil)

		Step("Deleting test namespace %s", meshNS)
		_ = client.IgnoreNotFound(hubClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: meshNS},
		}))
	})

	It("addon plumbing is ready", func(ctx SpecContext) {
		Step("Ensuring istio-system namespace exists on spoke clusters with network labels")
		for cluster, spokeClient := range spokeClients {
			util.CreateNamespaceWithLabels(ctx, spokeClient, cpNamespace, map[string]string{
				"topology.istio.io/network": networks[cluster],
			})
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

		Step("Waiting for controller to reconcile the mesh")
		Eventually(func(g Gomega) {
			g.Expect(hubClient.Get(ctx, key.For(mesh), mesh)).To(Succeed())
			g.Expect(meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)).NotTo(BeNil())
		}).WithTimeout(30 * time.Second).Should(Succeed())

		Step("Waiting for operator ManifestWorks to become Available")
		for _, cluster := range clusters {
			Eventually(func(g Gomega) {
				mw := &workv1.ManifestWork{}
				g.Expect(hubClient.Get(ctx, key.Of(meshcontroller.OperatorManifestWorkName, cluster), mw)).To(Succeed())
				available := meta.FindStatusCondition(mw.Status.Conditions, string(workv1.WorkAvailable))
				g.Expect(available).NotTo(BeNil())
				g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(3 * time.Minute).Should(Succeed())
		}

		Step("Waiting for cacerts ManifestWorks to become Available")
		for _, cluster := range clusters {
			Eventually(func(g Gomega) {
				mw := &workv1.ManifestWork{}
				g.Expect(hubClient.Get(ctx, key.Of(meshcontroller.ManifestWorkNameCacerts, cluster), mw)).To(Succeed())
				available := meta.FindStatusCondition(mw.Status.Conditions, string(workv1.WorkAvailable))
				g.Expect(available).NotTo(BeNil())
				g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(3 * time.Minute).Should(Succeed())
		}

		Step("Waiting for ManagedServiceAccount token secrets to exist on the hub")
		for _, cluster := range clusters {
			Eventually(func(g Gomega) {
				msaList := &msav1beta1.ManagedServiceAccountList{}
				g.Expect(hubClient.List(ctx, msaList,
					client.InNamespace(cluster),
					client.MatchingLabels{
						meshcontroller.MeshNameLabel:      mesh.Name,
						meshcontroller.MeshNamespaceLabel: mesh.Namespace,
					},
				)).To(Succeed())
				g.Expect(msaList.Items).To(HaveLen(1))
				g.Expect(msaList.Items[0].Status.TokenSecretRef).NotTo(BeNil())
			}).WithTimeout(2 * time.Minute).Should(Succeed())
		}
	}, SpecTimeout(5*time.Minute))

	It("Istio control planes become ready on both clusters", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Ensuring istio-system and istio-cni namespaces on %s", cluster)
			util.CreateNamespaceWithLabels(ctx, spokeClient, cpNamespace, map[string]string{
				"topology.istio.io/network": networks[cluster],
			})
			util.EnsureNamespace(ctx, spokeClient, "istio-cni")

			Step("Applying IstioCNI CR on %s", cluster)
			util.LoadAndApplyYAML(ctx, spokeClient, filepath.Join(testdataDir, "istiocni-cr.yaml"), nil)

			Step("Applying Istio CR on %s", cluster)
			util.LoadAndApplyYAML(ctx, spokeClient, filepath.Join(testdataDir, "istio-cr.yaml"), map[string]string{
				"CRName":      "default",
				"CPNamespace": cpNamespace,
				"TrustDomain": trustDomain,
				"MeshID":      meshID,
				"ClusterName": cluster,
				"Network":     networks[cluster],
			})
		}

		for cluster, spokeClient := range spokeClients {
			Step("Waiting for istiod to be ready on %s", cluster)
			util.WaitForDeploymentReady(ctx, spokeClient, "istiod", cpNamespace, 5*time.Minute)
			GinkgoWriter.Printf("istiod is ready on %s\n", cluster)
		}
	}, SpecTimeout(7*time.Minute))

	// REVISIT: Currently, the controller does not support secret distribution.
	// See PIt specs in mesh_lifecycle_test.go for what the controller will eventually do.
	It("remote secrets enable cross-cluster endpoint discovery", func(ctx SpecContext) {
		Step("Verifying remote secrets exist on each spoke with istio/multiCluster label")
		for _, source := range clusters {
			for _, target := range clusters {
				if source == target {
					continue
				}
				targetClient := spokeClients[target]
				secret := &corev1.Secret{}
				Expect(targetClient.Get(ctx, key.Of("istio-remote-secret-"+source, cpNamespace), secret)).
					To(Succeed(), "remote secret for %s not found on %s", source, target)
				Expect(secret.Labels).To(HaveKeyWithValue("istio/multiCluster", "true"))
			}
		}
	}, SpecTimeout(2*time.Minute))

	It("east-west gateways are functional", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Applying east-west Gateway API resource on %s", cluster)
			util.LoadAndApplyYAML(ctx, spokeClient, filepath.Join(testdataDir, "eastwest-gateway.yaml"), map[string]string{
				"CPNamespace": cpNamespace,
				"Network":     networks[cluster],
			})
		}

		for cluster, spokeClient := range spokeClients {
			Step("Waiting for east-west gateway deployment to be ready on %s", cluster)
			util.WaitForDeploymentReady(ctx, spokeClient, "eastwestgateway-istio", cpNamespace, 3*time.Minute)

			Step("Waiting for LoadBalancer IP on %s", cluster)
			ip := util.WaitForLoadBalancerIP(ctx, spokeClient, "eastwestgateway-istio", cpNamespace, 3*time.Minute)
			GinkgoWriter.Printf("East-west gateway on %s has IP: %s\n", cluster, ip)
		}
	}, SpecTimeout(5*time.Minute))

	It("helloworld cross-cluster traffic is load-balanced", func(ctx SpecContext) {
		Step("Creating sample namespace with istio-injection on both clusters")
		for _, spokeClient := range spokeClients {
			util.CreateNamespaceWithLabels(ctx, spokeClient, sampleNS, map[string]string{
				"istio-injection": "enabled",
			})
		}

		Step("Deploying helloworld Service on both clusters")
		for _, spokeClient := range spokeClients {
			util.LoadAndApplyYAML(ctx, spokeClient, filepath.Join(testdataDir, "helloworld-service.yaml"), map[string]string{
				"Namespace": sampleNS,
			})
		}

		Step("Deploying helloworld-v1 on cluster1")
		util.LoadAndApplyYAML(ctx, spokeClients["cluster1"], filepath.Join(testdataDir, "helloworld-v1.yaml"), map[string]string{
			"Namespace": sampleNS,
		})

		Step("Deploying helloworld-v2 on cluster2")
		util.LoadAndApplyYAML(ctx, spokeClients["cluster2"], filepath.Join(testdataDir, "helloworld-v2.yaml"), map[string]string{
			"Namespace": sampleNS,
		})

		Step("Waiting for helloworld-v1 to be ready on cluster1")
		util.WaitForDeploymentReady(ctx, spokeClients["cluster1"], "helloworld-v1", sampleNS, 2*time.Minute)

		Step("Waiting for helloworld-v2 to be ready on cluster2")
		util.WaitForDeploymentReady(ctx, spokeClients["cluster2"], "helloworld-v2", sampleNS, 2*time.Minute)

		Step("Deploying curl pod on cluster1")
		util.LoadAndApplyYAML(ctx, spokeClients["cluster1"], filepath.Join(testdataDir, "curl.yaml"), map[string]string{
			"Namespace": sampleNS,
		})

		Step("Waiting for curl pod to be ready on cluster1")
		curlPod := util.WaitForPodReady(ctx, spokeClients["cluster1"], sampleNS, map[string]string{"app": "curl"}, 2*time.Minute)
		GinkgoWriter.Printf("Curl pod ready: %s\n", curlPod)

		Step("Verifying cross-cluster traffic")
		// sawV1/sawV2 are intentionally outside the Eventually closure so that we can evaluate if the test-code
		// has seen response from both the helloworld applications.
		var sawV1, sawV2 bool
		Eventually(func(g Gomega) {
			for i := 0; i < 20; i++ {
				output, err := util.ExecInPod(ctx, spokeConfigs["cluster1"],
					sampleNS, curlPod, "curl",
					[]string{"curl", "-s", fmt.Sprintf("helloworld.%s:5000/hello", sampleNS)})
				if err != nil {
					GinkgoWriter.Printf("curl attempt %d failed: %v\n", i+1, err)
					continue
				}
				if strings.Contains(output, "v1") {
					sawV1 = true
				}
				if strings.Contains(output, "v2") {
					sawV2 = true
				}
				if sawV1 && sawV2 {
					break
				}
			}
			g.Expect(sawV1).To(BeTrue(), "never saw response from helloworld v1")
			g.Expect(sawV2).To(BeTrue(), "never saw response from helloworld v2")
		}).WithTimeout(3 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		GinkgoWriter.Println("Cross-cluster traffic verified: saw responses from both v1 and v2")
	}, SpecTimeout(8*time.Minute))
})
