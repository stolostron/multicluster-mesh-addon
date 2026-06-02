package mesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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

	OperatorManifestWorkName = "multicluster-mesh-operator"
	ManifestWorkNameCacerts  = "multicluster-mesh-cacerts"

	CacertsSecretName = "cacerts"

	FinalizerName = "mesh.open-cluster-management.io/finalizer"

	ClusterNameLabel   = "mesh.open-cluster-management.io/cluster-name"
	ManagedByLabel     = "app.kubernetes.io/managed-by"
	ManagedByValue     = "multicluster-mesh-addon"
	MeshNameLabel      = "mesh.open-cluster-management.io/mesh-name"
	MeshNamespaceLabel = "mesh.open-cluster-management.io/mesh-namespace"

	ClusterSetLabel     = "cluster.open-cluster-management.io/clusterset"
	clusterClaimProduct = "product.open-cluster-management.io"

	// Product claim values from github.com/stolostron/multicloud-operators-foundation/pkg/klusterlet/clusterclaim
	ProductOCP  = "OpenShift"
	ProductROSA = "ROSA"
	ProductARO  = "ARO"
	ProductROKS = "ROKS"
	ProductOSD  = "OpenShiftDedicated"

	Day = 24 * time.Hour
)

// Reconciler reconciles MultiClusterMesh resources
type Reconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	workApplier *applier.WorkApplier
}

// RegisterController registers the MultiClusterMesh controller with the manager
func RegisterController(mgr manager.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &meshv1alpha1.MultiClusterMesh{}, "spec.clusterSet", func(obj client.Object) []string {
		return []string{obj.(*meshv1alpha1.MultiClusterMesh).Spec.ClusterSet}
	}); err != nil {
		return fmt.Errorf("failed to create field index: %w", err)
	}

	workClient, err := workclient.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("failed to create work client: %w", err)
	}

	workInformerFactory := workinformers.NewSharedInformerFactory(workClient, 0)
	workLister := workInformerFactory.Work().V1().ManifestWorks().Lister()

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		workInformerFactory.Start(ctx.Done())
		for t, synced := range workInformerFactory.WaitForCacheSync(ctx.Done()) {
			if !synced {
				return fmt.Errorf("failed to sync work informer cache for %v", t)
			}
		}
		<-ctx.Done()
		return nil
	})); err != nil {
		return fmt.Errorf("failed to add work informer factory: %w", err)
	}

	reconciler := &Reconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		workApplier: applier.NewWorkApplierWithTypedClient(workClient, workLister),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&meshv1alpha1.MultiClusterMesh{}).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForCluster),
		).
		Watches(
			&clusterv1beta2.ManagedClusterSet{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForClusterSet),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(reconciler.mapSecretToMesh),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[MeshNameLabel] != "" && obj.GetLabels()[MeshNamespaceLabel] != ""
			})),
		).
		Watches(
			&workv1.ManifestWork{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForManifestWork),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[ManagedByLabel] == ManagedByValue
			})),
		).
		Complete(reconciler)
}

//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mesh.open-cluster-management.io,resources=multiclustermeshes/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements the reconcile loop for MultiClusterMesh resources
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (result reconcile.Result, reconcileErr error) {
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
		return
	}

	// Copy the status so we can avoid updating it in case nothing changed.
	oldStatus := mesh.Status.DeepCopy()

	var invalidCondition *metav1.Condition
	if invalidCondition, reconcileErr = r.validate(ctx, mesh); reconcileErr != nil {
		r.setErrorStatus(mesh, reconcileErr)
	} else if invalidCondition != nil {
		meta.SetStatusCondition(&mesh.Status.Conditions, *invalidCondition)
	} else {
		clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
		if err != nil {
			reconcileErr = fmt.Errorf("failed to get clusters from set %s: %w", mesh.Spec.ClusterSet, err)
		} else {
			result, reconcileErr = r.doReconcile(ctx, mesh, clusters)
		}

		if reconcileErr == nil {
			klog.Infof("Successfully reconciled MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
			reconcileErr = r.determineStatus(ctx, mesh, clusters)
		}

		if reconcileErr != nil {
			klog.Errorf("Encountered an error while reconciling MultiClusterMesh %s/%s: %v", mesh.Namespace, mesh.Name, reconcileErr)
			r.setErrorStatus(mesh, reconcileErr)
		}
	}

	// Update status only if something changed, to avoid unnecessary reconciliations
	var statusErr error
	if !reflect.DeepEqual(oldStatus, &mesh.Status) {
		statusErr = r.Status().Update(ctx, mesh)
	}

	return result, errors.Join(reconcileErr, statusErr)
}

