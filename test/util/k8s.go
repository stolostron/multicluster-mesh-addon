package util

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
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
// The name mirrors the controller's cacerts naming scheme (cacerts-<meshName>.<clusterName>)
func CreateCacertsSecret(ctx context.Context, k8sClient client.Client, mesh *meshv1alpha1.MultiClusterMesh, clusterName string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meshcontroller.CacertsName(mesh, clusterName),
			Namespace: mesh.Namespace,
			Labels: map[string]string{
				meshcontroller.ManagedByLabel:     meshcontroller.ManagedByValue,
				meshcontroller.MeshNameLabel:      mesh.Name,
				meshcontroller.MeshNamespaceLabel: mesh.Namespace,
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
	return secret
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
