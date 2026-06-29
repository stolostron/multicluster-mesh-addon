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
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

const (
	controllerNamespace = "multicluster-mesh-system"
	controllerName      = "multicluster-mesh-controller"
)

var clusters = []string{"cluster1", "cluster2"}

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
		mesh = util.CreateMultiClusterMesh(ctx, hubClient, util.UniqueName("test-mesh"), ns, "mesh-cluster-set")

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
			err := spokeClient.Get(ctx, key.Of(meshcontroller.DefaultOperatorNs), &corev1.Namespace{})
			Expect(err).NotTo(HaveOccurred())

			Step("Verifying OperatorGroup exists on %s", cluster)
			ogList := &operatorsv1.OperatorGroupList{}
			err = spokeClient.List(ctx, ogList, client.InNamespace(meshcontroller.DefaultOperatorNs))
			Expect(err).NotTo(HaveOccurred())
			Expect(ogList.Items).To(HaveLen(1))

			Step("Verifying Subscription content on %s", cluster)
			sub, err := getSubscription(ctx, spokeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(sub.Spec.Package).To(Equal(meshcontroller.OperatorNameSail))
			Expect(sub.Spec.Channel).To(Equal(meshcontroller.DefaultChannel))
			Expect(sub.Spec.CatalogSource).To(Equal(meshcontroller.DefaultCatalogSource))
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
			util.ExpectResourceDeleted(ctx, spokeClient, &operatorsv1alpha1.Subscription{}, meshcontroller.OperatorNameSail, meshcontroller.DefaultOperatorNs, 2*time.Minute)
		}
	})
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
	err := spokeClient.Get(ctx, key.Of(meshcontroller.OperatorNameSail, meshcontroller.DefaultOperatorNs), sub)
	return sub, err
}
