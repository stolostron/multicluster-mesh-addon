//go:build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	controllerName = "multicluster-mesh-controller"

	msaSpokeNamespace = "open-cluster-management-agent-addon"

	testIssuerName       = "mesh-test-root-ca"
	testIssuerKind       = "ClusterIssuer"
	testIssuerSecretName = "mesh-test-root-ca-secret"
)

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
	const (
		clusterSet    = "mesh-cluster-set"
		placementName = "test-mesh-placement"
	)

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

		Step("Binding ManagedClusterSet %s to namespace %s", clusterSet, ns)
		util.CreateManagedClusterSetBinding(ctx, hubClient, clusterSet, ns)

		Step("Creating Placement %s in namespace %s", placementName, ns)
		util.CreatePlacementForClusterSet(ctx, hubClient, placementName, ns, clusterSet)
	})

	AfterAll(func(ctx SpecContext) {
		spokeNamespaces := []string{testOperatorNamespace}
		if mesh != nil {
			spokeNamespaces = append(spokeNamespaces, mesh.GetControlPlaneNamespace())
		}
		collectArtifacts(ctx, "mesh-lifecycle",
			[]string{ns},
			spokeNamespaces,
		)

		// Do not leave behind any resources to be able to reuse the same env.
		if mesh != nil {
			Step("Deleting test mesh %s/%s", mesh.Namespace, mesh.Name)
			_ = hubClient.Delete(ctx, mesh)
		}

		Step("Deleting test namespace %s", ns)
		err := client.IgnoreNotFound(hubClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("should deploy the mesh", func(ctx SpecContext) {
		Step("Creating test mesh")
		mesh = util.CreateMultiClusterMesh(ctx, hubClient, util.UniqueName("test-mesh"), ns, placementName,
			meshv1alpha1.MultiClusterMeshSpec{
				Security: meshv1alpha1.SecurityConfig{
					Trust: meshv1alpha1.TrustConfig{
						CertManager: meshv1alpha1.CertManagerConfig{
							IssuerRef: meshv1alpha1.IssuerReference{
								Name: testIssuerName,
								Kind: testIssuerKind,
							},
						},
					},
				},
			})

		Step("Waiting for mesh to become ready")
		Eventually(func(g Gomega) {
			g.Expect(getMesh(ctx, mesh)).To(Succeed())
			g.Expect(meta.IsStatusConditionTrue(mesh.Status.Conditions, meshv1alpha1.ConditionReady)).To(BeTrue())
		}).Should(Succeed())
	})

	It("should populate status.controlPlaneNamespace and create labeled spoke namespaces", func(ctx SpecContext) {
		cpNamespace := mesh.GetControlPlaneNamespace()

		Step("Verifying status.controlPlaneNamespace")
		Expect(getMesh(ctx, mesh)).To(Succeed())
		Expect(mesh.Status.ControlPlaneNamespace).To(Equal(cpNamespace))

		for cluster, spokeClient := range spokeClients {
			Step("Verifying control plane namespace %s on %s", cpNamespace, cluster)
			cpns := &corev1.Namespace{}
			Eventually(func(g Gomega) {
				g.Expect(spokeClient.Get(ctx, key.Of(cpNamespace), cpns)).To(Succeed())
				g.Expect(cpns.Labels[meshcontroller.IstioNetworkLabel]).To(Equal(cluster))
				g.Expect(cpns.Labels[meshcontroller.MeshNameLabel]).To(Equal(mesh.Name))
				g.Expect(cpns.Labels[meshcontroller.MeshNamespaceLabel]).To(Equal(mesh.Namespace))
			}).Should(Succeed())
			track(cluster, spokeClient, cpns)
		}
	})

	It("should distribute cacerts secrets to spoke clusters", func(ctx SpecContext) {
		cpNamespace := mesh.GetControlPlaneNamespace()

		// The root CA is expected to be pre-created by setup-test-issuer before the test run.
		Step("Fetching hub root CA certificate")
		rootCASecret := &corev1.Secret{}
		Expect(hubClient.Get(ctx, key.Of(testIssuerSecretName, "cert-manager"), rootCASecret)).To(Succeed())
		hubRootCert := rootCASecret.Data["tls.crt"]
		Expect(hubRootCert).NotTo(BeEmpty())

		rootPool := x509.NewCertPool()
		Expect(rootPool.AppendCertsFromPEM(hubRootCert)).To(BeTrue(), "failed to parse hub root CA")

		for cluster, spokeClient := range spokeClients {
			cs := findClusterStatus(mesh, cluster)
			Expect(cs).NotTo(BeNil(), "missing cluster status for %s", cluster)
			Step("Verifying TrustDistributed condition for %s", cluster)
			Expect(meta.IsStatusConditionTrue(cs.Conditions, meshv1alpha1.ConditionTrustDistributed)).To(BeTrue(),
				"expected TrustDistributed=True for %s", cluster)

			Step("Verifying DiscoveryDistributed condition for %s", cluster)
			Expect(meta.IsStatusConditionTrue(cs.Conditions, meshv1alpha1.ConditionDiscoveryDistributed)).To(BeTrue(),
				"expected DiscoveryDistributed=True for %s", cluster)

			Step("Verifying cacerts secret and certificate chain on %s", cluster)
			secret := &corev1.Secret{}
			var cert *x509.Certificate
			Eventually(func(g Gomega) {
				g.Expect(spokeClient.Get(ctx, key.Of(meshcontroller.CacertsSecretName, cpNamespace), secret)).To(Succeed())
				g.Expect(secret.Type).To(Equal(corev1.SecretTypeTLS))
				g.Expect(secret.Data["tls.crt"]).NotTo(BeEmpty(), "tls.crt")
				g.Expect(secret.Data["tls.key"]).NotTo(BeEmpty(), "tls.key")
				g.Expect(secret.Data["ca.crt"]).To(Equal(hubRootCert), "ca.crt vs hub root CA")

				block, _ := pem.Decode(secret.Data["tls.crt"])
				g.Expect(block).NotTo(BeNil(), "failed to decode tls.crt PEM")

				var err error
				cert, err = x509.ParseCertificate(block.Bytes)
				g.Expect(err).NotTo(HaveOccurred())

				// cert-manager stamps NotBefore from the hub clock without backdating, so a
				// freshly issued intermediate can look not-yet-valid to a test runner whose
				// clock lags the hub. Retrying lets the local clock catch up.
				_, err = cert.Verify(x509.VerifyOptions{Roots: rootPool})
				g.Expect(err).NotTo(HaveOccurred(), "intermediate CA verification failed")
			}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
			track(cluster, spokeClient, secret)

			Step("Verifying certificate subject on %s", cluster)
			Expect(cert.IsCA).To(BeTrue())
			Expect(cert.Subject.CommonName).To(Equal("Istio CA"))
			Expect(cert.Subject.Organization).To(ConsistOf(mesh.GetTrustDomain()))
			Expect(cert.Subject.OrganizationalUnit).To(ConsistOf(cluster))
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
			}).Should(Succeed())
		}
	})

	It("should create istio-reader ClusterRole and ClusterRoleBinding on each spoke cluster", func(ctx SpecContext) {
		for cluster, spokeClient := range spokeClients {
			Step("Verifying istio-reader RBAC for %s", cluster)
			Eventually(func(g Gomega) {
				msaList := listMeshMSAs(g, ctx, mesh, client.InNamespace(cluster))
				g.Expect(msaList.Items).To(HaveLen(1),
					"expected exactly one MSA for cluster %s", cluster)
				rbacName := meshcontroller.IstioReaderName(mesh)

				cr := &rbacv1.ClusterRole{}
				g.Expect(spokeClient.Get(ctx, key.Of(rbacName), cr)).To(Succeed(),
					"ClusterRole %s should exist on spoke %s", rbacName, cluster)
				g.Expect(cr.Rules).NotTo(BeEmpty())
				track(cluster, spokeClient, cr)

				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(spokeClient.Get(ctx, key.Of(rbacName), crb)).To(Succeed(),
					"ClusterRoleBinding %s should exist on spoke %s", rbacName, cluster)
				g.Expect(crb.RoleRef.Kind).To(Equal("ClusterRole"))
				g.Expect(crb.RoleRef.Name).To(Equal(rbacName))
				g.Expect(crb.Subjects).To(HaveLen(1))
				g.Expect(crb.Subjects[0].Kind).To(Equal("ServiceAccount"))
				g.Expect(crb.Subjects[0].Name).To(Equal(msaList.Items[0].Name))
				g.Expect(crb.Subjects[0].Namespace).To(Equal(msaSpokeNamespace))
				track(cluster, spokeClient, crb)
			}).Should(Succeed())
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

func listMeshMSAs(g Gomega, ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, opts ...client.ListOption) *msav1beta1.ManagedServiceAccountList {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	listOpts := append([]client.ListOption{client.MatchingLabels{
		meshcontroller.MeshNameLabel:      mesh.Name,
		meshcontroller.MeshNamespaceLabel: mesh.Namespace,
	}}, opts...)
	g.Expect(hubClient.List(ctx, msaList, listOpts...)).To(Succeed())
	return msaList
}
