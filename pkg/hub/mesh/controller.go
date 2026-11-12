package mesh

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerapply "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/certmanager/v1"
	cmmetaapply "github.com/cert-manager/cert-manager/pkg/client/applyconfigurations/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	applyconfigv1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	ManifestWorkNameCPNSPrefix = "multicluster-mesh-cp-ns-"

	CacertsSecretName = "cacerts"

	FinalizerName = "mesh.open-cluster-management.io/finalizer"

	ClusterNameLabel   = "mesh.open-cluster-management.io/cluster-name"
	ManagedByLabel     = "app.kubernetes.io/managed-by"
	ManagedByValue     = "multicluster-mesh-addon"
	MeshNameLabel      = "mesh.open-cluster-management.io/mesh-name"
	MeshNamespaceLabel = "mesh.open-cluster-management.io/mesh-namespace"

	PlacementLabel    = "cluster.open-cluster-management.io/placement"
	IstioNetworkLabel = "topology.istio.io/network"

	Day = 24 * time.Hour
)

// errDecisionsPending means the PlacementDecision entries don't yet match Placement.Status.NumberOfSelectedClusters.
// ACM's Placement controller writes PlacementDecision objects one at a time, so during cluster redistribution a cluster can temporarily be missing from all decisions (see ACM-27799).
// The reconciler returns without making changes.
// The PlacementDecision and Placement watches re-trigger once the counts converge.
var errDecisionsPending = errors.New("PlacementDecision entries do not match Placement status")

// RemovalGracePeriod is how long a cluster that left the Placement selection keeps its resources before they are deleted.
// A cluster moving between PlacementDecision objects is temporarily missing from the observed selection while OCM writes the objects one at a time, so removal is delayed to let the redistribution settle.
var RemovalGracePeriod = 30 * time.Second

// Reconciler reconciles MultiClusterMesh resources
type Reconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	workApplier *applier.WorkApplier
}

var prerequisiteCRDs = map[string]schema.GroupVersionKind{
	"cert-manager (Certificate)":                     certmanagerv1.SchemeGroupVersion.WithKind("Certificate"),
	"managed-serviceaccount (ManagedServiceAccount)": msav1beta1.GroupVersion.WithKind("ManagedServiceAccount"),
}

