package mesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"encoding/json"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerapply "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/certmanager/v1"
	cmmetaapply "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	applyconfigv1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
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
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
)

const (
	OperatorNameSail = "sailoperator"

	DefaultOperatorNs    = "sail-operator"
	DefaultCatalogSource = "operatorhubio-catalog"
	DefaultCatalogNs     = "olm"
	DefaultChannel       = "stable"

	OperatorPolicyName = "multicluster-mesh-operator"

	ManifestWorkNameCacerts = "multicluster-mesh-cacerts"

	CacertsSecretName = "cacerts"

	FinalizerName = "mesh.open-cluster-management.io/finalizer"

	ClusterNameLabel   = "mesh.open-cluster-management.io/cluster-name"
	ManagedByLabel     = "app.kubernetes.io/managed-by"
	ManagedByValue     = "multicluster-mesh-addon"
	MeshNameLabel      = "mesh.open-cluster-management.io/mesh-name"
	MeshNamespaceLabel = "mesh.open-cluster-management.io/mesh-namespace"

	ClusterSetLabel     = "cluster.open-cluster-management.io/clusterset"
	clusterClaimProduct = "product.open-cluster-management.io"

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
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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
		Watches(
			&policyv1.Policy{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForPolicy),
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
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/bind,verbs=create
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersetbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies/status,verbs=get
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
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

	oldStatus := mesh.Status.DeepCopy()

	var conflict bool
	if conflict, reconcileErr = r.validate(ctx, mesh); reconcileErr != nil {
		mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonReconcileError, "%v", reconcileErr)
	} else if !conflict {
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
			mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonReconcileError, "%v", reconcileErr)
		}
	}

	var statusErr error
	if !reflect.DeepEqual(oldStatus, &mesh.Status) {
		newStatus := mesh.Status
		statusErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &meshv1alpha1.MultiClusterMesh{}
			if err := r.Get(ctx, req.NamespacedName, latest); err != nil {
				return err
			}
			latest.Status = newStatus
			return r.Status().Update(ctx, latest)
		})
	}

	return result, errors.Join(reconcileErr, statusErr)
}

// validate checks for conflicts that prevent reconciliation.
// Sets a condition on the mesh and returns true if a conflict is found.
func (r *Reconciler) validate(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (conflict bool, err error) {
	if err = r.forEachMeshInClusterSet(ctx, mesh.Spec.ClusterSet, func(other *meshv1alpha1.MultiClusterMesh) {
		if other.UID == mesh.UID || conflict {
			return
		}
		if isOlderMesh(mesh, other) {
			return
		}
		if mesh.GetControlPlaneNamespace() == other.GetControlPlaneNamespace() {
			mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonNamespaceConflict,
				"controlPlane.namespace %q conflicts with older mesh %s/%s targeting the same ClusterSet %s",
				mesh.GetControlPlaneNamespace(), other.Namespace, other.Name, mesh.Spec.ClusterSet)
			conflict = true
			return
		}
		if r.operatorConfigConflicts(mesh.Spec.Operator, other.Spec.Operator) {
			mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonOperatorConfigConflict,
				"operator config conflicts with older mesh %s/%s targeting the same ClusterSet %s",
				other.Namespace, other.Name, mesh.Spec.ClusterSet)
			conflict = true
		}
	}); err != nil {
		return false, fmt.Errorf("failed to validate: %w", err)
	}

	return conflict, nil
}

// isOlderMesh returns true if a is older than b, using namespace/name as tiebreaker for equal timestamps.
func isOlderMesh(a, b *meshv1alpha1.MultiClusterMesh) bool {
	return a.CreationTimestamp.Before(&b.CreationTimestamp) ||
		(a.CreationTimestamp.Equal(&b.CreationTimestamp) &&
			key.For(a).String() < key.For(b).String())
}

// operatorConfigConflicts compares two operator configs after applying defaults to detect real conflicts.
func (r *Reconciler) operatorConfigConflicts(a, b meshv1alpha1.OperatorConfig) bool {
	return applyOperatorDefaults(a) != applyOperatorDefaults(b)
}

