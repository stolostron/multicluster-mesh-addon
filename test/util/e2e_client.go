package util

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// E2EClient wraps client.Client with a kubeconfig path, providing kubectl-based
// operations (like pod exec, apply, delete) alongside the standard controller-runtime client.
type E2EClient struct {
	client.Client
	Kubeconfig string
}

func NewE2EClient(c client.Client, kubeconfig string) *E2EClient {
	return &E2EClient{Client: c, Kubeconfig: kubeconfig}
}

func (c *E2EClient) ApplyFile(ctx context.Context, path string, vars map[string]string, namespace ...string) {
	args := []string{"apply", "--kubeconfig", c.Kubeconfig}
	if len(namespace) > 0 && namespace[0] != "" {
		args = append(args, "-n", namespace[0])
	}
	args = append(args, "-f", "-")
	c.runKubectl(ctx, path, vars, args)
}

func (c *E2EClient) DeleteFile(ctx context.Context, path string, vars map[string]string, namespace ...string) {
	args := []string{"delete", "--kubeconfig", c.Kubeconfig, "--ignore-not-found"}
	if len(namespace) > 0 && namespace[0] != "" {
		args = append(args, "-n", namespace[0])
	}
	args = append(args, "-f", "-")
	c.runKubectl(ctx, path, vars, args)
}

func (c *E2EClient) runKubectl(ctx context.Context, path string, vars map[string]string, args []string) {
	rendered := renderYAML(path, vars)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = bytes.NewReader(rendered)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	Expect(cmd.Run()).To(Succeed(), "kubectl %s failed for %s: %s", args[0], path, strings.TrimSpace(stderr.String()))
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
