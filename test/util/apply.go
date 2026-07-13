package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

func LoadAndApplyYAML(ctx context.Context, k8sClient client.Client, path string, vars map[string]string) {
	loadAndApply(ctx, k8sClient, path, vars, "")
}

// LoadAndApplyYAMLInNamespace works like LoadAndApplyYAML but sets the given namespace
// on any resource that doesn't already have one (like kubectl apply -n).
func LoadAndApplyYAMLInNamespace(ctx context.Context, k8sClient client.Client, path, namespace string, vars map[string]string) {
	loadAndApply(ctx, k8sClient, path, vars, namespace)
}

func loadAndApply(ctx context.Context, k8sClient client.Client, path string, vars map[string]string, defaultNS string) {
	rendered := renderYAML(path, vars)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred(), "failed to decode YAML document from %s", path)
		}
		if obj.Object == nil {
			continue
		}
		if defaultNS != "" && obj.GetNamespace() == "" {
			obj.SetNamespace(defaultNS)
		}
		Expect(k8sClient.Patch(ctx, obj, client.Apply, client.FieldOwner("e2e-test"), client.ForceOwnership)). //nolint:staticcheck // client.Apply is the only way to SSA with unstructured objects
			To(Succeed(), "failed to apply %s %s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	}
}

func DeleteYAMLResources(ctx context.Context, k8sClient client.Client, path string, vars map[string]string) {
	deleteResources(ctx, k8sClient, path, vars, "")
}

// DeleteYAMLResourcesInNamespace works like DeleteYAMLResources but sets the given
// namespace on any resource that doesn't already have one.
func DeleteYAMLResourcesInNamespace(ctx context.Context, k8sClient client.Client, path, namespace string, vars map[string]string) {
	deleteResources(ctx, k8sClient, path, vars, namespace)
}

func deleteResources(ctx context.Context, k8sClient client.Client, path string, vars map[string]string, defaultNS string) {
	rendered := renderYAML(path, vars)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			Expect(err).NotTo(HaveOccurred(), "failed to decode YAML document from %s", path)
		}
		if obj.Object == nil {
			continue
		}
		if defaultNS != "" && obj.GetNamespace() == "" {
			obj.SetNamespace(defaultNS)
		}
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, obj))
	}
}

func renderYAML(path string, vars map[string]string) []byte {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), "failed to read YAML file %s", path)

	if len(vars) == 0 {
		return data
	}
	tmpl, err := template.New("manifest").Parse(string(data))
	Expect(err).NotTo(HaveOccurred(), "failed to parse template %s", path)
	var buf bytes.Buffer
	Expect(tmpl.Execute(&buf, vars)).To(Succeed(), "failed to execute template %s", path)
	return buf.Bytes()
}

func WaitForDeploymentReady(ctx context.Context, k8sClient client.Client, name, namespace string, timeout time.Duration) {
	Eventually(func(g Gomega) {
		deploy := &appsv1.Deployment{}
		g.Expect(k8sClient.Get(ctx, key.Of(name, namespace), deploy)).To(Succeed())
		var found bool
		for _, c := range deploy.Status.Conditions {
			if c.Type == appsv1.DeploymentAvailable {
				g.Expect(c.Status).To(Equal(corev1.ConditionTrue),
					"deployment %s/%s is not Available: %s", namespace, name, c.Message)
				found = true
				break
			}
		}
		g.Expect(found).To(BeTrue(), "deployment %s/%s has no Available condition", namespace, name)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
}

func WaitForLoadBalancerIP(ctx context.Context, k8sClient client.Client, name, namespace string, timeout time.Duration) string {
	var ip string
	Eventually(func(g Gomega) {
		svc := &corev1.Service{}
		g.Expect(k8sClient.Get(ctx, key.Of(name, namespace), svc)).To(Succeed())
		g.Expect(svc.Status.LoadBalancer.Ingress).NotTo(BeEmpty(),
			"service %s/%s has no LoadBalancer ingress", namespace, name)
		ip = svc.Status.LoadBalancer.Ingress[0].IP
		if ip == "" {
			ip = svc.Status.LoadBalancer.Ingress[0].Hostname
		}
		g.Expect(ip).NotTo(BeEmpty(), "service %s/%s has no IP or hostname", namespace, name)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return ip
}

func WaitForPodReady(ctx context.Context, k8sClient client.Client, namespace string, labels map[string]string, timeout time.Duration) string {
	var podName string
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(k8sClient.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels(labels))).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty(), "no pods found with labels %v in %s", labels, namespace)
		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.Status.Phase == corev1.PodRunning {
				for _, c := range pod.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						podName = pod.Name
						return
					}
				}
			}
		}
		g.Expect(false).To(BeTrue(), "no ready pod found with labels %v in %s", labels, namespace)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return podName
}

func ExecInPod(ctx context.Context, restConfig *rest.Config, namespace, podName, container string, command []string) (string, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		Param("container", container).
		Param("stdout", "true").
		Param("stderr", "true")
	for _, c := range command {
		req = req.Param("command", c)
	}

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("exec failed (stderr: %s): %w", strings.TrimSpace(stderr.String()), err)
	}

	return stdout.String(), nil
}

func CreateNamespaceWithLabels(ctx context.Context, k8sClient client.Client, name string, labels map[string]string) {
	nsObj := &unstructured.Unstructured{}
	nsObj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
	nsObj.SetName(name)
	nsObj.SetLabels(labels)
	Expect(k8sClient.Patch(ctx, nsObj, client.Apply, client.FieldOwner("e2e-test"), client.ForceOwnership)). //nolint:staticcheck // client.Apply is the only way to SSA with unstructured objects
		To(Succeed(), "failed to create/update namespace %s", name)
}

func EnsureNamespace(ctx context.Context, k8sClient client.Client, name string) {
	ns := &corev1.Namespace{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err == nil {
		return
	}
	Expect(client.IgnoreNotFound(err)).To(Succeed())
	CreateNamespace(ctx, k8sClient, name)
}
