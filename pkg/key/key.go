package key

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Of returns a NamespacedName from a name and optional namespace.
func Of(name string, namespace ...string) types.NamespacedName {
	k := types.NamespacedName{Name: name}
	if len(namespace) > 0 {
		k.Namespace = namespace[0]
	}
	return k
}

// For returns the NamespacedName for an existing object.
func For(obj client.Object) types.NamespacedName {
	return client.ObjectKeyFromObject(obj)
}
