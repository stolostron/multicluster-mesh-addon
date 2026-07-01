//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	msaRootWord = "istio-reader"
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

		msaList := &msav1beta1.ManagedServiceAccountList{}
		_ = k8sClient.List(ctx, msaList)
		for i := range msaList.Items {
			_ = k8sClient.Delete(ctx, &msaList.Items[i])
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
		When("two clusters exist", func() {
			var cluster2Name string

			BeforeEach(func() {
				cluster2Name = util.UniqueName("cluster")

				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateOCPManagedCluster(ctx, k8sClient, cluster2Name, testClusterSet, meshcontroller.ProductOCP)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should create ManifestWorks for each cluster", func() {
				work1 := expectOperatorManifestWork(clusterName)
				work2 := expectOperatorManifestWork(cluster2Name)

				Expect(work1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(work2.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))

				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonManifestWorkCreated)
			})

			It("should include feedback rules for the Operator Subscription status", func() {
				for _, cluster := range []string{clusterName, cluster2Name} {
					work := expectOperatorManifestWork(cluster)

					Expect(work.Spec.ManifestConfigs).To(HaveLen(1))
					Expect(work.Spec.ManifestConfigs[0].ResourceIdentifier.Resource).To(Equal("subscriptions"))
					Expect(work.Spec.ManifestConfigs[0].FeedbackRules).To(HaveLen(1))
					Expect(work.Spec.ManifestConfigs[0].FeedbackRules[0].Type).To(Equal(workv1.JSONPathsType))
					Expect(work.Spec.ManifestConfigs[0].FeedbackRules[0].JsonPaths[0].Path).To(Equal(".status.installedCSV"))
				}
			})

			It("should become ready after all clusters confirm operator installation", func() {
				expectMeshNotReady(meshName, testNs)

				By("setting feedback on one cluster, mesh should stay not-ready")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, clusterName,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonManifestWorkCreated)
				expectMeshNotReady(meshName, testNs)

				By("setting feedback on all clusters, mesh should become ready")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster2Name,
					meshcontroller.FeedbackInstalledCSV, "servicemeshoperator3.v3.0.0")

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonOperatorInstalled)
				expectMeshReady(meshName, testNs)
			})
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
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{Operator: customConfig})

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
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{Operator: customConfig})

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

		When("referencing a non-existent ClusterSet", func() {
			var otherClusterSet string

			BeforeEach(func() {
				otherClusterSet = util.UniqueName("late-set")
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, otherClusterSet)
			})

			It("should not create ManifestWorks", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoManifestWorks()
			})

			It("should reconcile when the ClusterSet is created", func() {
				expectMeshNotReady(meshName, testNs)
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, otherClusterSet)
				util.CreateManagedClusterSet(ctx, k8sClient, otherClusterSet)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
			})
		})

		When("referencing an empty ClusterSet", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should not process it", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoManifestWorks()
			})

			It("shouldn't process a cluster without clusterset label", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, "")
				expectMeshNotReady(meshName, testNs)
				expectNoManifestWorks()
			})

			It("should process a cluster when it's added", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
			})
		})

		When("referencing a cluster with no product claim", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should skip it and report missing product claim", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoManifestWorks()
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonMissingProductClaim)
			})

			It("should process it when a claim is set", func() {
				expectMeshNotReady(meshName, testNs)
				util.SetProductClaim(ctx, k8sClient, clusterName, "Other")
				expectOperatorManifestWork(clusterName)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
			})
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectFinalizer(meshName, testNs)
		})

		When("referencing a set with a cluster", func() {
			var work *workv1.ManifestWork

			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				work = expectOperatorManifestWork(clusterName)
			})

			It("should not update operator ManifestWork when operator config hasn't changed", func() {
				originalVersion := work.ResourceVersion

				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.ControlPlane.Namespace = "different-ns"
				})

				Consistently(func() string {
					return expectOperatorManifestWork(clusterName).ResourceVersion
				}).Should(Equal(originalVersion))
			})

			It("should update ManifestWork when operator config changes", func() {
				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.Operator.Channel = "tech-preview"
				})

				Eventually(func() string {
					work := expectOperatorManifestWork(clusterName)
					sub := &operatorsv1alpha1.Subscription{}
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[2], sub)).To(Succeed())
					return sub.Spec.Channel
				}).Should(Equal("tech-preview"))
			})

			It("should restore ManifestWork spec when externally modified", func() {
				sub := &operatorsv1alpha1.Subscription{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[2], sub)).To(Succeed())
				originalChannel := sub.Spec.Channel

				sub.Spec.Channel = "tampered"
				work.Spec.Workload.Manifests[2] = workv1.Manifest{
					RawExtension: runtime.RawExtension{Object: sub},
				}
				Expect(k8sClient.Update(ctx, work)).To(Succeed())

				// Ensure reconciliation after tamper (dual-cache race, #109).
				triggerReconcile(meshName, testNs)

				Eventually(func() string {
					work := expectOperatorManifestWork(clusterName)
					sub := &operatorsv1alpha1.Subscription{}
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[2], sub)).To(Succeed())
					return sub.Spec.Channel
				}).Should(Equal(originalChannel))
			})

			// TODO(mkolesnik): Enable once sdk-go WorkApplier cache fix is released
			// https://github.com/open-cluster-management-io/sdk-go/issues/223
			PIt("should restore ManifestWork labels when externally modified", func() {
				work.Labels[meshcontroller.ManagedByLabel] = "someone-else"
				Expect(k8sClient.Update(ctx, work)).To(Succeed())

				Eventually(func() string {
					work := expectOperatorManifestWork(clusterName)
					return work.Labels[meshcontroller.ManagedByLabel]
				}).Should(Equal(meshcontroller.ManagedByValue))
			})

			It("should cleanup ManifestWork when the cluster is removed from ClusterSet", func() {
				updateClusterSetLabel(clusterName, "")
				expectAllManifestWorksDeleted()
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should cleanup ManifestWork when the cluster is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1.ManagedCluster{}, clusterName, "")
				expectAllManifestWorksDeleted()
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should cleanup ManifestWork when the ClusterSet is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1beta2.ManagedClusterSet{}, testClusterSet, "")
				expectAllManifestWorksDeleted()
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should recreate ManifestWork when it is externally deleted", func() {
				work := expectOperatorManifestWork(clusterName)
				originalUID := work.UID
				Expect(k8sClient.Delete(ctx, work)).To(Succeed())
				Eventually(func() types.UID {
					return expectOperatorManifestWork(clusterName).UID
				}).ShouldNot(Equal(originalUID))
			})

			When("moving the cluster between sets", func() {
				var otherClusterSet string

				BeforeEach(func() {
					otherClusterSet = util.UniqueName("other-set")
					util.CreateManagedClusterSet(ctx, k8sClient, otherClusterSet)
				})

				It("should cleanup ManifestWork when no mesh targets the new set", func() {
					updateClusterSetLabel(clusterName, otherClusterSet)
					expectAllManifestWorksDeleted()
					expectNoClusterStatus(meshName, testNs, clusterName)
				})

				It("should recreate ManifestWork for the new mesh when cluster moves to a new set", func() {
					originalUID := work.UID

					otherMesh := util.UniqueName("other-mesh")
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, otherClusterSet)
					expectMeshNotReady(otherMesh, testNs)
					updateClusterSetLabel(clusterName, otherClusterSet)

					Eventually(func(g Gomega) {
						w := expectOperatorManifestWork(clusterName)
						g.Expect(w.Labels[meshcontroller.ClusterSetLabel]).To(Equal(otherClusterSet))
						g.Expect(w.UID).NotTo(Equal(originalUID))
					}).Should(Succeed())
				})
			})

			When("two meshes target the same cluster", func() {
				var otherNs, otherMesh string

				BeforeEach(func() {
					otherNs = util.UniqueName("other-ns")
					otherMesh = util.UniqueName("other-mesh")
					util.CreateNamespace(ctx, k8sClient, otherNs)
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet)
				})

				It("should keep the ManifestWork when one mesh is deleted", func() {
					expectMeshNotReady(otherMesh, otherNs)
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
					expectOperatorManifestWork(clusterName)
				})

				It("should delete the ManifestWork when both meshes are deleted", func() {
					expectMeshNotReady(otherMesh, otherNs)
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
					expectAllManifestWorksDeleted()
				})
			})
		})
	})

	Context("Validation", func() {
		var otherMesh string

		BeforeEach(func() {
			otherMesh = meshName + "-2"

			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectOperatorManifestWork(clusterName)
		})

		When("metadata.name exceeds 63 characters", func() {
			It("should reject creation", func() {
				longName := "a-mesh-name-that-is-way-too-long-and-exceeds-the-sixty-three-character-limit"
				mesh := &meshv1alpha1.MultiClusterMesh{
					ObjectMeta: metav1.ObjectMeta{Name: longName, Namespace: testNs},
					Spec:       meshv1alpha1.MultiClusterMeshSpec{ClusterSet: testClusterSet},
				}
				err := k8sClient.Create(ctx, mesh)
				Expect(err).To(HaveOccurred(), "expected validation error for long name")
				Expect(errors.IsInvalid(err)).To(BeTrue())
			})
		})

		When("spec.clusterSet is empty", func() {
			It("should reject creation", func() {
				mesh := &meshv1alpha1.MultiClusterMesh{
					ObjectMeta: metav1.ObjectMeta{Name: meshName + "-empty", Namespace: testNs},
					Spec:       meshv1alpha1.MultiClusterMeshSpec{ClusterSet: ""},
				}
				err := k8sClient.Create(ctx, mesh)
				Expect(err).To(HaveOccurred(), "expected validation error for empty clusterSet")
				Expect(errors.IsInvalid(err)).To(BeTrue())
			})
		})

		When("spec.clusterSet is changed on update", func() {
			It("should reject the update", func() {
				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
				mesh.Spec.ClusterSet = "different-set"
				err := k8sClient.Update(ctx, mesh)
				Expect(err).To(HaveOccurred(), "expected validation error when updating the clusterSet")
				Expect(errors.IsInvalid(err)).To(BeTrue())
			})
		})

		It("should allow two meshes with different control plane namespaces", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
			})

			expectMeshNotReady(otherMesh, testNs)
			expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
		})

		When("a newer mesh has a conflicting operator config", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
					Operator:     meshv1alpha1.OperatorConfig{Channel: "different-channel"},
				})
			})

			It("should block the newer mesh", func() {
				expectMeshConditionReason(otherMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonOperatorConfigConflict)
			})

			It("should unblock the newer mesh when the older mesh is deleted", func() {
				expectMeshConditionReason(otherMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonOperatorConfigConflict)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
			})
		})

		When("a newer mesh has the same control plane namespace", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testClusterSet)
			})

			It("should block the newer mesh", func() {
				expectMeshConditionReason(otherMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)
			})

			It("should detect conflict when one mesh uses the default namespace explicitly", func() {
				thirdMesh := meshName + "-3"
				util.CreateMultiClusterMesh(ctx, k8sClient, thirdMesh, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system"},
				})

				expectMeshConditionReason(thirdMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)
			})

			It("should unblock the newer mesh when the older mesh is deleted", func() {
				expectMeshConditionReason(otherMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
			})
		})
	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete related ManifestWorks", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectFinalizer(meshName, testNs)
			expectOperatorManifestWork(clusterName)
			expectOperatorManifestWork(cluster2)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, clusterName)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, cluster2)
		})

		It("should work when ClusterSet doesn't exist", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, util.UniqueName("nonexistent-set"))
			expectFinalizer(meshName, testNs)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})
	})

	Context("Certificate distribution", func() {
		When("cert-manager issuer is configured", func() {
			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
			})

			It("should create Certificate resource with owner reference", func() {
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mesh)).To(Succeed())

				Expect(cert.OwnerReferences).To(HaveLen(1))
				ownerRef := cert.OwnerReferences[0]
				Expect(ownerRef.APIVersion).To(Equal(meshv1alpha1.GroupVersion.String()))
				Expect(ownerRef.Kind).To(Equal("MultiClusterMesh"))
				Expect(ownerRef.Name).To(Equal(meshName))
				Expect(ownerRef.UID).To(Equal(mesh.UID))
				Expect(*ownerRef.Controller).To(BeTrue())
				Expect(*ownerRef.BlockOwnerDeletion).To(BeTrue())
			})

			It("should set Subject and URI SAN on Certificate", func() {
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				Expect(cert.Spec.Subject).NotTo(BeNil())
				Expect(cert.Spec.Subject.Organizations).To(ConsistOf(meshName))
				Expect(cert.Spec.Subject.OrganizationalUnits).To(ConsistOf(clusterName))

				expectedSAN := "spiffe://" + meshName + "/cluster/" + clusterName + "/ca/istio-ca"
				Expect(cert.Spec.URIs).To(ConsistOf(expectedSAN))
			})

			It("should restore Certificate spec when externally modified", func() {
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				cert.Spec.CommonName = "tampered"
				Expect(k8sClient.Update(ctx, cert)).To(Succeed())

				Eventually(func() string {
					c := &certmanagerv1.Certificate{}
					if err := k8sClient.Get(ctx, key.For(cert), c); err != nil {
						return ""
					}
					return c.Spec.CommonName
				}).Should(Equal("Istio CA"))
			})

			It("should recreate Certificate when it is externally deleted", func() {
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")
				originalUID := cert.UID
				Expect(k8sClient.Delete(ctx, cert)).To(Succeed())

				Eventually(func() types.UID {
					return expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer").UID
				}).ShouldNot(Equal(originalUID))
			})

			It("should create ManifestWork when cacerts secret is created", func() {
				// simulate creating the cacerts secret by cert-manager
				util.CreateCacertsSecret(ctx, k8sClient, testNs, clusterName, meshName, testNs)

				work := expectCacertsManifestWork(clusterName)
				expectCacertsSecret(work)
			})

			It("should update ManifestWork when cacerts secret is updated", func() {
				util.CreateCacertsSecret(ctx, k8sClient, testNs, clusterName, meshName, testNs)
				expectCacertsManifestWork(clusterName)

				secret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, key.Of(fmt.Sprintf("cacerts-%s", clusterName), testNs), secret)).To(Succeed())

				secret.Data["tls.crt"] = []byte("updated-cert-data")
				Expect(k8sClient.Update(ctx, secret)).To(Succeed())

				Eventually(func() string {
					work := &workv1.ManifestWork{}
					if err := k8sClient.Get(ctx, key.Of(meshcontroller.ManifestWorkNameCacerts, clusterName), work); err != nil {
						return ""
					}
					manifestSecret := &corev1.Secret{}
					if err := unmarshalManifest(work.Spec.Workload.Manifests[0], manifestSecret); err != nil {
						return ""
					}
					return string(manifestSecret.Data["tls.crt"])
				}).Should(Equal("updated-cert-data"))
			})
		})

		When("cert-manager ClusterIssuer is configured", func() {
			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpecWithKind("cluster-issuer", "ClusterIssuer"))
			})

			It("should create Certificate with ClusterIssuer kind", func() {
				expectCertificate(testNs, clusterName, meshName, "cluster-issuer", "ClusterIssuer")
			})
		})

		When("multiple clusters have cacerts secrets", func() {
			It("should create ManifestWork for each cluster", func() {
				cluster1 := util.UniqueName("cluster")
				cluster2 := util.UniqueName("cluster")

				util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))

				util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster1, meshName, testNs)
				util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster2, meshName, testNs)

				expectCacertsManifestWork(cluster1)
				expectCacertsManifestWork(cluster2)
			})
		})

		When("a cluster is removed from the ClusterSet", func() {
			It("should cleanup Certificate for that cluster", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
				expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				updateClusterSetLabel(clusterName, "")

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{},
					fmt.Sprintf("cacerts-%s", clusterName), testNs)
			})
		})

		When("issuer is removed after initial configuration", func() {
			It("should cleanup all Certificates", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
				expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.Security.Trust.CertManager.IssuerRef.Name = ""
				})

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{},
					fmt.Sprintf("cacerts-%s", clusterName), testNs)
			})
		})

		When("no issuer is configured", func() {
			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should not create cacerts ManifestWork", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoCacertsManifestWork(clusterName)
			})
		})

		When("cluster has no product claim and issuer is configured", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
			})

			It("should not create Certificate", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoCertificate(testNs, meshName)
			})

			It("should create Certificate when product claim is added", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoCertificate(testNs, meshName)

				util.SetProductClaim(ctx, k8sClient, clusterName, "Other")
				expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")
			})
		})
	})

	Context("Endpoint discovery", func() {
		var cluster1, cluster2, cluster3 string

		When("a MultiClusterMesh is created", func() {
			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster1")
				cluster2 = util.UniqueName("cluster2")
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
			})

			It("should create ManagedServiceAccount resources with default validity for each ManagedCluster in the ClusterSet", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

				msa1 := expectManagedServiceAccount(testNs, meshName, cluster1)
				msa2 := expectManagedServiceAccount(testNs, meshName, cluster2)

				Expect(msa1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(msa1.Labels[meshcontroller.MeshNameLabel]).To(Equal(meshName))
				Expect(msa1.Labels[meshcontroller.MeshNamespaceLabel]).To(Equal(testNs))
				Expect(msa1.Labels[meshcontroller.ClusterNameLabel]).To(Equal(cluster1))
				Expect(msa2.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 360 * time.Hour}))
				Expect(msa2.Labels[meshcontroller.ClusterNameLabel]).To(Equal(cluster2))
			})

			It("should create ManagedServiceAccount resources with custom TokenValidity value", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					Security: meshv1alpha1.SecurityConfig{
						Discovery: meshv1alpha1.DiscoveryConfig{
							TokenValidity: &metav1.Duration{Duration: 15 * time.Minute},
						},
					}})

				msa1 := expectManagedServiceAccount(testNs, meshName, cluster1)
				Expect(msa1.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 15 * time.Minute}))
			})

			It("should create a ManagedServiceAccount after adding a cluster to the ClusterSet", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				cluster3 = util.UniqueName("cluster3")
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster3, testClusterSet)
				expectManagedServiceAccount(testNs, meshName, cluster3)
			})

			It("should cleanup a ManagedServiceAccount after removing a cluster from the ClusterSet", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				updateClusterSetLabel(cluster2, "")
				util.ExpectResourceDeleted(ctx, k8sClient, &msav1beta1.ManagedServiceAccount{},
					expectedManagedServiceAccountName(testNs, meshName), cluster2)
			})
		})

		When("referencing an empty ClusterSet", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should not process ManagedServiceAccount", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoManagedServiceAccount(testNs, meshName, clusterName)
			})

			It("shouldn't process a cluster without clusterset label", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, "")
				expectMeshNotReady(meshName, testNs)
				expectNoManagedServiceAccount(testNs, meshName, clusterName)
			})

			It("should process a cluster when it's added", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectMeshNotReady(meshName, testNs)
				expectManagedServiceAccount(testNs, meshName, clusterName)
			})
		})

		When("two meshes target the same cluster", func() {
			var otherNs, otherMesh string

			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster1")
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				expectMeshNotReady(meshName, testNs)
				otherNs = util.UniqueName("other-ns")
				otherMesh = util.UniqueName("other-mesh")
				util.CreateNamespace(ctx, k8sClient, otherNs)
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet)
				expectMeshNotReady(otherMesh, otherNs)
			})

			It("should delete only the removed mesh's ManagedServiceAccount when one mesh is deleted", func() {
				// Verify both meshes have MSAs
				expectManagedServiceAccount(testNs, meshName, cluster1)
				expectManagedServiceAccount(otherNs, otherMesh, cluster1)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				util.ExpectResourceDeleted(ctx, k8sClient, &msav1beta1.ManagedServiceAccount{},
					expectedManagedServiceAccountName(testNs, meshName), cluster1)

				// Verify the other mesh's MSAs remain
				Consistently(func() error {
					msa := &msav1beta1.ManagedServiceAccount{}
					return k8sClient.Get(ctx, key.Of(expectedManagedServiceAccountName(otherNs, otherMesh), cluster1), msa)
				}).Should(Succeed())
			})
		})
	})

	Context("Platform detection", func() {
		DescribeTable("should detect OpenShift variants and use OSSM operator",
			func(productClaim string) {
				util.CreateOCPManagedCluster(ctx, k8sClient, clusterName, testClusterSet, productClaim)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

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
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

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
		if err := k8sClient.Get(ctx, key.Of(name, namespace), mesh); err != nil {
			return nil
		}
		return mesh.Finalizers
	}).Should(ContainElement(meshcontroller.FinalizerName))
}

func updateClusterSetLabel(clusterName, newClusterSet string) {
	cluster := &clusterv1.ManagedCluster{}
	Expect(k8sClient.Get(ctx, key.Of(clusterName), cluster)).To(Succeed())
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

// triggerReconcile forces a reconciliation by touching the mesh CR's annotations.
// Workaround for the dual-cache race (#109): the controller-runtime MW watch may
// trigger a reconcile before the WorkApplier's work informer lister syncs the update,
// causing safeToSkipApply to read stale generation and skip the patch.
// TODO(mkolesnik): Remove once WorkApplier uses the CR cache (sdk-go#226).
func triggerReconcile(meshName, namespace string) {
	updateMesh(meshName, namespace, func(mesh *meshv1alpha1.MultiClusterMesh) {
		if mesh.Annotations == nil {
			mesh.Annotations = map[string]string{}
		}
		mesh.Annotations["test.reconcile-trigger"] = mesh.ResourceVersion
	})
}

// updateMesh makes sure to retry the read-modify-write cycle in case of a conflict.
func updateMesh(meshName, namespace string, mutate func(*meshv1alpha1.MultiClusterMesh)) *meshv1alpha1.MultiClusterMesh {
	mesh := &meshv1alpha1.MultiClusterMesh{}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, key.Of(meshName, namespace), mesh); err != nil {
			return err
		}
		mutate(mesh)
		return k8sClient.Update(ctx, mesh)
	}).Should(Succeed())
	return mesh
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
		return k8sClient.Get(ctx, key.Of(name, namespace), work)
	}).Should(Succeed())
	return work
}

func expectOperatorManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.OperatorManifestWorkName, clusterNamespace)
}

func expectCacertsManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.ManifestWorkNameCacerts, clusterNamespace)
}

func expectNoCertificate(namespace, meshName string) {
	Consistently(func() []certmanagerv1.Certificate {
		certList := &certmanagerv1.CertificateList{}
		Expect(k8sClient.List(ctx, certList,
			client.InNamespace(namespace),
			client.MatchingLabels{meshcontroller.MeshNameLabel: meshName},
		)).To(Succeed())
		return certList.Items
	}).Should(BeEmpty())
}

func expectNoCacertsManifestWork(clusterNamespace string) {
	Consistently(func() bool {
		work := &workv1.ManifestWork{}
		err := k8sClient.Get(ctx, key.Of(meshcontroller.ManifestWorkNameCacerts, clusterNamespace), work)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func expectCertificate(namespace, clusterName, meshName, issuerName, issuerKind string) *certmanagerv1.Certificate {
	certList := &certmanagerv1.CertificateList{}
	Eventually(func() []certmanagerv1.Certificate {
		Expect(k8sClient.List(ctx, certList,
			client.InNamespace(namespace),
			client.MatchingLabels{
				meshcontroller.MeshNameLabel:    meshName,
				meshcontroller.ClusterNameLabel: clusterName,
			},
		)).To(Succeed())
		return certList.Items
	}).Should(HaveLen(1), "expected exactly one Certificate for cluster %s", clusterName)

	cert := &certList.Items[0]
	Expect(cert.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
	Expect(cert.Spec.SecretName).To(Equal(fmt.Sprintf("cacerts-%s", clusterName)))
	Expect(cert.Spec.IsCA).To(BeTrue())
	Expect(cert.Spec.IssuerRef.Name).To(Equal(issuerName))
	Expect(cert.Spec.IssuerRef.Kind).To(Equal(issuerKind))
	return cert
}

func expectCacertsSecret(work *workv1.ManifestWork) {
	Expect(work.Spec.Workload.Manifests).To(HaveLen(1))
	secret := &corev1.Secret{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], secret)).To(Succeed())
	Expect(secret.Name).To(Equal("cacerts"))
	Expect(secret.Namespace).To(Equal("istio-system"))
	Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))
	Expect(secret.Data).To(HaveKey("tls.crt"))
	Expect(secret.Data).To(HaveKey("tls.key"))
	Expect(secret.Data).To(HaveKey("ca.crt"))
}

