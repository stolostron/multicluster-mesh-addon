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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
)

const (
	operatorNameOCP = "servicemeshoperator3"
	operatorName    = "sailoperator"

	defaultOCPOperatorNs = "openshift-operators"
	defaultOperatorNs    = "sail-operator"

	defaultOCPCatalogSource = "redhat-operators"
	defaultOCPCatalogNs     = "openshift-marketplace"
	defaultCatalogSource    = "operatorhubio-catalog"
	defaultCatalogNs        = "olm"

	clusterClaimProduct = "product.open-cluster-management.io"

	productOCP  = "OpenShift"
	productROSA = "ROSA"
	productARO  = "ARO"
	productROKS = "ROKS"
	productOCPD = "OpenShiftDedicated"
)

// Reconciler reconciles MultiClusterMesh resources
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// RegisterController registers the MultiClusterMesh controller with the manager
func RegisterController(mgr manager.Manager) error {
	reconciler := &Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&meshv1alpha1.MultiClusterMesh{}).
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

	klog.Infof("MultiClusterMesh reconciling: %s/%s, ClusterSet: %s",
		req.Namespace, req.Name, mesh.Spec.ClusterSet)
	clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if err != nil {
		klog.Errorf("Failed to get clusters from set %s: %v", mesh.Spec.ClusterSet, err)
		return reconcile.Result{}, err
	}

	klog.Infof("Found %d clusters in set %s", len(clusters), mesh.Spec.ClusterSet)

	// Install the operator on each cluster
	for _, cluster := range clusters {
		if err := r.ensureOperatorInstalled(ctx, mesh, &cluster); err != nil {
			klog.Errorf("Failed to ensure mesh operator on cluster %s: %v", cluster.Name, err)
			return reconcile.Result{}, err
		}
	}

	klog.Infof("Successfully reconciled MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	return reconcile.Result{}, nil
}

// handleDeletion handles cleanup when the MultiClusterMesh is being deleted
func (r *Reconciler) handleDeletion(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (reconcile.Result, error) {
	klog.Infof("Handling deletion for MultiClusterMesh %s/%s", mesh.Namespace, mesh.Name)
	clusters, err := r.getClustersFromSet(ctx, mesh.Spec.ClusterSet)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get clusters for cleanup: %w", err)
	}

	for _, cluster := range clusters {
		operatorConfig := r.applyOperatorDefaults(mesh.Spec.Operator, &cluster)
		workName := getOperatorManifestWorkName(operatorConfig)
		work := &workv1.ManifestWork{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      workName,
			Namespace: cluster.Name,
		}, work)

		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(4).Infof("ManifestWork %s/%s already deleted", cluster.Name, workName)
				continue
			}
			return reconcile.Result{}, fmt.Errorf("failed to get ManifestWork %s/%s: %w", cluster.Name, workName, err)
		}

		if err := r.Delete(ctx, work); err != nil {
			if !errors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("failed to delete ManifestWork %s/%s: %w", cluster.Name, workName, err)
			}
			klog.V(4).Infof("ManifestWork %s/%s already deleted", cluster.Name, workName)
		} else {
			klog.Infof("Deleted ManifestWork %s/%s", cluster.Name, workName)
		}
	}

	// TODO: We don't remove finalizer here since we haven't added one yet
	// This will be added when we implement more complex lifecycle management
	return reconcile.Result{}, nil
}

