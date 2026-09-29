//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

var _ = Describe("MultiClusterMesh Controller", func() {
	var (
		testNs        string
		testPlacement string
		meshName      string
		clusterName   string
	)

	BeforeEach(func() {
		testNs = util.UniqueName("test-ns")
		testPlacement = util.UniqueName("placement")
		meshName = util.UniqueName("mesh")
		clusterName = util.UniqueName("cluster")

		util.CreateNamespace(ctx, k8sClient, testNs)
	})

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

		pdList := &clusterv1beta1.PlacementDecisionList{}
		_ = k8sClient.List(ctx, pdList)
		for i := range pdList.Items {
			_ = k8sClient.Delete(ctx, &pdList.Items[i])
		}

		placementList := &clusterv1beta1.PlacementList{}
		_ = k8sClient.List(ctx, placementList)
		for i := range placementList.Items {
			_ = k8sClient.Delete(ctx, &placementList.Items[i])
		}

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNs}}
		_ = k8sClient.Delete(ctx, ns)
	})

	Context("Basic reconciliation", func() {
		When("two clusters exist", func() {
			var cluster2Name string

			BeforeEach(func() {
				cluster2Name = util.UniqueName("cluster")

				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreateManagedCluster(ctx, k8sClient, cluster2Name)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2Name)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			})

			It("should create ManifestWorks for each cluster", func() {
				cpNsWork1, _ := expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")
				cpNsWork2, _ := expectControlPlaneNamespaceManifestWork(cluster2Name, "istio-system")

				expectMeshOwnedLabels(cpNsWork1.Labels, meshName, testNs, clusterName)
				expectMeshOwnedLabels(cpNsWork2.Labels, meshName, testNs, cluster2Name)

				work1 := expectOperatorManifestWork(clusterName)
				work2 := expectOperatorManifestWork(cluster2Name)

				Expect(work1.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(work2.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))

				expectOLMClusterRole(work1, 0)
				expectOLMClusterRole(work2, 0)

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

			It("should become ready after all clusters confirm operator installation", func() {
				expectMeshNotReady(meshName, testNs)

				By("setting feedback on one cluster, mesh should stay not-ready")
				util.SetManifestWorkFeedback(ctx, k8sClient,
					meshcontroller.OperatorManifestWorkName, clusterName,
					meshcontroller.FeedbackInstalledCSV, "sailoperator.v1.0.0")

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonInstallationPending)
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

			util.CreateManagedCluster(ctx, k8sClient, clusterName)
			util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
			util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{Operator: customConfig})

			work := expectOperatorManifestWork(clusterName)

			Expect(work.Spec.Workload.Manifests).To(HaveLen(4))
			expectOLMClusterRole(work, 0)
			expectNamespace(work, 1, customConfig.Namespace)
			expectOperatorGroup(work, 2, "operator-group", customConfig.Namespace)
			expectSubscription(work, 3, customConfig)
		})

		When("referencing a non-existent Placement", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, "nonexistent-placement")
			})

			It("should report PlacementNotFound", func() {
				expectMeshConditionReason(meshName, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonPlacementNotFound)
				expectNoManifestWorks()
			})

			It("should reconcile when the Placement is created with clusters", func() {
				expectMeshConditionReason(meshName, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonPlacementNotFound)
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, "nonexistent-placement", testNs)
				util.CreatePlacementDecision(ctx, k8sClient, "nonexistent-placement", testNs, clusterName)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
			})
		})

		When("Placement selects no clusters", func() {
			BeforeEach(func() {
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			})

			It("should report NoClustersSelected", func() {
				expectMeshConditionReason(meshName, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNoClustersSelected)
				expectNoManifestWorks()
			})

			It("should process a cluster when it's added to the PlacementDecision", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				expectOperatorManifestWork(clusterName)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
			})
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
			util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			expectFinalizer(meshName, testNs)
		})

		Context("Control plane namespace", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
			})

			It("should use custom control plane namespace when specified", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
				})

				expectControlPlaneNamespaceManifestWork(clusterName, "istio-system-2")
			})

			It("should default network label to cluster name", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)

				_, ns := expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")
				Expect(ns.Labels[meshcontroller.IstioNetworkLabel]).To(Equal(clusterName))
			})

			It("should use network label from ManagedCluster when set", func() {
				updateClusterLabel(clusterName, meshcontroller.IstioNetworkLabel, "network-east")
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)

				_, ns := expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")
				Expect(ns.Labels[meshcontroller.IstioNetworkLabel]).To(Equal("network-east"))
			})

			It("should sync network label when ManagedCluster label is updated after mesh creation", func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")

				updateClusterLabel(clusterName, meshcontroller.IstioNetworkLabel, "network-west")

				Eventually(func() string {
					_, ns := expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")
					return ns.Labels[meshcontroller.IstioNetworkLabel]
				}).Should(Equal("network-west"))
			})
		})

		When("referencing a Placement with a cluster", func() {
			var work *workv1.ManifestWork

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				work = expectOperatorManifestWork(clusterName)
			})

			It("should not update operator ManifestWork when operator config hasn't changed", func() {
				originalVersion := work.ResourceVersion

				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					metav1.SetMetaDataLabel(&mesh.ObjectMeta, "trigger", "reconcile")
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
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[3], sub)).To(Succeed())
					return sub.Spec.Channel
				}).Should(Equal("tech-preview"))
			})

			It("should restore ManifestWork spec when externally modified", func() {
				sub := &operatorsv1alpha1.Subscription{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[3], sub)).To(Succeed())
				originalChannel := sub.Spec.Channel

				sub.Spec.Channel = "tampered"
				work.Spec.Workload.Manifests[3] = workv1.Manifest{
					RawExtension: runtime.RawExtension{Object: sub},
				}
				Expect(k8sClient.Update(ctx, work)).To(Succeed())

				triggerReconcile(meshName, testNs)

				Eventually(func() string {
					work := expectOperatorManifestWork(clusterName)
					sub := &operatorsv1alpha1.Subscription{}
					Expect(unmarshalManifest(work.Spec.Workload.Manifests[3], sub)).To(Succeed())
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

			It("should cleanup ManifestWork when the cluster is removed from PlacementDecision", func() {
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
				expectAllManifestWorksDeleted()
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should cleanup ManifestWork when the cluster is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1.ManagedCluster{}, clusterName, "")
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

			When("two meshes target the same cluster", func() {
				var otherNs, otherMesh string

				BeforeEach(func() {
					otherNs = util.UniqueName("other-ns")
					otherMesh = util.UniqueName("other-mesh")
					util.CreateNamespace(ctx, k8sClient, otherNs)
					otherPlacement := util.UniqueName("placement")
					util.CreatePlacement(ctx, k8sClient, otherPlacement, otherNs)
					util.CreatePlacementDecision(ctx, k8sClient, otherPlacement, otherNs, clusterName)
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, otherPlacement, meshv1alpha1.MultiClusterMeshSpec{
						ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
					})
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

			util.CreateManagedCluster(ctx, k8sClient, clusterName)
			util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
			util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			expectOperatorManifestWork(clusterName)
		})

		When("metadata.name exceeds 63 characters", func() {
			It("should reject creation", func() {
				expectInvalidCreateMeshFailure(
					"a-mesh-name-that-is-way-too-long-and-exceeds-the-sixty-three-character-limit", testNs,
					meshv1alpha1.MultiClusterMeshSpec{PlacementRef: meshv1alpha1.PlacementReference{Name: testPlacement}},
					"metadata.name must not exceed 63 characters")
			})
		})

		When("spec.placementRef.name is empty", func() {
			It("should reject creation", func() {
				expectInvalidCreateMeshFailure(meshName+"-empty", testNs,
					meshv1alpha1.MultiClusterMeshSpec{PlacementRef: meshv1alpha1.PlacementReference{Name: ""}},
					"spec.placementRef.name")
			})
		})

		When("spec.controlPlane.namespace is changed on update", func() {
			It("should reject the update", func() {
				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
				mesh.Spec.ControlPlane.Namespace = "different-ns"
				err := k8sClient.Update(ctx, mesh)
				Expect(err).To(HaveOccurred(), "expected validation error when updating the controlPlane namespace")
				Expect(errors.IsInvalid(err)).To(BeTrue())
			})
		})

		DescribeTable("should reject reserved operator namespace",
			func(ns, expectedMessage string) {
				expectInvalidCreateMeshFailure(meshName+"-ns", testNs,
					meshv1alpha1.MultiClusterMeshSpec{
						PlacementRef: meshv1alpha1.PlacementReference{Name: testPlacement},
						Operator:     meshv1alpha1.OperatorConfig{Namespace: ns},
					}, expectedMessage)
			},
			Entry("openshift-operators", "openshift-operators", "openshift-"),
			Entry("openshift-monitoring", "openshift-monitoring", "openshift-"),
			Entry("kube-system", "kube-system", "kube-"),
			Entry("kube-public", "kube-public", "kube-"),
			Entry("default", "default", "'default'"),
		)

		It("should block mesh when control plane namespace equals operator namespace", func() {
			conflictMesh := meshName + "-cpns"
			util.CreateMultiClusterMesh(ctx, k8sClient, conflictMesh, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "multicluster-mesh-operator"},
			})
			expectMeshConditionReason(conflictMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)
		})

		It("should block mesh when operator namespace equals default control plane namespace", func() {
			conflictMesh := meshName + "-opns"
			util.CreateMultiClusterMesh(ctx, k8sClient, conflictMesh, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
				Operator: meshv1alpha1.OperatorConfig{Namespace: "istio-system"},
			})
			expectMeshConditionReason(conflictMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)
		})

		It("should allow two meshes with different control plane namespaces", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
			})

			expectMeshNotReady(otherMesh, testNs)
			expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonInstallationPending)
		})

		When("a newer mesh has a conflicting operator config", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
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
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testPlacement)
			})

			It("should block the newer mesh", func() {
				expectMeshConditionReason(otherMesh, testNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonNamespaceConflict)
			})

			It("should detect conflict when one mesh uses the default namespace explicitly", func() {
				thirdMesh := meshName + "-3"
				util.CreateMultiClusterMesh(ctx, k8sClient, thirdMesh, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
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

		It("should unblock a cross-namespace mesh when the conflicting mesh is deleted", func() {
			// Append "z" without a dash so the cross-mesh's key sorts after
			// the first mesh's key. isOlderMesh uses namespace/name as a
			// tiebreaker when timestamps fall within the same second, and
			// "-" (ASCII 45) sorts before "/" (ASCII 47) in the compound key.
			crossNs := testNs + "z"
			util.CreateNamespace(ctx, k8sClient, crossNs)
			crossPlacement := testPlacement + "z"
			util.CreatePlacement(ctx, k8sClient, crossPlacement, crossNs)
			util.CreatePlacementDecision(ctx, k8sClient, crossPlacement, crossNs, clusterName)

			crossMesh := meshName + "-cross"
			util.CreateMultiClusterMesh(ctx, k8sClient, crossMesh, crossNs, crossPlacement, meshv1alpha1.MultiClusterMeshSpec{
				ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
				Operator:     meshv1alpha1.OperatorConfig{Channel: "different-channel"},
			})
			expectMeshConditionReason(crossMesh, crossNs, meshv1alpha1.ConditionReady, meshv1alpha1.ReasonOperatorConfigConflict)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
			expectClusterOperatorConditionReason(crossMesh, crossNs, clusterName, meshv1alpha1.ReasonInstallationPending)
		})
	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete related ManifestWorks", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateManagedCluster(ctx, k8sClient, clusterName)
			util.CreateManagedCluster(ctx, k8sClient, cluster2)
			util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
			util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			expectFinalizer(meshName, testNs)
			expectOperatorManifestWork(clusterName)
			expectOperatorManifestWork(cluster2)
			expectControlPlaneNamespaceManifestWork(clusterName, "istio-system")
			expectControlPlaneNamespaceManifestWork(cluster2, "istio-system")

			cpNsMWName := meshcontroller.ManifestWorkNameCPNSPrefix + "istio-system"
			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, clusterName)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, cluster2)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, cpNsMWName, clusterName)
			util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, cpNsMWName, cluster2)
		})

		It("should work when Placement doesn't exist", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, "nonexistent-placement")
			expectFinalizer(meshName, testNs)

			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
		})
	})

	Context("Certificate distribution", func() {
		When("cert-manager issuer is configured", func() {
			var mesh *meshv1alpha1.MultiClusterMesh

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				mesh = util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, util.CertManagerSpec("mesh-issuer"))
			})

			It("should create Certificate resource with owner reference", func() {
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

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
				util.CreateManagedCluster(ctx, k8sClient, longCluster)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, longCluster)

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
				util.CreateCacertsSecret(ctx, k8sClient, mesh, clusterName)

				work := expectCacertsManifestWork(mesh, clusterName)
				expectCacertsSecretManifest(work, "istio-system")
			})

			It("should update ManifestWork when cacerts secret is updated", func() {
				secret := util.CreateCacertsSecret(ctx, k8sClient, mesh, clusterName)
				expectCacertsManifestWork(mesh, clusterName)

				secret.Data["tls.crt"] = []byte("updated-cert-data")
				Expect(k8sClient.Update(ctx, secret)).To(Succeed())

				Eventually(func() string {
					work := &workv1.ManifestWork{}
					if err := k8sClient.Get(ctx, key.Of(meshcontroller.CacertsManifestWorkName(mesh), clusterName), work); err != nil {
						return ""
					}
					manifestSecret := &corev1.Secret{}
					if err := unmarshalManifest(work.Spec.Workload.Manifests[0], manifestSecret); err != nil {
						return ""
					}
					return string(manifestSecret.Data["tls.crt"])
				}).Should(Equal("updated-cert-data"))
			})

			When("another mesh is targeting the same cluster", func() {
				var otherMeshName string
				var otherMesh *meshv1alpha1.MultiClusterMesh

				BeforeEach(func() {
					otherMeshName = util.UniqueName("other-mesh")
					otherSpec := util.CertManagerSpec("mesh-issuer")
					otherSpec.ControlPlane = meshv1alpha1.ControlPlaneConfig{Namespace: "other-istio-system"}

					otherMesh = util.CreateMultiClusterMesh(ctx, k8sClient, otherMeshName, testNs, testPlacement, otherSpec)
				})

				It("should have distinct names for certificates", func() {
					cert1 := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")
					cert2 := expectCertificate(testNs, clusterName, otherMeshName, "mesh-issuer", "Issuer")
					Expect(cert1.Name).NotTo(Equal(cert2.Name))
				})

				It("should have distinct names for secret ManifestWorks", func() {
					util.CreateCacertsSecret(ctx, k8sClient, mesh, clusterName)
					util.CreateCacertsSecret(ctx, k8sClient, otherMesh, clusterName)

					cacertsWork1 := expectCacertsManifestWork(mesh, clusterName)
					cacertsWork2 := expectCacertsManifestWork(otherMesh, clusterName)
					Expect(cacertsWork1.Name).NotTo(Equal(cacertsWork2.Name))
				})
			})
		})

		When("cert-manager ClusterIssuer is configured", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, util.CertManagerSpecWithKind("cluster-issuer", "ClusterIssuer"))
			})

			It("should create Certificate with ClusterIssuer kind", func() {
				expectCertificate(testNs, clusterName, meshName, "cluster-issuer", "ClusterIssuer")
			})
		})

		When("multiple clusters have cacerts secrets", func() {
			It("should create ManifestWork for each cluster", func() {
				cluster1 := util.UniqueName("cluster")
				cluster2 := util.UniqueName("cluster")

				util.CreateManagedCluster(ctx, k8sClient, cluster1)
				util.CreateManagedCluster(ctx, k8sClient, cluster2)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, cluster1, cluster2)
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, util.CertManagerSpec("mesh-issuer"))

				util.CreateCacertsSecret(ctx, k8sClient, mesh, cluster1)
				util.CreateCacertsSecret(ctx, k8sClient, mesh, cluster2)

				expectCacertsManifestWork(mesh, cluster1)
				expectCacertsManifestWork(mesh, cluster2)
			})
		})

		When("a cluster is removed from PlacementDecision", func() {
			It("should cleanup Certificate for that cluster", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, util.CertManagerSpec("mesh-issuer"))
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs)

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{}, cert.Name, testNs)
			})
		})

		When("issuer is removed after initial configuration", func() {
			It("should cleanup all Certificates", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, util.CertManagerSpec("mesh-issuer"))
				cert := expectCertificate(testNs, clusterName, meshName, "mesh-issuer", "Issuer")

				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.Security.Trust.CertManager.IssuerRef.Name = ""
				})

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{}, cert.Name, testNs)
			})
		})

		When("no issuer is configured", func() {
			It("should not create cacerts ManifestWork", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)

				expectMeshNotReady(meshName, testNs)
				expectNoCacertsManifestWork(mesh, clusterName)
			})
		})
	})

	Context("Endpoint discovery", func() {
		When("referencing a Placement with a cluster", func() {
			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
			})

			It("should create ManagedServiceAccount with correct labels and default validity", func() {
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				msa := expectManagedServiceAccount(mesh, clusterName)

				expectMeshOwnedLabels(msa.Labels, meshName, testNs, clusterName)
				Expect(msa.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 360 * time.Hour}))
			})

			It("should create ManagedServiceAccount with custom TokenValidity when specified", func() {
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
					Security: meshv1alpha1.SecurityConfig{
						Discovery: meshv1alpha1.DiscoveryConfig{
							TokenValidity: &metav1.Duration{Duration: 15 * time.Minute},
						},
					},
				})

				msa := expectManagedServiceAccount(mesh, clusterName)
				Expect(msa.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 15 * time.Minute}))
			})

			It("should create ManagedServiceAccount for newly added cluster", func() {
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				expectManagedServiceAccount(mesh, clusterName)

				cluster2 := util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster2)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2)
				expectManagedServiceAccount(mesh, cluster2)
			})

			It("should create istio-reader ManifestWork with ClusterRole and ClusterRoleBinding", func() {
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)

				work := expectIstioReaderManifestWork(mesh, clusterName)
				expectMeshOwnedLabels(work.Labels, meshName, testNs, clusterName)
				Expect(work.Spec.Workload.Manifests).To(HaveLen(2))

				cr := &rbacv1.ClusterRole{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], cr)).To(Succeed())
				expectedName := meshcontroller.IstioReaderName(mesh)
				Expect(cr.Name).To(Equal(expectedName))
				expectIstioReaderRules(cr.Rules)

				crb := &rbacv1.ClusterRoleBinding{}
				Expect(unmarshalManifest(work.Spec.Workload.Manifests[1], crb)).To(Succeed())
				Expect(crb.Name).To(Equal(expectedName))
				Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
				Expect(crb.RoleRef.Name).To(Equal(expectedName))
				Expect(crb.Subjects).To(HaveLen(1))
				Expect(crb.Subjects[0].Kind).To(Equal("ServiceAccount"))
				Expect(crb.Subjects[0].Name).To(Equal(meshcontroller.EndpointDiscoveryName(mesh)))
				Expect(crb.Subjects[0].Namespace).To(Equal(meshcontroller.MSANamespace))
			})

			It("should create istio-reader ManifestWork for newly added cluster", func() {
				mesh := util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				expectIstioReaderManifestWork(mesh, clusterName)

				cluster2 := util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster2)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2)
				expectIstioReaderManifestWork(mesh, cluster2)
			})

			When("the ManagedServiceAccount exists", func() {
				var mesh *meshv1alpha1.MultiClusterMesh

				BeforeEach(func() {
					mesh = util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
					expectManagedServiceAccount(mesh, clusterName)
				})

				It("should update ManagedServiceAccount validity when mesh spec changes", func() {
					updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
						mesh.Spec.Security.Discovery.TokenValidity = &metav1.Duration{Duration: 15 * time.Minute}
					})
					Eventually(func(g Gomega) {
						msa := getManagedServiceAccount(g, mesh, clusterName)
						g.Expect(msa.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 15 * time.Minute}))
					}).Should(Succeed())
				})

				It("should cleanup ManagedServiceAccount when cluster is removed from PlacementDecision", func() {
					util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
					util.ExpectResourceDeleted(ctx, k8sClient, &msav1beta1.ManagedServiceAccount{},
						meshcontroller.EndpointDiscoveryName(mesh), clusterName)
				})

				It("should cleanup ManagedServiceAccount when cluster is deleted", func() {
					util.DeleteResource(ctx, k8sClient, &clusterv1.ManagedCluster{}, clusterName, "")
					util.ExpectResourceDeleted(ctx, k8sClient, &msav1beta1.ManagedServiceAccount{},
						meshcontroller.EndpointDiscoveryName(mesh), clusterName)
				})

				It("should cleanup istio-reader ManifestWork when cluster is removed from PlacementDecision", func() {
					expectIstioReaderManifestWork(mesh, clusterName)
					util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
					util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{},
						meshcontroller.IstioReaderManifestWorkName(mesh), clusterName)
				})
			})
		})

		When("ManagedServiceAccount secret exists", func() {
			var mesh *meshv1alpha1.MultiClusterMesh

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				mesh = util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system"},
				})
				setupMsaTokenSecret(mesh, clusterName)
			})

			It("should create ManifestWorkReplicaSet referencing the user-managed Placement", func() {
				expectManifestWorkReplicaSetContent(meshName, testNs, func(g Gomega, mwrset *workv1alpha1.ManifestWorkReplicaSet) {
					g.Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).To(HaveLen(1))
				})
				mwrset := expectManifestWorkReplicaSet(meshName, testNs)
				Expect(mwrset.OwnerReferences).To(HaveLen(1))
				Expect(mwrset.OwnerReferences[0].Name).To(Equal(meshName))
				Expect(mwrset.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(mwrset.Labels[meshcontroller.MeshNameLabel]).To(Equal(meshName))
				Expect(mwrset.Labels[meshcontroller.MeshNamespaceLabel]).To(Equal(testNs))
				Expect(mwrset.Spec.PlacementRefs[0].Name).To(Equal(testPlacement))
				Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).To(HaveLen(1))
				expectRemoteSecret(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests[0], clusterName, "istio-system")
			})

			It("should update ManifestWorkReplicaSet for newly added cluster", func() {
				cluster2Name := util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster2Name)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2Name)
				setupMsaTokenSecret(mesh, cluster2Name)

				expectManifestWorkReplicaSetContent(meshName, testNs, func(g Gomega, mwrset *workv1alpha1.ManifestWorkReplicaSet) {
					g.Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).To(HaveLen(2))
					for _, cluster := range []string{clusterName, cluster2Name} {
						found := false
						for _, m := range mwrset.Spec.ManifestWorkTemplate.Workload.Manifests {
							secret := &corev1.Secret{}
							g.Expect(unmarshalManifest(m, secret)).To(Succeed())
							if secret.Name == "istio-remote-secret-"+cluster {
								found = true
								break
							}
						}
						g.Expect(found).To(BeTrue(), "no remote secret for %s", cluster)
					}
				})
			})

			It("should drop one ManifestWorkReplicaSet manifest when removing one cluster from the PlacementDecision", func() {
				cluster2Name := util.UniqueName("cluster")
				util.CreateManagedCluster(ctx, k8sClient, cluster2Name)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName, cluster2Name)
				setupMsaTokenSecret(mesh, cluster2Name)

				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, cluster2Name)

				expectManifestWorkReplicaSetContent(meshName, testNs, func(g Gomega, mwrset *workv1alpha1.ManifestWorkReplicaSet) {
					g.Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).To(HaveLen(1))
					expectRemoteSecret(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests[0], cluster2Name, "istio-system")
				})
			})

			It("should cleanup ManifestWorkReplicaSet Manifests when removing all clusters from the PlacementDecision", func() {
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
				Eventually(func(g Gomega) {
					mwrset := &workv1alpha1.ManifestWorkReplicaSet{}
					g.Expect(k8sClient.Get(ctx, key.Of(meshName, testNs), mwrset)).To(Succeed())
					g.Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).To(BeEmpty())
				}).Should(Succeed())
			})

			It("should update ManifestWorkReplicaSet when ManagedServiceAccount secret is updated", func() {
				msa := &msav1beta1.ManagedServiceAccount{}
				Expect(k8sClient.Get(ctx, key.Of(meshcontroller.EndpointDiscoveryName(mesh), clusterName), msa)).To(Succeed())
				oldTime := msa.Status.TokenSecretRef.LastRefreshTimestamp
				oldSec := &corev1.Secret{}

				expectManifestWorkReplicaSetContent(meshName, testNs, func(g Gomega, mwrset *workv1alpha1.ManifestWorkReplicaSet) {
					g.Expect(mwrset.Spec.ManifestWorkTemplate.Workload.Manifests).NotTo(BeEmpty())
					manifests := mwrset.Spec.ManifestWorkTemplate.Workload.Manifests
					g.Expect(unmarshalManifest(manifests[0], oldSec)).To(Succeed())
				})
				oldData := oldSec.Data[clusterName]

				simulateMsaTokenSecretRotation(mesh, clusterName)

				Eventually(func(g Gomega) {
					msa = &msav1beta1.ManagedServiceAccount{}
					g.Expect(k8sClient.Get(ctx, key.Of(meshcontroller.EndpointDiscoveryName(mesh), clusterName), msa)).To(Succeed())
					g.Expect(msa.Status.TokenSecretRef.LastRefreshTimestamp).NotTo(Equal(oldTime))
				}).Should(Succeed())

				expectManifestWorkReplicaSetContent(meshName, testNs, func(g Gomega, mwrset *workv1alpha1.ManifestWorkReplicaSet) {
					manifests := mwrset.Spec.ManifestWorkTemplate.Workload.Manifests
					newSec := &corev1.Secret{}
					g.Expect(unmarshalManifest(manifests[0], newSec)).To(Succeed())
					newData := newSec.Data[clusterName]
					g.Expect(newData).NotTo(Equal(oldData))
				})
			})
		})

		When("Placement selects no clusters", func() {
			var mesh *meshv1alpha1.MultiClusterMesh

			BeforeEach(func() {
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs)
				mesh = util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
			})

			It("should not create ManagedServiceAccount", func() {
				expectNoManagedServiceAccount(mesh, clusterName)
			})

			It("should create ManagedServiceAccount when cluster is added", func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.UpdatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				expectManagedServiceAccount(mesh, clusterName)
			})
		})

		When("two meshes target the same cluster", func() {
			var mesh, otherMesh *meshv1alpha1.MultiClusterMesh

			BeforeEach(func() {
				util.CreateManagedCluster(ctx, k8sClient, clusterName)
				util.CreatePlacement(ctx, k8sClient, testPlacement, testNs)
				util.CreatePlacementDecision(ctx, k8sClient, testPlacement, testNs, clusterName)
				mesh = util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testPlacement)
				expectMeshNotReady(meshName, testNs)

				otherMesh = util.CreateMultiClusterMesh(ctx, k8sClient, util.UniqueName("other-mesh"), testNs, testPlacement, meshv1alpha1.MultiClusterMeshSpec{
					ControlPlane: meshv1alpha1.ControlPlaneConfig{Namespace: "istio-system-2"},
				})
				expectMeshNotReady(otherMesh.Name, testNs)
			})

			It("should delete only the removed mesh's ManagedServiceAccount when one mesh is deleted", func() {
				expectManagedServiceAccount(mesh, clusterName)
				expectManagedServiceAccount(otherMesh, clusterName)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				util.ExpectResourceDeleted(ctx, k8sClient, &msav1beta1.ManagedServiceAccount{},
					meshcontroller.EndpointDiscoveryName(mesh), clusterName)

				Consistently(func(g Gomega) {
					getManagedServiceAccount(g, otherMesh, clusterName)
				}).Should(Succeed())
			})

			It("should create separate istio-reader ManifestWorks for each mesh", func() {
				work1 := expectIstioReaderManifestWork(mesh, clusterName)
				work2 := expectIstioReaderManifestWork(otherMesh, clusterName)

				Expect(work1.Name).NotTo(Equal(work2.Name))

				crb1 := &rbacv1.ClusterRoleBinding{}
				Expect(unmarshalManifest(work1.Spec.Workload.Manifests[1], crb1)).To(Succeed())
				crb2 := &rbacv1.ClusterRoleBinding{}
				Expect(unmarshalManifest(work2.Spec.Workload.Manifests[1], crb2)).To(Succeed())
			})

			It("should delete only the removed mesh's istio-reader ManifestWork when one mesh is deleted", func() {
				work1 := expectIstioReaderManifestWork(mesh, clusterName)
				work2 := expectIstioReaderManifestWork(otherMesh, clusterName)

				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				util.ExpectResourceDeleted(ctx, k8sClient, &workv1.ManifestWork{}, work1.Name, clusterName)

				Consistently(func() error {
					return k8sClient.Get(ctx, key.Of(work2.Name, clusterName), &workv1.ManifestWork{})
				}).Should(Succeed())
			})
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

func updateClusterLabel(clusterName, labelKey, labelValue string) {
	cluster := &clusterv1.ManagedCluster{}
	Expect(k8sClient.Get(ctx, key.Of(clusterName), cluster)).To(Succeed())
	if cluster.Labels == nil {
		cluster.Labels = make(map[string]string)
	}
	cluster.Labels[labelKey] = labelValue
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

func expectMeshOwnedLabels(labels map[string]string, meshName, meshNamespace, clusterName string) {
	Expect(labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
	Expect(labels[meshcontroller.MeshNameLabel]).To(Equal(meshName))
	Expect(labels[meshcontroller.MeshNamespaceLabel]).To(Equal(meshNamespace))
	Expect(labels[meshcontroller.ClusterNameLabel]).To(Equal(clusterName))
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

func expectCacertsManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.CacertsManifestWorkName(mesh), clusterNamespace)
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

func expectNoCacertsManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterNamespace string) {
	Consistently(func() bool {
		work := &workv1.ManifestWork{}
		err := k8sClient.Get(ctx, key.Of(meshcontroller.CacertsManifestWorkName(mesh), clusterNamespace), work)
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
	Expect(cert.Spec.SecretName).To(Equal(cert.Name))
	Expect(cert.Spec.IsCA).To(BeTrue())
	Expect(cert.Spec.IssuerRef.Name).To(Equal(issuerName))
	Expect(cert.Spec.IssuerRef.Kind).To(Equal(issuerKind))
	return cert
}

func expectCacertsSecretManifest(work *workv1.ManifestWork, expectedNamespace string) {
	Expect(work.Spec.Workload.Manifests).To(HaveLen(1))
	secret := &corev1.Secret{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[0], secret)).To(Succeed())
	Expect(secret.Name).To(Equal("cacerts"))
	Expect(secret.Namespace).To(Equal(expectedNamespace))
	Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))
	Expect(secret.Data).To(HaveKey("tls.crt"))
	Expect(secret.Data).To(HaveKey("tls.key"))
	Expect(secret.Data).To(HaveKey("ca.crt"))
}

func expectControlPlaneNamespaceManifestWork(clusterName, cpNamespace string) (*workv1.ManifestWork, *corev1.Namespace) {
	work := expectManifestWork(meshcontroller.ManifestWorkNameCPNSPrefix+cpNamespace, clusterName)
	Expect(work.Spec.Workload.Manifests).To(HaveLen(1))
	ns := expectNamespace(work, 0, cpNamespace)

	return work, ns
}

func expectInvalidCreateMeshFailure(name, namespace string, spec meshv1alpha1.MultiClusterMeshSpec, messageSubstring string) {
	mesh := &meshv1alpha1.MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
	err := k8sClient.Create(ctx, mesh)
	Expect(err).To(HaveOccurred())
	Expect(errors.IsInvalid(err)).To(BeTrue())
	Expect(err.Error()).To(ContainSubstring(messageSubstring))
}

func getManagedServiceAccount(g Gomega, mesh *meshv1alpha1.MultiClusterMesh, clusterName string) *msav1beta1.ManagedServiceAccount {
	msa := &msav1beta1.ManagedServiceAccount{}
	g.Expect(k8sClient.Get(ctx, key.Of(meshcontroller.EndpointDiscoveryName(mesh), clusterName), msa)).To(Succeed())
	return msa
}

func expectManagedServiceAccount(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) *msav1beta1.ManagedServiceAccount {
	var msa *msav1beta1.ManagedServiceAccount
	Eventually(func(g Gomega) {
		msa = getManagedServiceAccount(g, mesh, clusterName)
	}).Should(Succeed())
	return msa
}

// expectNoManagedServiceAccount makes sure that no ManagedServiceAccount is created for a cluster, checking consistently
func expectNoManagedServiceAccount(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) {
	Consistently(func() bool {
		msa := &msav1beta1.ManagedServiceAccount{}
		err := k8sClient.Get(ctx, key.Of(meshcontroller.EndpointDiscoveryName(mesh), clusterName), msa)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func expectManifestWorkReplicaSet(meshName, meshNamespace string) *workv1alpha1.ManifestWorkReplicaSet {
	mwrset := &workv1alpha1.ManifestWorkReplicaSet{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(meshName, meshNamespace), mwrset)
	}).Should(Succeed())
	return mwrset
}

func expectManifestWorkReplicaSetContent(meshName, meshNamespace string, assert func(Gomega, *workv1alpha1.ManifestWorkReplicaSet)) {
	Eventually(func(g Gomega) {
		mwrset := &workv1alpha1.ManifestWorkReplicaSet{}
		g.Expect(k8sClient.Get(ctx, key.Of(meshName, meshNamespace), mwrset)).To(Succeed())
		assert(g, mwrset)
	}).Should(Succeed())
}

func expectRemoteSecret(manifest workv1.Manifest, clusterName, expectedNamespace string) {
	secret := &corev1.Secret{}
	Expect(unmarshalManifest(manifest, secret)).To(Succeed())
	Expect(secret.Name).To(Equal("istio-remote-secret-" + clusterName))
	Expect(secret.Namespace).To(Equal(expectedNamespace))
	Expect(secret.Labels["istio/multiCluster"]).To(Equal("true"))
	Expect(secret.Annotations["networking.istio.io/cluster"]).To(Equal(clusterName))
	Expect(secret.Data).To(HaveKey(clusterName))
}

// createMsaSecret creates a ServiceAccount and a secret that simulates what ManagedServiceAccount controller would create.
func createMsaSecret(ctx context.Context, k8sClient client.Client, msaName, clusterName string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      msaName,
			Namespace: clusterName,
			Labels: map[string]string{
				"authentication.open-cluster-management.io/is-managed-serviceaccount": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey: []byte("test-ca-data"),
			corev1.ServiceAccountTokenKey:  []byte("test-token-data"),
		},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
}

// updateMsaSecret updates a ManagedServiceAccount secret that simulates token rotation.
func updateMsaSecret(ctx context.Context, k8sClient client.Client, msaName, clusterName string) {
	secret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, key.Of(msaName, clusterName), secret)).To(Succeed())
	secret.Data["token"] = []byte("new-token-data")
	Expect(k8sClient.Update(ctx, secret)).To(Succeed())
}

