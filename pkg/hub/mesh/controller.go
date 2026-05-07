package mesh

import (
	"context"
	"fmt"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

const (
	OperatorNameOSSM = "servicemeshoperator3"
	OperatorNameSail = "sailoperator"

	DefaultOCPOperatorNs = "openshift-operators"
	DefaultOperatorNs    = "sail-operator"

	DefaultOCPCatalogSource = "redhat-operators"
	DefaultOCPCatalogNs     = "openshift-marketplace"
	DefaultCatalogSource    = "operatorhubio-catalog"
	DefaultCatalogNs        = "olm"

	DefaultChannel = "stable"

	ManifestWorkNameOSSM = "multicluster-mesh-operator-ossm"
	ManifestWorkNameSail = "multicluster-mesh-operator-sail"

	FinalizerName = "mesh.open-cluster-management.io/finalizer"

	LabelMeshName      = "mesh.open-cluster-management.io/mesh-name"
	LabelMeshNamespace = "mesh.open-cluster-management.io/mesh-namespace"

	ClusterSetLabel     = "cluster.open-cluster-management.io/clusterset"
	clusterClaimProduct = "product.open-cluster-management.io"

	// Product claim values from github.com/stolostron/multicloud-operators-foundation/pkg/klusterlet/clusterclaim
	ProductOCP  = "OpenShift"
	ProductROSA = "ROSA"
	ProductARO  = "ARO"
	ProductROKS = "ROKS"
	ProductOSD  = "OpenShiftDedicated"
)

// Reconciler reconciles MultiClusterMesh resources
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// RegisterController registers the MultiClusterMesh controller with the manager
func RegisterController(mgr manager.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &meshv1alpha1.MultiClusterMesh{}, "spec.clusterSet", func(obj client.Object) []string {
		return []string{obj.(*meshv1alpha1.MultiClusterMesh).Spec.ClusterSet}
	}); err != nil {
		return fmt.Errorf("failed to create field index: %w", err)
	}

	reconciler := &Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&meshv1alpha1.MultiClusterMesh{}).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForCluster),
		).
		Complete(reconciler)
}

//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the reconcile loop for MultiClusterMesh resources
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling MultiClusterMesh: %s/%s", req.Namespace, req.Name)

	// Fetch the MultiClusterMesh resource
	mesh := &meshv1alpha1.MultiClusterMesh{}
	if err := r.Get(ctx, req.NamespacedName, mesh); err != nil {
		klog.V(4).Infof("MultiClusterMesh not found, may have been deleted: %s/%s", req.Namespace, req.Name)
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !mesh.DeletionTimestamp.IsZero() {
		klog.Infof("MultiClusterMesh being deleted: %s/%s", req.Namespace, req.Name)
		return r.handleDeletion(ctx, mesh)
	}

	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.Infof("Adding finalizer to MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
		controllerutil.AddFinalizer(mesh, FinalizerName)
		if err := r.Update(ctx, mesh); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return reconcile.Result{}, nil
	}

	klog.Infof("MultiClusterMesh reconciling: %s/%s, ClusterSet: %s",
		req.Namespace, req.Name, mesh.Spec.ClusterSet)
	clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if err != nil {
		klog.Errorf("Failed to get clusters from set %s: %v", mesh.Spec.ClusterSet, err)
		return reconcile.Result{}, err
	}

	klog.Infof("Found %d clusters in set %s", len(clusters), mesh.Spec.ClusterSet)

	for _, cluster := range clusters {
		if getProductClaim(&cluster) == "" {
			klog.V(4).Infof("Cluster %s missing product claim (needed for platform detection), skipping", cluster.Name)
			continue
		}

		if err := r.ensureOperatorInstalled(ctx, mesh, &cluster); err != nil {
			klog.Errorf("Failed to ensure mesh operator on cluster %s: %v", cluster.Name, err)
			return reconcile.Result{}, err
		}
	}

	if err := r.cleanupOrphanedManifestWorks(ctx, mesh, clusters); err != nil {
		klog.Errorf("Failed to cleanup orphaned ManifestWorks: %v", err)
		return reconcile.Result{}, err
	}

	klog.Infof("Successfully reconciled MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	return reconcile.Result{}, nil
}

// findMeshesForCluster returns a list of all meshes to reconcile following a cluster change
func (r *Reconciler) findMeshesForCluster(ctx context.Context, obj client.Object) (requests []reconcile.Request) {
	cluster := obj.(*clusterv1.ManagedCluster)
	clusterSetName := cluster.Labels[ClusterSetLabel]
	if clusterSetName == "" {
		klog.V(4).Infof("Cluster %s has no clusterset label, skipping", cluster.Name)
		return
	}

	meshList := &meshv1alpha1.MultiClusterMeshList{}
	if err := r.List(ctx, meshList, client.MatchingFields{"spec.clusterSet": clusterSetName}); err != nil {
		klog.Errorf("Failed to list MultiClusterMeshes when handling cluster %s: %v", cluster.Name, err)
		return
	}

	for _, mesh := range meshList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      mesh.Name,
				Namespace: mesh.Namespace,
			},
		})
	}

	return requests
}

