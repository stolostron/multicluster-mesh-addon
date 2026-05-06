//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

var _ = Describe("MultiClusterMesh Controller", func() {
	var (
		testNs         string
		testClusterSet string
		meshName       string
		clusterName    string
	)

	BeforeEach(func() {
		testNs = util.UniqueName("test-ns")
		testClusterSet = util.UniqueName("test-set")
		meshName = util.UniqueName("mesh")
		clusterName = util.UniqueName("cluster")

		util.CreateNamespace(ctx, k8sClient, testNs)
		util.CreateManagedClusterSet(ctx, k8sClient, testClusterSet)
	})

	// Delete test resources (envtest won't fully delete namespaces, but we clean up anyway)
	// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
	AfterEach(func() {
		meshList := &meshv1alpha1.MultiClusterMeshList{}
		_ = k8sClient.List(ctx, meshList)
		for i := range meshList.Items {
			_ = k8sClient.Delete(ctx, &meshList.Items[i])
		}

		workList := &workv1.ManifestWorkList{}
		_ = k8sClient.List(ctx, workList)
		for i := range workList.Items {
			_ = k8sClient.Delete(ctx, &workList.Items[i])
		}

		clusterList := &clusterv1.ManagedClusterList{}
		_ = k8sClient.List(ctx, clusterList)
		for i := range clusterList.Items {
			_ = k8sClient.Delete(ctx, &clusterList.Items[i])
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterList.Items[i].Name}}
			_ = k8sClient.Delete(ctx, ns)
		}

		clusterSetList := &clusterv1beta2.ManagedClusterSetList{}
		_ = k8sClient.List(ctx, clusterSetList)
		for i := range clusterSetList.Items {
			_ = k8sClient.Delete(ctx, &clusterSetList.Items[i])
		}

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNs}}
		_ = k8sClient.Delete(ctx, ns)
	})

	Context("Basic reconciliation", func() {
		It("should create ManifestWorks for clusters in the specified ClusterSet", func() {
			cluster1 := util.UniqueName("cluster")
			cluster2 := util.UniqueName("cluster")

			util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
			util.CreateK8sManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

			Eventually(func() int {
				workList := &workv1.ManifestWorkList{}
				if err := k8sClient.List(ctx, workList); err != nil {
					return 0
				}
				return len(workList.Items)
			}).Should(Equal(2))

			expectSailManifestWork(cluster1)
			expectSailManifestWork(cluster2)
		})

		It("should use custom operator configuration on K8s when specified", func() {
			customConfig := meshv1alpha1.OperatorConfig{
				Namespace:           "custom-ns",
				Channel:             "1.23",
				Source:              "custom-catalog",
				SourceNamespace:     "custom-catalog-ns",
				StartingCSV:         "sailoperator.v1.23.0",
				InstallPlanApproval: operatorsv1alpha1.ApprovalManual,
			}

			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, customConfig)

			work := expectSailManifestWork(clusterName)

			Expect(work.Spec.Workload.Manifests).To(HaveLen(3))
			expectNamespace(work, 0, customConfig.Namespace)
			expectOperatorGroup(work, 1, "operator-group", customConfig.Namespace)
			expectSubscription(work, 2, false, operatorsv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: customConfig.Namespace,
				},
				Spec: &operatorsv1alpha1.SubscriptionSpec{
					Channel:                customConfig.Channel,
					CatalogSource:          customConfig.Source,
					CatalogSourceNamespace: customConfig.SourceNamespace,
					StartingCSV:            customConfig.StartingCSV,
					InstallPlanApproval:    customConfig.InstallPlanApproval,
				},
			})
		})

		It("should use custom operator configuration on OpenShift when specified", func() {
			customConfig := meshv1alpha1.OperatorConfig{
				Namespace:           "custom-ossm-ns",
				Channel:             "tech-preview",
				Source:              "custom-catalog",
				SourceNamespace:     "custom-catalog-ns",
				StartingCSV:         "servicemeshoperator3.v3.0.0",
				InstallPlanApproval: operatorsv1alpha1.ApprovalManual,
			}

			util.CreateOCPManagedCluster(ctx, k8sClient, clusterName, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, customConfig)

			work := expectOSSMManifestWork(clusterName)

			Expect(work.Spec.Workload.Manifests).To(HaveLen(3))
			expectNamespace(work, 0, customConfig.Namespace)
			expectOperatorGroup(work, 1, "operator-group", customConfig.Namespace)
			expectSubscription(work, 2, true, operatorsv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: customConfig.Namespace,
				},
				Spec: &operatorsv1alpha1.SubscriptionSpec{
					Channel:                customConfig.Channel,
					CatalogSource:          customConfig.Source,
					CatalogSourceNamespace: customConfig.SourceNamespace,
					StartingCSV:            customConfig.StartingCSV,
					InstallPlanApproval:    customConfig.InstallPlanApproval,
				},
			})
		})

		It("should not create ManifestWorks when ClusterSet doesn't exist", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, util.UniqueName("set"), meshv1alpha1.OperatorConfig{})

			// Give controller time to process reconciliation
			time.Sleep(2 * time.Second)

			workList := &workv1.ManifestWorkList{}
			Expect(k8sClient.List(ctx, workList)).To(Succeed())
			Expect(workList.Items).To(BeEmpty())
		})

		It("should skip clusters without product claims and requeue", func() {
			util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

			// Give controller time to process reconciliation
			time.Sleep(2 * time.Second)

			// No ManifestWork should be created because the cluster lacks product claim
			workList := &workv1.ManifestWorkList{}
			Expect(k8sClient.List(ctx, workList)).To(Succeed())
			Expect(workList.Items).To(BeEmpty())

			// Now add the product claim and expect the corresponding ManifestWork to be created
			util.SetProductClaim(ctx, k8sClient, clusterName, "Other")
			expectSailManifestWork(clusterName)
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
		})
	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete related ManifestWorks", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
			expectSailManifestWork(clusterName)
			expectOSSMManifestWork(cluster2)

			mesh := &meshv1alpha1.MultiClusterMesh{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mesh)).To(Succeed())

			expectResourceDeleted(&workv1.ManifestWork{}, meshcontroller.ManifestWorkNameSail, clusterName)
			expectResourceDeleted(&workv1.ManifestWork{}, meshcontroller.ManifestWorkNameOSSM, cluster2)
			expectResourceDeleted(&meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})

		It("should work when ClusterSet doesn't exist", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, util.UniqueName("nonexistent-set"), meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)

			mesh := &meshv1alpha1.MultiClusterMesh{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mesh)).To(Succeed())

			expectResourceDeleted(&meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})

		It("should delete ManifestWork even when the ClusterSet is deleted first", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
			expectSailManifestWork(clusterName)

			clusterSet := &clusterv1beta2.ManagedClusterSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testClusterSet}, clusterSet)).To(Succeed())
			Expect(k8sClient.Delete(ctx, clusterSet)).To(Succeed())
			expectResourceDeleted(&clusterv1beta2.ManagedClusterSet{}, testClusterSet, "")

			mesh := &meshv1alpha1.MultiClusterMesh{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
			Expect(k8sClient.Delete(ctx, mesh)).To(Succeed())

			expectResourceDeleted(&workv1.ManifestWork{}, meshcontroller.ManifestWorkNameSail, clusterName)
			expectResourceDeleted(&meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})
	})

	Context("Certificate distribution", func() {
		It("should create ManifestWork when cacerts secret is created", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMeshWithCertManager(ctx, k8sClient, meshName, testNs, testClusterSet, "mesh-issuer")

			// Simulate cert-manager creating the cacerts secret
			util.CreateCacertsSecret(ctx, k8sClient, testNs, clusterName, meshName, testNs)

			// Verify cacerts ManifestWork is created
			work := expectManifestWork(meshcontroller.ManifestWorkNameCacerts, clusterName)

			// Verify the ManifestWork contains a secret
			Expect(work.Spec.Workload.Manifests).To(HaveLen(1))

			secret := &corev1.Secret{}
			Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], secret)).To(Succeed())
			Expect(secret.Name).To(Equal("cacerts"))
			Expect(secret.Namespace).To(Equal("istio-system"))
			Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))
			Expect(secret.Data).To(HaveKey("tls.crt"))
			Expect(secret.Data).To(HaveKey("tls.key"))
			Expect(secret.Data).To(HaveKey("ca.crt"))
		})

		It("should create ManifestWork for each cluster when secrets are created", func() {
			cluster1 := util.UniqueName("cluster")
			cluster2 := util.UniqueName("cluster")

			util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
			util.CreateK8sManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
			util.CreateMultiClusterMeshWithCertManager(ctx, k8sClient, meshName, testNs, testClusterSet, "mesh-issuer")

			// Simulate cert-manager creating secrets for both clusters
			util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster1, meshName, testNs)
			util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster2, meshName, testNs)

			// Verify cacerts ManifestWorks are created for both clusters
			expectManifestWork(meshcontroller.ManifestWorkNameCacerts, cluster1)
			expectManifestWork(meshcontroller.ManifestWorkNameCacerts, cluster2)
		})

		It("should update ManifestWork when cacerts secret is updated", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMeshWithCertManager(ctx, k8sClient, meshName, testNs, testClusterSet, "mesh-issuer")

			util.CreateCacertsSecret(ctx, k8sClient, testNs, clusterName, meshName, testNs)

			expectManifestWork(meshcontroller.ManifestWorkNameCacerts, clusterName)

			// Update the secret
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("cacerts-%s", clusterName),
				Namespace: testNs,
			}, secret)).To(Succeed())

			secret.Data["tls.crt"] = []byte("updated-cert-data")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			// Verify ManifestWork data is updated
			Eventually(func() string {
				work := &workv1.ManifestWork{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      meshcontroller.ManifestWorkNameCacerts,
					Namespace: clusterName,
				}, work); err != nil {
					return ""
				}
				manifestSecret := &corev1.Secret{}
				if err := unmarshalManifest(work.Spec.Workload.Manifests[0], manifestSecret); err != nil {
					return ""
				}
				return string(manifestSecret.Data["tls.crt"])
			}).Should(Equal("updated-cert-data"))
		})

		It("should not create cacerts ManifestWork when no issuer is configured", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

			// Give controller time to process
			time.Sleep(2 * time.Second)

			// Verify no cacerts ManifestWork is created
			work := &workv1.ManifestWork{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      meshcontroller.ManifestWorkNameCacerts,
				Namespace: clusterName,
			}, work)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("Platform detection", func() {
		DescribeTable("should detect OpenShift variants and use OSSM operator",
			func(productClaim string) {
				util.CreateOCPManagedCluster(ctx, k8sClient, clusterName, testClusterSet, productClaim)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

				work := expectOSSMManifestWork(clusterName)

				// OpenShift: expect only Subscription (openshift-operators namespace/OperatorGroup exist by default)
				Expect(work.Spec.Workload.Manifests).To(HaveLen(1))

				expectSubscription(work, 0, true, operatorsv1alpha1.Subscription{})
			},
			Entry(meshcontroller.ProductOCP, meshcontroller.ProductOCP),
			Entry(meshcontroller.ProductROSA, meshcontroller.ProductROSA),
			Entry(meshcontroller.ProductARO, meshcontroller.ProductARO),
			Entry(meshcontroller.ProductROKS, meshcontroller.ProductROKS),
			Entry(meshcontroller.ProductOSD, meshcontroller.ProductOSD),
		)

		It("should detect vanilla Kubernetes and use Sail operator", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

			work := expectSailManifestWork(clusterName)

			// Kubernetes: Namespace + OperatorGroup + Subscription (unlike OpenShift)
			Expect(work.Spec.Workload.Manifests).To(HaveLen(3))

			expectNamespace(work, 0, meshcontroller.DefaultOperatorNs)
			expectOperatorGroup(work, 1, "operator-group", meshcontroller.DefaultOperatorNs)
			expectSubscription(work, 2, false, operatorsv1alpha1.Subscription{})
		})
	})
})

