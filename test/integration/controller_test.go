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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"

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
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

			Eventually(func() int {
				workList := &workv1.ManifestWorkList{}
				if err := k8sClient.List(ctx, workList); err != nil {
					return 0
				}
				return len(workList.Items)
			}).Should(Equal(2))

			work1 := expectOperatorManifestWork(cluster1)
			work2 := expectOperatorManifestWork(cluster2)

			Expect(work1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
			Expect(work2.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
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

			work := expectOperatorManifestWork(clusterName)

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

			work := expectOperatorManifestWork(clusterName)

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
			expectNoManifestWorks()
		})

		When("referencing an empty ClusterSet", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
				awaitReconcileFinished()
			})

			It("should not process it", func() {
				expectNoManifestWorks()
			})

			It("shouldn't process a cluster without clusterset label", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, "")
				expectNoManifestWorks()
			})

			It("should process a cluster when it's added", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectOperatorManifestWork(clusterName)
			})
		})

		When("referencing a cluster with no product claim", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
				awaitReconcileFinished()
			})

			It("should skip it", func() {
				expectNoManifestWorks()
			})

			It("should process it when a claim is set", func() {
				util.SetProductClaim(ctx, k8sClient, clusterName, "Other")
				expectOperatorManifestWork(clusterName)
			})
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
		})

		When("referencing a set with a cluster", func() {
			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
				expectOperatorManifestWork(clusterName)
			})

			It("should cleanup ManifestWork when the cluster is removed from ClusterSet", func() {
				updateClusterSetLabel(clusterName, "")
				expectAllManifestWorksDeleted()
			})

			It("should cleanup ManifestWork when the cluster is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1.ManagedCluster{}, clusterName, "")
				expectAllManifestWorksDeleted()
			})

			When("moving the cluster between sets", func() {
				var otherClusterSet string

				BeforeEach(func() {
					otherClusterSet = util.UniqueName("other-set")
					util.CreateManagedClusterSet(ctx, k8sClient, otherClusterSet)
					awaitReconcileFinished()
				})

				It("should cleanup ManifestWork when no mesh targets the new set", func() {
					updateClusterSetLabel(clusterName, otherClusterSet)
					expectAllManifestWorksDeleted()
				})

				It("should keep ManifestWork when another mesh targets the new set", func() {
					otherMesh := util.UniqueName("other-mesh")
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, otherClusterSet, meshv1alpha1.OperatorConfig{})
					awaitReconcileFinished()

					updateClusterSetLabel(clusterName, otherClusterSet)
					awaitReconcileFinished()

					expectOperatorManifestWork(clusterName)
				})
			})

			When("two meshes target the same cluster", func() {
				var otherNs, otherMesh string

				BeforeEach(func() {
					otherNs = util.UniqueName("other-ns")
					otherMesh = util.UniqueName("other-mesh")
					util.CreateNamespace(ctx, k8sClient, otherNs)
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet, meshv1alpha1.OperatorConfig{})
					awaitReconcileFinished()
				})

				It("should keep the ManifestWork when one mesh is deleted", func() {
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
					expectOperatorManifestWork(clusterName)
				})

				It("should delete the ManifestWork when both meshes are deleted", func() {
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
					expectAllManifestWorksDeleted()
				})
			})
		})

	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete related ManifestWorks", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
			expectOperatorManifestWork(clusterName)
			expectOperatorManifestWork(cluster2)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, clusterName)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, cluster2)
		})

		It("should work when ClusterSet doesn't exist", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, util.UniqueName("nonexistent-set"), meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})

		It("should delete ManifestWork even when the ClusterSet is deleted first", func() {
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})
			expectFinalizer(meshName, testNs)
			expectOperatorManifestWork(clusterName)

			util.DeleteResource(ctx, k8sClient, &clusterv1beta2.ManagedClusterSet{}, testClusterSet, "")
			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)

			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, clusterName)
		})
	})

	Context("Platform detection", func() {
		DescribeTable("should detect OpenShift variants and use OSSM operator",
			func(productClaim string) {
				util.CreateOCPManagedCluster(ctx, k8sClient, clusterName, testClusterSet, productClaim)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.OperatorConfig{})

				work := expectOperatorManifestWork(clusterName)

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

			work := expectOperatorManifestWork(clusterName)

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

func awaitReconcileFinished() {
	time.Sleep(1 * time.Second)
}

func updateClusterSetLabel(clusterName, newClusterSet string) {
	cluster := &clusterv1.ManagedCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName}, cluster)).To(Succeed())
	cluster.Labels[meshcontroller.ClusterSetLabel] = newClusterSet
	Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
}

// expectNoManifestWorks makes sure that no ManifestWorks are created, checking consistently
func expectNoManifestWorks() {
	Consistently(func() []workv1.ManifestWork {
		workList := &workv1.ManifestWorkList{}
		Expect(k8sClient.List(ctx, workList)).To(Succeed())
		return workList.Items
	}).Should(BeEmpty())
}

// expectAllManifestWorksDeleted makes sure ManifestWorks are deleted and none remain
func expectAllManifestWorksDeleted() {
	Eventually(func() []workv1.ManifestWork {
		workList := &workv1.ManifestWorkList{}
		Expect(k8sClient.List(ctx, workList)).To(Succeed())
		return workList.Items
	}).Should(BeEmpty())
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

func expectOperatorManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.OperatorManifestWorkName, clusterNamespace)
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