// handleDeletion handles cleanup when the MultiClusterMesh is being deleted
func (r *Reconciler) handleDeletion(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.V(4).Infof("MultiClusterMesh %s/%s has no finalizer, nothing to clean up", mesh.Namespace, mesh.Name)
		return reconcile.Result{}, nil
	}

	klog.Infof("Handling deletion for MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	workList := &workv1.ManifestWorkList{}
	if err := r.List(ctx, workList, meshLabelSelector(mesh)); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list ManifestWorks for cleanup: %w", err)
	}

	klog.V(4).Infof("Found %d ManifestWorks to clean up for MultiClusterMesh %s/%s", len(workList.Items), mesh.Namespace, mesh.Name)
	for i := range workList.Items {
		work := &workList.Items[i]
		if err := r.Delete(ctx, work); err != nil {
			if !errors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("failed to delete ManifestWork %s/%s: %w", work.Namespace, work.Name, err)
			}
			klog.V(4).Infof("ManifestWork %s/%s already deleted", work.Namespace, work.Name)
		} else {
			klog.Infof("Deleted ManifestWork %s/%s", work.Namespace, work.Name)
		}
	}

	klog.Infof("Removing finalizer from MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	controllerutil.RemoveFinalizer(mesh, FinalizerName)
	if err := r.Update(ctx, mesh); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) ensureOperatorInstalled(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	klog.V(4).Infof("Ensuring mesh operator on cluster %s for mesh %s", cluster.Name, mesh.Name)
	isOCP := isOpenShift(cluster)
	workName := getOperatorManifestWorkName(isOCP)

	// Check if ManifestWork already exists
	existingWork := &workv1.ManifestWork{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      workName,
		Namespace: cluster.Name,
	}, existingWork)

	if err == nil {
		if !existingWork.DeletionTimestamp.IsZero() {
			return fmt.Errorf("ManifestWork is terminating, requeueing")
		}

		klog.V(4).Infof("ManifestWork %s/%s already exists", cluster.Name, workName)
		// TODO: Add logic to check if the work needs updating (e.g., channel change)
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManifestWork: %w", err)
	}

	klog.Infof("Creating ManifestWork to install operator on cluster %s", cluster.Name)
	work := r.buildOperatorManifestWork(mesh, cluster)

	if err := r.Create(ctx, work); err != nil {
		return fmt.Errorf("failed to create ManifestWork: %w", err)
	}

	klog.Infof("Successfully created ManifestWork %s/%s for operator installation", cluster.Name, work.Name)
	return nil
}

func (r *Reconciler) cleanupOrphanedManifestWorks(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	expectedClusters := make(map[string]bool)
	for _, cluster := range clusters {
		expectedClusters[cluster.Name] = true
	}

	// List ALL ManifestWorks that were created for this mesh, some of which might be orphaned
	workList := &workv1.ManifestWorkList{}
	if err := r.List(ctx, workList, meshLabelSelector(mesh)); err != nil {
		return fmt.Errorf("failed to list ManifestWorks: %w", err)
	}

	for _, work := range workList.Items {
		// Skip any clusters that are still in the set the mesh is referencing
		if expectedClusters[work.Namespace] {
			continue
		}

		klog.Infof("Deleting orphaned ManifestWork %s/%s (cluster no longer in ClusterSet %s)",
			work.Namespace, work.Name, mesh.Spec.ClusterSet)
		if err := r.Delete(ctx, &work); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete orphaned ManifestWork %s/%s: %w", work.Namespace, work.Name, err)
		}
	}

	return nil
}

func meshLabelSelector(mesh *meshv1alpha1.MultiClusterMesh) client.MatchingLabels {
	return client.MatchingLabels{
		LabelMeshName:      mesh.Name,
		LabelMeshNamespace: mesh.Namespace,
	}
}

func isOpenShift(cluster *clusterv1.ManagedCluster) bool {
	switch getProductClaim(cluster) {
	case ProductOCP, ProductROSA, ProductARO, ProductROKS, ProductOSD:
		return true
	}
	return false
}