// validate checks for conflicts that prevent reconciliation.
// Returns a condition to set on the mesh if validation fails, or nil if ok.
func (r *Reconciler) validate(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (conflict *metav1.Condition, err error) {
	if err = r.forEachMeshInClusterSet(ctx, mesh.Spec.ClusterSet, func(other *meshv1alpha1.MultiClusterMesh) {
		if other.UID == mesh.UID || conflict != nil {
			return
		}
		if isOlderMesh(mesh, other) {
			return
		}
		if mesh.GetControlPlaneNamespace() == other.GetControlPlaneNamespace() {
			conflict = &metav1.Condition{
				Type:   meshv1alpha1.ConditionReady,
				Status: metav1.ConditionFalse,
				Reason: meshv1alpha1.ReasonNamespaceConflict,
				Message: fmt.Sprintf("controlPlane.namespace %q conflicts with older mesh %s/%s targeting the same ClusterSet %s",
					mesh.GetControlPlaneNamespace(), other.Namespace, other.Name, mesh.Spec.ClusterSet),
			}
			return
		}
		if r.operatorConfigConflicts(mesh.Spec.Operator, other.Spec.Operator) {
			conflict = &metav1.Condition{
				Type:   meshv1alpha1.ConditionReady,
				Status: metav1.ConditionFalse,
				Reason: meshv1alpha1.ReasonOperatorConfigConflict,
				Message: fmt.Sprintf("operator config conflicts with older mesh %s/%s targeting the same ClusterSet %s",
					other.Namespace, other.Name, mesh.Spec.ClusterSet),
			}
		}
	}); err != nil {
		return nil, fmt.Errorf("failed to validate: %w", err)
	}

	return conflict, nil
}

// isOlderMesh returns true if a is older than b, using namespace/name as tiebreaker for equal timestamps.
func isOlderMesh(a, b *meshv1alpha1.MultiClusterMesh) bool {
	return a.CreationTimestamp.Before(&b.CreationTimestamp) ||
		(a.CreationTimestamp.Equal(&b.CreationTimestamp) &&
			client.ObjectKeyFromObject(a).String() < client.ObjectKeyFromObject(b).String())
}

// operatorConfigConflicts compares two operator configs after applying defaults to detect real conflicts.
// This avoids false positives when one mesh explicitly sets a value that matches the other's default.
func (r *Reconciler) operatorConfigConflicts(a, b meshv1alpha1.OperatorConfig) bool {
	return r.applyOperatorDefaults(a, false) != r.applyOperatorDefaults(b, false) ||
		r.applyOperatorDefaults(a, true) != r.applyOperatorDefaults(b, true)
}

func (r *Reconciler) doReconcile(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) (reconcile.Result, error) {
	for _, cluster := range clusters {
		klog.V(4).Infof("Reconciling cluster %s", cluster.Name)

		if getProductClaim(&cluster) == "" {
			klog.V(4).Infof("Cluster %s missing product claim (needed for platform detection), skipping", cluster.Name)
			continue
		}

		result, err := r.ensureOperatorInstalled(ctx, mesh, &cluster)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to ensure mesh operator on cluster %s: %w", cluster.Name, err)
		} else if result.RequeueAfter > 0 {
			return result, nil
		}
	}

	// Create certificates for each cluster if cert-manager is configured
	if mesh.Spec.Security.Trust.CertManager.IssuerRef.Name != "" {
		if err := r.ensureCertificatesCreated(ctx, mesh, clusters); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.ensureCacertsDistributed(ctx, mesh, clusters); err != nil {
			return reconcile.Result{}, err
		}
	}

	if err := r.cleanupManifestWorks(ctx, mesh.Spec.ClusterSet); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to cleanup ManifestWorks: %w", err)
	}

	return reconcile.Result{}, nil
}

