package mesh

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	rbacv1 "k8s.io/api/rbac/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

// embeddedIstioReaderClusterRolePath and embeddedIstioReaderClusterRoleBindingPath are
// the addon copies of Istio's reader RBAC Helm chart templates.
const (
	embeddedIstioReaderClusterRolePath        = "pkg/hub/mesh/manifests/istio-reader-clusterrole.yaml"
	embeddedIstioReaderClusterRoleBindingPath = "pkg/hub/mesh/manifests/istio-reader-clusterrolebinding.yaml"
)

// upstreamIstioReaderClusterRoleTemplate is the upstream Istio Helm chart template path
// for the embedded ClusterRole manifest.
const upstreamIstioReaderClusterRoleTemplate = "manifests/charts/istio-control/istio-discovery/templates/reader-clusterrole.yaml"

// upstreamIstioReaderTemplatesURL points at the Istio Helm chart templates the embedded
// manifests are copied from. See https://github.com/stolostron/multicluster-mesh-addon/issues/259.
const upstreamIstioReaderTemplatesURL = "https://raw.githubusercontent.com/istio/istio/%s/manifests/charts/istio-control/istio-discovery/templates/%s"

// upstreamMCSAPIGroup is Istio's default for $mcsAPIGroup (env.MCS_API_GROUP), used
// to expand the "{{ $mcsAPIGroup }}" placeholder in the upstream ClusterRole template.
const upstreamMCSAPIGroup = "multicluster.x-k8s.io"

var (
	mcsAPIGroupPattern = regexp.MustCompile(`\{\{\s*\$mcsAPIGroup\s*\}\}`)
	wholeLineTemplate  = regexp.MustCompile(`^\{\{.*\}\}$`)
	inlineTemplate     = regexp.MustCompile(`\{\{.*?\}\}`)
)

// TestIstioReaderRBACMatchesUpstream compares the addon's embedded istio-reader
// ClusterRole and ClusterRoleBinding (pkg/hub/mesh/manifests/) against Istio's
// upstream Helm chart templates, to catch any drift.
//
// It renders the upstream templates assuming the addon's effective defaults
// (global.resourceScope=all, global.enableReaderRBAC=true, istiodRemote.enabled=true,
// env.MCS_API_GROUP=multicluster.x-k8s.io) and semantically compares the ClusterRole
// rules and the ClusterRoleBinding's roleRef/subject shape. Naming (mesh-scoped
// names, MSA subject name/namespace, Helm release metadata) is intentionally not
// compared, since the addon overrides those at runtime.
//
// This testcase requires network access. Inorder to run this test, use
// `make verify-istio-reader-rbac` and to skip export ISTIO_READER_SKIP=1.
// This is automatically skipped for short tests (i.e., `go test -short`)
func TestIstioReaderRBACMatchesUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping upstream Istio RBAC drift check in short mode")
	}
	if os.Getenv("ISTIO_READER_SKIP") == "1" {
		t.Skip("ISTIO_READER_SKIP=1 is configured, skipping upstream Istio RBAC drift validation")
	}

	ref := os.Getenv("ISTIO_READER_REF")
	if ref == "" {
		ref = "master"
	}

	upstreamClusterRole := fetchUpstreamClusterRole(t, ref)
	upstreamClusterRoleBinding := fetchUpstreamClusterRoleBinding(t, ref)

	wantRules := normalizeRules(upstreamClusterRole.Rules)
	gotRules := normalizeRules(baseClusterRole.Rules)

	if diff := cmp.Diff(wantRules, gotRules); diff != "" {
		t.Errorf("%s has drifted from upstream Istio (ref %s, https://github.com/istio/istio/blob/%s/%s).\n"+
			"Update the embedded ClusterRole to match upstream. Diff (-upstream +addon):\n%s",
			embeddedIstioReaderClusterRolePath, ref, ref, upstreamIstioReaderClusterRoleTemplate, diff)
	}

	if baseClusterRoleBinding.RoleRef.APIGroup != upstreamClusterRoleBinding.RoleRef.APIGroup ||
		baseClusterRoleBinding.RoleRef.Kind != upstreamClusterRoleBinding.RoleRef.Kind {
		t.Errorf("%s roleRef has drifted from upstream Istio (ref %s): got apiGroup=%q kind=%q, want apiGroup=%q kind=%q",
			embeddedIstioReaderClusterRoleBindingPath,
			ref, baseClusterRoleBinding.RoleRef.APIGroup, baseClusterRoleBinding.RoleRef.Kind,
			upstreamClusterRoleBinding.RoleRef.APIGroup, upstreamClusterRoleBinding.RoleRef.Kind)
	}

	if len(upstreamClusterRoleBinding.Subjects) != 1 {
		t.Fatalf("expected upstream ClusterRoleBinding template (ref %s) to have exactly 1 subject, got %d",
			ref, len(upstreamClusterRoleBinding.Subjects))
	}
	if len(baseClusterRoleBinding.Subjects) != 1 {
		t.Fatalf("expected embedded ClusterRoleBinding to have exactly 1 subject, got %d",
			len(baseClusterRoleBinding.Subjects))
	}
	if baseClusterRoleBinding.Subjects[0].Kind != upstreamClusterRoleBinding.Subjects[0].Kind {
		t.Errorf("%s subject kind has drifted from upstream Istio (ref %s): got %q, want %q",
			embeddedIstioReaderClusterRoleBindingPath,
			ref, baseClusterRoleBinding.Subjects[0].Kind, upstreamClusterRoleBinding.Subjects[0].Kind)
	}
}

