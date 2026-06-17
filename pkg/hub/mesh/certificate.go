package mesh

import (
	"crypto/sha256"
	"fmt"
)

const maxOULength = 63

// formatOU formats a cluster name for use as the X.509 Organizational Unit (OU) field.
// If the name fits within the 63-character limit, it is returned as-is.
// Otherwise, it is truncated to 54 characters and suffixed with a dash and the
// first 8 hex characters of the SHA-256 hash of the full name (54 + 1 + 8 = 63).
func formatOU(clusterName string) string {
	if len(clusterName) <= maxOULength {
		return clusterName
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(clusterName)))
	return clusterName[:54] + "-" + hash[:8]
}

func certURI(clusterName, trustDomain string) string {
	return "spiffe://" + trustDomain + "/cluster/" + clusterName + "/ca/istio-ca"
}