// getProductClaim returns the value for the cluster, or empty string if not found
func getProductClaim(cluster *clusterv1.ManagedCluster) string {
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == clusterClaimProduct {
			return claim.Value
		}
	}
	return ""
}

func getOperatorManifestWorkName(isOCP bool) string {
	if isOCP {
		return ManifestWorkNameOSSM
	}
	return ManifestWorkNameSail
}

func (r *Reconciler) getClustersFromSet(ctx context.Context, clusterSetName string) ([]clusterv1.ManagedCluster, error) {
	clusterSet := &clusterv1beta2.ManagedClusterSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterSetName}, clusterSet); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("ManagedClusterSet %s not found", clusterSetName)
			return []clusterv1.ManagedCluster{}, nil
		}
		return nil, fmt.Errorf("failed to get ManagedClusterSet %s: %w", clusterSetName, err)
	}

	// Only support ExclusiveClusterSetLabel selector type (legacy/default mode)
	selectorType := clusterSet.Spec.ClusterSelector.SelectorType
	if len(selectorType) > 0 && selectorType != clusterv1beta2.ExclusiveClusterSetLabel {
		return nil, fmt.Errorf("unsupported ManagedClusterSet selector type %q, only %q is supported",
			selectorType, clusterv1beta2.ExclusiveClusterSetLabel)
	}

	clusterList := &clusterv1.ManagedClusterList{}
	labelSelector := client.MatchingLabels{
		ClusterSetLabel: clusterSetName,
	}

	if err := r.List(ctx, clusterList, labelSelector); err != nil {
		return nil, fmt.Errorf("failed to list clusters in set %s: %w", clusterSetName, err)
	}

	return clusterList.Items, nil
}

func (r *Reconciler) buildOperatorManifestWork(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) *workv1.ManifestWork {
	manifests := []workv1.Manifest{}
	isOCP := isOpenShift(cluster)
	config := r.applyOperatorDefaults(mesh.Spec.Operator, isOCP)

	// openshift-operators exists by default on OCP and already has a global OperatorGroup
	if config.Namespace != DefaultOCPOperatorNs {
		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: config.Namespace,
				},
			}},
		})

		manifests = append(manifests, workv1.Manifest{
			RawExtension: runtime.RawExtension{Object: &operatorsv1.OperatorGroup{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operators.coreos.com/v1",
					Kind:       "OperatorGroup",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "operator-group",
					Namespace: config.Namespace,
				},
				Spec: operatorsv1.OperatorGroupSpec{
					// Empty spec = "AllNamespaces" scope
				},
			}},
		})
	}

	packageName := OperatorNameSail
	if isOCP {
		packageName = OperatorNameOSSM
	}

	manifests = append(manifests, workv1.Manifest{
		RawExtension: runtime.RawExtension{Object: &operatorsv1alpha1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operators.coreos.com/v1alpha1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      packageName,
				Namespace: config.Namespace,
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Channel:                config.Channel,
				InstallPlanApproval:    config.InstallPlanApproval,
				Package:                packageName,
				CatalogSource:          config.Source,
				CatalogSourceNamespace: config.SourceNamespace,
				StartingCSV:            config.StartingCSV,
			},
		}},
	})

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getOperatorManifestWorkName(isOCP),
			Namespace: cluster.Name,
			Labels: map[string]string{
				LabelMeshName:      mesh.Name,
				LabelMeshNamespace: mesh.Namespace,
			},
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

// applyOperatorDefaults applies platform-specific defaults for the given cluster to the operator config
func (r *Reconciler) applyOperatorDefaults(config meshv1alpha1.OperatorConfig, isOCP bool) meshv1alpha1.OperatorConfig {
	if config.Namespace == "" {
		if isOCP {
			config.Namespace = DefaultOCPOperatorNs
		} else {
			config.Namespace = DefaultOperatorNs
		}
	}

	if config.Source == "" {
		if isOCP {
			config.Source = DefaultOCPCatalogSource
		} else {
			config.Source = DefaultCatalogSource
		}
	}

	if config.SourceNamespace == "" {
		if isOCP {
			config.SourceNamespace = DefaultOCPCatalogNs
		} else {
			config.SourceNamespace = DefaultCatalogNs
		}
	}

	if config.Channel == "" {
		config.Channel = DefaultChannel
	}

	if config.InstallPlanApproval == "" {
		config.InstallPlanApproval = operatorsv1alpha1.ApprovalAutomatic
	}

	return config
}
