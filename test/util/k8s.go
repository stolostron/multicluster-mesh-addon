package util

import (
	"context"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// DeleteResource deletes a Kubernetes resource and waits for it to be fully removed.
func DeleteResource(ctx context.Context, k8sClient client.Client, obj client.Object, name, namespace string) {
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)).To(Succeed())
	Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	ExpectResourceDeleted(ctx, k8sClient, obj, name, namespace)
}

// ExpectResourceDeleted waits for a resource to be fully removed (e.g. after a side-effect deletion by a controller).
func ExpectResourceDeleted(ctx context.Context, k8sClient client.Client, obj client.Object, name, namespace string) {
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
		return errors.IsNotFound(err)
	}).Should(BeTrue())
}