// setMsaStatus updates a ManagedServiceAccount's status TokenSecretRef,
// simulating what the ManagedServiceAccount controller does
func setMsaStatus(ctx context.Context, k8sClient client.Client, msaName, clusterName string, testDuration time.Duration) {
	msa := &msav1beta1.ManagedServiceAccount{}
	Expect(k8sClient.Get(ctx, key.Of(msaName, clusterName), msa)).To(Succeed())
	msa.Status = msav1beta1.ManagedServiceAccountStatus{
		TokenSecretRef: &msav1beta1.SecretRef{
			Name:                 msaName,
			LastRefreshTimestamp: metav1.NewTime(metav1.Now().Add(testDuration)),
		},
	}
	Expect(k8sClient.Status().Update(ctx, msa)).To(Succeed())
}

func setupMsaTokenSecret(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) {
	msa := expectManagedServiceAccount(mesh, clusterName)
	createMsaSecret(ctx, k8sClient, msa.Name, clusterName)
	setMsaStatus(ctx, k8sClient, msa.Name, clusterName, 0)
	expectMeshNotReady(mesh.Name, mesh.Namespace)
}

func simulateMsaTokenSecretRotation(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) {
	msa := expectManagedServiceAccount(mesh, clusterName)
	updateMsaSecret(ctx, k8sClient, msa.Name, clusterName)
	setMsaStatus(ctx, k8sClient, msa.Name, clusterName, time.Duration(time.Minute))
	expectMeshNotReady(mesh.Name, mesh.Namespace)
}

