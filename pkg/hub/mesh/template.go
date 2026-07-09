package mesh

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	stdssh "golang.org/x/crypto/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	gittransport "github.com/go-git/go-git/v5/plumbing/transport"
)

const (
	defaultConfigMapKey = "istio.yaml"

	istioAPIVersion = "sailoperator.io/v1"
	istioKind       = "Istio"
)

const gitCacheTTL = 5 * time.Minute

type templateCacheEntry struct {
	commitSHA  string
	fetchedAt  time.Time
	template   map[string]any
}

// templateCache provides in-memory caching for git-sourced templates to avoid
// redundant clones on repeated reconciles when the remote hasn't changed.
type templateCache struct {
	mu      sync.Mutex
	entries map[string]*templateCacheEntry
}

func newTemplateCache() *templateCache {
	return &templateCache{entries: make(map[string]*templateCacheEntry)}
}

func (c *templateCache) get(key string) (*templateCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Since(entry.fetchedAt) > gitCacheTTL {
		return nil, false
	}
	return entry, true
}

func (c *templateCache) set(key string, entry *templateCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry

	for k, e := range c.entries {
		if time.Since(e.fetchedAt) > gitCacheTTL {
			delete(c.entries, k)
		}
	}
}

func gitCacheKey(url, path string, ref *meshv1alpha1.GitRef) string {
	refStr := "main"
	if ref != nil {
		if ref.Commit != "" {
			refStr = "commit:" + ref.Commit
		} else if ref.Tag != "" {
			refStr = "tag:" + ref.Tag
		} else if ref.Branch != "" {
			refStr = "branch:" + ref.Branch
		}
	}
	return url + "|" + refStr + "|" + path
}

// TemplateSourceError wraps errors from template resolution so the
// Reconcile method can set ReasonTemplateSourceUnavailable instead
// of the generic ReasonReconcileError.
type TemplateSourceError struct {
	Err error
}

func (e *TemplateSourceError) Error() string { return e.Err.Error() }
func (e *TemplateSourceError) Unwrap() error { return e.Err }

// resolveIstioCRTemplate dispatches to the appropriate template source resolver.
// Returns nil when None mode is active (caller should skip mesh resource creation).
func (r *Reconciler) resolveIstioCRTemplate(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (map[string]any, error) {
	ts := mesh.Spec.ControlPlane.TemplateSource
	if ts == nil {
		return buildBasicTemplate(), nil
	}

	if ts.Basic != nil {
		return buildBasicTemplate(), nil
	}

	if ts.None != nil {
		return nil, nil
	}

	if ts.ConfigMapRef != nil {
		tmpl, err := r.resolveConfigMapTemplate(ctx, mesh)
		if err != nil {
			return nil, &TemplateSourceError{Err: err}
		}
		return tmpl, nil
	}

	if ts.Git != nil {
		tmpl, err := r.resolveGitTemplate(ctx, mesh)
		if err != nil {
			return nil, &TemplateSourceError{Err: err}
		}
		return tmpl, nil
	}

	return buildBasicTemplate(), nil
}

// buildBasicTemplate returns the hardcoded Istio CR template that matches
// the original buildIstioCR output. Suitable for demos and quick starts.
func buildBasicTemplate() map[string]any {
	return map[string]any{
		"apiVersion": istioAPIVersion,
		"kind":       istioKind,
		"spec": map[string]any{
			"values": map[string]any{
				"meshConfig": map[string]any{
					"defaultConfig": map[string]any{
						"proxyMetadata": map[string]any{
							"ISTIO_META_DNS_AUTO_ALLOCATE": "true",
							"ISTIO_META_DNS_CAPTURE":       "true",
						},
					},
				},
			},
		},
	}
}

// resolveConfigMapTemplate reads the Istio CR template from a ConfigMap
// in the same namespace as the mesh.
func (r *Reconciler) resolveConfigMapTemplate(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (map[string]any, error) {
	ref := mesh.Spec.ControlPlane.TemplateSource.ConfigMapRef

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: mesh.Namespace, Name: ref.Name}, cm); err != nil {
		return nil, fmt.Errorf("failed to get template ConfigMap %s/%s: %w", mesh.Namespace, ref.Name, err)
	}

	key := ref.Key
	if key == "" {
		key = defaultConfigMapKey
	}

	data, ok := cm.Data[key]
	if !ok {
		return nil, fmt.Errorf("template ConfigMap %s/%s has no key %q", mesh.Namespace, ref.Name, key)
	}

	template, err := parseTemplate(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template from ConfigMap %s/%s key %q: %w", mesh.Namespace, ref.Name, key, err)
	}

	if err := validateTemplate(template); err != nil {
		return nil, fmt.Errorf("invalid template in ConfigMap %s/%s key %q: %w", mesh.Namespace, ref.Name, key, err)
	}

	return template, nil
}

const gitCloneTimeout = 30 * time.Second