func expectedManagedServiceAccountName(meshNamespace, meshName string) string {
	return fmt.Sprintf("%s-istio-reader-%s", meshNamespace, meshName)
}

func expectManagedServiceAccount(meshNamespace, meshName, clusterName string) *msav1beta1.ManagedServiceAccount {
	msa := &msav1beta1.ManagedServiceAccount{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(expectedManagedServiceAccountName(meshNamespace, meshName), clusterName), msa)
	}).Should(Succeed())
	return msa
}

// expectNoManagedServiceAccount makes sure that no ManagedServiceAccount is created for a cluster, checking consistently
func expectNoManagedServiceAccount(meshNamespace, meshName, clusterName string) {
	Consistently(func() bool {
		msa := &msav1beta1.ManagedServiceAccount{}
		err := k8sClient.Get(ctx, key.Of(expectedManagedServiceAccountName(meshNamespace, meshName), clusterName), msa)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func unmarshalManifest(manifest workv1.Manifest, into interface{}) error {
	return json.Unmarshal(manifest.Raw, into)
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

func findCondition(g Gomega, conditions []metav1.Condition, conditionType string) *metav1.Condition {
	c := meta.FindStatusCondition(conditions, conditionType)
	g.Expect(c).NotTo(BeNil(), "condition %s not found", conditionType)
	return c
}

func expectMeshNotReady(meshName, namespace string) {
	Eventually(func(g Gomega) {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, namespace), mesh)).To(Succeed())
		c := findCondition(g, mesh.Status.Conditions, meshv1alpha1.ConditionReady)
		g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(c.ObservedGeneration).To(Equal(mesh.Generation))
	}).Should(Succeed())
}

func expectMeshReady(meshName, namespace string) {
	Eventually(func(g Gomega) {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, namespace), mesh)).To(Succeed())
		c := findCondition(g, mesh.Status.Conditions, meshv1alpha1.ConditionReady)
		g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(c.ObservedGeneration).To(Equal(mesh.Generation))
	}).Should(Succeed())
}