func expectFinalizer(name, namespace string) {
	Eventually(func() []string {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, mesh); err != nil {
			return nil
		}
		return mesh.Finalizers
	}).Should(ContainElement(meshcontroller.FinalizerName))
}

func expectResourceDeleted(obj client.Object, name, namespace string) {
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func expectManifestWork(name, namespace string) *workv1.ManifestWork {
	work := &workv1.ManifestWork{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, work)
	}).Should(Succeed())
	return work
}

func expectSailManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.ManifestWorkNameSail, clusterNamespace)
}

func expectOSSMManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.ManifestWorkNameOSSM, clusterNamespace)
}

// unmarshalManifest extracts a manifest from ManifestWork's RawExtension.
// The Object field is nil when reading from API, so we unmarshal from Raw bytes.
func unmarshalManifest(manifest workv1.Manifest, into interface{}) error {
	if manifest.RawExtension.Object != nil {
		return fmt.Errorf("Object field should be nil when reading from API")
	}
	return json.Unmarshal(manifest.RawExtension.Raw, into)
}

func expectNamespace(work *workv1.ManifestWork, index int, expectedName string) {
	ns := &corev1.Namespace{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], ns)).To(Succeed())
	Expect(ns.Name).To(Equal(expectedName))
}

func expectOperatorGroup(work *workv1.ManifestWork, index int, expectedName, expectedNamespace string) {
	og := &operatorsv1.OperatorGroup{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], og)).To(Succeed())
	Expect(og.Name).To(Equal(expectedName))
	Expect(og.Namespace).To(Equal(expectedNamespace))
}

