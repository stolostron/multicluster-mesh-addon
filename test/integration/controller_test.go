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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	multiClusterSecretLabel   = "istio/multiCluster"
	manifestWorkNameMsaPrefix = "multicluster-mesh-msa-secrets"
	remoteSecretPrefix        = "istio-remote-secret"
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
		It("should create ManifestWorks for clusters in the specified ClusterSet", func() {
			cluster1 := util.UniqueName("cluster")
			cluster2 := util.UniqueName("cluster")

			util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)

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

			expectMeshNotReady(meshName, testNs)
			expectClusterOperatorConditionReason(meshName, testNs, cluster1, meshv1alpha1.ReasonManifestWorkCreated)
			expectClusterOperatorConditionReason(meshName, testNs, cluster2, meshv1alpha1.ReasonManifestWorkCreated)
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

				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
				mesh.Spec.ControlPlane.Namespace = "different-ns"
				Expect(k8sClient.Update(ctx, mesh)).To(Succeed())

				Consistently(func() string {
					return expectOperatorManifestWork(clusterName).ResourceVersion
				}).Should(Equal(originalVersion))
			})

			It("should update ManifestWork when operator config changes", func() {
				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
				mesh.Spec.Operator.Channel = "tech-preview"
				Expect(k8sClient.Update(ctx, mesh)).To(Succeed())

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

		It("should allow two meshes with the same operator config", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testClusterSet)

			expectMeshNotReady(otherMesh, testNs)
			expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonManifestWorkCreated)
		})

		When("a newer mesh has a conflicting operator config", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{
					Operator: meshv1alpha1.OperatorConfig{Channel: "different-channel"},
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
				cert := expectCertificate(testNs, clusterName, "mesh-issuer")

				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())

				Expect(cert.OwnerReferences).To(HaveLen(1))
				ownerRef := cert.OwnerReferences[0]
				Expect(ownerRef.APIVersion).To(Equal(meshv1alpha1.GroupVersion.String()))
				Expect(ownerRef.Kind).To(Equal("MultiClusterMesh"))
				Expect(ownerRef.Name).To(Equal(meshName))
				Expect(ownerRef.UID).To(Equal(mesh.UID))
				Expect(*ownerRef.Controller).To(BeTrue())
				Expect(*ownerRef.BlockOwnerDeletion).To(BeTrue())
			})

			It("should restore Certificate spec when externally modified", func() {
				cert := expectCertificate(testNs, clusterName, "mesh-issuer")

				cert.Spec.CommonName = "tampered"
				Expect(k8sClient.Update(ctx, cert)).To(Succeed())

				Eventually(func() string {
					c := &certmanagerv1.Certificate{}
					if err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      cert.Name,
						Namespace: cert.Namespace,
					}, c); err != nil {
						return ""
					}
					return c.Spec.CommonName
				}).Should(Equal("Intermediate Istio CA"))
			})

			It("should recreate Certificate when it is externally deleted", func() {
				cert := expectCertificate(testNs, clusterName, "mesh-issuer")
				originalUID := cert.UID
				Expect(k8sClient.Delete(ctx, cert)).To(Succeed())

				Eventually(func() types.UID {
					return expectCertificate(testNs, clusterName, "mesh-issuer").UID
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
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("cacerts-%s", clusterName),
					Namespace: testNs,
				}, secret)).To(Succeed())

				secret.Data["tls.crt"] = []byte("updated-cert-data")
				Expect(k8sClient.Update(ctx, secret)).To(Succeed())

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
				expectCertificate(testNs, clusterName, "mesh-issuer")

				updateClusterSetLabel(clusterName, "")

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{},
					fmt.Sprintf("cacerts-%s", clusterName), testNs)
			})
		})

		When("issuer is removed after initial configuration", func() {
			It("should cleanup all Certificates", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
				expectCertificate(testNs, clusterName, "mesh-issuer")

				mesh := &meshv1alpha1.MultiClusterMesh{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: testNs}, mesh)).To(Succeed())
				mesh.Spec.Security.Trust.CertManager.IssuerRef.Name = ""
				Expect(k8sClient.Update(ctx, mesh)).To(Succeed())

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
	})

	Context("Endpoint discovery", func() {
		var cluster1, cluster2, cluster3, cluster4 string

		BeforeEach(func() {
			cluster1 = util.UniqueName("cluster1")
			cluster2 = util.UniqueName("cluster2")
			util.CreateK8sManagedCluster(ctx, k8sClient, cluster1, testClusterSet)
			util.CreateK8sManagedCluster(ctx, k8sClient, cluster2, testClusterSet)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, meshv1alpha1.MultiClusterMeshSpec{})
		})

		It("should create ManagedServiceAccount resources with default validity for each ManagedCluster in the ClusterSet", func() {
			expectManagedServiceAccount(fmt.Sprintf("%s-istio-reader", meshName), cluster1)
			expectManagedServiceAccount(fmt.Sprintf("%s-istio-reader", meshName), cluster2)
		})

		It("should create ManifestWork when ManagedServiceAccount secret is created", func() {
			// simulate creating the cacerts secret by cert-manager
			util.CreateMsaSecret(ctx, k8sClient, cluster1, meshName, testNs)
			util.CreateMsaSecret(ctx, k8sClient, cluster2, meshName, testNs)

			expectManifestWork(fmt.Sprintf("%s-%s", manifestWorkNameMsaPrefix, meshName), cluster1)
			expectManifestWork(fmt.Sprintf("%s-%s", manifestWorkNameMsaPrefix, meshName), cluster2)
			expectIstioRemoteSecret(testNs, cluster1)
			expectIstioRemoteSecret(testNs, cluster2)
		})

		When("add a cluster to the ClusterSet", func() {
			BeforeEach(func() {
				cluster3 = util.UniqueName("cluster3")
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster3, testClusterSet)
			})

			It("should process a ManagedServiceAccount", func() {
				expectManagedServiceAccount(fmt.Sprintf("%s-istio-reader", meshName), cluster3)
			})

			It("should create ManifestWork when ManagedServiceAccount secret is created", func() {
				// simulate creating the cacerts secret by cert-manager
				util.CreateMsaSecret(ctx, k8sClient, cluster3, meshName, testNs)

				expectManifestWork(fmt.Sprintf("%s-%s", manifestWorkNameMsaPrefix, meshName), cluster3)
				expectIstioRemoteSecret(testNs, cluster3)
			})
		})

		When("remove a cluster from the ClusterSet", func() {
			BeforeEach(func() {
				updateClusterSetLabel(cluster2, "")
			})

			It("should cleanup the ManagedServiceAccount", func() {
				expectNoManagedServiceAccount(meshName, cluster2)
			})

			It("should cleanup the istio remote secret", func() {
				expectNoIstioRemoteSecret(testNs, cluster2)
			})
		})

		When("create a cluster without clusterset label", func() {
			BeforeEach(func() {
				cluster4 = util.UniqueName("cluster4")
				util.CreateK8sManagedCluster(ctx, k8sClient, cluster4, "")
			})

			It("shouldn't process ManagedServiceAccount", func() {
				expectNoManagedServiceAccount(meshName, cluster4)
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

			It("should keep the ManagedServiceAccount when one mesh is deleted", func() {
				expectMeshNotReady(otherMesh, otherNs)
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
				expectManagedServiceAccount(fmt.Sprintf("%s-istio-reader", meshName), cluster1)
			})

			It("should delete the ManagedServiceAccount when both meshes are deleted", func() {
				expectMeshNotReady(otherMesh, otherNs)
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
				expectNoManagedServiceAccount(meshName, cluster1)
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
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, mesh); err != nil {
			return nil
		}
		return mesh.Finalizers
	}).Should(ContainElement(meshcontroller.FinalizerName))
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

// triggerReconcile forces a reconciliation by touching the mesh CR's annotations.
// Workaround for the dual-cache race (#109): the controller-runtime MW watch may
// trigger a reconcile before the WorkApplier's work informer lister syncs the update,
// causing safeToSkipApply to read stale generation and skip the patch.
// TODO(mkolesnik): Remove once WorkApplier uses the CR cache (sdk-go#226).
func triggerReconcile(meshName, namespace string) {
	mesh := &meshv1alpha1.MultiClusterMesh{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: namespace}, mesh)).To(Succeed())
	if mesh.Annotations == nil {
		mesh.Annotations = map[string]string{}
	}
	mesh.Annotations["test.reconcile-trigger"] = mesh.ResourceVersion
	Expect(k8sClient.Update(ctx, mesh)).To(Succeed())
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

func expectCacertsManifestWork(clusterNamespace string) *workv1.ManifestWork {
	return expectManifestWork(meshcontroller.ManifestWorkNameCacerts, clusterNamespace)
}

func expectNoCacertsManifestWork(clusterNamespace string) {
	Consistently(func() bool {
		work := &workv1.ManifestWork{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      meshcontroller.ManifestWorkNameCacerts,
			Namespace: clusterNamespace,
		}, work)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func expectCertificate(namespace, clusterName, issuerName string) *certmanagerv1.Certificate {
	cert := &certmanagerv1.Certificate{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("cacerts-%s", clusterName),
			Namespace: namespace,
		}, cert)
	}).Should(Succeed())

	Expect(cert.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
	Expect(cert.Spec.SecretName).To(Equal(fmt.Sprintf("cacerts-%s", clusterName)))
	Expect(cert.Spec.IsCA).To(BeTrue())
	Expect(cert.Spec.IssuerRef.Name).To(Equal(issuerName))
	Expect(cert.Spec.IssuerRef.Kind).To(Equal("Issuer"))
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

func expectManagedServiceAccount(msaName, namespace string) *msav1beta1.ManagedServiceAccount {
	msa := &msav1beta1.ManagedServiceAccount{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{
			Name:      msaName,
			Namespace: namespace,
		}, msa)
	}).Should(Succeed())
	Expect(msa.ObjectMeta.Labels).To(HaveKeyWithValue(meshcontroller.ManagedByLabel, meshcontroller.ManagedByValue))
	Expect(msa.Spec.Rotation.Validity).To(Equal(metav1.Duration{Duration: 360 * time.Hour}))
	return msa
}

func expectIstioRemoteSecret(meshNamespace, clusterName string) {
	secret := &corev1.Secret{}
	Expect(secret.Name).To(Equal(fmt.Sprintf("%s-%s", remoteSecretPrefix, clusterName)))
	Expect(secret.Namespace).To(Equal(meshNamespace))
	Expect(secret.Type).To(Equal(corev1.SecretTypeOpaque))
	Expect(secret.Labels).To(HaveKeyWithValue(multiClusterSecretLabel, "true"))
	Expect(secret.Data).To(HaveKey("token"))
	Expect(secret.Data).To(HaveKey("ca.crt"))
}

// expectNoManagedServiceAccount makes sure that no ManagedServiceAccount is created for a cluster, checking consistently
func expectNoManagedServiceAccount(meshName, clusterName string) {
	Consistently(func() bool {
		msa := &msav1beta1.ManagedServiceAccount{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-istio-reader", meshName),
			Namespace: clusterName,
		}, msa)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

// expectNoIstioRemoteSecret makes sure that no istio remote secret is created for a cluster, checking consistently
func expectNoIstioRemoteSecret(meshNamespace, clusterName string) {
	Consistently(func() bool {
		sec := &corev1.Secret{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-%s", remoteSecretPrefix, clusterName),
			Namespace: meshNamespace,
		}, sec)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

// unmarshalManifest extracts a manifest from ManifestWork's RawExtension.
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

func expectMeshNotReady(meshName, namespace string) {
	Eventually(func() metav1.ConditionStatus {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: namespace}, mesh); err != nil {
			return ""
		}
		for _, c := range mesh.Status.Conditions {
			if c.Type == meshv1alpha1.ConditionReady {
				return c.Status
			}
		}
		return ""
	}).Should(Equal(metav1.ConditionFalse))
}

func expectClusterOperatorConditionReason(meshName, namespace, clusterName, reason string) {
	Eventually(func() string {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: namespace}, mesh); err != nil {
			return ""
		}
		for _, cs := range mesh.Status.ClusterStatus {
			if cs.ClusterName == clusterName {
				for _, c := range cs.Conditions {
					if c.Type == meshv1alpha1.ConditionOperatorInstalled {
						return c.Reason
					}
				}
			}
		}
		return ""
	}).Should(Equal(reason))
}

func expectNoClusterStatus(meshName, namespace, clusterName string) {
	Eventually(func() bool {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: namespace}, mesh); err != nil {
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
	Eventually(func() string {
		mesh := &meshv1alpha1.MultiClusterMesh{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: meshName, Namespace: namespace}, mesh); err != nil {
			return ""
		}
		for _, c := range mesh.Status.Conditions {
			if c.Type == conditionType {
				return c.Reason
			}
		}
		return ""
	}).Should(Equal(reason))
}
