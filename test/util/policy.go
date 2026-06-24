package util

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stolostron/multicluster-mesh-addon/pkg/key"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetOperatorPolicyCompliance simulates the governance policy framework by setting per-cluster
// compliance status on the root Policy.
func SetOperatorPolicyCompliance(ctx context.Context, k8sClient client.Client, policyName, policyNs string, clusterCompliance map[string]policyv1.ComplianceState) {
	policy := &policyv1.Policy{}
	Eventually(func() error {
		return k8sClient.Get(ctx, key.Of(policyName, policyNs), policy)
	}).Should(Succeed())

	statuses := make([]*policyv1.CompliancePerClusterStatus, 0, len(clusterCompliance))
	for cluster, state := range clusterCompliance {
		statuses = append(statuses, &policyv1.CompliancePerClusterStatus{
			ClusterName:      cluster,
			ClusterNamespace: cluster,
			ComplianceState:  state,
		})
	}
	policy.Status.Status = statuses
	Expect(k8sClient.Status().Update(ctx, policy)).To(Succeed())
}

// SimulatePolicyFrameworkDeletion runs in the background and simulates the policy framework's
// behavior during operator removal: when the OperatorPolicy's complianceType is set to mustnothave,
// it sets the Policy status to Compliant (as if the operator was removed on all spoke clusters).
func SimulatePolicyFrameworkDeletion(ctx context.Context, k8sClient client.Client, policyName, policyNs string, clusters []string) {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				policy := &policyv1.Policy{}
				if err := k8sClient.Get(ctx, key.Of(policyName, policyNs), policy); err != nil {
					if apierrors.IsNotFound(err) {
						return
					}
					continue
				}

				if len(policy.Spec.PolicyTemplates) == 0 {
					continue
				}

				var opPolicy map[string]interface{}
				if err := json.Unmarshal(policy.Spec.PolicyTemplates[0].ObjectDefinition.Raw, &opPolicy); err != nil {
					continue
				}

				spec, _ := opPolicy["spec"].(map[string]interface{})
				if spec == nil {
					continue
				}

				ct, _ := spec["complianceType"].(string)
				if !strings.EqualFold(ct, "mustnothave") {
					continue
				}

				fresh := &policyv1.Policy{}
				if err := k8sClient.Get(ctx, key.Of(policyName, policyNs), fresh); err != nil {
					continue
				}
				statuses := make([]*policyv1.CompliancePerClusterStatus, 0, len(clusters))
				for _, cluster := range clusters {
					statuses = append(statuses, &policyv1.CompliancePerClusterStatus{
						ClusterName:      cluster,
						ClusterNamespace: cluster,
						ComplianceState:  policyv1.Compliant,
					})
				}
				fresh.Status.Status = statuses
				_ = k8sClient.Status().Update(ctx, fresh)
				return
			}
		}
	}()
}