func (r *Reconciler) doReconcile(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) (reconcile.Result, error) {
	if err := r.ensureOperatorPolicy(ctx, mesh); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to ensure operator policy: %w", err)
	}

	if mesh.Spec.Security.Trust.CertManager.IssuerRef.Name != "" {
		if err := r.ensureCertificatesCreated(ctx, mesh, clusters); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.ensureCacertsDistributed(ctx, mesh, clusters); err != nil {
			return reconcile.Result{}, err
		}
	}

	forceCleanupAll := mesh.Spec.Security.Trust.CertManager.IssuerRef.Name == ""
	if err := r.cleanupCertificates(ctx, mesh, clusters, forceCleanupAll); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to cleanup Certificates: %w", err)
	}

	if err := r.cleanupCacertsManifestWorks(ctx, mesh.Spec.ClusterSet); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to cleanup cacerts ManifestWorks: %w", err)
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
		requests = append(requests, reconcile.Request{NamespacedName: key.For(mesh)})
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
	if err := r.Get(ctx, key.Of(obj.GetNamespace()), cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get ManagedCluster %s for ManifestWork %s: %v", obj.GetNamespace(), obj.GetName(), err)
		}
		return nil
	}

	return r.findMeshesForCluster(ctx, cluster)
}

// handleDeletion handles cleanup when the MultiClusterMesh is being deleted.
// If other meshes still target the same ClusterSet, the operator Policy is left intact.
// Only when this is the last mesh does it set the policy to mustnothave and wait for
// the operator to be removed before cleaning up.
func (r *Reconciler) handleDeletion(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.V(4).Infof("MultiClusterMesh %s/%s has no finalizer, nothing to clean up", mesh.Namespace, mesh.Name)
		return reconcile.Result{}, nil
	}

	klog.Infof("Handling deletion for MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)

	isLastMesh, err := r.isLastMeshInClusterSet(ctx, mesh)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to check for other meshes: %w", err)
	}
	klog.V(4).Infof("isLastMesh=%v for %s/%s", isLastMesh, mesh.Namespace, mesh.Name)

	if isLastMesh {
		result, err := r.removeOperatorFromSpokes(ctx, mesh)
		if err != nil || result.RequeueAfter > 0 {
			return result, err
		}

		if err := r.deleteOperatorPolicyResources(ctx, mesh); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to cleanup operator policy resources: %w", err)
		}
	}

	if err := r.cleanupCacertsManifestWorks(ctx, mesh.Spec.ClusterSet); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to cleanup cacerts ManifestWorks: %w", err)
	}

	r.triggerReconcileForNotReadyMeshes(ctx, mesh)

	klog.Infof("Removing finalizer from MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	controllerutil.RemoveFinalizer(mesh, FinalizerName)
	if err := r.Update(ctx, mesh); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return reconcile.Result{}, nil
}

