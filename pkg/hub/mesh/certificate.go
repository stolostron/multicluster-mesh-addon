package mesh

import (
	"crypto/sha256"
	"fmt"
)

const maxOULength = 64

// formatOU formats a cluster name for use as the X.509 Organizational Unit (OU) field.
// If the name fits within the 64-character limit, it is returned as-is.
// Otherwise, it is truncated to 55 characters and suffixed with a dash and the
// first 8 hex characters of the SHA-256 hash of the full name (55 + 1 + 8 = 64).
func formatOU(clusterName string) string {
	if len(clusterName) <= maxOULength {
		return clusterName
	}

	return fmt.Sprintf("%.55s-%.4x", clusterName, sha256.Sum256([]byte(clusterName)))
}

func certURI(clusterName, trustDomain string) string {
	return "spiffe://" + trustDomain + "/cluster/" + clusterName + "/ca/istio-ca"
}
