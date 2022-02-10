package translate

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	maistrav1 "maistra.io/api/core/v1"
	maistrav2 "maistra.io/api/core/v2"

	meshv1alpha1 "github.com/morvencao/multicluster-mesh-addon/apis/mesh/v1alpha1"
	constants "github.com/morvencao/multicluster-mesh-addon/pkg/constants"
)

func TranslateToLogicMesh(smcp *maistrav2.ServiceMeshControlPlane, smmr *maistrav1.ServiceMeshMemberRoll, cluster string) (*meshv1alpha1.Mesh, error) {
	meshMember := []string{}
	if smmr != nil {
		meshMember = smmr.Spec.Members
	}

	trustDomain := "cluster.local"
	if smcp.Spec.Security != nil && smcp.Spec.Security.Trust != nil && smcp.Spec.Security.Trust.Domain != "" {
		trustDomain = smcp.Spec.Security.Trust.Domain
	}

	allComponents := make([]string, 0, 4)
	for _, v := range smcp.Status.Readiness.Components {
		if len(v) == 1 && v[0] == "" {
			continue
		}
		allComponents = append(allComponents, v...)
	}

	meshName := cluster + "-" + smcp.GetNamespace() + "-" + smcp.GetName()
	mesh := &meshv1alpha1.Mesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meshName,
			Namespace: cluster,
			Labels:    map[string]string{constants.LabelKeyForDiscoveriedMesh: "true"},
		},
		Spec: meshv1alpha1.MeshSpec{
			MeshProvider: meshv1alpha1.MeshProviderOpenshift,
			Cluster:      cluster,
			ControlPlane: &meshv1alpha1.MeshControlPlane{
				Namespace:  smcp.GetNamespace(),
				Profiles:   smcp.Spec.Profiles,
				Version:    smcp.Spec.Version,
				Components: allComponents,
			},
			MeshMemberRoll: meshMember,
			TrustDomain:    trustDomain,
		},
		Status: meshv1alpha1.MeshStatus{
			Readiness: smcp.Status.Readiness,
		},
	}

	return mesh, nil
}
