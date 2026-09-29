package util

import (
	"context"

	. "github.com/onsi/gomega"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreatePlacement creates a Placement in the given namespace.
func CreatePlacement(ctx context.Context, k8sClient client.Client, name, namespace string) {
	Expect(k8sClient.Create(ctx, &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})).To(Succeed())
}

// CreatePlacementDecision creates a PlacementDecision for the given Placement and
// updates its status with the specified cluster decisions. Also updates the
// Placement's NumberOfSelectedClusters to match, simulating what the real
// Placement controller does.
func CreatePlacementDecision(ctx context.Context, k8sClient client.Client, placementName, namespace string, clusterNames ...string) {
	pd := &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      placementName + "-decision-1",
			Namespace: namespace,
			Labels: map[string]string{
				"cluster.open-cluster-management.io/placement": placementName,
			},
		},
	}
	Expect(k8sClient.Create(ctx, pd)).To(Succeed())

	decisions := make([]clusterv1beta1.ClusterDecision, len(clusterNames))
	for i, name := range clusterNames {
		decisions[i] = clusterv1beta1.ClusterDecision{ClusterName: name}
	}
	pd.Status.Decisions = decisions
	Expect(k8sClient.Status().Update(ctx, pd)).To(Succeed())

	updatePlacementSelectedCount(ctx, k8sClient, placementName, namespace, int32(len(clusterNames)))
}

// UpdatePlacementDecision updates an existing PlacementDecision's status with new cluster decisions.
// Also updates the Placement's NumberOfSelectedClusters to match.
func UpdatePlacementDecision(ctx context.Context, k8sClient client.Client, placementName, namespace string, clusterNames ...string) {
	pd := &clusterv1beta1.PlacementDecision{}
	Expect(k8sClient.Get(ctx, key.Of(placementName+"-decision-1", namespace), pd)).To(Succeed())

	decisions := make([]clusterv1beta1.ClusterDecision, len(clusterNames))
	for i, name := range clusterNames {
		decisions[i] = clusterv1beta1.ClusterDecision{ClusterName: name}
	}
	pd.Status.Decisions = decisions
	Expect(k8sClient.Status().Update(ctx, pd)).To(Succeed())

	updatePlacementSelectedCount(ctx, k8sClient, placementName, namespace, int32(len(clusterNames)))
}

func updatePlacementSelectedCount(ctx context.Context, k8sClient client.Client, name, namespace string, count int32) {
	placement := &clusterv1beta1.Placement{}
	Expect(k8sClient.Get(ctx, key.Of(name, namespace), placement)).To(Succeed())
	placement.Status.NumberOfSelectedClusters = count
	Expect(k8sClient.Status().Update(ctx, placement)).To(Succeed())
}

// CreateManagedClusterSetBinding binds a ManagedClusterSet to a namespace,
// allowing Placements in that namespace to select clusters from the set.
func CreateManagedClusterSetBinding(ctx context.Context, k8sClient client.Client, clusterSetName, namespace string) {
	Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &clusterv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterSetName, Namespace: namespace},
		Spec:       clusterv1beta2.ManagedClusterSetBindingSpec{ClusterSet: clusterSetName},
	}))).To(Succeed())
}

// CreatePlacementForClusterSet creates a Placement that selects clusters from the given ClusterSet.
// In an environment with the ACM Placement controller running (e2e), this will
// automatically produce PlacementDecision resources.
func CreatePlacementForClusterSet(ctx context.Context, k8sClient client.Client, name, namespace, clusterSetName string) {
	Expect(k8sClient.Create(ctx, &clusterv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: clusterv1beta1.PlacementSpec{
			ClusterSets: []string{clusterSetName},
		},
	})).To(Succeed())
}

// CreateManagedCluster creates a ManagedCluster and its namespace (required for ManifestWorks).
func CreateManagedCluster(ctx context.Context, k8sClient client.Client, name string) {
	CreateNamespace(ctx, k8sClient, name)
	Expect(k8sClient.Create(ctx, &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1.ManagedClusterSpec{
			ManagedClusterClientConfigs: []clusterv1.ClientConfig{{URL: "https://" + name + ":6443"}},
		},
	})).To(Succeed())
}

// SetManifestWorkFeedback updates a ManifestWork's status to include a string feedback value,
// simulating what the OCM work agent does on a real spoke cluster.
func SetManifestWorkFeedback(ctx context.Context, k8sClient client.Client, workName, namespace, feedbackName, feedbackValue string) {
	work := &workv1.ManifestWork{}
	Expect(k8sClient.Get(ctx, key.Of(workName, namespace), work)).To(Succeed())
	work.Status.ResourceStatus = workv1.ManifestResourceStatus{
		Manifests: []workv1.ManifestCondition{{
			Conditions: []metav1.Condition{{
				Type:               workv1.ManifestApplied,
				Status:             metav1.ConditionTrue,
				Reason:             "Applied",
				LastTransitionTime: metav1.Now(),
			}},
			StatusFeedbacks: workv1.StatusFeedbackResult{
				Values: []workv1.FeedbackValue{{
					Name: feedbackName,
					Value: workv1.FieldValue{
						Type:   workv1.String,
						String: &feedbackValue,
					},
				}},
			},
		}},
	}
	Expect(k8sClient.Status().Update(ctx, work)).To(Succeed())
}