func expectClusterOperatorConditionReason(meshName, namespace, clusterName, reason string) {
	Eventually(func(g Gomega) {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, namespace), mesh)).To(Succeed())
		for _, cs := range mesh.Status.ClusterStatus {
			if cs.ClusterName == clusterName {
				c := findCondition(g, cs.Conditions, meshv1alpha1.ConditionOperatorInstalled)
				g.Expect(c.Reason).To(Equal(reason))
				g.Expect(c.ObservedGeneration).To(Equal(mesh.Generation))
				return
			}
		}
		g.Expect(false).To(BeTrue(), "cluster %s not found in status", clusterName)
	}).Should(Succeed())
}

func expectNoClusterStatus(meshName, namespace, clusterName string) {
	Eventually(func() bool {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, key.Of(meshName, namespace), mesh); err != nil {
			return false
		}
		for _, cs := range mesh.Status.ClusterStatus {
			if cs.ClusterName == clusterName {
				return false
			}
		}
		return true
	}).Should(BeTrue())
}

func expectMeshConditionReason(meshName, namespace, conditionType, reason string) {
	Eventually(func(g Gomega) {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, namespace), mesh)).To(Succeed())
		c := findCondition(g, mesh.Status.Conditions, conditionType)
		g.Expect(c.Reason).To(Equal(reason))
		g.Expect(c.ObservedGeneration).To(Equal(mesh.Generation))
	}).Should(Succeed())
}
