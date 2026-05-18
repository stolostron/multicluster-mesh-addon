package util

import (
	"encoding/json"

	workv1 "open-cluster-management.io/api/work/v1"
)

// UnmarshalManifest extracts a manifest from ManifestWork's RawExtension.
// When reading from the API, the Object field is nil and data is in Raw bytes.
func UnmarshalManifest(manifest workv1.Manifest, into interface{}) error {
	return json.Unmarshal(manifest.RawExtension.Raw, into)
}