func fetchUpstreamClusterRole(t *testing.T, ref string) rbacv1.ClusterRole {
	t.Helper()
	raw := fetchIstioTemplate(t, ref, "reader-clusterrole.yaml")

	var cr rbacv1.ClusterRole
	if err := sigsyaml.Unmarshal(stripHelmTemplating(raw), &cr); err != nil {
		t.Fatalf("failed to parse normalized upstream ClusterRole template (ref %s): %v", ref, err)
	}
	return cr
}

func fetchUpstreamClusterRoleBinding(t *testing.T, ref string) rbacv1.ClusterRoleBinding {
	t.Helper()
	raw := fetchIstioTemplate(t, ref, "reader-clusterrolebinding.yaml")

	var crb rbacv1.ClusterRoleBinding
	if err := sigsyaml.Unmarshal(stripHelmTemplating(raw), &crb); err != nil {
		t.Fatalf("failed to parse normalized upstream ClusterRoleBinding template (ref %s): %v", ref, err)
	}
	return crb
}

// fetchIstioTemplate downloads a single Istio Helm chart template file, retrying a
// couple of times on transient transport errors from client.Get.
func fetchIstioTemplate(t *testing.T, ref, filename string) []byte {
	t.Helper()
	url := fmt.Sprintf(upstreamIstioReaderTemplatesURL, ref, filename)

	client := &http.Client{Timeout: 15 * time.Second}
	const maxAttempts = 3

	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		resp, lastErr = client.Get(url)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		t.Fatalf("failed to fetch %s after %d attempts: %v", url, maxAttempts, lastErr)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response from %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d fetching %s", resp.StatusCode, url)
	}
	return body
}

// stripHelmTemplating converts an Istio Helm chart template into plain YAML that
// approximates rendering it with the addon's effective defaults:
//   - global.resourceScope=all, global.enableReaderRBAC=true (the outer guard wrapping
//     the whole document is dropped, since it's always emitted in that configuration)
//   - istiodRemote.enabled=true (matches the extra configmap/webhook rules embedded
//     in the addon's copy)
//   - env.MCS_API_GROUP=multicluster.x-k8s.io (Istio's default)
//
// Template expressions used only for Helm release metadata or naming (revision,
// release namespace, reader service account name/namespace) are not semantically
// compared by this test, so they are stripped rather than precisely rendered; this
// can leave harmless leftovers (empty scalars, duplicate map keys resolved by
// "last value wins") in fields the test does not assert on.
func stripHelmTemplating(raw []byte) []byte {
	text := mcsAPIGroupPattern.ReplaceAllString(string(raw), upstreamMCSAPIGroup)

	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if wholeLineTemplate.MatchString(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	text = strings.Join(kept, "\n")

	return []byte(inlineTemplate.ReplaceAllString(text, ""))
}

// normalizeRules sorts each rule's permission fields, then sorts the rules themselves,
// so that insignificant reordering (upstream vs. the addon's copy, or a future upstream
// reshuffle) does not register as drift.
func normalizeRules(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	out := make([]rbacv1.PolicyRule, len(rules))
	for i, r := range rules {
		apiGroups := append([]string(nil), r.APIGroups...)
		resources := append([]string(nil), r.Resources...)
		verbs := append([]string(nil), r.Verbs...)
		resourceNames := append([]string(nil), r.ResourceNames...)
		nonResourceURLs := append([]string(nil), r.NonResourceURLs...)
		sort.Strings(apiGroups)
		sort.Strings(resources)
		sort.Strings(verbs)
		sort.Strings(resourceNames)
		sort.Strings(nonResourceURLs)
		out[i] = rbacv1.PolicyRule{
			APIGroups:       apiGroups,
			Resources:       resources,
			Verbs:           verbs,
			ResourceNames:   resourceNames,
			NonResourceURLs: nonResourceURLs,
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return ruleSortKey(out[i]) < ruleSortKey(out[j])
	})
	return out
}

func ruleSortKey(r rbacv1.PolicyRule) string {
	return strings.Join(r.APIGroups, ",") + "|" +
		strings.Join(r.Resources, ",") + "|" +
		strings.Join(r.Verbs, ",") + "|" +
		strings.Join(r.ResourceNames, ",") + "|" +
		strings.Join(r.NonResourceURLs, ",")
}
