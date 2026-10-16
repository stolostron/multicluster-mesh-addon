# Integration Tests

Integration tests for the multicluster-mesh-addon controller using [envtest].

## Running Tests

```bash
# Run all integration tests
make test-integration

# Run specific test
KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.35 -p path)" \
  go run github.com/onsi/ginkgo/v2/ginkgo -v --tags=integration \
  --focus="Basic Reconciliation" ./test/integration/
```

## CRD Management

OCM CRDs are automatically copied from `open-cluster-management.io/api` dependency to `test/integration/crds/ocm/` by the `update-test-crds` Makefile target.

### Updating CRDs

When you update the `open-cluster-management.io/api` dependency version:

```bash
# Update dependency
go get open-cluster-management.io/api@v1.x.x
go mod tidy

# Refresh test CRDs (happens automatically during make test-integration)
make update-test-crds
```

The `update-test-crds` target will:
1. Find the OCM API module in go mod cache
2. Copy CRDs to `test/integration/crds/ocm/`
3. Include CRDs from: `cluster/v1`, `cluster/v1beta2`, `work/v1`

### Adding External CRDs

To test with external CRDs (e.g., Istio, Sail operator):

1. Create a subdirectory under `test/integration/crds/`:
   ```bash
   mkdir -p test/integration/crds/istio
   ```

2. Copy the CRD YAML files:
   ```bash
   cp /path/to/istio/crds/*.yaml test/integration/crds/istio/
   ```

3. Update suite_test.go to include the new directory:
   ```go
   CRDDirectoryPaths: []string{
       filepath.Join("..", "..", "chart", "crds"),
       filepath.Join("..", "..", "test", "integration", "crds", "ocm"),
       filepath.Join("..", "..", "test", "integration", "crds", "istio"), // Add this
   },
   ```

4. Check the CRDs into git (they won't be auto-generated)

## Test Structure

- `suite_test.go` - Test suite setup, envtest initialization
- `controller_test.go` - Controller reconciliation tests
- `doc.go` - Package documentation
- Shared helpers live in `test/util/`

## Test Coverage

### Controller Tests (`controller_test.go`)

- **Basic reconciliation**: Control plane namespace ManifestWorks, derived namespace labels, Placement not found, no clusters selected, and cleanup when membership changes
- **Validation**: Rejects mesh names that are longer than 63 characters or are not DNS labels, and rejects an empty `spec.placementRef.name`
- **Deletion**: Removes the finalizer and mesh-owned ManifestWorks
- **Certificate distribution**: cert-manager Certificates and cacerts ManifestWorks, including Ready only after the cacerts ManifestWork is Applied and Available
- **Endpoint discovery**: ManagedServiceAccounts, istio-reader RBAC, and ManifestWorkReplicaSet remote secrets

## Notes

- Tests use `//go:build integration` tag to separate from unit tests
- Each test uses unique resource names (with UnixNano timestamps) to avoid conflicts
- CRDs in `test/integration/crds/ocm/` are auto-generated from go mod dependencies

<!-- Reference links -->
[envtest]: https://book.kubebuilder.io/reference/envtest.html
