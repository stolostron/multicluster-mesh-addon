package util

import (
	"context"
	"fmt"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PlatformConfig struct {
	OperatorName      string
	OperatorNamespace string
	CatalogSource     string
	CatalogNamespace  string
}

func DetectPlatform(ctx context.Context, hubClient client.Reader, clusterName string) (PlatformConfig, error) {
	cluster := &clusterv1.ManagedCluster{}
	if err := hubClient.Get(ctx, client.ObjectKey{Name: clusterName}, cluster); err != nil {
		return PlatformConfig{}, fmt.Errorf("failed to get ManagedCluster %s: %w", clusterName, err)
	}

	if isOpenShift(cluster) {
		return PlatformConfig{
			OperatorName:      "servicemeshoperator3",
			OperatorNamespace: "sail-operator",
			CatalogSource:     "redhat-operators",
			CatalogNamespace:  "openshift-marketplace",
		}, nil
	}

	return PlatformConfig{
		OperatorName:      "sailoperator",
		OperatorNamespace: "sail-operator",
		CatalogSource:     "operatorhubio-catalog",
		CatalogNamespace:  "olm",
	}, nil
}

func isOpenShift(cluster *clusterv1.ManagedCluster) bool {
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == "product.open-cluster-management.io" {
			switch claim.Value {
			case "OpenShift", "ROSA", "ROSAHcp", "ARO", "OSD", "OpenShiftDedicated":
				return true
			}
		}
	}
	return false
}
