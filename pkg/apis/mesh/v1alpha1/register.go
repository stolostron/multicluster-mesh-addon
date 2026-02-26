package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupName     = "mesh.open-cluster-management.io"
	GroupVersion  = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}
	schemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	Install       = schemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&MultiClusterMesh{},
		&MultiClusterMeshList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
