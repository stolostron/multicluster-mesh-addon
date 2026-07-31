package util

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// E2EClient wraps client.Client with a kubeconfig path, providing kubectl-based
// operations (like pod exec, apply, delete) alongside the standard controller-runtime client.
// ApplyFile calls are automatically recorded so Cleanup can reverse-delete them.
type E2EClient struct {
	client.Client
	Kubeconfig string
	applied    []appliedFile
}

type appliedFile struct {
	path      string
	vars      map[string]string
	namespace string
}

func NewE2EClient(c client.Client, kubeconfig string) *E2EClient {
	return &E2EClient{Client: c, Kubeconfig: kubeconfig}
}

// ApplyFile renders a YAML template and applies it via kubectl. // The call is recorded so that Cleanup can reverse-delete all applied files.
func (c *E2EClient) ApplyFile(ctx context.Context, path string, vars map[string]string, namespace ...string) {
	ns := ""
	if len(namespace) > 0 {
		ns = namespace[0]
	}
	args := []string{"apply", "--kubeconfig", c.Kubeconfig}
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "-f", "-")
	c.runKubectl(ctx, path, vars, args)
	c.applied = append(c.applied, appliedFile{path: path, vars: vars, namespace: ns})
}

// DeleteFile renders a YAML template and deletes matching resources via kubectl.
func (c *E2EClient) DeleteFile(ctx context.Context, path string, vars map[string]string, namespace ...string) {
	args := []string{"delete", "--kubeconfig", c.Kubeconfig, "--ignore-not-found"}
	if len(namespace) > 0 && namespace[0] != "" {
		args = append(args, "-n", namespace[0])
	}
	args = append(args, "-f", "-")
	c.runKubectl(ctx, path, vars, args)
}

// Cleanup reverse-deletes all files that were applied via ApplyFile.
func (c *E2EClient) Cleanup(ctx context.Context) {
	for i := len(c.applied) - 1; i >= 0; i-- {
		f := c.applied[i]
		c.DeleteFile(ctx, f.path, f.vars, f.namespace)
	}
	c.applied = nil
}

func (c *E2EClient) runKubectl(ctx context.Context, path string, vars map[string]string, args []string) {
	rendered := renderYAML(path, vars)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = bytes.NewReader(rendered)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	Expect(cmd.Run()).To(Succeed(), "kubectl %s failed for %s: %s", args[0], path, strings.TrimSpace(stderr.String()))
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

func (c *E2EClient) Exec(ctx context.Context, namespace, podName, container string, command []string) (string, error) {
	args := []string{"exec", "-n", namespace, podName, "-c", container, "--kubeconfig", c.Kubeconfig, "--"}
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec failed (stderr: %s): %w", strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}