func checkPrerequisiteCRDs(mapper meta.RESTMapper) error {
	var missing []string
	for name, gvk := range prerequisiteCRDs {
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			if !meta.IsNoMatchError(err) {
				return fmt.Errorf("failed to check prerequisite CRD %s: %w", name, err)
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("prerequisite CRDs not installed on the hub: %s", strings.Join(missing, ", "))
	}
	return nil
}

// RegisterController registers the MultiClusterMesh controller with the manager
func RegisterController(mgr manager.Manager) error {
	if err := checkPrerequisiteCRDs(mgr.GetRESTMapper()); err != nil {
		return err
	}

	// Controller-runtime prefixes namespaced index keys with the object's namespace.
	// A mesh team-a/mesh-a that references mesh-placement is stored as team-a/mesh-placement.
	// The same Placement name in team-b is stored as team-b/mesh-placement.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &meshv1alpha1.MultiClusterMesh{}, "spec.placementRef.name", func(obj client.Object) []string {
		return []string{obj.(*meshv1alpha1.MultiClusterMesh).Spec.PlacementRef.Name}
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
			&clusterv1beta1.PlacementDecision{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForPlacementDecision),
		).
		Watches(
			&clusterv1beta1.Placement{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findMeshesForPlacement),
		).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(reconciler.mapSecretToMesh),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()[MeshNameLabel] != "" && obj.GetLabels()[MeshNamespaceLabel] != ""
			})),
		).
		Watches(&msav1beta1.ManagedServiceAccount{},
			handler.EnqueueRequestsFromMapFunc(reconciler.mapMsaToMesh),
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
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placementdecisions,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworkreplicasets,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authentication.open-cluster-management.io,resources=managedserviceaccounts,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements the reconcile loop for MultiClusterMesh resources
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (result reconcile.Result, reconcileErr error) {
	klog.Infof("Reconciling MultiClusterMesh: %s/%s", req.Namespace, req.Name)

	// Fetch the MultiClusterMesh resource
	mesh := &meshv1alpha1.MultiClusterMesh{}
	if err := r.Get(ctx, req.NamespacedName, mesh); err != nil {
		klog.V(4).Infof("MultiClusterMesh not found, may have been deleted: %s/%s", req.Namespace, req.Name)
		return result, client.IgnoreNotFound(err)
	}

	if !mesh.DeletionTimestamp.IsZero() {
		klog.Infof("MultiClusterMesh being deleted: %s/%s", req.Namespace, req.Name)
		return result, r.handleDeletion(ctx, mesh)
	}

	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.Infof("Adding finalizer to MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
		controllerutil.AddFinalizer(mesh, FinalizerName)
		if err := r.Update(ctx, mesh); err != nil {
			return result, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return
	}

	oldStatus := mesh.Status.DeepCopy()

	placement := &clusterv1beta1.Placement{}
	if err := r.Get(ctx, key.Of(mesh.Spec.PlacementRef.Name, mesh.Namespace), placement); err != nil {
		if !apierrors.IsNotFound(err) {
			reconcileErr = fmt.Errorf("failed to get Placement %s: %w", mesh.Spec.PlacementRef.Name, err)
		}
		placement = nil
	}

	if reconcileErr == nil {
		var clusters, effectiveClusters map[string]*clusterv1.ManagedCluster
		var requeueAfter time.Duration

		// When the Placement is missing, doReconcile must not run because its cleanup functions would tear down all infrastructure.
		// The Placement might be created later or the reference might be fixed, and clusterStatus is cleared while it is missing.
		if placement != nil {
			clusters, reconcileErr = r.getClustersFromPlacement(ctx, placement)
			if errors.Is(reconcileErr, errDecisionsPending) {
				klog.Warningf("Placement %s is still reconciling, waiting for it to finish", placement.Name)
				return result, nil
			}

			if reconcileErr == nil {
				effectiveClusters, requeueAfter = effectiveMembership(mesh, clusters)
				reconcileErr = r.doReconcile(ctx, mesh, effectiveClusters)
			}
		}

		if reconcileErr == nil {
			klog.Infof("Successfully reconciled MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
			mesh.Status.ClusterStatus = slices.DeleteFunc(mesh.Status.ClusterStatus, func(cs meshv1alpha1.ClusterMeshStatus) bool {
				return !isMember(effectiveClusters, cs.ClusterName)
			})
			reconcileErr = r.determineStatus(ctx, mesh, clusters, placement)
			if reconcileErr == nil && requeueAfter != 0 {
				result.RequeueAfter = requeueAfter
			}
		}
	}

	if reconcileErr != nil {
		klog.Errorf("Encountered an error while reconciling MultiClusterMesh %s/%s: %v", mesh.Namespace, mesh.Name, reconcileErr)
		mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonReconcileError, "%v", reconcileErr)
	}

	mesh.Status.ControlPlaneNamespace = mesh.GetControlPlaneNamespace()

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

// doReconcile creates resources for clusters with a resolved object and cleans up resources for clusters outside the effective membership.
// clusters is the effective membership: it maps each cluster name whose resources must be preserved to its ManagedCluster object.
// A nil value means no object is available: the cluster is pending removal or its ManagedCluster object does not exist.
// Cleanup functions use the keys, the install loop uses the non-nil values.
func (r *Reconciler) doReconcile(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters map[string]*clusterv1.ManagedCluster) error {
	for _, cluster := range clusters {
		if cluster == nil {
			continue
		}

		klog.V(4).Infof("Reconciling cluster %s", cluster.Name)

		cpNsWork, err := r.workApplier.Apply(ctx, r.buildControlPlaneNamespaceManifestWork(mesh, cluster))
		if err != nil {
			return fmt.Errorf("failed to apply control plane namespace ManifestWork on cluster %s: %w", cluster.Name, err)
		}
		klog.V(4).Infof("Applied control plane namespace ManifestWork %s/%s", cpNsWork.Namespace, cpNsWork.Name)

		if err := r.ensureManagedServiceAccount(ctx, mesh, cluster); err != nil {
			return fmt.Errorf("failed to ensure ManagedServiceAccount for cluster %s: %w", cluster.Name, err)
		}

		readerWork, err := r.workApplier.Apply(ctx, buildIstioReaderManifestWork(mesh, cluster))
		if err != nil {
			return fmt.Errorf("failed to apply istio-reader ManifestWork on cluster %s: %w", cluster.Name, err)
		}
		klog.V(4).Infof("Applied istio-reader ManifestWork %s/%s", readerWork.Namespace, readerWork.Name)

		if mesh.HasTrustConfigured() {
			if err := r.ensureCertificateForCluster(ctx, mesh, cluster); err != nil {
				return fmt.Errorf("failed to ensure certificate for cluster %s: %w", cluster.Name, err)
			}
			if err := r.ensureCacertsManifestWork(ctx, mesh, cluster); err != nil {
				return fmt.Errorf("failed to ensure cacerts ManifestWork for cluster %s: %w", cluster.Name, err)
			}
		}
	}

	if !mesh.HasTrustConfigured() {
		if err := r.deleteAllCertificates(ctx, mesh); err != nil {
			return fmt.Errorf("failed to cleanup Certificates: %w", err)
		}
	} else {
		if err := r.deleteCertificatesForRemovedClusters(ctx, mesh, clusters); err != nil {
			return fmt.Errorf("failed to cleanup Certificates: %w", err)
		}
	}

	if err := r.cleanupMeshOwnedManifestWorks(ctx, mesh, clusters); err != nil {
		return fmt.Errorf("failed to cleanup mesh-owned ManifestWorks: %w", err)
	}

	if err := r.cleanupManagedServiceAccounts(ctx, mesh, clusters); err != nil {
		return fmt.Errorf("failed to cleanup ManagedServiceAccounts: %w", err)
	}

	if err := r.ensureRemoteSecretDistribution(ctx, mesh, clusters); err != nil {
		return fmt.Errorf("failed to ensure ManifestWorkReplicaSet for mesh %s/%s: %w", mesh.Namespace, mesh.Name, err)
	}

	return nil
}

// findMeshesForPlacement returns reconcile requests for all meshes referencing the given Placement.
func (r *Reconciler) findMeshesForPlacement(ctx context.Context, obj client.Object) []reconcile.Request {
	placement := obj.(*clusterv1beta1.Placement)
	return r.reconcileRequestsForPlacement(ctx, placement.Name, placement.Namespace)
}

// findMeshesForPlacementDecision returns reconcile requests for all meshes whose Placement
// owns the given PlacementDecision (cluster membership changed).
func (r *Reconciler) findMeshesForPlacementDecision(ctx context.Context, obj client.Object) []reconcile.Request {
	pd := obj.(*clusterv1beta1.PlacementDecision)
	placementName := pd.Labels[PlacementLabel]
	if placementName == "" {
		return nil
	}

	klog.V(4).Infof("PlacementDecision %s/%s changed, reconciling meshes using Placement %s", pd.Namespace, pd.Name, placementName)
	return r.reconcileRequestsForPlacement(ctx, placementName, pd.Namespace)
}

// findMeshesForCluster returns reconcile requests for meshes that have ManifestWorks on the cluster.
func (r *Reconciler) findMeshesForCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	cluster := obj.(*clusterv1.ManagedCluster)

	workList := &workv1.ManifestWorkList{}
	if err := r.List(ctx, workList,
		client.InNamespace(cluster.Name),
		client.MatchingLabels{ManagedByLabel: ManagedByValue},
	); err != nil {
		klog.Errorf("Failed to list ManifestWorks for cluster %s: %v", cluster.Name, err)
		return nil
	}

	seen := make(map[string]bool)
	var requests []reconcile.Request
	for _, work := range workList.Items {
		meshName := work.Labels[MeshNameLabel]
		meshNamespace := work.Labels[MeshNamespaceLabel]
		if meshName == "" || meshNamespace == "" {
			continue
		}
		reqKey := meshNamespace + "/" + meshName
		if seen[reqKey] {
			continue
		}
		seen[reqKey] = true
		klog.V(4).Infof("ManagedCluster %s changed, reconciling mesh %s/%s", cluster.Name, meshNamespace, meshName)
		requests = append(requests, reconcile.Request{NamespacedName: key.Of(meshName, meshNamespace)})
	}

	return requests
}

// findMeshesForManifestWork returns reconcile requests for meshes affected by a ManifestWork change.
// All ManifestWorks managed by this addon are mesh-owned and carry mesh labels.
func (r *Reconciler) findMeshesForManifestWork(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	meshName := labels[MeshNameLabel]
	meshNamespace := labels[MeshNamespaceLabel]
	if meshName != "" && meshNamespace != "" {
		klog.V(4).Infof("ManifestWork %s/%s changed, reconciling mesh %s/%s", obj.GetNamespace(), obj.GetName(), meshNamespace, meshName)
		return []reconcile.Request{{NamespacedName: key.Of(meshName, meshNamespace)}}
	}
	return nil
}

func (r *Reconciler) reconcileRequestsForPlacement(ctx context.Context, placementName, namespace string) []reconcile.Request {
	meshList := &meshv1alpha1.MultiClusterMeshList{}
	if err := r.List(ctx, meshList,
		client.InNamespace(namespace),
		client.MatchingFields{"spec.placementRef.name": placementName},
	); err != nil {
		klog.Errorf("Failed to list meshes for Placement %s/%s: %v", namespace, placementName, err)
		return nil
	}

	var requests []reconcile.Request
	for i := range meshList.Items {
		requests = append(requests, reconcile.Request{NamespacedName: key.For(&meshList.Items[i])})
	}
	return requests
}

// handleDeletion handles cleanup when the MultiClusterMesh is being deleted
func (r *Reconciler) handleDeletion(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	if !controllerutil.ContainsFinalizer(mesh, FinalizerName) {
		klog.V(4).Infof("MultiClusterMesh %s/%s has no finalizer, nothing to clean up", mesh.Namespace, mesh.Name)
		return nil
	}

	klog.Infof("Handling deletion for MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)

	if err := r.cleanupMeshOwnedManifestWorks(ctx, mesh, nil); err != nil {
		return fmt.Errorf("failed to cleanup mesh-owned ManifestWorks: %w", err)
	}

	if err := r.deleteAllManagedServiceAccounts(ctx, mesh); err != nil {
		return fmt.Errorf("failed to cleanup ManagedServiceAccount resources: %w", err)
	}

	klog.Infof("Removing finalizer from MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	controllerutil.RemoveFinalizer(mesh, FinalizerName)
	if err := r.Update(ctx, mesh); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return nil
}

// deleteAllCertificates deletes all mesh-owned Certificates (e.g. when the issuer is removed).
func (r *Reconciler) deleteAllCertificates(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) error {
	certList := &certmanagerv1.CertificateList{}
	if err := r.List(ctx, certList,
		client.InNamespace(mesh.Namespace),
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace},
	); err != nil {
		return fmt.Errorf("failed to list Certificates: %w", err)
	}

	for _, cert := range certList.Items {
		klog.Infof("Deleting Certificate %s/%s (issuer configuration removed)", cert.Namespace, cert.Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &cert)); err != nil {
			return fmt.Errorf("failed to delete Certificate %s/%s: %w", cert.Namespace, cert.Name, err)
		}
	}

	return nil
}

// deleteCertificatesForRemovedClusters deletes Certificates for clusters outside the effective membership.
func (r *Reconciler) deleteCertificatesForRemovedClusters(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters map[string]*clusterv1.ManagedCluster) error {
	certList := &certmanagerv1.CertificateList{}
	if err := r.List(ctx, certList,
		client.InNamespace(mesh.Namespace),
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace},
	); err != nil {
		return fmt.Errorf("failed to list Certificates: %w", err)
	}

	for _, cert := range certList.Items {
		clusterName := cert.Labels[ClusterNameLabel]
		if isMember(clusters, clusterName) {
			continue
		}

		klog.Infof("Deleting Certificate %s/%s (cluster %s no longer selected by Placement)", cert.Namespace, cert.Name, clusterName)
		if err := client.IgnoreNotFound(r.Delete(ctx, &cert)); err != nil {
			return fmt.Errorf("failed to delete Certificate %s/%s: %w", cert.Namespace, cert.Name, err)
		}
	}

	return nil
}

func (r *Reconciler) cleanupMeshOwnedManifestWorks(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters map[string]*clusterv1.ManagedCluster) error {
	workList := &workv1.ManifestWorkList{}
	if err := r.List(ctx, workList,
		client.MatchingLabels{MeshNameLabel: mesh.Name, MeshNamespaceLabel: mesh.Namespace},
	); err != nil {
		return fmt.Errorf("failed to list mesh-owned ManifestWorks: %w", err)
	}

	for _, work := range workList.Items {
		if isMember(clusters, work.Namespace) {
			continue
		}

		klog.Infof("Deleting mesh-owned ManifestWork %s/%s", work.Namespace, work.Name)
		if err := r.workApplier.Delete(ctx, work.Namespace, work.Name); err != nil {
			return fmt.Errorf("failed to delete ManifestWork %s/%s: %w", work.Namespace, work.Name, err)
		}
	}

	return nil
}

// hasSelectedCluster reports whether any observed cluster has a ManagedCluster object.
func hasSelectedCluster(clusters map[string]*clusterv1.ManagedCluster) bool {
	for _, cluster := range clusters {
		if cluster != nil {
			return true
		}
	}
	return false
}

// isMember reports whether the cluster is still a mesh member.
func isMember(clusters map[string]*clusterv1.ManagedCluster, name string) bool {
	_, ok := clusters[name]
	return ok
}

// effectiveMembership returns the cluster names whose resources must be preserved: every observed name,
// plus clusters whose PlacementSelected condition is False (pending removal) and whose grace period has not expired yet.
// The result maps each name to its ManagedCluster object, or to nil when no object is available.
// requeueAfter is the time until the earliest pending expiry, or zero when nothing is pending.
// A cluster missing from the observed set without a False PlacementSelected condition is newly missing, so its grace period starts now.
func effectiveMembership(mesh *meshv1alpha1.MultiClusterMesh, clusters map[string]*clusterv1.ManagedCluster) (effectiveClusters map[string]*clusterv1.ManagedCluster, requeueAfter time.Duration) {
	now := time.Now()
	effectiveClusters = make(map[string]*clusterv1.ManagedCluster, len(clusters)+len(mesh.Status.ClusterStatus))
	maps.Copy(effectiveClusters, clusters)

	for _, cs := range mesh.Status.ClusterStatus {
		if isMember(clusters, cs.ClusterName) {
			continue
		}

		deadline := now
		if c := meta.FindStatusCondition(cs.Conditions, meshv1alpha1.ConditionPlacementSelected); c != nil && c.Status == metav1.ConditionFalse {
			deadline = c.LastTransitionTime.Time
		}
		expiry := deadline.Add(RemovalGracePeriod)
		if now.Before(expiry) {
			effectiveClusters[cs.ClusterName] = nil
			if until := expiry.Sub(now); requeueAfter == 0 || until < requeueAfter {
				requeueAfter = until
			}
		}
	}

	return effectiveClusters, requeueAfter
}

func (r *Reconciler) determineStatus(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters map[string]*clusterv1.ManagedCluster, placement *clusterv1beta1.Placement) error {
	if placement == nil {
		mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonPlacementNotFound,
			"Placement %s not found in namespace %s", mesh.Spec.PlacementRef.Name, mesh.Namespace)
		return nil
	}

	if !mesh.HasTrustConfigured() {
		for _, cs := range mesh.Status.ClusterStatus {
			mesh.RemoveClusterCondition(cs.ClusterName, meshv1alpha1.ConditionTrustDistributed)
		}
	}

	allReady := true

	// Determine status for all clusters selected by the placement.
	// The sorted iteration keeps clusterStatus entry creation deterministic.
	for _, name := range slices.Sorted(maps.Keys(clusters)) {
		mesh.SetClusterCondition(name, meshv1alpha1.ConditionPlacementSelected,
			metav1.ConditionTrue, meshv1alpha1.ReasonSelected, "Cluster is selected by the Placement")
		if clusters[name] == nil {
			continue
		}
		if ready, err := r.updateTrustStatusCondition(ctx, mesh, name); err != nil {
			return err
		} else if !ready {
			allReady = false
		}
	}

	// Mark any clusters that aren't in the placement anymore as pending removal.
	for _, cs := range mesh.Status.ClusterStatus {
		if isMember(clusters, cs.ClusterName) {
			continue
		}
		mesh.SetClusterCondition(cs.ClusterName, meshv1alpha1.ConditionPlacementSelected,
			metav1.ConditionFalse, meshv1alpha1.ReasonRemovalPending, "Cluster left the Placement selection, removal pending")
	}

	if !hasSelectedCluster(clusters) {
		mesh.SetReadyCondition(metav1.ConditionFalse, meshv1alpha1.ReasonNoClustersSelected,
			"Placement %s has not selected any clusters", mesh.Spec.PlacementRef.Name)
	} else if !allReady {
		mesh.SetReadyCondition(metav1.ConditionFalse,
			meshv1alpha1.ReasonClustersNotReady, "Not all clusters are ready, check individual cluster statuses for details")
	} else {
		mesh.SetReadyCondition(metav1.ConditionTrue,
			meshv1alpha1.ReasonAllClustersReady, "All clusters are ready")
	}

	return nil
}

