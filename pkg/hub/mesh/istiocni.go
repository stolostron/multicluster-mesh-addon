package mesh

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

func (r *Reconciler) ensureIstioCNI(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	work, err := r.workApplier.Apply(ctx, r.buildIstioCNIManifestWork(mesh, cluster))
	if err != nil {
		return fmt.Errorf("failed to apply IstioCNI ManifestWork on cluster %s: %w", cluster.Name, err)
	}
	klog.V(4).Infof("Applied IstioCNI ManifestWork %s/%s", work.Namespace, work.Name)
	return nil
}

// buildIstioCNIManifestWork constructs the shared IstioCNI ManifestWork. Uses CreateOnly
// strategy since IstioCNI is a cluster singleton — first mesh to create it wins.
func (r *Reconciler) buildIstioCNIManifestWork(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) *workv1.ManifestWork {
	spec := map[string]any{
		"namespace": IstioCNINamespace,
	}
	if mesh.Spec.ControlPlane.Version != "" {
		spec["version"] = mesh.Spec.ControlPlane.Version
	}

	istioCNI := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "sailoperator.io/v1",
			"kind":       "IstioCNI",
			"metadata": map[string]any{
				"name": "default",
			},
			"spec": spec,
		},
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ClusterSetLabel: mesh.Spec.ClusterSet,
				ManagedByLabel:  ManagedByValue,
			},
			Name:      IstioCNIManifestWorkName,
			Namespace: cluster.Name,
		},
		Spec: workv1.ManifestWorkSpec{
			ManifestConfigs: []workv1.ManifestConfigOption{{
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:    "sailoperator.io",
					Name:     "default",
					Resource: "istiocnis",
				},
				UpdateStrategy: &workv1.UpdateStrategy{
					Type: workv1.UpdateStrategyTypeCreateOnly,
				},
			}},
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{
						RawExtension: runtime.RawExtension{Object: &corev1.Namespace{
							TypeMeta: metav1.TypeMeta{
								APIVersion: "v1",
								Kind:       "Namespace",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name: IstioCNINamespace,
							},
						}},
					},
					{
						RawExtension: runtime.RawExtension{Object: istioCNI},
					},
				},
			},
		},
	}
}