// forEachMeshInClusterSet lists all non-deleting meshes targeting the given ClusterSet and calls fn for each.
func (r *Reconciler) forEachMeshInClusterSet(ctx context.Context, clusterSet string, fn func(*meshv1alpha1.MultiClusterMesh)) error {
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	if err := r.List(ctx, meshList, client.MatchingFields{"spec.clusterSet": clusterSet}); err != nil {
		return fmt.Errorf("failed to list meshes for ClusterSet %s: %w", clusterSet, err)
	}

	for i := range meshList.Items {
		if meshList.Items[i].DeletionTimestamp.IsZero() {
			fn(&meshList.Items[i])
		}
	}

	return nil
}

func (r *Reconciler) reconcileRequestsForClusterSet(ctx context.Context, clusterSet string) []reconcile.Request {
	var requests []reconcile.Request
	if err := r.forEachMeshInClusterSet(ctx, clusterSet, func(mesh *meshv1alpha1.MultiClusterMesh) {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mesh.Name, Namespace: mesh.Namespace},
		})
	}); err != nil {
		klog.Errorf("Error when trying to reconcile meshes for ClusterSet %s: %v", clusterSet, err)
	}
	return requests
}

// findMeshesForCluster returns a list of all meshes to reconcile following a cluster change
func (r *Reconciler) findMeshesForCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	cluster := obj.(*clusterv1.ManagedCluster)
	clusterSetName := cluster.Labels[ClusterSetLabel]
	if clusterSetName == "" {
		klog.V(4).Infof("Cluster %s has no clusterset label, skipping", cluster.Name)
		return nil
	}

	klog.V(4).Infof("ManagedCluster %s changed, reconciling meshes using ClusterSet %s", cluster.Name, clusterSetName)
	return r.reconcileRequestsForClusterSet(ctx, clusterSetName)
}

// findMeshesForClusterSet returns a list of all meshes to reconcile following a ClusterSet change
func (r *Reconciler) findMeshesForClusterSet(ctx context.Context, obj client.Object) []reconcile.Request {
	clusterSet := obj.(*clusterv1beta2.ManagedClusterSet)
	klog.V(4).Infof("ManagedClusterSet %s changed, reconciling meshes using it", clusterSet.Name)
	return r.reconcileRequestsForClusterSet(ctx, clusterSet.Name)
}

// findMeshesForManifestWork returns a list of all meshes to reconcile following a ManifestWork change
func (r *Reconciler) findMeshesForManifestWork(ctx context.Context, obj client.Object) []reconcile.Request {
	cluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: obj.GetNamespace()}, cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get ManagedCluster %s for ManifestWork %s: %v", obj.GetNamespace(), obj.GetName(), err)
		}
		return nil
	}

	return r.findMeshesForCluster(ctx, cluster)
}