func (r *Reconciler) updateTrustStatusCondition(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusterName string) (ready bool, err error) {
	if !mesh.HasTrustConfigured() {
		return true, nil
	}

	cacertsWork := &workv1.ManifestWork{}
	err = r.Get(ctx, key.Of(CacertsManifestWorkName(mesh), clusterName), cacertsWork)
	if apierrors.IsNotFound(err) {
		mesh.SetClusterCondition(clusterName, meshv1alpha1.ConditionTrustDistributed, metav1.ConditionFalse,
			meshv1alpha1.ReasonCertificatePending, "Waiting for the CA certificate to be issued")
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get cacerts ManifestWork for cluster %s: %w", clusterName, err)
	}

	applied := meta.FindStatusCondition(cacertsWork.Status.Conditions, workv1.WorkApplied)
	if applied != nil && applied.Status == metav1.ConditionTrue &&
		applied.ObservedGeneration == cacertsWork.Generation &&
		meta.IsStatusConditionTrue(cacertsWork.Status.Conditions, workv1.WorkAvailable) {
		mesh.SetClusterCondition(clusterName, meshv1alpha1.ConditionTrustDistributed, metav1.ConditionTrue,
			meshv1alpha1.ReasonDistributed, "Trust distributed")
		return true, nil
	}

	mesh.SetClusterCondition(clusterName, meshv1alpha1.ConditionTrustDistributed, metav1.ConditionFalse,
		meshv1alpha1.ReasonDistributionPending, "Trust distribution is pending")
	return false, nil
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

func (r *Reconciler) mapMsaToMesh(_ context.Context, obj client.Object) []reconcile.Request {
	meshName := obj.GetLabels()[MeshNameLabel]
	meshNamespace := obj.GetLabels()[MeshNamespaceLabel]

	klog.V(4).Infof("ManagedServiceAccount %s/%s triggered reconcile for mesh %s/%s",
		obj.GetNamespace(), obj.GetName(), meshNamespace, meshName)

	return []reconcile.Request{{NamespacedName: key.Of(meshName, meshNamespace)}}
}

// CacertsName returns a unique name to use for cacert resources in the mesh's hub namespace.
// The cert/secret are namespaced to the mesh's namespace, so the mesh name and cluster name identify each resource uniquely enough.
func CacertsName(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) string {
	return fmt.Sprintf("cacerts-%s.%s", mesh.Name, clusterName)
}

// CacertsManifestWorkName returns the name of the ManifestWork distributing the cacerts secret for a mesh.
// It lives in a cluster's namespace, so it is keyed on the mesh namespace and name to stay unique per mesh.
func CacertsManifestWorkName(mesh *meshv1alpha1.MultiClusterMesh) string {
	return fmt.Sprintf("multicluster-mesh-cacerts-%s.%s", mesh.Namespace, mesh.Name)
}

// ensureCertificateForCluster applies the desired Certificate state for a specific cluster using server-side apply.
func (r *Reconciler) ensureCertificateForCluster(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	certName := CacertsName(mesh, cluster.Name)

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

// ensureCacertsManifestWork creates a ManifestWork to distribute the cacerts secret to a cluster
func (r *Reconciler) ensureCacertsManifestWork(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	secretName := CacertsName(mesh, cluster.Name)
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

func (r *Reconciler) buildControlPlaneNamespaceManifestWork(mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) *workv1.ManifestWork {
	cpNamespace := mesh.GetControlPlaneNamespace()

	network := cluster.Name
	if v, ok := cluster.Labels[IstioNetworkLabel]; ok && v != "" {
		network = v
	}

	return buildMeshOwnedManifestWork(mesh, cluster.Name, ManifestWorkNameCPNSPrefix+cpNamespace, &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: cpNamespace,
			Labels: map[string]string{
				IstioNetworkLabel:  network,
				MeshNameLabel:      mesh.Name,
				MeshNamespaceLabel: mesh.Namespace,
			},
		},
	})
}

// buildCacertsManifestWork builds a ManifestWork for distributing the cacerts secret
func (r *Reconciler) buildCacertsManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterName string, secret *corev1.Secret) *workv1.ManifestWork {
	return buildMeshOwnedManifestWork(mesh, clusterName, CacertsManifestWorkName(mesh), &corev1.Secret{
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
	})
}

