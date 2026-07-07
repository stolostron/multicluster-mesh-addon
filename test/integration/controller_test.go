//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, cluster2Name, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should create ManifestWorks for each cluster", func() {
				work1 := expectOperatorManifestWork(clusterName)
				work2 := expectOperatorManifestWork(cluster2Name)

				Expect(work1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(work2.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))

				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonInstallationPending)
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

			It("should become ready after all clusters complete all phases", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)

				expectMeshNotReady(meshName, testNs)

				By("setting operator feedback on one cluster, mesh should stay not-ready")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, clusterName,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonInstallationPending)
				expectMeshNotReady(meshName, testNs)

				By("setting operator feedback on all clusters")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster2Name,
					meshcontroller.FeedbackInstalledCSV, "servicemeshoperator3.v3.0.0")

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonOperatorInstalled)

				By("simulating CP ready and gateway LB on all clusters")
				expectManifestWork(cpWorkName, clusterName)
				expectManifestWork(cpWorkName, cluster2Name)
				simulateCPReady(cpWorkName, clusterName)
				simulateCPReady(cpWorkName, cluster2Name)

				expectManifestWork(gwWorkName, clusterName)
				expectManifestWork(gwWorkName, cluster2Name)
				simulateGatewayLB(gwWorkName, clusterName, "10.0.0.1")
				simulateGatewayLB(gwWorkName, cluster2Name, "10.0.0.2")

				expectMeshReady(meshName, testNs)
			})
		})

		It("should use custom operator configuration when specified", func() {
			customConfig := meshv1alpha1.OperatorConfig{
				Name:                "sailoperator",
				Namespace:           "custom-ns",
				Channel:             "1.23",
				Source:              "custom-catalog",
				SourceNamespace:     "custom-catalog-ns",
				StartingCSV:         "sailoperator.v1.23.0",
				InstallPlanApproval: operatorsv1alpha1.ApprovalManual,
			}

			util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{Operator: customConfig})

			work := expectOperatorManifestWork(clusterName)

			Expect(work.Spec.Workload.Manifests).To(HaveLen(3))
			expectNamespace(work, 0, customConfig.Namespace)
			expectOperatorGroup(work, 1, "operator-group", customConfig.Namespace)
			expectSubscription(work, 2, customConfig)
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, otherClusterSet)
				util.CreateManagedClusterSet(ctx, k8sClient, otherClusterSet)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, "")
				expectMeshNotReady(meshName, testNs)
				expectNoManifestWorks()
			})

			It("should process a cluster when it's added", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
			})
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectFinalizer(meshName, testNs)
		})

		When("referencing a set with a cluster", func() {
			var work *workv1.ManifestWork

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
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
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], sub)).To(Succeed())
					return sub.Spec.Channel
				}).Should(Equal("tech-preview"))
			})

			It("should restore ManifestWork spec when externally modified", func() {
				sub := &operatorsv1alpha1.Subscription{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], sub)).To(Succeed())
				originalChannel := sub.Spec.Channel

				sub.Spec.Channel = "tampered"
				work.Spec.Workload.Manifests[0] = workv1.Manifest{
					RawExtension: runtime.RawExtension{Object: sub},
				}
				Expect(k8sClient.Update(ctx, work)).To(Succeed())

				// Ensure reconciliation after tamper (dual-cache race, #109).
				triggerReconcile(meshName, testNs)

				Eventually(func() string {
					work := expectOperatorManifestWork(clusterName)
					sub := &operatorsv1alpha1.Subscription{}
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], sub)).To(Succeed())
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

			util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
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
			expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
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
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
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
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
			})
		})

		When("primaryCluster is set for MultiPrimary topology", func() {
			It("should reject creation", func() {
				mesh := &meshv1alpha1.MultiClusterMesh{
					ObjectMeta: metav1.ObjectMeta{Name: meshName + "-topo", Namespace: testNs},
					Spec: meshv1alpha1.MultiClusterMeshSpec{
						ClusterSet: testClusterSet,
						ControlPlane: meshv1alpha1.ControlPlaneConfig{
							Namespace: "istio-system-topo",
						},
						Topology: meshv1alpha1.TopologyConfig{
							Type:           meshv1alpha1.TopologyMultiPrimary,
							PrimaryCluster: "some-cluster",
						},
					},
				}
				err := k8sClient.Create(ctx, mesh)
				Expect(err).To(HaveOccurred(), "expected validation error for primaryCluster with MultiPrimary")
				Expect(errors.IsInvalid(err)).To(BeTrue())
			})
		})
	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete related ManifestWorks", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
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
				longCluster := "ci-managed-cluster-with-a-long-generated-name-for-subject-test"
				util.CreateManagedCluster(ctx, k8sClient, longCluster, testClusterSet)

				cert := expectCertificate(testNs, longCluster, meshName, "mesh-issuer", "Issuer")

				Expect(cert.Spec.Subject).NotTo(BeNil())
				Expect(cert.Spec.Subject.Organizations).To(ConsistOf(meshName))
				Expect(cert.Spec.Subject.OrganizationalUnits).To(ConsistOf(longCluster))

				expectedSAN := "spiffe://" + meshName + "/cluster/" + longCluster + "/ca/istio-ca"
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
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

				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))

				util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster1, meshName, testNs)
				util.CreateCacertsSecret(ctx, k8sClient, testNs, cluster2, meshName, testNs)

				expectCacertsManifestWork(cluster1)
				expectCacertsManifestWork(cluster2)
			})
		})

		When("a cluster is removed from the ClusterSet", func() {
			It("should cleanup Certificate for that cluster", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
				expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				updateClusterSetLabel(clusterName, "")

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{},
					fmt.Sprintf("cacerts-%s", clusterName), testNs)
			})
		})

		When("issuer is removed after initial configuration", func() {
			It("should cleanup all Certificates", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should not create cacerts ManifestWork", func() {
				expectMeshNotReady(meshName, testNs)
				expectNoCacertsManifestWork(clusterName)
			})
		})

	})

	Context("Endpoint discovery", func() {
		var cluster1, cluster2, cluster3 string

		When("a MultiClusterMesh is created", func() {
			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster1")
				cluster2 = util.UniqueName("cluster2")
				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
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
				util.CreateManagedCluster(ctx, k8sClient, cluster3, testClusterSet)
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
				util.CreateManagedCluster(ctx, k8sClient, clusterName, "")
				expectMeshNotReady(meshName, testNs)
				expectNoManagedServiceAccount(testNs, meshName, clusterName)
			})

			It("should process a cluster when it's added", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectMeshNotReady(meshName, testNs)
				expectManagedServiceAccount(testNs, meshName, clusterName)
			})
		})

		When("two meshes target the same cluster", func() {
			var otherNs, otherMesh string

			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster1")
				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				expectMeshNotReady(meshName, testNs)
				otherNs = util.UniqueName("other-ns")
				otherMesh = util.UniqueName("other-mesh")
				util.CreateNamespace(ctx, k8sClient, otherNs)
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
				})
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

	Context("IstioCNI ManifestWork", func() {
		When("a mesh is created with clusters", func() {
			var cluster2Name string

			BeforeEach(func() {
				cluster2Name = util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, cluster2Name, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should create IstioCNI ManifestWork for all clusters", func() {
				work1 := expectIstioCNIManifestWork(clusterName)
				work2 := expectIstioCNIManifestWork(cluster2Name)

				Expect(work1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(work2.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
			})

			It("should have CreateOnly update strategy for the IstioCNI CR", func() {
				work := expectIstioCNIManifestWork(clusterName)

				Expect(work.Spec.ManifestConfigs).To(HaveLen(1))
				Expect(work.Spec.ManifestConfigs[0].ResourceIdentifier.Resource).To(Equal("istiocnis"))
				Expect(work.Spec.ManifestConfigs[0].ResourceIdentifier.Group).To(Equal("sailoperator.io"))
				Expect(work.Spec.ManifestConfigs[0].UpdateStrategy).NotTo(BeNil())
				Expect(work.Spec.ManifestConfigs[0].UpdateStrategy.Type).To(Equal(workv1.UpdateStrategyTypeCreateOnly))
			})

			It("should have ClusterSet-scoped labels (not mesh-owned)", func() {
				work := expectIstioCNIManifestWork(clusterName)

				Expect(work.Labels[meshcontroller.ClusterSetLabel]).To(Equal(testClusterSet))
				Expect(work.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(work.Labels).NotTo(HaveKey(meshcontroller.MeshNameLabel))
				Expect(work.Labels).NotTo(HaveKey(meshcontroller.MeshNamespaceLabel))
			})

			It("should contain IstioCNI CR and istio-cni Namespace manifests", func() {
				work := expectIstioCNIManifestWork(clusterName)

				Expect(work.Spec.Workload.Manifests).To(HaveLen(2))

				ns := &corev1.Namespace{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], ns)).To(Succeed())
				Expect(ns.Name).To(Equal("istio-cni"))

				istioCNI := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCNI)).To(Succeed())
				Expect(istioCNI.GetKind()).To(Equal("IstioCNI"))
				Expect(istioCNI.GetAPIVersion()).To(Equal("sailoperator.io/v1"))
				Expect(istioCNI.GetName()).To(Equal("default"))
			})
		})
	})

	Context("Control plane ManifestWork", func() {
		When("operator is installed and mesh uses MultiPrimary topology", func() {
			var cluster1, cluster2 string

			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster")
				cluster2 = util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

				By("simulating operator installation on both clusters")
				expectOperatorManifestWork(cluster1)
				expectOperatorManifestWork(cluster2)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster2,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			})

			It("should create CP ManifestWork with Istio CR, Namespace, and RBAC", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				work := expectManifestWork(cpWorkName, cluster1)

				Expect(work.Spec.Workload.Manifests).To(HaveLen(4))
				Expect(work.Labels[meshcontroller.MeshNameLabel]).To(Equal(meshName))
				Expect(work.Labels[meshcontroller.MeshNamespaceLabel]).To(Equal(testNs))

				ns := &corev1.Namespace{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], ns)).To(Succeed())
				Expect(ns.Name).To(Equal("istio-system"))
				Expect(ns.Labels).To(HaveKeyWithValue("topology.istio.io/network", "network-"+cluster1))

				istioCR := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCR)).To(Succeed())
				Expect(istioCR.GetKind()).To(Equal("Istio"))
				Expect(istioCR.GetAPIVersion()).To(Equal("sailoperator.io/v1"))

				cr := &rbacv1.ClusterRole{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[2], cr)).To(Succeed())
				Expect(cr.Rules).NotTo(BeEmpty())

				crb := &rbacv1.ClusterRoleBinding{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[3], crb)).To(Succeed())
				Expect(crb.RoleRef.Name).To(Equal(cr.Name))
			})

			It("should include FeedbackRules and ConditionRules for Istio CR", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				work := expectManifestWork(cpWorkName, cluster1)

				Expect(work.Spec.ManifestConfigs).To(HaveLen(1))
				cfg := work.Spec.ManifestConfigs[0]
				Expect(cfg.ResourceIdentifier.Resource).To(Equal("istios"))
				Expect(cfg.ResourceIdentifier.Group).To(Equal("sailoperator.io"))

				Expect(cfg.FeedbackRules).To(HaveLen(1))
				Expect(cfg.FeedbackRules[0].JsonPaths).To(HaveLen(1))
				Expect(cfg.FeedbackRules[0].JsonPaths[0].Name).To(Equal("readyStatus"))

				Expect(cfg.ConditionRules).To(HaveLen(1))
				Expect(cfg.ConditionRules[0].Condition).To(Equal("ControlPlaneReady"))
				Expect(cfg.ConditionRules[0].Type).To(Equal(workv1.CelConditionExpressionsType))
				Expect(cfg.ConditionRules[0].CelExpressions).NotTo(BeEmpty())
			})

			It("should create identical Istio CRs on all clusters for MultiPrimary", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				work1 := expectManifestWork(cpWorkName, cluster1)
				work2 := expectManifestWork(cpWorkName, cluster2)

				cr1 := &unstructured.Unstructured{}
				cr2 := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work1.Spec.Workload.Manifests[1], cr1)).To(Succeed())
				Expect(unmarshalManifest(work2.Spec.Workload.Manifests[1], cr2)).To(Succeed())

				spec1, _, _ := unstructured.NestedMap(cr1.Object, "spec")
				spec2, _, _ := unstructured.NestedMap(cr2.Object, "spec")

				Expect(spec1).To(HaveKey("namespace"))
				Expect(spec2).To(HaveKey("namespace"))
				Expect(spec1["namespace"]).To(Equal(spec2["namespace"]))

				_, hasProfile1, _ := unstructured.NestedString(cr1.Object, "spec", "profile")
				_, hasProfile2, _ := unstructured.NestedString(cr2.Object, "spec", "profile")
				Expect(hasProfile1).To(BeFalse(), "MultiPrimary clusters should not have a profile")
				Expect(hasProfile2).To(BeFalse(), "MultiPrimary clusters should not have a profile")

				values1, _, _ := unstructured.NestedMap(cr1.Object, "spec", "values", "global")
				values2, _, _ := unstructured.NestedMap(cr2.Object, "spec", "values", "global")
				Expect(values1).NotTo(HaveKey("remotePilotAddress"))
				Expect(values2).NotTo(HaveKey("remotePilotAddress"))
				Expect(values1).NotTo(HaveKey("externalIstiod"))
				Expect(values2).NotTo(HaveKey("externalIstiod"))
			})
		})

		When("operator is installed and mesh uses PrimaryRemote topology", func() {
			var primary, remote string

			BeforeEach(func() {
				primary = util.UniqueName("primary")
				remote = util.UniqueName("remote")
				util.CreateManagedCluster(ctx, k8sClient, primary, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, remote, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					Topology: meshv1alpha1.TopologyConfig{
						Type:           meshv1alpha1.TopologyPrimaryRemote,
						PrimaryCluster: primary,
					},
				})

				By("simulating operator installation on both clusters")
				expectOperatorManifestWork(primary)
				expectOperatorManifestWork(remote)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, primary,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, remote,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			})

			It("should set externalIstiod on primary and profile:remote on remotes", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				primaryWork := expectManifestWork(cpWorkName, primary)

				primaryCR := &unstructured.Unstructured{}
				Expect(unmarshalManifest(primaryWork.Spec.Workload.Manifests[1], primaryCR)).To(Succeed())

				externalIstiod, found, _ := unstructured.NestedBool(primaryCR.Object, "spec", "values", "global", "externalIstiod")
				Expect(found).To(BeTrue(), "primary should have externalIstiod set")
				Expect(externalIstiod).To(BeTrue())

				_, hasProfile, _ := unstructured.NestedString(primaryCR.Object, "spec", "profile")
				Expect(hasProfile).To(BeFalse(), "primary should not have a profile")

				By("simulating CP ready and gateway LB on primary to unblock remote")
				simulateCPReady(cpWorkName, primary)

				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				expectManifestWork(gwWorkName, primary)
				simulateGatewayLB(gwWorkName, primary, "10.0.0.1")

				remoteWork := expectManifestWork(cpWorkName, remote)
				remoteCR := &unstructured.Unstructured{}
				Expect(unmarshalManifest(remoteWork.Spec.Workload.Manifests[1], remoteCR)).To(Succeed())

				profile, _, _ := unstructured.NestedString(remoteCR.Object, "spec", "profile")
				Expect(profile).To(Equal("remote"))

				remotePilotAddr, found, _ := unstructured.NestedString(remoteCR.Object, "spec", "values", "global", "remotePilotAddress")
				Expect(found).To(BeTrue(), "remote should have remotePilotAddress set")
				Expect(remotePilotAddr).To(Equal("10.0.0.1"))
			})
		})
	})

	Context("Gateway ManifestWork", func() {
		When("control plane is ready", func() {
			var cluster1 string

			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

				By("simulating operator installation")
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				By("simulating control plane ready")
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, cluster1)
				simulateCPReady(cpWorkName, cluster1)
			})

			It("should create gateway ManifestWork with Deployment, Service, SA, and Gateway", func() {
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				work := expectManifestWork(gwWorkName, cluster1)

				Expect(work.Labels[meshcontroller.MeshNameLabel]).To(Equal(meshName))
				Expect(work.Labels[meshcontroller.MeshNamespaceLabel]).To(Equal(testNs))

				Expect(work.Spec.Workload.Manifests).To(HaveLen(4))

				sa := &corev1.ServiceAccount{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], sa)).To(Succeed())
				Expect(sa.Name).To(Equal("istio-eastwestgateway"))

				deploy := &appsv1.Deployment{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], deploy)).To(Succeed())
				Expect(deploy.Name).To(Equal("istio-eastwestgateway"))

				svc := &corev1.Service{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[2], svc)).To(Succeed())
				Expect(svc.Name).To(Equal("istio-eastwestgateway"))
				Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))

				gw := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[3], gw)).To(Succeed())
				Expect(gw.GetKind()).To(Equal("Gateway"))
				Expect(gw.GetName()).To(Equal("cross-network-gateway"))
			})

			It("should have SSA update strategy for Deployment and LB feedback for Service", func() {
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				work := expectManifestWork(gwWorkName, cluster1)

				var deployCfg, svcCfg *workv1.ManifestConfigOption
				for i := range work.Spec.ManifestConfigs {
					cfg := &work.Spec.ManifestConfigs[i]
					if cfg.ResourceIdentifier.Resource == "deployments" {
						deployCfg = cfg
					}
					if cfg.ResourceIdentifier.Resource == "services" {
						svcCfg = cfg
					}
				}

				Expect(deployCfg).NotTo(BeNil())
				Expect(deployCfg.UpdateStrategy).NotTo(BeNil())
				Expect(deployCfg.UpdateStrategy.Type).To(Equal(workv1.UpdateStrategyTypeServerSideApply))

				Expect(svcCfg).NotTo(BeNil())
				Expect(svcCfg.FeedbackRules).To(HaveLen(1))

				jsonPaths := svcCfg.FeedbackRules[0].JsonPaths
				pathNames := make([]string, len(jsonPaths))
				for i, p := range jsonPaths {
					pathNames[i] = p.Name
				}
				Expect(pathNames).To(ContainElements("lbIP", "lbHostname"))
			})
		})

		When("mesh uses PrimaryRemote topology", func() {
			var primary, remote string

			BeforeEach(func() {
				primary = util.UniqueName("primary")
				remote = util.UniqueName("remote")
				util.CreateManagedCluster(ctx, k8sClient, primary, testClusterSet)
				util.CreateManagedCluster(ctx, k8sClient, remote, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					Topology: meshv1alpha1.TopologyConfig{
						Type:           meshv1alpha1.TopologyPrimaryRemote,
						PrimaryCluster: primary,
					},
				})

				By("simulating operator installation on both clusters")
				expectOperatorManifestWork(primary)
				expectOperatorManifestWork(remote)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, primary,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, remote,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				By("simulating control plane ready on primary")
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, primary)
				simulateCPReady(cpWorkName, primary)
			})

			It("should include istiod Gateway and VirtualService on the primary cluster", func() {
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				work := expectManifestWork(gwWorkName, primary)

				Expect(work.Spec.Workload.Manifests).To(HaveLen(6))

				istiodGW := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[4], istiodGW)).To(Succeed())
				Expect(istiodGW.GetKind()).To(Equal("Gateway"))
				Expect(istiodGW.GetName()).To(Equal("istiod-gateway"))

				istiodVS := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[5], istiodVS)).To(Succeed())
				Expect(istiodVS.GetKind()).To(Equal("VirtualService"))
				Expect(istiodVS.GetName()).To(Equal("istiod-vs"))
			})

			It("should not include istiod Gateway and VirtualService on the remote cluster", func() {
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)

				By("simulating gateway LB on primary to unblock remote")
				expectManifestWork(gwWorkName, primary)
				simulateGatewayLB(gwWorkName, primary, "10.0.0.1")

				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, remote)
				simulateCPReady(cpWorkName, remote)

				remoteWork := expectManifestWork(gwWorkName, remote)
				Expect(remoteWork.Spec.Workload.Manifests).To(HaveLen(4))
			})
		})
	})

	Context("Cleanup: shared vs per-mesh ManifestWorks", func() {
		When("two meshes target the same ClusterSet", func() {
			var otherNs, otherMesh string

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)

				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				expectOperatorManifestWork(clusterName)
				expectIstioCNIManifestWork(clusterName)

				otherNs = util.UniqueName("other-ns")
				otherMesh = util.UniqueName("other-mesh")
				util.CreateNamespace(ctx, k8sClient, otherNs)
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
				})
				expectMeshNotReady(otherMesh, otherNs)
			})

			It("should preserve shared ManifestWorks when one mesh is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)

				Consistently(func() error {
					work := &workv1.ManifestWork{}
					return k8sClient.Get(ctx, key.Of(meshcontroller.OperatorManifestWorkName, clusterName), work)
				}).Should(Succeed())

				Consistently(func() error {
					work := &workv1.ManifestWork{}
					return k8sClient.Get(ctx, key.Of(meshcontroller.IstioCNIManifestWorkName, clusterName), work)
				}).Should(Succeed())
			})

			It("should delete shared ManifestWorks when both meshes are deleted", func() {
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
				expectAllManifestWorksDeleted()
			})
		})

		When("per-mesh ManifestWorks exist on a cluster", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

				By("simulating operator installation to trigger CP/GW phases")
				expectOperatorManifestWork(clusterName)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, clusterName,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, clusterName)
				simulateCPReady(cpWorkName, clusterName)

				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				expectManifestWork(gwWorkName, clusterName)
			})

			It("should delete per-mesh ManifestWorks when mesh is deleted", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)

				util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, cpWorkName, clusterName)
				util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, gwWorkName, clusterName)
			})
		})
	})

	Context("Status reporting", func() {
		When("phases progress through the pipeline", func() {
			var cluster1 string

			BeforeEach(func() {
				cluster1 = util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should report ControlPlaneReady and GatewayReady per-cluster conditions", func() {
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, cluster1, meshv1alpha1.ReasonInstallationPending)

				By("simulating operator installation")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				expectClusterConditionReason(meshName, testNs, cluster1, meshv1alpha1.ConditionControlPlaneReady, meshv1alpha1.ReasonControlPlaneNotReady)

				By("simulating control plane ready")
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, cluster1)
				simulateCPReady(cpWorkName, cluster1)

				expectClusterConditionReason(meshName, testNs, cluster1, meshv1alpha1.ConditionControlPlaneReady, meshv1alpha1.ReasonControlPlaneReady)
				expectClusterConditionReason(meshName, testNs, cluster1, meshv1alpha1.ConditionGatewayReady, meshv1alpha1.ReasonGatewayNotReady)

				By("simulating gateway LB ready")
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				expectManifestWork(gwWorkName, cluster1)
				simulateGatewayLB(gwWorkName, cluster1, "10.0.0.1")

				expectClusterConditionReason(meshName, testNs, cluster1, meshv1alpha1.ConditionGatewayReady, meshv1alpha1.ReasonGatewayReady)
			})
		})

		When("PrimaryRemote topology is used", func() {
			var primary string

			BeforeEach(func() {
				primary = util.UniqueName("primary")
				util.CreateManagedCluster(ctx, k8sClient, primary, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					Topology: meshv1alpha1.TopologyConfig{
						Type:           meshv1alpha1.TopologyPrimaryRemote,
						PrimaryCluster: primary,
					},
				})

				By("progressing through all phases on primary")
				expectOperatorManifestWork(primary)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, primary,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				expectManifestWork(cpWorkName, primary)
				simulateCPReady(cpWorkName, primary)

				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				expectManifestWork(gwWorkName, primary)
				simulateGatewayLB(gwWorkName, primary, "10.0.0.99")
			})

			It("should report PrimaryGatewayAddress in mesh status", func() {
				Eventually(func(g Gomega) {
					mesh := &meshv1alpha1.MultiClusterMesh{}
					g.Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mesh)).To(Succeed())
					g.Expect(mesh.Status.PrimaryGatewayAddress).To(Equal("10.0.0.99"))
				}).Should(Succeed())
			})
		})
	})

	Context("Template source: ConfigMap", func() {
		var cluster1 string

		BeforeEach(func() {
			cluster1 = clusterName
			util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
		})

		When("a valid ConfigMap template is referenced", func() {
			BeforeEach(func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "istio-template", Namespace: testNs},
					Data: map[string]string{
						"istio.yaml": `apiVersion: sailoperator.io/v1
kind: Istio
spec:
  values:
    pilot:
      resources:
        requests:
          cpu: 500m
          memory: 2Gi
    meshConfig:
      accessLogFile: /dev/stdout
`,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())

				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{
						TemplateSource: &meshv1alpha1.TemplateSourceConfig{
							ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "istio-template"},
						},
					},
				})
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			})

			It("should include user values AND controller-managed fields in CP ManifestWork", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				work := expectManifestWork(cpWorkName, cluster1)

				istioCR := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCR)).To(Succeed())

				meshID, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "global", "meshID")
				Expect(meshID).To(Equal(testNs + "-" + meshName))

				trustDomain, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "meshConfig", "trustDomain")
				Expect(trustDomain).To(Equal(meshName))

				cpu, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "pilot", "resources", "requests", "cpu")
				Expect(cpu).To(Equal("500m"))

				accessLog, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "meshConfig", "accessLogFile")
				Expect(accessLog).To(Equal("/dev/stdout"))
			})
		})

		When("a ConfigMap template has conflicting controller-managed fields", func() {
			BeforeEach(func() {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-template", Namespace: testNs},
					Data: map[string]string{
						"istio.yaml": `apiVersion: sailoperator.io/v1
kind: Istio
spec:
  values:
    global:
      meshID: wrong-id
    meshConfig:
      trustDomain: wrong-domain
`,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())

				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{
						TemplateSource: &meshv1alpha1.TemplateSourceConfig{
							ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "bad-template"},
						},
					},
				})
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			})

			It("should override user meshID and trustDomain with controller values", func() {
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				work := expectManifestWork(cpWorkName, cluster1)

				istioCR := &unstructured.Unstructured{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCR)).To(Succeed())

				meshID, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "global", "meshID")
				Expect(meshID).To(Equal(testNs + "-" + meshName))

				trustDomain, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "meshConfig", "trustDomain")
				Expect(trustDomain).To(Equal(meshName))
			})
		})

		When("a ConfigMap is missing", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{
						TemplateSource: &meshv1alpha1.TemplateSourceConfig{
							ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "nonexistent"},
						},
					},
				})
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			})

			It("should set Ready=False with TemplateSourceUnavailable", func() {
				expectMeshConditionReason(meshName, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonTemplateSourceUnavailable)
			})
		})
	})

	Context("Template source: None mode", func() {
		var cluster1 string

		BeforeEach(func() {
			cluster1 = clusterName
			util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
		})

		When("none template source is set", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{
						TemplateSource: &meshv1alpha1.TemplateSourceConfig{
							None: &meshv1alpha1.NoneTemplateSource{},
						},
					},
				})
			})

			It("should create operator ManifestWork but no CP/GW/RS ManifestWorks even after operator is installed", func() {
				By("waiting for operator ManifestWork and simulating installation")
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				By("verifying CP/GW/RS ManifestWorks are NOT created despite operator being installed")
				cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
				gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)
				rsWorkName := fmt.Sprintf("multicluster-mesh-rs-%s-%s", testNs, meshName)

				Consistently(func() bool {
					work := &workv1.ManifestWork{}
					cpErr := k8sClient.Get(ctx, key.Of(cpWorkName, cluster1), work)
					gwErr := k8sClient.Get(ctx, key.Of(gwWorkName, cluster1), work)
					rsErr := k8sClient.Get(ctx, key.Of(rsWorkName, cluster1), work)
					return errors.IsNotFound(cpErr) && errors.IsNotFound(gwErr) && errors.IsNotFound(rsErr)
				}).Should(BeTrue())
			})

			It("should only report OperatorInstalled condition per cluster", func() {
				By("verifying mesh starts not-ready before operator feedback")
				expectMeshNotReady(meshName, testNs)

				By("simulating operator installation")
				expectOperatorManifestWork(cluster1)
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, cluster1,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				By("verifying mesh becomes ready with only OperatorInstalled condition")
				expectMeshReady(meshName, testNs)
				expectClusterConditionReason(meshName, testNs, cluster1,
					meshv1alpha1.ConditionOperatorInstalled, meshv1alpha1.ReasonOperatorInstalled)

				Eventually(func(g Gomega) {
					mesh := &meshv1alpha1.MultiClusterMesh{}
					g.Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mesh)).To(Succeed())
					for _, cs := range mesh.Status.ClusterStatus {
						if cs.ClusterName == cluster1 {
							for _, cond := range cs.Conditions {
								g.Expect(cond.Type).NotTo(BeElementOf(
									meshv1alpha1.ConditionControlPlaneReady,
									meshv1alpha1.ConditionGatewayReady,
									meshv1alpha1.ConditionDiscoveryReady,
								))
							}
						}
					}
				}).Should(Succeed())
			})
		})
	})

	Context("ConfigMap change triggers reconciliation", func() {
		var cluster1 string

		BeforeEach(func() {
			cluster1 = clusterName
			util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
		})

		It("should update ManifestWork when ConfigMap content changes", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "template-v1", Namespace: testNs},
				Data: map[string]string{
					"istio.yaml": "apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      resources:\n        requests:\n          cpu: 100m\n",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{
					TemplateSource: &meshv1alpha1.TemplateSourceConfig{
						ConfigMapRef: &meshv1alpha1.ConfigMapTemplateRef{Name: "template-v1"},
					},
				},
			})
			expectOperatorManifestWork(cluster1)
			util.SetManifestWorkFeedback(ctx, k8sClient,
				meshcontroller.OperatorManifestWorkName, cluster1,
				meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

			cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
			work := expectManifestWork(cpWorkName, cluster1)
			istioCR := &unstructured.Unstructured{}
			Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCR)).To(Succeed())
			cpu, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "pilot", "resources", "requests", "cpu")
			Expect(cpu).To(Equal("100m"))

			By("updating ConfigMap with new values")
			Expect(k8sClient.Get(ctx, key.Of("template-v1", testNs), cm)).To(Succeed())
			cm.Data["istio.yaml"] = "apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      resources:\n        requests:\n          cpu: 2000m\n"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("verifying ManifestWork is updated with new CPU value")
			Eventually(func(g Gomega) {
				w := &workv1.ManifestWork{}
				g.Expect(k8sClient.Get(ctx, key.Of(cpWorkName, cluster1), w)).To(Succeed())
				cr := &unstructured.Unstructured{}
				g.Expect(unmarshalManifest(w.Spec.Workload.Manifests[1], cr)).To(Succeed())
				v, _, _ := unstructured.NestedString(cr.Object, "spec", "values", "pilot", "resources", "requests", "cpu")
				g.Expect(v).To(Equal("2000m"))
			}).Should(Succeed())
		})
	})

	Context("Mode transitions", func() {
		var cluster1 string

		BeforeEach(func() {
			cluster1 = clusterName
			util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
		})

		It("should tear down mesh ManifestWorks when switching from Basic to None", func() {
			cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
			gwWorkName := fmt.Sprintf("multicluster-mesh-gw-%s-%s", testNs, meshName)

			By("creating mesh in Basic mode and waiting for CP ManifestWork")
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectOperatorManifestWork(cluster1)
			util.SetManifestWorkFeedback(ctx, k8sClient,
				meshcontroller.OperatorManifestWorkName, cluster1,
				meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			expectManifestWork(cpWorkName, cluster1)

			By("switching to None mode")
			mesh := &meshv1alpha1.MultiClusterMesh{}
			Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mesh)).To(Succeed())
			mesh.Spec.ControlPlane.TemplateSource = &meshv1alpha1.TemplateSourceConfig{
				None: &meshv1alpha1.NoneTemplateSource{},
			}
			Expect(k8sClient.Update(ctx, mesh)).To(Succeed())

			By("verifying CP and GW ManifestWorks are deleted")
			Eventually(func() bool {
				w := &workv1.ManifestWork{}
				return errors.IsNotFound(k8sClient.Get(ctx, key.Of(cpWorkName, cluster1), w)) &&
					errors.IsNotFound(k8sClient.Get(ctx, key.Of(gwWorkName, cluster1), w))
			}).Should(BeTrue())

			By("verifying operator ManifestWork still exists")
			expectOperatorManifestWork(cluster1)
		})

		It("should create mesh ManifestWorks when switching from None to Basic", func() {
			cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)

			By("creating mesh in None mode")
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{
					TemplateSource: &meshv1alpha1.TemplateSourceConfig{
						None: &meshv1alpha1.NoneTemplateSource{},
					},
				},
			})
			expectOperatorManifestWork(cluster1)
			util.SetManifestWorkFeedback(ctx, k8sClient,
				meshcontroller.OperatorManifestWorkName, cluster1,
				meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")
			expectMeshReady(meshName, testNs)

			Consistently(func() bool {
				w := &workv1.ManifestWork{}
				return errors.IsNotFound(k8sClient.Get(ctx, key.Of(cpWorkName, cluster1), w))
			}).Should(BeTrue())

			By("switching to Basic mode")
			mesh := &meshv1alpha1.MultiClusterMesh{}
			Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mesh)).To(Succeed())
			mesh.Spec.ControlPlane.TemplateSource = nil
			Expect(k8sClient.Update(ctx, mesh)).To(Succeed())

			By("verifying CP ManifestWork is created")
			expectManifestWork(cpWorkName, cluster1)
		})
	})

	Context("Template source: Git", func() {
		var cluster1 string

		BeforeEach(func() {
			cluster1 = clusterName
			util.CreateManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
		})

		It("should resolve template from a local git repo", func() {
			repoDir := initIntegrationGitRepo("istio.yaml",
				"apiVersion: sailoperator.io/v1\nkind: Istio\nspec:\n  values:\n    pilot:\n      resources:\n        requests:\n          cpu: 750m\n")

			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{
					TemplateSource: &meshv1alpha1.TemplateSourceConfig{
						Git: &meshv1alpha1.GitTemplateSource{
							URL:  repoDir,
							Path: "istio.yaml",
							Ref:  &meshv1alpha1.GitRef{Branch: "master"},
						},
					},
				},
			})
			expectOperatorManifestWork(cluster1)
			util.SetManifestWorkFeedback(ctx, k8sClient,
				meshcontroller.OperatorManifestWorkName, cluster1,
				meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

			cpWorkName := fmt.Sprintf("multicluster-mesh-cp-%s-%s", testNs, meshName)
			work := expectManifestWork(cpWorkName, cluster1)

			istioCR := &unstructured.Unstructured{}
			Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], istioCR)).To(Succeed())

			cpu, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "pilot", "resources", "requests", "cpu")
			Expect(cpu).To(Equal("750m"))

			meshID, _, _ := unstructured.NestedString(istioCR.Object, "spec", "values", "global", "meshID")
			Expect(meshID).To(Equal(testNs + "-" + meshName))
		})
	})
})