// resolveGitTemplate pulls the Istio CR template from a git repository.
// Uses an in-memory cache (5-minute TTL) to avoid redundant clones on repeated
// reconciles. For branch/tag refs, checks the remote HEAD via ls-remote before
// cloning. For pinned commit refs, compares directly against the cached SHA.
//
// Known limitation: pinned commit refs require a full-history clone (the git
// protocol cannot shallow-clone to an arbitrary SHA). For large repos, this
// means the entire history is held in process memory until GC. Branch/tag refs
// use Depth:1 shallow clones and are not affected.
func (r *Reconciler) resolveGitTemplate(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh) (map[string]any, error) {
	source := mesh.Spec.ControlPlane.TemplateSource.Git
	cacheKey := gitCacheKey(source.URL, source.Path, source.Ref)
	isCommitSHA := source.Ref != nil && source.Ref.Commit != ""

	// Fast path: return cached template within TTL without any network calls.
	// For pinned commit refs, also verify the SHA hasn't changed (spec update).
	if cached, ok := r.templateCache.get(cacheKey); ok {
		if !isCommitSHA || cached.commitSHA == source.Ref.Commit {
			klog.V(4).Infof("Git template cache hit for %s (within TTL)", source.URL)
			return cached.template, nil
		}
	}

	// Cache miss or expired — need network access, so resolve auth now.
	auth, err := r.gitAuthFromSecret(ctx, mesh.Namespace, source.SecretRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve git credentials: %w", err)
	}

	template, commitSHA, err := r.cloneAndReadTemplate(ctx, source, auth)
	if err != nil {
		return nil, err
	}

	r.templateCache.set(cacheKey, &templateCacheEntry{
		commitSHA: commitSHA,
		fetchedAt: time.Now(),
		template:  template,
	})

	return template, nil
}

// cloneAndReadTemplate performs the actual git clone, reads the template file,
// and returns the parsed template along with the resolved commit SHA.
func (r *Reconciler) cloneAndReadTemplate(ctx context.Context, source *meshv1alpha1.GitTemplateSource, auth gittransport.AuthMethod) (map[string]any, string, error) {
	cloneCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()

	cloneOpts := &git.CloneOptions{
		Auth: auth,
		URL:  source.URL,
	}

	isCommitSHA := source.Ref != nil && source.Ref.Commit != ""

	if !isCommitSHA {
		cloneOpts.Depth = 1
		cloneOpts.ReferenceName = resolveGitRef(source.Ref)
		cloneOpts.SingleBranch = true
	}

	repo, err := git.CloneContext(cloneCtx, memory.NewStorage(), memfs.New(), cloneOpts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to clone git repo %s: %w", source.URL, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get worktree: %w", err)
	}

	if isCommitSHA {
		if err := wt.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash(source.Ref.Commit),
		}); err != nil {
			return nil, "", fmt.Errorf("failed to checkout commit %s: %w", source.Ref.Commit, err)
		}
	}

	head, err := repo.Head()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	commitSHA := head.Hash().String()

	f, err := wt.Filesystem.Open(source.Path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file %s in git repo %s: %w", source.Path, source.URL, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file %s: %w", source.Path, err)
	}

	template, err := parseTemplate(string(data))
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse template from git repo %s path %s: %w", source.URL, source.Path, err)
	}

	if err := validateTemplate(template); err != nil {
		return nil, "", fmt.Errorf("invalid template from git repo %s path %s: %w", source.URL, source.Path, err)
	}

	klog.Infof("Cloned git template from %s (commit %s)", source.URL, commitSHA)
	return template, commitSHA, nil
}

// resolveGitRef converts a GitRef to a plumbing.ReferenceName for branch/tag cloning.
// Not used for commit-SHA refs (those use a full clone + checkout instead).
func resolveGitRef(ref *meshv1alpha1.GitRef) plumbing.ReferenceName {
	if ref == nil {
		return plumbing.NewBranchReferenceName("main")
	}
	if ref.Tag != "" {
		return plumbing.NewTagReferenceName(ref.Tag)
	}
	if ref.Branch != "" {
		return plumbing.NewBranchReferenceName(ref.Branch)
	}
	return plumbing.NewBranchReferenceName("main")
}

