//go:build integration

package integration

import (
	"encoding/json"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"

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

		policyList := &policyv1.PolicyList{}
		_ = k8sClient.List(ctx, policyList)
		for i := range policyList.Items {
			_ = k8sClient.Delete(ctx, &policyList.Items[i])
		}

		bindingList := &policyv1.PlacementBindingList{}
		_ = k8sClient.List(ctx, bindingList)
		for i := range bindingList.Items {
			_ = k8sClient.Delete(ctx, &bindingList.Items[i])
		}

		placementList := &clusterv1beta1.PlacementList{}
		_ = k8sClient.List(ctx, placementList)
		for i := range placementList.Items {
			_ = k8sClient.Delete(ctx, &placementList.Items[i])
		}

		clusterSetBindingList := &clusterv1beta2.ManagedClusterSetBindingList{}
		_ = k8sClient.List(ctx, clusterSetBindingList)
		for i := range clusterSetBindingList.Items {
			_ = k8sClient.Delete(ctx, &clusterSetBindingList.Items[i])
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

			It("should create operator Policy with correct labels", func() {
				policy := expectOperatorPolicy(meshName, testNs)

				Expect(policy.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
				Expect(policy.Labels[meshcontroller.ClusterSetLabel]).To(Equal(testClusterSet))
				Expect(policy.Labels[meshcontroller.ClusterSetLabel]).To(Equal(testClusterSet))

				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonPolicyCreated)
			})

			It("should create Placement, PlacementBinding, and ManagedClusterSetBinding", func() {
				expectOperatorPlacement(meshName, testNs, testClusterSet)
				expectOperatorPlacementBinding(testNs)
				expectManagedClusterSetBinding(testNs, testClusterSet)
			})

			It("should become ready after all clusters are compliant", func() {
				expectMeshNotReady(meshName, testNs)

				By("setting compliance on one cluster, mesh should stay not-ready")
				util.SetOperatorPolicyCompliance(ctx, k8sClient,
					meshcontroller.OperatorPolicyName, testNs,
					map[string]policyv1.ComplianceState{clusterName: policyv1.Compliant})

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonPolicyCreated)
				expectMeshNotReady(meshName, testNs)

				By("setting compliance on all clusters, mesh should become ready")
				util.SetOperatorPolicyCompliance(ctx, k8sClient,
					meshcontroller.OperatorPolicyName, testNs,
					map[string]policyv1.ComplianceState{
						clusterName:  policyv1.Compliant,
						cluster2Name: policyv1.Compliant,
					})

				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonOperatorInstalled)
				expectClusterOperatorConditionReason(meshName, testNs, cluster2Name, meshv1alpha1.ReasonOperatorInstalled)
				expectMeshReady(meshName, testNs)
			})
		})

		It("should use custom operator configuration when specified", func() {
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

			policy := expectOperatorPolicy(meshName, testNs)
			sub := extractSubscriptionFromPolicy(policy)

			Expect(sub["namespace"]).To(Equal(customConfig.Namespace))
			Expect(sub["channel"]).To(Equal(customConfig.Channel))
			Expect(sub["source"]).To(Equal(customConfig.Source))
			Expect(sub["sourceNamespace"]).To(Equal(customConfig.SourceNamespace))
		})

		When("referencing a non-existent ClusterSet", func() {
			var otherClusterSet string

			BeforeEach(func() {
				otherClusterSet = util.UniqueName("late-set")
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, otherClusterSet)
			})

			It("should create operator Policy even without clusters", func() {
				expectMeshNotReady(meshName, testNs)
				expectOperatorPolicy(meshName, testNs)
			})

			It("should reconcile when the ClusterSet is created", func() {
				expectMeshNotReady(meshName, testNs)
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, otherClusterSet)
				util.CreateManagedClusterSet(ctx, k8sClient, otherClusterSet)
				expectOperatorPolicy(meshName, testNs)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
			})
		})

		When("referencing an empty ClusterSet", func() {
			BeforeEach(func() {
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			})

			It("should create operator Policy but stay not-ready", func() {
				expectMeshNotReady(meshName, testNs)
				expectOperatorPolicy(meshName, testNs)
			})

			It("should process a cluster when it's added", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				expectOperatorPolicy(meshName, testNs)
				expectMeshNotReady(meshName, testNs)
				expectClusterOperatorConditionReason(meshName, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
			})
		})

		It("should add finalizer on MultiClusterMesh creation", func() {
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectFinalizer(meshName, testNs)
		})

		When("referencing a set with a cluster", func() {
			BeforeEach(func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
				expectOperatorPolicy(meshName, testNs)
			})

			It("should not update operator Policy spec when non-operator config changes", func() {
				policy := expectOperatorPolicy(meshName, testNs)
				originalVersion := policy.ResourceVersion

				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.ControlPlane.Namespace = "different-ns"
				})

				Consistently(func() string {
					return expectOperatorPolicy(meshName, testNs).ResourceVersion
				}).Should(Equal(originalVersion))
			})

			It("should update Policy spec when operator config changes", func() {
				updateMesh(meshName, testNs, func(mesh *meshv1alpha1.MultiClusterMesh) {
					mesh.Spec.Operator.Channel = "tech-preview"
				})

				Eventually(func() string {
					policy := expectOperatorPolicy(meshName, testNs)
					sub := extractSubscriptionFromPolicy(policy)
					channel, _ := sub["channel"].(string)
					return channel
				}).Should(Equal("tech-preview"))
			})

			It("should restore Policy spec when externally modified", func() {
				policy := expectOperatorPolicy(meshName, testNs)
				sub := extractSubscriptionFromPolicy(policy)
				originalChannel, _ := sub["channel"].(string)

				// Tamper with the policy spec
				tamperedSpec := policy.Spec.DeepCopy()
				tamperedTemplate := modifySubscriptionChannel(tamperedSpec, "tampered")
				policy.Spec.PolicyTemplates = tamperedTemplate
				Expect(k8sClient.Update(ctx, policy)).To(Succeed())

				triggerReconcile(meshName, testNs)

				Eventually(func() string {
					p := expectOperatorPolicy(meshName, testNs)
					s := extractSubscriptionFromPolicy(p)
					ch, _ := s["channel"].(string)
					return ch
				}).Should(Equal(originalChannel))
			})

			It("should restore Policy labels when externally modified", func() {
				policy := expectOperatorPolicy(meshName, testNs)
				policy.Labels[meshcontroller.ManagedByLabel] = "someone-else"
				Expect(k8sClient.Update(ctx, policy)).To(Succeed())

				triggerReconcile(meshName, testNs)

				Eventually(func() string {
					p := expectOperatorPolicy(meshName, testNs)
					return p.Labels[meshcontroller.ManagedByLabel]
				}).Should(Equal(meshcontroller.ManagedByValue))
			})

			It("should recreate Policy when it is externally deleted", func() {
				policy := expectOperatorPolicy(meshName, testNs)
				originalUID := policy.UID
				Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
				Eventually(func() types.UID {
					return expectOperatorPolicy(meshName, testNs).UID
				}).ShouldNot(Equal(originalUID))
			})

			It("should cleanup cluster status when the cluster is removed from ClusterSet", func() {
				updateClusterSetLabel(clusterName, "")
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should cleanup cluster status when the cluster is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1.ManagedCluster{}, clusterName, "")
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			It("should cleanup cluster status when the ClusterSet is deleted", func() {
				util.DeleteResource(ctx, k8sClient, &clusterv1beta2.ManagedClusterSet{}, testClusterSet, "")
				expectNoClusterStatus(meshName, testNs, clusterName)
			})

			When("two meshes target the same ClusterSet", func() {
				var otherNs, otherMesh string

				BeforeEach(func() {
					otherNs = util.UniqueName("other-ns")
					otherMesh = util.UniqueName("other-mesh")
					util.CreateNamespace(ctx, k8sClient, otherNs)
					util.CreateMultiClusterMesh(ctx, k8sClient, otherMesh, otherNs, testClusterSet)
				})

				It("should keep one Policy when one mesh is deleted", func() {
					expectMeshNotReady(otherMesh, otherNs)
					expectOperatorPolicy(otherMesh, otherNs)

					util.SimulatePolicyFrameworkDeletion(ctx, k8sClient, meshcontroller.OperatorPolicyName, otherNs, []string{clusterName})
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)

					// The first mesh's Policy should remain
					expectOperatorPolicy(meshName, testNs)
				})

				It("should delete Policy only when the last mesh is deleted", func() {
					expectMeshNotReady(otherMesh, otherNs)
					expectOperatorPolicy(otherMesh, otherNs)

					// Delete first mesh (not last): Policy stays since the other mesh still needs it
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
					expectOperatorPolicy(otherMesh, otherNs)

					// Delete second mesh (last): operator gets removed, Policy gets deleted
					util.SimulatePolicyFrameworkDeletion(ctx, k8sClient, meshcontroller.OperatorPolicyName, otherNs, []string{clusterName})
					util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, otherMesh, otherNs)
					expectOperatorPolicyDeleted(otherNs)
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
			expectOperatorPolicy(meshName, testNs)
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
			expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
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

				util.SimulatePolicyFrameworkDeletion(ctx, k8sClient, meshcontroller.OperatorPolicyName, testNs, []string{clusterName})
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
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

				util.SimulatePolicyFrameworkDeletion(ctx, k8sClient, meshcontroller.OperatorPolicyName, testNs, []string{clusterName})
				util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
				expectClusterOperatorConditionReason(otherMesh, testNs, clusterName, meshv1alpha1.ReasonPolicyCreated)
			})
		})
	})

	Context("Deleting MultiClusterMesh", func() {
		It("should delete operator Policy and related resources", func() {
			cluster2 := util.UniqueName("cluster2")
			util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
			util.CreateOCPManagedCluster(ctx, k8sClient, cluster2, testClusterSet, meshcontroller.ProductOCP)
			util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet)
			expectFinalizer(meshName, testNs)
			expectOperatorPolicy(meshName, testNs)

			util.SimulatePolicyFrameworkDeletion(ctx, k8sClient, meshcontroller.OperatorPolicyName, testNs, []string{clusterName, cluster2})
			util.DeleteResource(ctx, k8sClient, &meshv1alpha1.MultiClusterMesh{}, meshName, testNs)
			expectOperatorPolicyDeleted(testNs)
			util.ExpectResourceDeleted(ctx, k8sClient, &policyv1.PlacementBinding{}, meshcontroller.OperatorPolicyName, testNs)
			util.ExpectResourceDeleted(ctx, k8sClient, &clusterv1beta1.Placement{}, meshcontroller.OperatorPolicyName, testNs)
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
				cert := expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")

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
				cert := expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")

				Expect(cert.Spec.Subject).NotTo(BeNil())
				Expect(cert.Spec.Subject.Organizations).To(ConsistOf(meshName))
				Expect(cert.Spec.Subject.OrganizationalUnits).To(ConsistOf(clusterName))

				expectedSAN := "spiffe://" + meshName + "/cluster/" + clusterName + "/ca/istio-ca"
				Expect(cert.Spec.URIs).To(ConsistOf(expectedSAN))
			})

			It("should restore Certificate spec when externally modified", func() {
				cert := expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")

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
				cert := expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")
				originalUID := cert.UID
				Expect(k8sClient.Delete(ctx, cert)).To(Succeed())

				Eventually(func() types.UID {
					return expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer").UID
				}).ShouldNot(Equal(originalUID))
			})

			It("should create ManifestWork when cacerts secret is created", func() {
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
				expectCertificate(testNs, clusterName, "cluster-issuer", "ClusterIssuer")
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
				expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")

				updateClusterSetLabel(clusterName, "")

				util.ExpectResourceDeleted(ctx, k8sClient, &certmanagerv1.Certificate{},
					fmt.Sprintf("cacerts-%s", clusterName), testNs)
			})
		})

		When("issuer is removed after initial configuration", func() {
			It("should cleanup all Certificates", func() {
				util.CreateK8sManagedCluster(ctx, k8sClient, clusterName, testClusterSet)
				util.CreateMultiClusterMesh(ctx, k8sClient, meshName, testNs, testClusterSet, util.CertManagerSpec("mesh-issuer"))
				expectCertificate(testNs, clusterName, "mesh-issuer", "Issuer")

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

// triggerReconcile forces a reconciliation by touching the mesh CR's annotations.
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

// expectOperatorPolicy waits for the operator Policy to exist in the mesh namespace and returns it.
func expectOperatorPolicy(meshName, namespace string) *policyv1.Policy {
	policy := &policyv1.Policy{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(meshcontroller.OperatorPolicyName, namespace), policy)
	}).Should(Succeed())
	return policy
}

// expectOperatorPolicyDeleted waits for the operator Policy to be deleted from the given namespace.
func expectOperatorPolicyDeleted(namespace string) {
	util.ExpectResourceDeleted(ctx, k8sClient, &policyv1.Policy{}, meshcontroller.OperatorPolicyName, namespace)
}

// expectOperatorPlacement waits for the operator Placement to exist and validates its ClusterSet.
func expectOperatorPlacement(meshName, namespace, clusterSet string) *clusterv1beta1.Placement {
	placement := &clusterv1beta1.Placement{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(meshcontroller.OperatorPolicyName, namespace), placement)
	}).Should(Succeed())
	Expect(placement.Spec.ClusterSets).To(ContainElement(clusterSet))
	Expect(placement.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
	return placement
}

// expectOperatorPlacementBinding waits for the operator PlacementBinding to exist.
func expectOperatorPlacementBinding(namespace string) *policyv1.PlacementBinding {
	binding := &policyv1.PlacementBinding{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(meshcontroller.OperatorPolicyName, namespace), binding)
	}).Should(Succeed())
	Expect(binding.PlacementRef.Name).To(Equal(meshcontroller.OperatorPolicyName))
	Expect(binding.PlacementRef.Kind).To(Equal("Placement"))
	Expect(binding.Subjects).To(HaveLen(1))
	Expect(binding.Subjects[0].Name).To(Equal(meshcontroller.OperatorPolicyName))
	Expect(binding.Subjects[0].Kind).To(Equal("Policy"))
	Expect(binding.Labels[meshcontroller.ManagedByLabel]).To(Equal(meshcontroller.ManagedByValue))
	return binding
}