func buildMeshOwnedManifestWork(mesh *meshv1alpha1.MultiClusterMesh, clusterName, name string, objs ...runtime.Object) *workv1.ManifestWork {
	manifests := make([]workv1.Manifest, len(objs))
	for i, obj := range objs {
		manifests[i] = workv1.Manifest{RawExtension: runtime.RawExtension{Object: obj}}
	}
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
			Labels:    meshOwnedLabels(mesh, clusterName),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

func meshOwnedLabels(mesh *meshv1alpha1.MultiClusterMesh, clusterName string) map[string]string {
	return map[string]string{
		ManagedByLabel:     ManagedByValue,
		MeshNameLabel:      mesh.Name,
		MeshNamespaceLabel: mesh.Namespace,
		ClusterNameLabel:   clusterName,
	}
}

// getClustersFromPlacement maps the Placement's PlacementDecision entries to the observed cluster names, each mapped to its ManagedCluster object.
// A nil value means the cluster is selected but its ManagedCluster object does not exist.
func (r *Reconciler) getClustersFromPlacement(ctx context.Context, placement *clusterv1beta1.Placement) (map[string]*clusterv1.ManagedCluster, error) {
	pdList := &clusterv1beta1.PlacementDecisionList{}
	if err := r.List(ctx, pdList,
		client.InNamespace(placement.Namespace),
		client.MatchingLabels{PlacementLabel: placement.Name},
	); err != nil {
		return nil, fmt.Errorf("failed to list PlacementDecisions for Placement %s: %w", placement.Name, err)
	}

	clusters := make(map[string]*clusterv1.ManagedCluster)
	for _, pd := range pdList.Items {
		for _, decision := range pd.Status.Decisions {
			clusters[decision.ClusterName] = nil
		}
	}

	// OCM writes PlacementDecision objects one at a time and updates NumberOfSelectedClusters only after every object is written.
	// A mismatch means the decisions are still being redistributed, so the observed names are not a complete membership snapshot.
	if int32(len(clusters)) != placement.Status.NumberOfSelectedClusters {
		return nil, errDecisionsPending
	}

	// The Placement controller only adds a cluster to the PlacementDecision after the ManagedCluster exists on the hub.
	// A NotFound here means the cluster is being deleted; the Placement controller will update the PlacementDecision shortly,
	// which triggers a correct reconciliation via the PlacementDecision watch.
	for name := range clusters {
		cluster := &clusterv1.ManagedCluster{}
		if err := r.Get(ctx, key.Of(name), cluster); err != nil {
			// When a cluster is not found, we still keep the name in the map so cleanup preserves its resources until it leaves the decisions.
			if apierrors.IsNotFound(err) {
				klog.V(4).Infof("Cluster %s from PlacementDecision not found, skipping", name)
				continue
			}
			return nil, fmt.Errorf("failed to get ManagedCluster %s: %w", name, err)
		}
		clusters[name] = cluster
	}

	return clusters, nil
}
