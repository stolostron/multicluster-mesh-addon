//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"reflect"
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

	msaSpokeNamespace = "open-cluster-management-agent-addon"
)

var clusters = []string{"cluster1", "cluster2"}

type trackedResource struct {
	cluster string
	c       client.Client
	obj     client.Object
}

func (r trackedResource) Key() string {
	kind := reflect.TypeOf(r.obj).Elem().Name()
	return fmt.Sprintf("%s %s on %s", kind, client.ObjectKeyFromObject(r.obj), r.cluster)
}

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
		mesh    *meshv1alpha1.MultiClusterMesh
		ns      string
		created = map[string]trackedResource{}
	)

	track := func(cluster string, c client.Client, obj client.Object) {
		r := trackedResource{cluster: cluster, c: c, obj: obj.DeepCopyObject().(client.Object)}
		created[r.Key()] = r
	}

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

	It("should deploy the mesh", func(ctx SpecContext) {
		Step("Creating test mesh")
		mesh = util.CreateMultiClusterMesh(ctx, hubClient, util.UniqueName("test-mesh"), ns, "mesh-cluster-set",
			meshv1alpha1.MultiClusterMeshSpec{
				Operator: meshv1alpha1.OperatorConfig{
					Name:            testOperatorName,
					Namespace:       testOperatorNamespace,
					Source:          testCatalogSource,
					SourceNamespace: testCatalogNamespace,
				}})

		Step("Waiting for mesh to become ready")
		Eventually(func(g Gomega) {
			g.Expect(getMesh(ctx, mesh)).To(Succeed())
			g.Expect(meta.IsStatusConditionTrue(mesh.Status.Conditions, meshv1alpha1.ConditionReady)).To(BeTrue())
		}).WithTimeout(2 * time.Minute).Should(Succeed())
	})

	It("should deploy operator resources to spoke clusters", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Verifying OperatorInstalled condition for %s", cluster)
			cs := findClusterStatus(mesh, cluster)
			Expect(cs).NotTo(BeNil(), "missing cluster status for %s", cluster)
			Expect(meta.IsStatusConditionTrue(cs.Conditions, meshv1alpha1.ConditionOperatorInstalled)).To(BeTrue(),
				"expected OperatorInstalled=True for %s", cluster)

			Step("Verifying control plane namespace exists on %s", cluster)
			cpns := &corev1.Namespace{}
			Eventually(func(g Gomega) {
				g.Expect(spokeClient.Get(ctx, key.Of("istio-system"), cpns)).To(Succeed())
				g.Expect(cpns.Labels[meshcontroller.IstioNetworkLabel]).To(Equal(cluster))
			}).Should(Succeed())
			track(cluster, spokeClient, cpns)

			Step("Verifying operator namespace exists on %s", cluster)
			ns := &corev1.Namespace{}
			Expect(spokeClient.Get(ctx, key.Of(testOperatorNamespace), ns)).To(Succeed())
			track(cluster, spokeClient, ns)

			Step("Verifying OperatorGroup exists on %s", cluster)
			ogList := &operatorsv1.OperatorGroupList{}
			err := spokeClient.List(ctx, ogList, client.InNamespace(testOperatorNamespace))
			Expect(err).NotTo(HaveOccurred())
			Expect(ogList.Items).To(HaveLen(1))

			Step("Verifying Subscription content on %s", cluster)
			sub, err := getSubscription(ctx, spokeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(sub.Spec.Package).To(Equal(testOperatorName))
			Expect(sub.Spec.Channel).To(Equal(testDefaultChannel))
			Expect(sub.Spec.CatalogSource).To(Equal(testCatalogSource))
			Expect(sub.Spec.CatalogSourceNamespace).To(Equal(testCatalogNamespace))
		}
	})

	It("should update spoke resources when operator config changes", func(ctx SpecContext) {
		Step("Updating mesh operator channel")
		Expect(getMesh(ctx, mesh)).To(Succeed())
		mesh.Spec.Operator.Channel = "candidate"
		Expect(hubClient.Update(ctx, mesh)).To(Succeed())

		for cluster, spokeClient := range spokeClients {
			Step("Verifying Subscription channel is updated on %s", cluster)
			Eventually(func(g Gomega) {
				g.Expect(getSubscription(ctx, spokeClient)).To(HaveField("Spec.Channel", "candidate"))
			}).Should(Succeed())
		}
	})

	It("should create service accounts and token secrets for each spoke cluster", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Verifying service account and token secret for %s", cluster)
			Eventually(func(g Gomega) {
				msaList := listMeshMSAs(g, ctx, mesh, client.InNamespace(cluster))
				g.Expect(msaList.Items).To(HaveLen(1),
					"expected exactly one MSA for cluster %s", cluster)

				msa := msaList.Items[0]
				sa := &corev1.ServiceAccount{}
				g.Expect(spokeClient.Get(ctx, key.Of(msa.Name, msaSpokeNamespace), sa)).To(Succeed(),
					"ServiceAccount %s/%s should exist on spoke %s", msaSpokeNamespace, msa.Name, cluster)
				track(cluster, spokeClient, sa)

				g.Expect(msa.Status.TokenSecretRef).NotTo(BeNil(),
					"expected MSA %s/%s to have tokenSecretRef", msa.Namespace, msa.Name)

				secret := &corev1.Secret{}
				g.Expect(hubClient.Get(ctx, key.Of(msa.Status.TokenSecretRef.Name, cluster), secret)).To(Succeed(),
					"token secret %s/%s should exist on hub", cluster, msa.Status.TokenSecretRef.Name)
				track(cluster, hubClient, secret)
			}).WithTimeout(2 * time.Minute).Should(Succeed())
		}
	})

	It("should remove spoke resources when mesh is deleted", func(ctx SpecContext) {
		Step("Deleting the mesh CR")
		util.DeleteResource(ctx, hubClient, mesh, mesh.Name, mesh.Namespace)

		for label, r := range created {
			Step("Verifying %s is removed", label)
			util.ExpectResourceDeleted(ctx, r.c, r.obj, r.obj.GetName(), r.obj.GetNamespace(), 2*time.Minute)
		}
	})
})

func getMesh(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	return hubClient.Get(ctx, key.For(mesh), mesh)
}

func findClusterStatus(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) *meshv1alpha1.ClusterMeshStatus {
	for _, cs := range mesh.Status.ClusterStatus {
		if cs.ClusterName == clusterName {
			return &cs
		}
	}
	return nil
}

func getSubscription(ctx context.Context, spokeClient client.Client) (*operatorsv1alpha1.Subscription, error) {
	sub := &operatorsv1alpha1.Subscription{}
	err := spokeClient.Get(ctx, key.Of(testOperatorName, testOperatorNamespace), sub)
	return sub, err
}

func listMeshMSAs(g Gomega, ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, opts ...client.ListOption) *msav1beta1.ManagedServiceAccountList {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	listOpts := append([]client.ListOption{client.MatchingLabels{
		meshcontroller.MeshNameLabel:      mesh.Name,
		meshcontroller.MeshNamespaceLabel: mesh.Namespace,
	}}, opts...)
	g.Expect(hubClient.List(ctx, msaList, listOpts...)).To(Succeed())
	return msaList
}