func (r *Reconciler) ensureOperatorInstalled(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, cluster *clusterv1.ManagedCluster) error {
	klog.V(4).Infof("Ensuring mesh operator on cluster %s for mesh %s", cluster.Name, mesh.Name)
	operatorConfig := r.applyOperatorDefaults(mesh.Spec.Operator, cluster)
	workName := getOperatorManifestWorkName(operatorConfig)

	// Check if ManifestWork already exists
	existingWork := &workv1.ManifestWork{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      workName,
		Namespace: cluster.Name,
	}, existingWork)

	if err == nil {
		klog.V(4).Infof("ManifestWork %s/%s already exists", cluster.Name, workName)
		// TODO: Add logic to check if the work needs updating (e.g., channel change)
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ManifestWork: %w", err)
	}

	klog.Infof("Creating ManifestWork to install operator on cluster %s", cluster.Name)
	work := r.buildOperatorManifestWork(operatorConfig, cluster.Name)

	if err := r.Create(ctx, work); err != nil {
		return fmt.Errorf("failed to create ManifestWork: %w", err)
	}

	klog.Infof("Successfully created ManifestWork %s/%s for operator installation", cluster.Name, work.Name)
	return nil
}

// applyOperatorDefaults applies platform-specific defaults for the given cluster to the operator config
func (r *Reconciler) applyOperatorDefaults(config meshv1alpha1.OperatorConfig, cluster *clusterv1.ManagedCluster) meshv1alpha1.OperatorConfig {
	result := config
	isOCP := isOpenShift(cluster)

	if result.Namespace == "" {
		if isOCP {
			result.Namespace = defaultOCPOperatorNs
		} else {
			result.Namespace = defaultOperatorNs
		}
	}

	if result.Source == "" {
		if isOCP {
			result.Source = defaultOCPCatalogSource
		} else {
			result.Source = defaultCatalogSource
		}
	}

	if result.SourceNamespace == "" {
		if isOCP {
			result.SourceNamespace = defaultOCPCatalogNs
		} else {
			result.SourceNamespace = defaultCatalogNs
		}
	}

	if result.Channel == "" {
		result.Channel = "stable"
	}

	if result.InstallPlanApproval == "" {
		result.InstallPlanApproval = "Automatic"
	}

	return result
}

func isOpenShift(cluster *clusterv1.ManagedCluster) bool {
	// Primary: Check product claim (most reliable)
	// This is set automatically by klusterlet agent on the managed cluster
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == clusterClaimProduct {
			switch claim.Value {
			case productOCP, productROSA, productARO, productROKS, productOCPD:
				return true
			}
		}
	}

	// Fallback: Check labels (for edge cases where claims might not be set yet)
	if vendor, ok := cluster.Labels["vendor"]; ok && vendor == productOCP {
		return true
	}
	if product, ok := cluster.Labels["product"]; ok && product == productOCP {
		return true
	}

	return false
}

func getOperatorManifestWorkName(config meshv1alpha1.OperatorConfig) string {
	if config.Source == defaultOCPCatalogSource {
		return "multicluster-mesh-operator-ossm"
	}
	return "multicluster-mesh-operator-sail"
}

func (r *Reconciler) getClustersFromSet(ctx context.Context, clusterSetName string) ([]clusterv1.ManagedCluster, error) {
	clusterSet := &clusterv1beta2.ManagedClusterSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterSetName}, clusterSet); err != nil {
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
		"cluster.open-cluster-management.io/clusterset": clusterSetName,
	}

	if err := r.List(ctx, clusterList, labelSelector); err != nil {
		return nil, fmt.Errorf("failed to list clusters in set %s: %w", clusterSetName, err)
	}

	return clusterList.Items, nil
}

func (r *Reconciler) buildOperatorManifestWork(config meshv1alpha1.OperatorConfig, clusterName string) *workv1.ManifestWork {
	manifests := []workv1.Manifest{}

	// openshift-operators exists by default on OCP and already has a global OperatorGroup
	if config.Namespace != defaultOCPOperatorNs {
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

	packageName := operatorName
	if config.Source == defaultOCPCatalogSource {
		packageName = operatorNameOCP
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
				InstallPlanApproval:    operatorsv1alpha1.Approval(config.InstallPlanApproval),
				Package:                packageName,
				CatalogSource:          config.Source,
				CatalogSourceNamespace: config.SourceNamespace,
				StartingCSV:            config.StartingCSV,
			},
		}},
	})

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getOperatorManifestWorkName(config),
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}