// expectManagedClusterSetBinding waits for the ManagedClusterSetBinding to exist.
func expectManagedClusterSetBinding(namespace, clusterSet string) *clusterv1beta2.ManagedClusterSetBinding {
	binding := &clusterv1beta2.ManagedClusterSetBinding{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(clusterSet, namespace), binding)
	}).Should(Succeed())
	Expect(binding.Spec.ClusterSet).To(Equal(clusterSet))
	return binding
}

// extractSubscriptionFromPolicy extracts the subscription map from the OperatorPolicy embedded in the Policy.
func extractSubscriptionFromPolicy(policy *policyv1.Policy) map[string]interface{} {
	Expect(policy.Spec.PolicyTemplates).To(HaveLen(1))

	var opPolicy map[string]interface{}
	Expect(json.Unmarshal(policy.Spec.PolicyTemplates[0].ObjectDefinition.Raw, &opPolicy)).To(Succeed())

	spec, ok := opPolicy["spec"].(map[string]interface{})
	Expect(ok).To(BeTrue(), "expected spec in OperatorPolicy")

	sub, ok := spec["subscription"].(map[string]interface{})
	Expect(ok).To(BeTrue(), "expected subscription in OperatorPolicy spec")

	return sub
}

// modifySubscriptionChannel modifies the channel in the Policy spec to produce a tampered spec.
func modifySubscriptionChannel(spec *policyv1.PolicySpec, channel string) []*policyv1.PolicyTemplate {
	if len(spec.PolicyTemplates) == 0 {
		return spec.PolicyTemplates
	}

	var opPolicy map[string]interface{}
	_ = json.Unmarshal(spec.PolicyTemplates[0].ObjectDefinition.Raw, &opPolicy)
	if s, ok := opPolicy["spec"].(map[string]interface{}); ok {
		if sub, ok := s["subscription"].(map[string]interface{}); ok {
			sub["channel"] = channel
		}
	}
	rawBytes, _ := json.Marshal(opPolicy)
	spec.PolicyTemplates[0].ObjectDefinition.Raw = rawBytes
	return spec.PolicyTemplates
}

func expectCacertsManifestWork(clusterNamespace string) *workv1.ManifestWork {
	work := &workv1.ManifestWork{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(meshcontroller.ManifestWorkNameCacerts, clusterNamespace), work)
	}).Should(Succeed())
	return work
}

func expectNoCacertsManifestWork(clusterNamespace string) {
	Consistently(func() bool {
		work := &workv1.ManifestWork{}
		err := k8sClient.Get(ctx, key.Of(meshcontroller.ManifestWorkNameCacerts, clusterNamespace), work)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}

func expectCertificate(namespace, clusterName, issuerName, issuerKind string) *certmanagerv1.Certificate {
	cert := &certmanagerv1.Certificate{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(fmt.Sprintf("cacerts-%s", clusterName), namespace), cert)
	}).Should(Succeed())

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

func unmarshalManifest(manifest workv1.Manifest, into interface{}) error {
	return json.Unmarshal(manifest.Raw, into)
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