// handleDeletion handles cleanup when the MultiClusterMesh is being deleted
func (r *Reconciler) handleDeletion(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.V(4).Infof("MultiClusterMesh %s/%s has no finalizer, nothing to clean up", mesh.Namespace, mesh.Name)
		return reconcile.Result{}, nil
	}

	klog.Infof("Handling deletion for MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	if err := r.cleanupManifestWorks(ctx, mesh.Spec.ClusterSet); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to cleanup ManifestWorks: %w", err)
	}

	// Trigger reconciliation for other meshes targeting the same cluster set.
	// If this fails, we don't want to block the mesh deletion. The other meshes will eventually reconcile.
	r.triggerReconcileForNotReadyMeshes(ctx, mesh)

	klog.Infof("Removing finalizer from MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	controllerutil.RemoveFinalizer(mesh, FinalizerName)
	if err := r.Update(ctx, mesh); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) ensureOperatorInstalled(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) (reconcile.Result, error) {
	klog.V(4).Infof("Ensuring mesh operator on cluster %s for mesh %s", cluster.Name, mesh.Name)

	work, err := r.workApplier.Apply(ctx, r.buildOperatorManifestWork(mesh, cluster))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to apply operator ManifestWork on cluster %s: %w", cluster.Name, err)
	}

	klog.Infof("Successfully applied operator ManifestWork %s/%s", work.Namespace, work.Name)
	return reconcile.Result{}, nil
}

// cleanupManifestWorks deletes ManifestWorks on clusters that no mesh in the given ClusterSet needs anymore.
func (r *Reconciler) cleanupManifestWorks(ctx context.Context, clusterSet string) error {
	neededClusters, err := r.getMeshEnabledClusters(ctx, clusterSet)
	if err != nil {
		return fmt.Errorf("failed to determine needed clusters: %w", err)
	}

	workList := &workv1.ManifestWorkList{}
	if err := r.List(ctx, workList, client.MatchingLabels{ManagedByLabel: ManagedByValue, ClusterSetLabel: clusterSet}); err != nil {
		return fmt.Errorf("failed to list ManifestWorks for ClusterSet %s: %w", clusterSet, err)
	}

	for _, work := range workList.Items {
		if neededClusters[work.Namespace] {
			continue
		}

		klog.Infof("Deleting ManifestWork %s/%s (no mesh targets this cluster)", work.Namespace, work.Name)
		if err := r.workApplier.Delete(ctx, work.Namespace, work.Name); err != nil {
			return fmt.Errorf("failed to delete ManifestWork %s/%s: %w", work.Namespace, work.Name, err)
		}
	}

	return nil
}

// triggerReconcileForNotReadyMeshes triggers reconciliation for not-ready meshes targeting the same ClusterSet.
func (r *Reconciler) triggerReconcileForNotReadyMeshes(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) {
	if err := r.forEachMeshInClusterSet(ctx, mesh.Spec.ClusterSet, func(other *meshv1alpha1.MultiClusterMesh) {
		if other.UID == mesh.UID {
			return
		}
		if meta.IsStatusConditionTrue(other.Status.Conditions, meshv1alpha1.ConditionReady) {
			return
		}

		patch := client.MergeFrom(other.DeepCopy())
		metav1.SetMetaDataAnnotation(&other.ObjectMeta, "mesh.open-cluster-management.io/reconcile-trigger", time.Now().Format(time.RFC3339Nano))
		if err := r.Patch(ctx, other, patch); err != nil {
			klog.Errorf("Failed to trigger reconcile for peer mesh %s/%s: %v", other.Namespace, other.Name, err)
		}
	}); err != nil {
		klog.Errorf("Failed to list peer meshes for ClusterSet %s: %v", mesh.Spec.ClusterSet, err)
	}
}

func (r *Reconciler) determineStatus(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	var clusterStatuses []meshv1alpha1.ClusterMeshStatus
	allReady := len(clusters) > 0

	for _, cluster := range clusters {
		status := meshv1alpha1.ClusterMeshStatus{
			ClusterName: cluster.Name,
		}

		if getProductClaim(&cluster) == "" {
			allReady = false
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:    meshv1alpha1.ConditionOperatorInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  meshv1alpha1.ReasonMissingProductClaim,
				Message: "Cluster is missing product claim, cannot determine platform",
			})
			clusterStatuses = append(clusterStatuses, status)
			continue
		}

		work := &workv1.ManifestWork{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      OperatorManifestWorkName,
			Namespace: cluster.Name,
		}, work)

		if err == nil {
			// TODO: Set to actual status when operator installation is confirmed via ManifestWork status feedback
			allReady = false
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:    meshv1alpha1.ConditionOperatorInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  meshv1alpha1.ReasonManifestWorkCreated,
				Message: "Operator ManifestWork has been created, awaiting installation confirmation",
			})
		} else {
			return fmt.Errorf("failed to get operator ManifestWork for cluster %s: %w", cluster.Name, err)
		}

		clusterStatuses = append(clusterStatuses, status)
	}

	mesh.Status.ClusterStatus = clusterStatuses

	readyCondition := metav1.Condition{Type: meshv1alpha1.ConditionReady}
	if allReady {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = meshv1alpha1.ReasonAllClustersReady
		readyCondition.Message = "All clusters are ready"
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = meshv1alpha1.ReasonClustersNotReady
		readyCondition.Message = "Not all clusters are ready, check individual cluster statuses for details"
	}
	meta.SetStatusCondition(&mesh.Status.Conditions, readyCondition)

	return nil
}

