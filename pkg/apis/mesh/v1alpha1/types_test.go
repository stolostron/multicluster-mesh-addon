package v1alpha1

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetControlPlaneNamespace(t *testing.T) {
	tests := []struct {
		name      string
		meshName  string
		meshNS    string
		wantExact string
	}{
		{
			name:     "normal mesh",
			meshName: "prod-mesh",
			meshNS:   "mesh-team-a",
			wantExact: "istio-system-prod-mesh-" +
				expectedHash("mesh-team-a", "prod-mesh"),
		},
		{
			name:     "same name, different namespace produces different hash",
			meshName: "prod-mesh",
			meshNS:   "mesh-team-b",
			wantExact: "istio-system-prod-mesh-" +
				expectedHash("mesh-team-b", "prod-mesh"),
		},
		{
			name:     "short name",
			meshName: "m",
			meshNS:   "ns",
			wantExact: "istio-system-m-" +
				expectedHash("ns", "m"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mesh := &MultiClusterMesh{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.meshName,
					Namespace: tc.meshNS,
				},
			}
			got := mesh.GetControlPlaneNamespace()
			if got != tc.wantExact {
				t.Errorf("GetControlPlaneNamespace() = %q, want %q", got, tc.wantExact)
			}
		})
	}
}

func TestGetControlPlaneNamespaceDeterministic(t *testing.T) {
	mesh := &MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mesh", Namespace: "my-ns"},
	}
	first := mesh.GetControlPlaneNamespace()
	for i := 0; i < 10; i++ {
		got := mesh.GetControlPlaneNamespace()
		if got != first {
			t.Fatalf("not deterministic: call 0 = %q, call %d = %q", first, i+1, got)
		}
	}
}

func TestGetControlPlaneNamespaceUniqueness(t *testing.T) {
	pairs := [][2]string{
		{"ns-a", "mesh"},
		{"ns-b", "mesh"},
		{"ns", "mesh-a"},
		{"ns", "mesh-b"},
		{"a", "b-c"},
		{"a-b", "c"},
	}

	seen := map[string]string{}
	for _, pair := range pairs {
		mesh := &MultiClusterMesh{
			ObjectMeta: metav1.ObjectMeta{Name: pair[1], Namespace: pair[0]},
		}
		ns := mesh.GetControlPlaneNamespace()
		key := pair[0] + "/" + pair[1]
		if prev, ok := seen[ns]; ok {
			t.Errorf("collision: %q and %q both produce %q", prev, key, ns)
		}
		seen[ns] = key
	}
}

func TestGetControlPlaneNamespaceLongName(t *testing.T) {
	longName := "abcdefghij-klmnopqrst-uvwxyz-0123456789-abcdefghij-more-chars"
	if len(longName) <= 39 {
		t.Fatalf("test setup: name must be > 39 chars, got %d", len(longName))
	}

	mesh := &MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: longName, Namespace: "ns"},
	}
	ns := mesh.GetControlPlaneNamespace()

	if len(ns) > 63 {
		t.Errorf("namespace %q exceeds 63 chars (len=%d)", ns, len(ns))
	}
}

func TestGetControlPlaneNamespaceKeepsHyphenAtTruncation(t *testing.T) {
	// 39th character is a hyphen: "abcdefghijklmnopqrstuvwxyz-0123456789-" + more
	name := "abcdefghijklmnopqrstuvwxyz-0123456789--extra"
	if len(name) <= 39 {
		t.Fatalf("test setup: name must be > 39 chars, got %d", len(name))
	}
	if name[38] != '-' {
		t.Fatalf("test setup: char at index 38 should be '-', got %c", name[38])
	}

	mesh := &MultiClusterMesh{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
	}
	ns := mesh.GetControlPlaneNamespace()

	want := "istio-system-" + name[:39] + "-" + expectedHash("ns", name)
	if ns != want {
		t.Errorf("GetControlPlaneNamespace() = %q, want %q", ns, want)
	}
	if len(ns) > 63 {
		t.Errorf("namespace %q exceeds 63 chars (len=%d)", ns, len(ns))
	}
	dnsLabel := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	if !dnsLabel.MatchString(ns) {
		t.Errorf("namespace %q is not a valid DNS label", ns)
	}
}

func expectedHash(namespace, name string) string {
	hash := sha256.Sum256([]byte(namespace + "/" + name))
	return hex.EncodeToString(hash[:])[:10]
}
