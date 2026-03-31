package util

import (
	"context"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