func (r *Reconciler) isLastMeshInClusterSet(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (bool, error) {
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	if err := r.List(ctx, meshList, client.MatchingFields{"spec.clusterSet": mesh.Spec.ClusterSet}); err != nil {
		return false, fmt.Errorf("failed to list meshes for ClusterSet %s: %w", mesh.Spec.ClusterSet, err)
	}

	for i := range meshList.Items {
		if meshList.Items[i].UID != mesh.UID {
			return false, nil
		}
	}
	return true, nil
}

func (r *Reconciler) removeOperatorFromSpokes(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (reconcile.Result, error) {
	policy := &policyv1.Policy{}
	if err := r.Get(ctx, key.Of(OperatorPolicyName, mesh.Namespace), policy); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("failed to get operator Policy: %w", err)
	}

	if err := r.setOperatorPolicyMustnothave(ctx, policy); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to set operator policy to mustnothave: %w", err)
	}

	clusters, _ := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if !r.isOperatorRemovalComplete(policy, len(clusters)) {
		klog.Infof("Waiting for operator removal on spoke clusters for %s/%s", mesh.Namespace, mesh.Name)
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	klog.Infof("Operator removal confirmed on all spoke clusters for %s/%s", mesh.Namespace, mesh.Name)
	return reconcile.Result{}, nil
}

func (r *Reconciler) setOperatorPolicyMustnothave(ctx context.Context, policy *policyv1.Policy) error {
	if len(policy.Spec.PolicyTemplates) == 0 {
		return nil
	}

	raw := policy.Spec.PolicyTemplates[0].ObjectDefinition.Raw
	var opPolicy map[string]interface{}
	if err := json.Unmarshal(raw, &opPolicy); err != nil {
		return fmt.Errorf("failed to unmarshal OperatorPolicy: %w", err)
	}

	spec, _ := opPolicy["spec"].(map[string]interface{})
	if spec == nil {
		return nil
	}

	if ct, _ := spec["complianceType"].(string); strings.EqualFold(ct, "mustnothave") {
		return nil
	}

	spec["complianceType"] = "mustnothave"
	newRaw, err := json.Marshal(opPolicy)
	if err != nil {
		return fmt.Errorf("failed to marshal OperatorPolicy: %w", err)
	}

	policy.Spec.PolicyTemplates[0].ObjectDefinition.Raw = newRaw
	klog.Infof("Setting operator policy %s/%s to mustnothave", policy.Namespace, policy.Name)
	return r.Update(ctx, policy)
}

// isOperatorRemovalComplete checks whether the mustnothave policy has been fulfilled on all clusters.
// If there are no clusters in the ClusterSet, removal is trivially complete.
// Otherwise, every cluster must report Compliant (operator removed).
func (r *Reconciler) isOperatorRemovalComplete(policy *policyv1.Policy, expectedClusters int) bool {
	if expectedClusters == 0 {
		return true
	}
	if len(policy.Status.Status) < expectedClusters {
		return false
	}
	for _, s := range policy.Status.Status {
		if s.ComplianceState != policyv1.Compliant {
			return false
		}
	}
	return true
}

// cleanupCacertsManifestWorks deletes cacerts ManifestWorks on clusters that no mesh in the given ClusterSet needs anymore.
func (r *Reconciler) cleanupCacertsManifestWorks(ctx context.Context, clusterSet string) error {
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

// cleanupCertificates deletes mesh-owned Certificates. When forceCleanupAll is true, all Certificates for the mesh
// are removed (e.g. when the issuer is cleared). Otherwise, only Certificates for clusters no longer in the
// ClusterSet are removed.
func (r *Reconciler) cleanupCertificates(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, forceCleanupAll bool) error {
	clusterNames := clusterNameSet(clusters)

	certList := &certmanagerv1.CertificateList{}
	if err := r.List(ctx, certList,
		client.InNamespace(mesh.Namespace),
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace},
	); err != nil {
		return fmt.Errorf("failed to list Certificates: %w", err)
	}

	for _, cert := range certList.Items {
		clusterName := cert.Labels[ClusterNameLabel]
		if !forceCleanupAll && clusterNames[clusterName] {
			continue
		}

		klog.Infof("Deleting Certificate %s/%s (cluster %s no longer in ClusterSet %s)", cert.Namespace, cert.Name, clusterName, mesh.Spec.ClusterSet)
		if err := r.Delete(ctx, &cert); err != nil {
			return fmt.Errorf("failed to delete Certificate %s/%s: %w", cert.Namespace, cert.Name, err)
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
	mesh.Status.ClusterStatus = make([]meshv1alpha1.ClusterMeshStatus, 0, len(clusters))
	allReady := len(clusters) > 0

	policy := &policyv1.Policy{}
	policyKey := key.Of(OperatorPolicyName, mesh.Namespace)
	policyFound := true
	if err := r.Get(ctx, policyKey, policy); err != nil {
		if apierrors.IsNotFound(err) {
			policyFound = false
		} else {
			return fmt.Errorf("failed to get operator Policy: %w", err)
		}
	}

	clusterCompliance := make(map[string]policyv1.ComplianceState)
	if policyFound {
		for _, status := range policy.Status.Status {
			clusterCompliance[status.ClusterName] = status.ComplianceState
		}
	}

	for _, cluster := range clusters {
		compliance := clusterCompliance[cluster.Name]
		if compliance == policyv1.Compliant {
			mesh.SetClusterCondition(cluster.Name, meshv1alpha1.ConditionOperatorInstalled, metav1.ConditionTrue,
				meshv1alpha1.ReasonOperatorInstalled, "Operator policy is compliant")
		} else {
			allReady = false
			reason := meshv1alpha1.ReasonPolicyCreated
			msg := "Operator policy created, awaiting compliance"
			if compliance == policyv1.NonCompliant {
				reason = meshv1alpha1.ReasonPolicyNonCompliant
				msg = "Operator policy is non-compliant, check policy status for details"
			}
			mesh.SetClusterCondition(cluster.Name, meshv1alpha1.ConditionOperatorInstalled, metav1.ConditionFalse,
				reason, "%s", msg)
		}
	}

	if allReady {
		mesh.SetReadyCondition(metav1.ConditionTrue,
			meshv1alpha1.ReasonAllClustersReady, "All clusters are ready")
	} else {
		mesh.SetReadyCondition(metav1.ConditionFalse,
			meshv1alpha1.ReasonClustersNotReady, "Not all clusters are ready, check individual cluster statuses for details")
	}

	return nil
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

	return clusterNameSet(clusters), nil
}

func clusterNameSet(clusters []clusterv1.ManagedCluster) map[string]bool {
	set := make(map[string]bool, len(clusters))
	for _, c := range clusters {
		set[c.Name] = true
	}
	return set
}

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
	if err := r.Get(ctx, key.Of(clusterSetName), clusterSet); err != nil {
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

// ensureOperatorPolicy ensures the Policy, Placement, PlacementBinding, and ManagedClusterSetBinding
// exist for the operator installation.
func (r *Reconciler) ensureOperatorPolicy(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	if err := r.ensureManagedClusterSetBinding(ctx, mesh); err != nil {
		return fmt.Errorf("failed to ensure ManagedClusterSetBinding: %w", err)
	}

	config := applyOperatorDefaults(mesh.Spec.Operator)

	policy := r.buildOperatorPolicy(mesh, config)
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		policy.Spec = r.buildOperatorPolicySpec(config)
		policy.Labels = operatorPolicyLabels(mesh)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create/update Policy: %w", err)
	}
	klog.V(4).Infof("Operator Policy %s/%s %s", policy.Namespace, policy.Name, result)

	placement := r.buildOperatorPlacement(mesh)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, placement, func() error {
		placement.Spec = clusterv1beta1.PlacementSpec{
			ClusterSets: []string{mesh.Spec.ClusterSet},
			Tolerations: []clusterv1beta1.Toleration{
				{Key: "cluster.open-cluster-management.io/unreachable", Operator: clusterv1beta1.TolerationOpExists},
				{Key: "cluster.open-cluster-management.io/unavailable", Operator: clusterv1beta1.TolerationOpExists},
			},
		}
		placement.Labels = operatorPolicyLabels(mesh)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update Placement: %w", err)
	}

	binding := r.buildOperatorPlacementBinding(mesh)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, binding, func() error {
		binding.PlacementRef = policyv1.PlacementSubject{
			APIGroup: "cluster.open-cluster-management.io",
			Kind:     "Placement",
			Name:     OperatorPolicyName,
		}
		binding.Subjects = []policyv1.Subject{{
			APIGroup: "policy.open-cluster-management.io",
			Kind:     "Policy",
			Name:     OperatorPolicyName,
		}}
		binding.Labels = operatorPolicyLabels(mesh)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update PlacementBinding: %w", err)
	}

	return nil
}

func (r *Reconciler) buildOperatorPolicySpec(config meshv1alpha1.OperatorConfig) policyv1.PolicySpec {
	opPolicy := map[string]interface{}{
		"apiVersion": "policy.open-cluster-management.io/v1beta1",
		"kind":       "OperatorPolicy",
		"metadata": map[string]interface{}{
			"name": OperatorPolicyName,
		},
		"spec": map[string]interface{}{
			"remediationAction": "enforce",
			"severity":          "medium",
			"complianceType":    "musthave",
			"upgradeApproval":   "Automatic",
			"subscription": map[string]interface{}{
				"name":            OperatorNameSail,
				"namespace":       config.Namespace,
				"channel":         config.Channel,
				"source":          config.Source,
				"sourceNamespace": config.SourceNamespace,
			},
		},
	}

	rawBytes, _ := json.Marshal(opPolicy)

	return policyv1.PolicySpec{
		RemediationAction: policyv1.Enforce,
		PolicyTemplates: []*policyv1.PolicyTemplate{{
			ObjectDefinition: runtime.RawExtension{Raw: rawBytes},
		}},
	}
}

func (r *Reconciler) buildOperatorPolicy(mesh *meshv1alpha1.MultiClusterMesh, _ meshv1alpha1.OperatorConfig) *policyv1.Policy {
	return &policyv1.Policy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorPolicyName,
			Namespace: mesh.Namespace,
		},
	}
}

func (r *Reconciler) buildOperatorPlacement(mesh *meshv1alpha1.MultiClusterMesh) *clusterv1beta1.Placement {
	return &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorPolicyName,
			Namespace: mesh.Namespace,
		},
	}
}