func (r *Reconciler) setErrorStatus(mesh *meshv1alpha1.MultiClusterMesh, reconcileErr error) {
	meta.SetStatusCondition(&mesh.Status.Conditions, metav1.Condition{
		Type:    meshv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  meshv1alpha1.ReasonReconcileError,
		Message: reconcileErr.Error(),
	})
}

// getMeshEnabledClusters returns all clusters in the given ClusterSet if any non-deleting mesh targets it, or an empty set otherwise.
func (r *Reconciler) getMeshEnabledClusters(ctx context.Context, clusterSet string) (map[string]bool, error) {
	needed := make(map[string]bool)

	hasActiveMesh := false
	if err := r.forEachMeshInClusterSet(ctx, clusterSet, func(_ *meshv1alpha1.MultiClusterMesh) {
		hasActiveMesh = true
	}); err != nil {
		return nil, err
	}

	if !hasActiveMesh {
		return needed, nil
	}

	clusters, err := r.getClustersFromSet(ctx, clusterSet)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters from set %s: %w", clusterSet, err)
	}

	for _, cluster := range clusters {
		needed[cluster.Name] = true
	}

	return needed, nil
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

func (r *Reconciler) getClustersFromSet(ctx context.Context, clusterSetName string) ([]clusterv1.ManagedCluster, error) {
	clusterSet := &clusterv1beta2.ManagedClusterSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterSetName}, clusterSet); err != nil {
		if apierrors.IsNotFound(err) {
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

	// Sort the clusters to guarantee a deterministic order in all operations that need them
	slices.SortFunc(clusterList.Items, func(a, b clusterv1.ManagedCluster) int {
		return strings.Compare(a.Name, b.Name)
	})

	return clusterList.Items, nil
}

func (r *Reconciler) buildOperatorManifestWork(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) *workv1.ManifestWork {
	manifests := []workv1.Manifest{}
	isOCP := false
	switch getProductClaim(cluster) {
	case ProductOCP, ProductROSA, ProductARO, ProductROKS, ProductOSD:
		isOCP = true
	}

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
			Name:      OperatorManifestWorkName,
			Namespace: cluster.Name,
			Labels: map[string]string{
				ManagedByLabel:  ManagedByValue,
				ClusterSetLabel: mesh.Spec.ClusterSet,
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

// mapSecretToMesh maps a Secret to the MultiClusterMesh that owns it
func (r *Reconciler) mapSecretToMesh(_ context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	meshName := secret.Labels[MeshNameLabel]
	meshNamespace := secret.Labels[MeshNamespaceLabel]

	klog.V(4).Infof("Secret %s/%s triggered reconcile for mesh %s/%s",
		secret.Namespace, secret.Name, meshNamespace, meshName)

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      meshName,
			Namespace: meshNamespace,
		},
	}}
}

// getCacertsName returns the name for the certificate and secret for a specific cluster
func getCacertsName(clusterName string) string {
	return fmt.Sprintf("cacerts-%s", clusterName)
}