// gitAuthFromSecret builds a go-git AuthMethod from a Secret. Supports HTTPS
// (username/password or token) and SSH (ssh-privatekey with optional known_hosts
// for host key verification). Returns nil for public repos (no secretRef).
func (r *Reconciler) gitAuthFromSecret(ctx context.Context, namespace string, secretRef *meshv1alpha1.SecretRef) (gittransport.AuthMethod, error) {
	if secretRef == nil {
		return nil, nil
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretRef.Name}, secret); err != nil {
		return nil, fmt.Errorf("failed to get git credentials Secret %s/%s: %w", namespace, secretRef.Name, err)
	}

	if sshKey, ok := secret.Data["ssh-privatekey"]; ok {
		publicKeys, err := gitssh.NewPublicKeys("git", sshKey, "")
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
		}
		if knownHosts, ok := secret.Data["known_hosts"]; ok {
			tmpFile, err := os.CreateTemp("", "known_hosts-*")
			if err != nil {
				return nil, fmt.Errorf("failed to create temp known_hosts file: %w", err)
			}
			defer os.Remove(tmpFile.Name())
			if _, err := tmpFile.Write(knownHosts); err != nil {
				tmpFile.Close()
				return nil, fmt.Errorf("failed to write known_hosts: %w", err)
			}
			tmpFile.Close()
			hostKeyCallback, err := gitssh.NewKnownHostsCallback(tmpFile.Name())
			if err != nil {
				return nil, fmt.Errorf("failed to parse known_hosts: %w", err)
			}
			publicKeys.HostKeyCallback = hostKeyCallback
		} else {
			klog.Warningf("Git Secret %s/%s has ssh-privatekey but no known_hosts — SSH host key verification is disabled (MITM risk)", namespace, secretRef.Name)
			publicKeys.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
		}
		return publicKeys, nil
	}

	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	if password == "" {
		password = string(secret.Data["token"])
	}
	if username == "" && password == "" {
		return nil, fmt.Errorf("git credentials Secret %s/%s has no recognized auth fields (username/password, token, or ssh-privatekey)", namespace, secretRef.Name)
	}
	if username == "" {
		username = "git"
	}

	return &githttp.BasicAuth{Username: username, Password: password}, nil
}

// parseTemplate unmarshals YAML data into a map, rejecting empty content.
func parseTemplate(data string) (map[string]any, error) {
	var result map[string]any
	if err := yaml.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("template is empty")
	}
	return result, nil
}

// validateTemplate checks that the parsed template is a sailoperator.io/v1 Istio CR.
func validateTemplate(template map[string]any) error {
	apiVersion, _ := template["apiVersion"].(string)
	kind, _ := template["kind"].(string)

	if apiVersion == "" {
		return fmt.Errorf("template missing apiVersion")
	}
	if kind == "" {
		return fmt.Errorf("template missing kind")
	}
	if apiVersion != istioAPIVersion {
		return fmt.Errorf("template apiVersion is %q, expected %s", apiVersion, istioAPIVersion)
	}
	if kind != istioKind {
		return fmt.Errorf("template kind is %q, expected %s", kind, istioKind)
	}
	return nil
}

// applyControllerManagedFields deep-merges the controller's required fields
// onto the user-provided template. Controller fields always win.
func (r *Reconciler) applyControllerManagedFields(ctx context.Context, template map[string]any, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster, cluster *clusterv1.ManagedCluster) (*unstructured.Unstructured, error) {
	clusterName := cluster.Name
	isPrimaryRemote := mesh.Spec.Topology.Type == meshv1alpha1.TopologyPrimaryRemote
	primary := getPrimaryCluster(mesh, clusters)
	isPrimary := primary != nil && primary.Name == clusterName

	spec := ensureMap(template, "spec")

	spec["namespace"] = mesh.GetControlPlaneNamespace()
	if mesh.Spec.ControlPlane.Version != "" {
		spec["version"] = mesh.Spec.ControlPlane.Version
	}

	if isPrimaryRemote && !isPrimary {
		spec["profile"] = "remote"
	}

	values := ensureMap(spec, "values")
	global := ensureMap(values, "global")

	global["meshID"] = getMeshID(mesh)
	global["network"] = getNetworkID(clusterName)

	multiCluster := ensureMap(global, "multiCluster")
	multiCluster["clusterName"] = clusterName

	meshConfig := ensureMap(values, "meshConfig")
	meshConfig["trustDomain"] = mesh.Name

	if isPrimaryRemote && isPrimary {
		global["externalIstiod"] = true
	}

	if isPrimaryRemote && !isPrimary {
		addr, err := r.getGatewayAddressForCluster(ctx, mesh, primary)
		if err != nil {
			return nil, fmt.Errorf("failed to get gateway address for primary cluster %s: %w", primary.Name, err)
		}
		if addr != "" {
			global["remotePilotAddress"] = addr
		}
	}

	template["apiVersion"] = istioAPIVersion
	template["kind"] = istioKind
	metadata := ensureMap(template, "metadata")
	metadata["name"] = getIstioCRName(mesh)

	return &unstructured.Unstructured{Object: template}, nil
}

// copyMap creates a deep copy of a map[string]any, recursively copying nested maps.
func copyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if nested, ok := v.(map[string]any); ok {
			dst[k] = copyMap(nested)
		} else {
			dst[k] = v
		}
	}
	return dst
}

// ensureMap ensures a nested map exists at the given key, creating it if needed.
// Logs a warning if an existing non-map value is replaced.
func ensureMap(parent map[string]any, key string) map[string]any {
	if v, ok := parent[key].(map[string]any); ok {
		return v
	}
	if _, exists := parent[key]; exists {
		klog.V(4).Infof("Replacing non-map value at key %q with map", key)
	}
	m := make(map[string]any)
	parent[key] = m
	return m
}