func unmarshalManifest(manifest workv1.Manifest, into interface{}) error {
	return json.Unmarshal(manifest.Raw, into)
}

func expectIstioReaderManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.IstioReaderManifestWorkName(mesh), clusterNamespace)
}

func expectIstioReaderRules(rules []rbacv1.PolicyRule) {
	Expect(rules).NotTo(BeEmpty())
}

func expectOLMClusterRole(work *workv1.ManifestWork, index int) {
	cr := &rbacv1.ClusterRole{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], cr)).To(Succeed())
	Expect(cr.Name).To(Equal("klusterlet-work-olm-ossm"))
	Expect(cr.Labels).To(HaveKeyWithValue("open-cluster-management.io/aggregate-to-work", "true"))
	Expect(cr.Rules).To(HaveLen(1))
	Expect(cr.Rules[0].APIGroups).To(ConsistOf("operators.coreos.com"))
	Expect(cr.Rules[0].Resources).To(ConsistOf("operatorgroups", "subscriptions", "catalogsources", "clusterserviceversions"))
	Expect(cr.Rules[0].Verbs).To(ConsistOf("create", "get", "list", "update", "patch", "delete"))
}

func expectNamespace(work *workv1.ManifestWork, index int, expectedName string) *corev1.Namespace {
	ns := &corev1.Namespace{}
	Expect(unmarshalManifest(work.Spec.Workload.Manifests[index], ns)).To(Succeed())
	Expect(ns.Name).To(Equal(expectedName))
	return ns
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
