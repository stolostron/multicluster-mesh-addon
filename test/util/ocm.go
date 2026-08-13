package util

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateManagedClusterSet creates a ManagedClusterSet.
func CreateManagedClusterSet(ctx context.Context, k8sClient client.Client, name string) {
	Expect(k8sClient.Create(ctx, &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			},
		},
	})).To(Succeed())
}

// CreateManagedCluster creates a ManagedCluster and its namespace (required for ManifestWorks).
func CreateManagedCluster(ctx context.Context, k8sClient client.Client, name, clusterSet string) {
	CreateNamespace(ctx, k8sClient, name)
	Expect(k8sClient.Create(ctx, &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"cluster.open-cluster-management.io/clusterset": clusterSet,
			},
		},
		Spec: clusterv1.ManagedClusterSpec{
			ManagedClusterClientConfigs: []clusterv1.ClientConfig{{URL: "https://" + name + ":6443"}},
		},
	})).To(Succeed())
}

// SetMsaStatus updates a ManagedServiceAccount's status TokenSecretRef,
// simulating what the ManagedServiceAccount controller does
func SetMsaStatus(ctx context.Context, k8sClient client.Client, msaName, clusterName string, testDuration time.Duration) {
	msa := &msav1beta1.ManagedServiceAccount{}
	Expect(k8sClient.Get(ctx, key.Of(msaName, clusterName), msa)).To(Succeed())
	msa.Status = msav1beta1.ManagedServiceAccountStatus{
		TokenSecretRef: &msav1beta1.SecretRef{
			Name:                 msaName,
			LastRefreshTimestamp: metav1.NewTime(metav1.Now().Add(testDuration)),
		},
	}
	Expect(k8sClient.Status().Update(ctx, msa)).To(Succeed())
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