// initIntegrationGitRepo creates a local git repo for integration tests.
func initIntegrationGitRepo(filePath, content string) string {
	dir, err := os.MkdirTemp("", "git-template-test-*")
	Expect(err).NotTo(HaveOccurred())

	repo, err := git.PlainInit(dir, false)
	Expect(err).NotTo(HaveOccurred())

	wt, err := repo.Worktree()
	Expect(err).NotTo(HaveOccurred())

	fullPath := filepath.Join(dir, filePath)
	Expect(os.MkdirAll(filepath.Dir(fullPath), 0o755)).To(Succeed())
	Expect(os.WriteFile(fullPath, []byte(content), 0o644)).To(Succeed())

	_, err = wt.Add(filePath)
	Expect(err).NotTo(HaveOccurred())

	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	Expect(err).NotTo(HaveOccurred())

	return dir
}

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

func expectIstioCNIManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.IstioCNIManifestWorkName, clusterNamespace)
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

func unmarshalManifest(manifest workv1.Manifest, into any) error {
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

func expectSubscription(work *workv1.ManifestWork, index int, expected meshv1alpha1.OperatorConfig) {
	sub := &operatorsv1alpha1.Subscription{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], sub)).To(Succeed())

	Expect(sub.Name).To(Equal(expected.Name))
	Expect(sub.Namespace).To(Equal(expected.Namespace))
	Expect(sub.Spec.Package).To(Equal(expected.Name))
	Expect(sub.Spec.CatalogSource).To(Equal(expected.Source))
	Expect(sub.Spec.CatalogSourceNamespace).To(Equal(expected.SourceNamespace))
	Expect(sub.Spec.Channel).To(Equal(expected.Channel))
	Expect(sub.Spec.InstallPlanApproval).To(Equal(expected.InstallPlanApproval))
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

