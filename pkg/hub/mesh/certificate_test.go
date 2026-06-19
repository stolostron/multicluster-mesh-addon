package mesh

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestFormatOU(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    string
	}{
		{
			name:        "name up to 64 characters is used as-is",
			clusterName: strings.Repeat("a", 64),
			expected:    strings.Repeat("a", 64),
		},
		{
			name:        "65 characters triggers truncation",
			clusterName: strings.Repeat("a", 65),
			expected:    strings.Repeat("a", 55) + "-" + hashPrefix(strings.Repeat("a", 65)),
		},
		{
			name:        "long production name",
			clusterName: "production-environment-kubernetes-cluster-us-east-2-deployment-pipeline-id-992384",
			expected:    "production-environment-kubernetes-cluster-us-east-2-dep-9cdc3049",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatOU(tc.clusterName)
			if got != tc.expected {
				t.Errorf("formatOU(%q) = %q, want %q", tc.clusterName, got, tc.expected)
			}
			if len(got) > maxOULength {
				t.Errorf("formatOU(%q) produced %d characters, exceeds limit of %d", tc.clusterName, len(got), maxOULength)
			}
		})
	}
}

func TestFormatLabelValue(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    string
	}{
		{
			name:        "name up to 63 characters is used as-is",
			clusterName: strings.Repeat("a", 63),
			expected:    strings.Repeat("a", 63),
		},
		{
			name:        "long CI-generated name",
			clusterName: "ci-managed-cluster-with-a-very-long-generated-name-that-exceeds-64-chars",
			expected:    "ci-managed-cluster-with-a-very-long-generated-name-tha-1d71f97d",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatLabelValue(tc.clusterName)
			if got != tc.expected {
				t.Errorf("formatLabelValue(%q) = %q, want %q", tc.clusterName, got, tc.expected)
			}
			if len(got) > maxLabelLength {
				t.Errorf("formatLabelValue(%q) produced %d characters, exceeds limit of %d", tc.clusterName, len(got), maxLabelLength)
			}
		})
	}
}

func TestCertURI(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		trustDomain string
		expected    string
	}{
		{
			name:        "short cluster name",
			clusterName: "dev-cluster",
			trustDomain: "mesh.local",
			expected:    "spiffe://mesh.local/cluster/dev-cluster/ca/istio-ca",
		},
		{
			name:        "long cluster name preserves full name",
			clusterName: "production-environment-kubernetes-cluster-us-east-2-deployment-pipeline-id-992384",
			trustDomain: "company.internal",
			expected:    "spiffe://company.internal/cluster/production-environment-kubernetes-cluster-us-east-2-deployment-pipeline-id-992384/ca/istio-ca",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := certURI(tc.clusterName, tc.trustDomain)
			if got != tc.expected {
				t.Errorf("certURI(%q, %q) = %q, want %q", tc.clusterName, tc.trustDomain, got, tc.expected)
			}
		})
	}
}

func hashPrefix(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))[:8]
}