func (r *Reconciler) buildOperatorPlacementBinding(mesh *meshv1alpha1.MultiClusterMesh) *policyv1.PlacementBinding {
	return &policyv1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorPolicyName,
			Namespace: mesh.Namespace,
		},
	}
}

func (r *Reconciler) ensureManagedClusterSetBinding(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	binding := &clusterv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mesh.Spec.ClusterSet,
			Namespace: mesh.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, binding, func() error {
		binding.Spec.ClusterSet = mesh.Spec.ClusterSet
		return nil
	})
	return err
}

func (r *Reconciler) deleteOperatorPolicyResources(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	klog.Infof("Deleting operator Policy resources for %s/%s", mesh.Namespace, mesh.Name)
	for _, obj := range []client.Object{
		&policyv1.Policy{ObjectMeta: metav1.ObjectMeta{Name: OperatorPolicyName, Namespace: mesh.Namespace}},
		&policyv1.PlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: OperatorPolicyName, Namespace: mesh.Namespace}},
		&clusterv1beta1.Placement{ObjectMeta: metav1.ObjectMeta{Name: OperatorPolicyName, Namespace: mesh.Namespace}},
		&clusterv1beta2.ManagedClusterSetBinding{ObjectMeta: metav1.ObjectMeta{Name: mesh.Spec.ClusterSet, Namespace: mesh.Namespace}},
	} {
		if err := client.IgnoreNotFound(r.Delete(ctx, obj)); err != nil {
			return fmt.Errorf("failed to delete %T %s/%s: %w", obj, obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

// findMeshesForPolicy maps a Policy change back to all meshes in the same ClusterSet
func (r *Reconciler) findMeshesForPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	clusterSet := obj.GetLabels()[ClusterSetLabel]
	if clusterSet == "" {
		return nil
	}
	return r.reconcileRequestsForClusterSet(ctx, clusterSet)
}

func operatorPolicyLabels(mesh *meshv1alpha1.MultiClusterMesh) map[string]string {
	return map[string]string{
		ManagedByLabel:  ManagedByValue,
		ClusterSetLabel: mesh.Spec.ClusterSet,
	}
}

func applyOperatorDefaults(config meshv1alpha1.OperatorConfig) meshv1alpha1.OperatorConfig {
	if config.Namespace == "" {
		config.Namespace = DefaultOperatorNs
	}
	if config.Source == "" {
		config.Source = DefaultCatalogSource
	}
	if config.SourceNamespace == "" {
		config.SourceNamespace = DefaultCatalogNs
	}
	if config.Channel == "" {
		config.Channel = DefaultChannel
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

	return []reconcile.Request{{NamespacedName: key.Of(meshName, meshNamespace)}}
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

// ensureCertificateForCluster applies the desired Certificate state for a specific cluster using server-side apply.
func (r *Reconciler) ensureCertificateForCluster(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	certName := getCacertsName(cluster.Name)

	gvk, err := r.GroupVersionKindFor(mesh)
	if err != nil {
		return fmt.Errorf("failed to get GVK for MultiClusterMesh: %w", err)
	}
	cert := certmanagerapply.Certificate(certName, mesh.Namespace).
		WithLabels(meshOwnedLabels(mesh, cluster.Name)).
		WithOwnerReferences(applyconfigv1.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(mesh.Name).
			WithUID(mesh.UID).
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithSpec(certmanagerapply.CertificateSpec().
			WithSecretName(certName).
			WithSecretTemplate(certmanagerapply.CertificateSecretTemplate().
				WithLabels(meshOwnedLabels(mesh, cluster.Name))).
			WithDuration(metav1.Duration{Duration: 60 * Day}).
			WithRenewBefore(metav1.Duration{Duration: 15 * Day}).
			WithCommonName("Istio CA").
			WithSubject(certmanagerapply.X509Subject().
				WithOrganizations(mesh.GetTrustDomain()).
				WithOrganizationalUnits(cluster.Name)).
			WithURIs("spiffe://"+mesh.GetTrustDomain()+"/cluster/"+cluster.Name+"/ca/istio-ca").
			WithIsCA(true).
			WithUsages(
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageCertSign,
			).
			WithIssuerRef(cmmetaapply.IssuerReference().
				WithName(mesh.Spec.Security.Trust.CertManager.IssuerRef.Name).
				WithKind(mesh.Spec.Security.Trust.CertManager.IssuerRef.Kind).
				WithGroup("cert-manager.io")))

	if err := r.Apply(ctx, cert, client.FieldOwner(ManagedByValue), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply Certificate %s/%s: %w", mesh.Namespace, certName, err)
	}

	klog.Infof("Successfully applied Certificate %s/%s", mesh.Namespace, certName)
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
	err := r.Get(ctx, key.Of(secretName, mesh.Namespace), secret)

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
