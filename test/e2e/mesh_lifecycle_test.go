//go:build e2e

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

const (
	controllerNamespace = "multicluster-mesh-system"
	controllerName      = "multicluster-mesh-controller"

	testOperatorName      = "sailoperator"
	testOperatorNamespace = "sail-operator"
	testCatalogSource     = "operatorhubio-catalog"
	testCatalogNamespace  = "olm"
	testDefaultChannel    = "stable"
)

var clusters = []string{"cluster1", "cluster2"}

var _ = Describe("Addon registration", func() {
	It("should have ClusterManagementAddOn registered", func(ctx SpecContext) {
		cmao := &addonv1beta1.ClusterManagementAddOn{}
		err := hubClient.Get(ctx, key.Of("multicluster-mesh-addon"), cmao)
		Expect(err).NotTo(HaveOccurred())
		Expect(cmao.Spec.AddOnMeta.DisplayName).To(Equal("Multi-Cluster Mesh Add-on"))
		Expect(cmao.Spec.AddOnMeta.Description).To(Equal("Hub-side controller for orchestrating multi-cluster Istio service mesh deployments"))
		Expect(cmao.Spec.InstallStrategy.Type).To(Equal(addonv1beta1.AddonInstallStrategyManual))
	})
})

var _ = Describe("Controller health", func() {
	It("should have the controller deployment available", func(ctx SpecContext) {
		deploy := &appsv1.Deployment{}
		err := hubClient.Get(ctx, key.Of(controllerName, controllerNamespace), deploy)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.Status.Conditions).To(ContainElement(And(
			HaveField("Type", appsv1.DeploymentAvailable),
			HaveField("Status", corev1.ConditionTrue),
		)))
	})
})