// ensureCertificatesCreated creates Certificate resources for each cluster in the mesh
func (r *Reconciler) ensureCertificatesCreated(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	for _, cluster := range clusters {
		if err := r.ensureCertificateForCluster(ctx, mesh, &cluster); err != nil {
			return fmt.Errorf("failed to ensure certificate for cluster %s: %w", cluster.Name, err)
		}
	}
	return nil
}

// ensureCertificateForCluster creates a Certificate resource for a specific cluster
func (r *Reconciler) ensureCertificateForCluster(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	certName := getCacertsName(cluster.Name)
	cert := &certmanagerv1.Certificate{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      certName,
		Namespace: mesh.Namespace,
	}, cert)

	if err == nil {
		klog.V(4).Infof("Certificate %s/%s already exists", mesh.Namespace, certName)
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get Certificate: %w", err)
	}

	klog.Infof("Creating Certificate %s/%s for cluster %s", mesh.Namespace, certName, cluster.Name)

	cert = &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: mesh.Namespace,
			Labels:    meshOwnedLabels(mesh, cluster.Name),
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: meshOwnedLabels(mesh, cluster.Name),
			},
			Duration:    &metav1.Duration{Duration: 60 * Day},
			RenewBefore: &metav1.Duration{Duration: 15 * Day},
			CommonName:  "Intermediate Istio CA",
			IsCA:        true,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageCertSign,
			},
			IssuerRef: cmmeta.IssuerReference{
				Name:  mesh.Spec.Security.Trust.CertManager.IssuerRef.Name,
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}

	if err := r.Create(ctx, cert); err != nil {
		return fmt.Errorf("failed to create Certificate: %w", err)
	}

	klog.Infof("Successfully created Certificate %s/%s", mesh.Namespace, certName)
	return nil
}

// ensureCacertsDistributed creates ManifestWorks to distribute cacerts secrets to clusters
func (r *Reconciler) ensureCacertsDistributed(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	for _, cluster := range clusters {
		if err := r.ensureCacertsManifestWork(ctx, mesh, &cluster); err != nil {
			return fmt.Errorf("failed to ensure cacerts ManifestWork for cluster %s: %w", cluster.Name, err)
		}
	}
	return nil
}

// ensureCacertsManifestWork creates a ManifestWork to distribute the cacerts secret to a cluster
func (r *Reconciler) ensureCacertsManifestWork(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	secretName := getCacertsName(cluster.Name)
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: mesh.Namespace,
	}, secret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).Infof("Secret %s/%s not found yet, waiting for cert-manager to create it", mesh.Namespace, secretName)
			return nil
		}
		return fmt.Errorf("failed to get secret: %w", err)
	}

	work, err := r.workApplier.Apply(ctx, r.buildCacertsManifestWork(mesh, cluster.Name, secret))
	if err != nil {
		return fmt.Errorf("failed to apply cacerts ManifestWork on cluster %s: %w", cluster.Name, err)
	}

	klog.Infof("Successfully applied cacerts ManifestWork %s/%s", work.Namespace, work.Name)
	return nil
}

// buildCacertsManifestWork builds a ManifestWork for distributing the cacerts secret
func (r *Reconciler) buildCacertsManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, secret *corev1.Secret) *workv1.ManifestWork {
	cacertsSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CacertsSecretName,
			Namespace: mesh.GetControlPlaneNamespace(),
		},
		Type: corev1.SecretTypeTLS,
		Data: secret.Data,
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManifestWorkNameCacerts,
			Namespace: clusterName,
			Labels:    meshOwnedLabels(mesh, clusterName),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{{
					RawExtension: runtime.RawExtension{Object: cacertsSecret},
				}},
			},
		},
	}
}

func meshOwnedLabels(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) map[string]string {
	return map[string]string{
		ManagedByLabel:     ManagedByValue,
		ClusterSetLabel:    mesh.Spec.ClusterSet,
		MeshNameLabel:      mesh.Name,
		MeshNamespaceLabel: mesh.Namespace,
		ClusterNameLabel:   clusterName,
	}
}
