package mesh

import (
	"crypto/sha256"
	"fmt"
)

const (
	maxOULength    = 64
	maxLabelLength = 63
)

func formatOU(clusterName string) string {
	return truncateWithHash(clusterName, maxOULength)
}

func formatLabelValue(clusterName string) string {
	return truncateWithHash(clusterName, maxLabelLength)
}

// truncateWithHash returns s as-is if it fits within maxLen.
// Otherwise, it truncates to (maxLen - 9) characters and appends a dash
// followed by the first 8 hex characters of the SHA-256 hash of the full string.
func truncateWithHash(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	hash := fmt.Sprintf("%.4x", sha256.Sum256([]byte(s)))
	return s[:maxLen-9] + "-" + hash
}

func certURI(clusterName, trustDomain string) string {
	return "spiffe://" + trustDomain + "/cluster/" + clusterName + "/ca/istio-ca"
}
