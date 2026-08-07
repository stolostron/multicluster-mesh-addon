package util

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

// MustAddToScheme registers types with the global scheme, failing the test on error.
func MustAddToScheme(fns ...func(*runtime.Scheme) error) {
	for _, fn := range fns {
		Expect(fn(scheme.Scheme)).To(Succeed())
	}
}

// UniqueName generates a unique name with the given prefix.
func UniqueName(prefix string) string {
	return prefix + "-" + rand.String(6)
}

// CreateNamespace creates a namespace with optional labels. If the namespace
// already exists, the call is a no-op.
func CreateNamespace(ctx context.Context, k8sClient client.Client, name string, labels ...map[string]string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if len(labels) > 0 && labels[0] != nil {
		ns.Labels = labels[0]
	}
	Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
}

// CreateCacertsSecret creates a TLS secret that simulates what cert-manager would create.
func CreateCacertsSecret(ctx context.Context, k8sClient client.Client, namespace, clusterName, meshName, meshNamespace string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cacerts-%s", clusterName),
			Namespace: namespace,
			Labels: map[string]string{
				meshcontroller.ManagedByLabel:     meshcontroller.ManagedByValue,
				meshcontroller.MeshNameLabel:      meshName,
				meshcontroller.MeshNamespaceLabel: meshNamespace,
				meshcontroller.ClusterNameLabel:   clusterName,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": []byte("test-cert-data"),
			"tls.key": []byte("test-key-data"),
			"ca.crt":  []byte("test-ca-data"),
		},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
}

// CreateMsaSecret creates a ServiceAccount and a secret that simulates what ManagedServiceAccount controller would create.
func CreateMsaSecret(ctx context.Context, k8sClient client.Client, clusterName, meshName, meshNamespace string) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", meshNamespace, "istio-reader", meshName),
			Namespace: clusterName,
		},
	}
	Expect(k8sClient.Create(ctx, sa)).To(Succeed())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", meshNamespace, "istio-reader", meshName),
			Namespace: clusterName,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": fmt.Sprintf("%s-%s-%s", meshNamespace, "istio-reader", meshName),
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"ca.crt": []byte("test-ca-data"),
			"token":  []byte("test-token-data"),
		},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
}

// CreateIstioRemoteSecret creates a secret that simulates what ManifestWorkReplicaSet controller would distribute.
func CreateIstioRemoteSecret(ctx context.Context, k8sClient client.Client, clusterName, meshName, meshNamespace, cpNamespace string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s-%s", meshNamespace, meshName, "istio-remote-secret", clusterName),
			Namespace: cpNamespace,
			Labels: map[string]string{
				"istio/multiCluster":              "true",
				meshcontroller.ManagedByLabel:     meshcontroller.ManagedByValue,
				meshcontroller.MeshNameLabel:      meshName,
				meshcontroller.MeshNamespaceLabel: meshNamespace,
				meshcontroller.ClusterNameLabel:   clusterName,
			},
			Annotations: map[string]string{
				"networking.istio.io/cluster": clusterName,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": []byte("test-ca-data"),
			"token":  []byte("test-token-data"),
		},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
}

// DeleteResource deletes a Kubernetes resource and waits for it to be fully removed.
func DeleteResource(ctx context.Context, k8sClient client.Client, obj client.Object, name, namespace string) {
	Expect(k8sClient.Get(ctx, key.Of(name, namespace), obj)).To(Succeed())
	Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	ExpectResourceDeleted(ctx, k8sClient, obj, name, namespace)
}

// ExpectResourceDeleted waits for a resource to be fully removed (e.g. after a side-effect deletion by a controller).
// An optional timeout overrides the default Eventually timeout.
func ExpectResourceDeleted(ctx context.Context, k8sClient client.Client, obj client.Object, name, namespace string, timeout ...time.Duration) {
	e := Eventually(func() bool {
		err := k8sClient.Get(ctx, key.Of(name, namespace), obj)
		return errors.IsNotFound(err)
	})
	if len(timeout) > 0 {
		e = e.WithTimeout(timeout[0])
	}
	e.Should(BeTrue())
}