var _ = Describe("MultiClusterMesh lifecycle", Ordered, func() {
	var (
		mesh *meshv1alpha1.MultiClusterMesh
		ns   string
	)

	BeforeAll(func(ctx SpecContext) {
		ns = util.UniqueName("test-ns")
		util.CreateNamespace(ctx, hubClient, ns)
	})

	AfterAll(func(ctx SpecContext) {
		Step("Deleting test namespace %s", ns)
		err := client.IgnoreNotFound(hubClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	BeforeEach(func(ctx SpecContext) {
		Step("Creating test mesh")
		// Kind clusters don't have the OSSM catalog, so we override to use Sail from upstream.
		mesh = util.CreateMultiClusterMesh(ctx, hubClient, util.UniqueName("test-mesh"), ns, "mesh-cluster-set",
			meshv1alpha1.MultiClusterMeshSpec{
				Operator: meshv1alpha1.OperatorConfig{
					Name:            testOperatorName,
					Namespace:       testOperatorNamespace,
					Source:          testCatalogSource,
					SourceNamespace: testCatalogNamespace,
				}})

		Step("Waiting for controller to reconcile")
		Eventually(func(g Gomega) {
			g.Expect(getMesh(ctx, mesh)).To(Succeed())
			// TODO(mkolesni): Once feedback rules are implemented (#89), check for Status=True instead of just existence.
			g.Expect(meta.FindStatusCondition(mesh.Status.Conditions, meshv1alpha1.ConditionReady)).NotTo(BeNil())
		}).Should(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		Step("Deleting test mesh %s", mesh.Name)
		err := client.IgnoreNotFound(hubClient.Delete(ctx, mesh))
		Expect(err).NotTo(HaveOccurred())

		Step("Waiting for ManagedServiceAccounts to be cleaned up")
		Eventually(func(g Gomega) {
			msaList := &msav1beta1.ManagedServiceAccountList{}
			err := hubClient.List(ctx, msaList, client.MatchingLabels{meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(msaList.Items).To(BeEmpty(), "ManagedServiceAccounts still exist")
		}).Should(Succeed())

		Step("Waiting for ManifestWorks to be cleaned up")
		Eventually(func(g Gomega) {
			mwList := &workv1.ManifestWorkList{}
			err := hubClient.List(ctx, mwList, client.MatchingLabels{meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(mwList.Items).To(BeEmpty(), "ManifestWorks still exist")
		}).Should(Succeed())
	})

	// TODO(mkolesni): Once feedback rules are implemented (#89), replace MW/spoke checks
	// with mesh status assertions (ConditionOperatorInstalled=True per cluster).
	It("creates operator resources on spoke clusters", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Verifying ManifestWork is Available on hub for %s", cluster)
			Eventually(func(g Gomega) {
				mw, err := getOperatorMW(ctx, cluster)
				g.Expect(err).NotTo(HaveOccurred())
				available := meta.FindStatusCondition(mw.Status.Conditions, string(workv1.WorkAvailable))
				g.Expect(available).NotTo(BeNil())
				g.Expect(available.Status).To(Equal(metav1.ConditionTrue))
			}).Should(Succeed())

			Step("Verifying operator namespace exists on %s", cluster)
			err := spokeClient.Get(ctx, key.Of(testOperatorNamespace), &corev1.Namespace{})
			Expect(err).NotTo(HaveOccurred())

			Step("Verifying OperatorGroup exists on %s", cluster)
			ogList := &operatorsv1.OperatorGroupList{}
			err = spokeClient.List(ctx, ogList, client.InNamespace(testOperatorNamespace))
			Expect(err).NotTo(HaveOccurred())
			Expect(ogList.Items).To(HaveLen(1))

			Step("Verifying Subscription content on %s", cluster)
			sub, err := getSubscription(ctx, spokeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(sub.Spec.Package).To(Equal(testOperatorName))
			Expect(sub.Spec.Channel).To(Equal(testDefaultChannel))
			Expect(sub.Spec.CatalogSource).To(Equal(testCatalogSource))
		}
	})

	It("updates spoke resources when operator config changes", func(ctx SpecContext) {
		Step("Updating mesh operator channel")
		Expect(getMesh(ctx, mesh)).To(Succeed())
		mesh.Spec.Operator.Channel = "candidate"
		Expect(hubClient.Update(ctx, mesh)).To(Succeed())

		Step("Verifying Subscription channel is updated on spoke clusters")
		for cluster, spokeClient := range spokeClients {
			Eventually(func(g Gomega) {
				g.Expect(getSubscription(ctx, spokeClient)).To(HaveField("Spec.Channel", "candidate"),
					"expected Subscription channel on %s to be updated", cluster)
			}).Should(Succeed())
		}
	})

	It("cleans up resources on mesh deletion", func(ctx SpecContext) {
		Step("Deleting the mesh CR")
		util.DeleteResource(ctx, hubClient, mesh, mesh.Name, mesh.Namespace)

		Step("Verifying ManifestWorks are removed from hub")
		for _, cluster := range clusters {
			util.ExpectResourceDeleted(ctx, hubClient, &workv1.ManifestWork{}, meshcontroller.OperatorManifestWorkName, cluster)
		}

		Step("Verifying Subscriptions are removed from spoke clusters")
		for _, spokeClient := range spokeClients {
			// Spoke-side cleanup depends on the OCM work agent processing the
			// ManifestWork deletion and OLM processing any Subscription finalizers,
			// which can take longer than the default timeout in CI.
			util.ExpectResourceDeleted(ctx, spokeClient, &operatorsv1alpha1.Subscription{}, testOperatorName, testOperatorNamespace, 2*time.Minute)
		}
	})

	It("creates ManagedServiceAccounts for each spoke cluster", func(ctx SpecContext) {
		Eventually(func(g Gomega) {
			msaList := listMeshMSAs(g, ctx, mesh)
			g.Expect(msaList.Items).To(HaveLen(len(clusters)),
				"expected one MSA per cluster in the ClusterSet")
		}).Should(Succeed())
	})

	It("MSA addon creates token secrets on the hub", func(ctx SpecContext) {
		Eventually(func(g Gomega) {
			msaList := listMeshMSAs(g, ctx, mesh)
			for _, msa := range msaList.Items {
				g.Expect(msa.Status.TokenSecretRef).NotTo(BeNil(),
					"expected MSA %s/%s to have tokenSecretRef", msa.Namespace, msa.Name)

				secretCreated := meta.FindStatusCondition(msa.Status.Conditions, msav1beta1.ConditionTypeSecretCreated)
				g.Expect(secretCreated).NotTo(BeNil())
				g.Expect(secretCreated.Status).To(Equal(metav1.ConditionTrue))

				secret := &corev1.Secret{}
				g.Expect(hubClient.Get(ctx, key.Of(msa.Status.TokenSecretRef.Name, msa.Namespace), secret)).To(Succeed(),
					"token secret %s/%s should exist", msa.Namespace, msa.Status.TokenSecretRef.Name)
			}
		}).WithTimeout(2 * time.Minute).Should(Succeed())
	})

	It("cleans up ManagedServiceAccounts on mesh deletion", func(ctx SpecContext) {
		Step("Deleting the mesh CR")
		util.DeleteResource(ctx, hubClient, mesh, mesh.Name, mesh.Namespace)

		Step("Verifying ManagedServiceAccounts are removed from hub")
		Eventually(func(g Gomega) {
			msaList := listMeshMSAs(g, ctx, mesh)
			g.Expect(msaList.Items).To(BeEmpty(), "expected all mesh-owned MSAs to be deleted")
		}).Should(Succeed())
	})

	// TODO: Once the controller builds Istio remote secrets from MSA token secrets,
	// verify that for each cluster a kubeconfig-style Secret with the "istio/multiCluster"
	// label is created on the hub. Assert the secret data contains a valid kubeconfig with
	// the spoke API server URL and the token from the MSA token secret.
	PIt("constructs Istio remote secrets from MSA token secrets")

	// TODO: Once the controller distributes remote secrets to peer clusters via
	// ManifestWork, verify that for N clusters each cluster receives (N-1) remote secret
	// ManifestWorks. Assert the ManifestWork payload contains the peer's remote secret.
	// Verify the Secret actually appears on the spoke cluster (using spokeClients).
	// Also verify that when a cluster is removed, its remote secrets are cleaned up from
	// all remaining peers.
	PIt("distributes remote secrets to peer clusters via ManifestWork")
})

func getMesh(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	return hubClient.Get(ctx, key.For(mesh), mesh)
}

func getOperatorMW(ctx context.Context, cluster string) (*workv1.ManifestWork, error) {
	mw := &workv1.ManifestWork{}
	err := hubClient.Get(ctx, key.Of(meshcontroller.OperatorManifestWorkName, cluster), mw)
	return mw, err
}

func getSubscription(ctx context.Context, spokeClient client.Client) (*operatorsv1alpha1.Subscription, error) {
	sub := &operatorsv1alpha1.Subscription{}
	err := spokeClient.Get(ctx, key.Of(testOperatorName, testOperatorNamespace), sub)
	return sub, err
}

func listMeshMSAs(g Gomega, ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) *msav1beta1.ManagedServiceAccountList {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	g.Expect(hubClient.List(ctx, msaList, client.MatchingLabels{
		meshcontroller.MeshNameLabel:      mesh.Name,
		meshcontroller.MeshNamespaceLabel: mesh.Namespace,
	})).To(Succeed())
	return msaList
}