func expectClusterConditionReason(meshName, namespace, clusterName, conditionType, reason string) {
	Eventually(func(g Gomega) {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, namespace), mesh)).To(Succeed())
		for _, cs := range mesh.Status.ClusterStatus {
			if cs.ClusterName == clusterName {
				c := findCondition(g, cs.Conditions, conditionType)
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

// simulateCPReady sets the ControlPlaneReady condition on the CP ManifestWork,
// mimicking what the OCM work agent would do after processing CEL ConditionRules.
func simulateCPReady(cpWorkName, clusterNamespace string) {
	work := &workv1.ManifestWork{}
	Expect(k8sClient.Get(ctx, key.Of(cpWorkName, clusterNamespace), work)).To(Succeed())
	work.Status.ResourceStatus = workv1.ManifestResourceStatus{
		Manifests: []workv1.ManifestCondition{{
			Conditions: []metav1.Condition{{
				Type:               "ControlPlaneReady",
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				LastTransitionTime: metav1.Now(),
			}},
		}},
	}
	Expect(k8sClient.Status().Update(ctx, work)).To(Succeed())
}

// simulateGatewayLB sets the LB IP feedback on the gateway ManifestWork's Service,
// mimicking what the OCM work agent would report from the spoke cluster.
func simulateGatewayLB(gwWorkName, clusterNamespace, ip string) {
	work := &workv1.ManifestWork{}
	Expect(k8sClient.Get(ctx, key.Of(gwWorkName, clusterNamespace), work)).To(Succeed())
	work.Status.ResourceStatus = workv1.ManifestResourceStatus{
		Manifests: []workv1.ManifestCondition{{
			Conditions: []metav1.Condition{{
				Type:               "Applied",
				Status:             metav1.ConditionTrue,
				Reason:             "Applied",
				LastTransitionTime: metav1.Now(),
			}},
			ResourceMeta: workv1.ManifestResourceMeta{
				Kind: "Service",
			},
			StatusFeedbacks: workv1.StatusFeedbackResult{
				Values: []workv1.FeedbackValue{{
					Name: "lbIP",
					Value: workv1.FieldValue{
						Type:   workv1.String,
						String: &ip,
					},
				}},
			},
		}},
	}
	Expect(k8sClient.Status().Update(ctx, work)).To(Succeed())
}
