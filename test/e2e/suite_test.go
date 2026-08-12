//go:build e2e || e2e_multicluster

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/test/util"
)

const controllerNamespace = "multicluster-mesh-system"

var (
	clusters = []string{"cluster1", "cluster2"}

	hubClient    *util.E2EClient
	spokeClients map[string]*util.E2EClient

	testOperatorName      string
	testOperatorNamespace string
	testCatalogSource     string
	testCatalogNamespace  string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Test Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(250 * time.Millisecond)

	util.MustAddToScheme(
		meshv1alpha1.Install,
		clusterv1.Install,
		clusterv1beta2.Install,
		workv1.Install,
		operatorsv1.AddToScheme,
		operatorsv1alpha1.AddToScheme,
		msav1beta1.AddToScheme,
		addonv1beta1.Install,
	)

	hubKubeconfig := env("HUB_KUBECONFIG", ".kube/hub.config")
	cluster1Kubeconfig := env("CLUSTER1_KUBECONFIG", ".kube/cluster1.config")
	cluster2Kubeconfig := env("CLUSTER2_KUBECONFIG", ".kube/cluster2.config")

	hubClient = util.NewE2EClient(clientFrom(hubKubeconfig), hubKubeconfig)
	spokeClients = make(map[string]*util.E2EClient)
	for name, kc := range map[string]string{
		"cluster1": cluster1Kubeconfig,
		"cluster2": cluster2Kubeconfig,
	} {
		spokeClients[name] = util.NewE2EClient(clientFrom(kc), kc)
	}

	Step("Detecting platform (kind vs OCP)")
	detectPlatform()

	Step("Verifying cluster connectivity")
	verifyConnection(ctx, hubClient, "hub")
	for name, c := range spokeClients {
		verifyConnection(ctx, c, name)
	}

	Step("Checking for existing resources that can interfere with our testing")
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	Expect(hubClient.List(ctx, meshList)).To(Succeed())
	Expect(meshList.Items).To(BeEmpty(),
		"existing MultiClusterMesh resources found; run 'make dev-clean-meshes' to clean up")
})

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func clientFrom(kubeconfig string) client.Client {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig from %s", kubeconfig)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create client from %s", kubeconfig)
	return c
}

func Step(format string, args ...any) {
	By(fmt.Sprintf(format, args...))
}

func Success(format string, args ...any) {
	GinkgoWriter.Println("* " + fmt.Sprintf(format, args...))
}

func detectPlatform() {
	platform := os.Getenv("PLATFORM")
	switch platform {
	case "openshift":
		testOperatorName = "servicemeshoperator3"
		testOperatorNamespace = "multicluster-mesh-operator"
		testCatalogSource = "redhat-operators"
		testCatalogNamespace = "openshift-marketplace"
	case "kind":
		testOperatorName = "sailoperator"
		testOperatorNamespace = "sail-operator"
		testCatalogSource = "operatorhubio-catalog"
		testCatalogNamespace = "olm"
	default:
		Fail(fmt.Sprintf("PLATFORM env must be set to 'openshift' or 'kind', got %q", platform))
	}
	GinkgoWriter.Printf("Platform %s: operator=%s/%s, catalog=%s/%s\n",
		platform, testOperatorNamespace, testOperatorName, testCatalogNamespace, testCatalogSource)
}

func collectArtifacts(ctx context.Context, testName string, hubNamespaces []string, spokeNamespaces []string) {
	if os.Getenv("ARTIFACT_DIR") == "" {
		return
	}
	dir := filepath.Join(os.Getenv("ARTIFACT_DIR"), testName)
	Step("Collecting artifacts to %s", dir)

	hubDir := filepath.Join(dir, "hub")
	collectNamespaceArtifacts(ctx, hubClient, hubDir, append(hubNamespaces, controllerNamespace))
	collectHubResources(ctx, hubDir)

	for name, spokeClient := range spokeClients {
		collectNamespaceArtifacts(ctx, spokeClient, filepath.Join(dir, name), spokeNamespaces)
	}
}

func collectHubResources(ctx context.Context, dir string) {
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	if err := hubClient.List(ctx, meshList); err == nil {
		writeYAML(dir, "multiclustermeshes.yaml", meshList)
	}
	mwList := &workv1.ManifestWorkList{}
	if err := hubClient.List(ctx, mwList, client.MatchingLabels{
		meshcontroller.ManagedByLabel: meshcontroller.ManagedByValue,
	}); err == nil {
		for i := range mwList.Items {
			hashSecretsInManifestWork(&mwList.Items[i])
		}
		writeYAML(dir, "manifestworks.yaml", mwList)
	}
}

func hashSecretsInManifestWork(mw *workv1.ManifestWork) {
	for i := range mw.Spec.Workload.Manifests {
		raw := mw.Spec.Workload.Manifests[i].Raw
		if raw == nil {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		if obj["kind"] != "Secret" {
			continue
		}
		hashStringValues(obj, "data")
		hashStringValues(obj, "stringData")
		if updated, err := json.Marshal(obj); err == nil {
			mw.Spec.Workload.Manifests[i].Raw = updated
		}
	}
}

func hashStringValues(obj map[string]any, field string) {
	m, ok := obj[field].(map[string]any)
	if !ok {
		return
	}
	for k, v := range m {
		h := sha256.Sum256([]byte(fmt.Sprint(v)))
		m[k] = "sha256:" + hex.EncodeToString(h[:])
	}
}

func collectNamespaceArtifacts(ctx context.Context, c *util.E2EClient, dir string, namespaces []string) {
	for _, ns := range namespaces {
		nsDir := filepath.Join(dir, ns)
		collectPods(ctx, c, nsDir, ns)
		collectEvents(ctx, c, nsDir, ns)
	}
}

func collectPods(ctx context.Context, c *util.E2EClient, dir, namespace string) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(namespace)); err != nil {
		return
	}

	var lines []string
	for _, p := range pods.Items {
		lines = append(lines, fmt.Sprintf("%-50s %-10s %-20s %s",
			p.Name, string(p.Status.Phase), p.Status.PodIP, p.Spec.NodeName))
		c.SaveLogs(ctx, dir, namespace, p.Name)
	}
	if len(lines) > 0 {
		writeFile(dir, "pods.txt", []byte(fmt.Sprintf("%-50s %-10s %-20s %s\n%s\n",
			"NAME", "STATUS", "IP", "NODE",
			strings.Join(lines, "\n"))))
	}
}

func collectEvents(ctx context.Context, c *util.E2EClient, dir, namespace string) {
	events := &corev1.EventList{}
	if err := c.List(ctx, events, client.InNamespace(namespace)); err != nil {
		return
	}
	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.Before(&events.Items[j].LastTimestamp)
	})

	var lines []string
	for _, e := range events.Items {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
			e.LastTimestamp.Format(time.RFC3339),
			e.Type, e.Reason,
			e.InvolvedObject.Name, e.Message))
	}
	if len(lines) > 0 {
		writeFile(dir, "events.txt", []byte(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n%s\n",
			"LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE",
			strings.Join(lines, "\n"))))
	}
}

func writeYAML(dir, filename string, obj any) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return
	}
	writeFile(dir, filename, data)
}

func writeFile(dir, filename string, data []byte) {
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, filename), data, 0o644)
}


func verifyConnection(ctx context.Context, c client.Client, name string) {
	nsList := &corev1.NamespaceList{}
	Expect(c.List(ctx, nsList)).To(Succeed(),
		"failed to connect to %s cluster", name)
	GinkgoWriter.Printf("Connected to %s cluster (%d namespaces)\n", name, len(nsList.Items))
}
