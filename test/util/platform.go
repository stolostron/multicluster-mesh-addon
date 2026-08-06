package util

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PlatformConfig struct {
	OperatorName      string
	OperatorNamespace string
	CatalogSource     string
	CatalogNamespace  string
}

func DetectPlatform(ctx context.Context, c client.Reader) (PlatformConfig, error) {
	ns := &corev1.Namespace{}
	err := c.Get(ctx, client.ObjectKey{Name: "openshift-marketplace"}, ns)
	if err == nil {
		return PlatformConfig{
			OperatorName:      "servicemeshoperator3",
			OperatorNamespace: "openshift-operators",
			CatalogSource:     "redhat-operators",
			CatalogNamespace:  "openshift-marketplace",
		}, nil
	}
	if apierrors.IsNotFound(err) {
		return PlatformConfig{
			OperatorName:      "sailoperator",
			OperatorNamespace: "sail-operator",
			CatalogSource:     "operatorhubio-catalog",
			CatalogNamespace:  "olm",
		}, nil
	}
	return PlatformConfig{}, fmt.Errorf("failed to detect platform: %w", err)
}