func expectSubscription(work *workv1.ManifestWork, index int, isOCP bool, expected operatorsv1alpha1.Subscription) {
	sub := &operatorsv1alpha1.Subscription{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], sub)).To(Succeed())

	expectedNamespace := expected.Namespace
	if expectedNamespace == "" {
		if isOCP {
			expectedNamespace = meshcontroller.DefaultOCPOperatorNs
		} else {
			expectedNamespace = meshcontroller.DefaultOperatorNs
		}
	}

	var expectedName, expectedPackage, expectedCatalogSource, expectedCatalogSourceNamespace, expectedChannel string
	var expectedInstallPlanApproval operatorsv1alpha1.Approval

	if expected.Spec != nil {
		expectedCatalogSource = expected.Spec.CatalogSource
		expectedCatalogSourceNamespace = expected.Spec.CatalogSourceNamespace
		expectedChannel = expected.Spec.Channel
		expectedInstallPlanApproval = expected.Spec.InstallPlanApproval
	}

	if isOCP {
		expectedName = meshcontroller.OperatorNameOSSM
		expectedPackage = meshcontroller.OperatorNameOSSM
	} else {
		expectedName = meshcontroller.OperatorNameSail
		expectedPackage = meshcontroller.OperatorNameSail
	}

	if expectedCatalogSource == "" {
		if isOCP {
			expectedCatalogSource = meshcontroller.DefaultOCPCatalogSource
		} else {
			expectedCatalogSource = meshcontroller.DefaultCatalogSource
		}
	}

	if expectedCatalogSourceNamespace == "" {
		if isOCP {
			expectedCatalogSourceNamespace = meshcontroller.DefaultOCPCatalogNs
		} else {
			expectedCatalogSourceNamespace = meshcontroller.DefaultCatalogNs
		}
	}

	if expectedChannel == "" {
		expectedChannel = meshcontroller.DefaultChannel
	}

	if expectedInstallPlanApproval == "" {
		expectedInstallPlanApproval = operatorsv1alpha1.ApprovalAutomatic
	}

	Expect(sub.Name).To(Equal(expectedName))
	Expect(sub.Namespace).To(Equal(expectedNamespace))
	Expect(sub.Spec.Package).To(Equal(expectedPackage))
	Expect(sub.Spec.CatalogSource).To(Equal(expectedCatalogSource))
	Expect(sub.Spec.CatalogSourceNamespace).To(Equal(expectedCatalogSourceNamespace))
	Expect(sub.Spec.Channel).To(Equal(expectedChannel))
	Expect(sub.Spec.InstallPlanApproval).To(Equal(expectedInstallPlanApproval))
}
