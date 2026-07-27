package util

import (
	"bytes"
	"context"
	"os"
	"text/template"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

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
		g.Expect(deploy.Status.ObservedGeneration).To(Equal(deploy.Generation),
			"deployment %s/%s status not yet observed for current generation", namespace, name)
		g.Expect(deploy.Status.Conditions).To(ContainElement(And(
			HaveField("Type", appsv1.DeploymentAvailable),
			HaveField("Status", corev1.ConditionTrue),
		)), "deployment %s/%s is not Available", namespace, name)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
}

func WaitForLoadBalancerIP(ctx context.Context, k8sClient client.Client, name, namespace string, timeout time.Duration) string {
	var address string
	Eventually(func(g Gomega) {
		svc := &corev1.Service{}
		g.Expect(k8sClient.Get(ctx, key.Of(name, namespace), svc)).To(Succeed())
		g.Expect(svc.Status.LoadBalancer.Ingress).NotTo(BeEmpty(),
			"service %s/%s has no LoadBalancer ingress", namespace, name)
		address = svc.Status.LoadBalancer.Ingress[0].IP
		if address == "" {
			address = svc.Status.LoadBalancer.Ingress[0].Hostname
		}
		g.Expect(address).NotTo(BeEmpty(), "service %s/%s has no IP or hostname", namespace, name)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return address
}

func WaitForPodReady(ctx context.Context, k8sClient client.Client, namespace string, labels map[string]string, timeout time.Duration) string {
	var podName string
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(k8sClient.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels(labels))).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty(), "no pods found with labels %v in %s", labels, namespace)
		found := false
		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.Status.Phase == corev1.PodRunning {
				for _, c := range pod.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						podName = pod.Name
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		g.Expect(found).To(BeTrue(), "no ready pod found with labels %v in %s", labels, namespace)
	}).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return podName
}
