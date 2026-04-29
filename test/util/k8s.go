package util

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
)

// UniqueName generates a unique name with the given prefix.
func UniqueName(prefix string) string {
	return prefix + "-" + rand.String(6)
}

// CreateNamespace creates a namespace.
func CreateNamespace(ctx context.Context, k8sClient client.Client, name string) {
	Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})).To(Succeed())
}

// CreateCacertsSecret creates a TLS secret that simulates what cert-manager would create.
func CreateCacertsSecret(ctx context.Context, k8sClient client.Client, namespace, clusterName, meshName, meshNamespace string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cacerts-%s", clusterName),
			Namespace: namespace,
			Labels: map[string]string{
				meshcontroller.LabelManagedBy:     "multicluster-mesh-addon",
				meshcontroller.LabelMeshName:      meshName,
				meshcontroller.LabelMeshNamespace: meshNamespace,
				meshcontroller.LabelClusterName:   clusterName,
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
