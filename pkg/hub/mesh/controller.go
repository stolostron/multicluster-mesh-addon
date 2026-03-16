package mesh

import (
	"context"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

// Reconciler reconciles MultiClusterMesh resources
type Reconciler struct {
	client.Client
}

// RegisterController registers the MultiClusterMesh controller with the manager
func RegisterController(mgr manager.Manager) error {
	reconciler := &Reconciler{
		Client: mgr.GetClient(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&meshv1alpha1.MultiClusterMesh{}).
		Complete(reconciler)
}

//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/finalizers,verbs=update

// Reconcile implements the reconcile loop for MultiClusterMesh resources
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling MultiClusterMesh: %s/%s", req.Namespace, req.Name)

	// Fetch the MultiClusterMesh resource
	mesh := &meshv1alpha1.MultiClusterMesh{}
	if err := r.Get(ctx, req.NamespacedName, mesh); err != nil {
		klog.V(4).Infof("MultiClusterMesh not found, may have been deleted: %s/%s", req.Namespace, req.Name)
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Log the event
	if !mesh.DeletionTimestamp.IsZero() {
		klog.Infof("MultiClusterMesh being deleted: %s/%s", req.Namespace, req.Name)
	} else {
		klog.Infof("MultiClusterMesh reconciling: %s/%s, ClusterSet: %s",
			req.Namespace, req.Name, mesh.Spec.ClusterSet)
	}

	// TODO: Add actual reconciliation logic here
	// For now, just log and return success
	return reconcile.Result{}, nil
}
