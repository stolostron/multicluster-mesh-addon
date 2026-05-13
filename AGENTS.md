# Agent Instructions

## Project Overview

OCM Service Mesh Add-on - a Kubernetes controller that orchestrates multi-cluster Istio service mesh deployments via [Open Cluster Management (OCM)](https://open-cluster-management.io/).

The controller manages the `MultiClusterMesh` custom resource on the hub cluster and automates:
- **Operator Lifecycle**: Installing Sail/OSSM operator on managed clusters via ManifestWork
- **Trust Distribution**: Distributing intermediate CA certificates via cert-manager (in progress)
- **Endpoint Discovery**: Exchanging discovery secrets via ManagedServiceAccount (in progress)

## Project Structure

- `main.go` - Entry point, scheme registration
- `pkg/apis/mesh/v1alpha1/` - CRD type definitions (`MultiClusterMesh`)
- `pkg/hub/mesh/controller.go` - Main reconciliation controller
- `pkg/version/` - Build version info
- `config/crd/` - Generated CRD manifests
- `test/integration/` - Ginkgo/Gomega integration tests using envtest
- `test/util/` - Shared test helpers (k8s resource creation/deletion, mesh helpers, OCM helpers)
- `tools/` - Tool dependencies (golangci-lint, ginkgo, etc.)

## Build & Test

- `make build` - Build the binary
- `make verify` - Run all checks (gofmt, modules, vet) - run after every change
- `make test` - Run unit tests
- `make test-integration` - Run integration tests (requires envtest)
- `make gen` - Regenerate deepcopy code and CRDs after changing types.go

## Key Patterns

- Controller uses `controller-runtime` with `client.Client` from the manager
- Watches: `MultiClusterMesh` (primary), `ManagedCluster`, `ManagedClusterSet`
- Field index on `spec.clusterSet` for efficient mesh lookups in mapper functions
- Operator ManifestWorks are addon-owned (labeled `app.kubernetes.io/managed-by: multicluster-mesh-addon`), not mesh-owned
- Cleanup uses `getClustersNeededByAnyMesh()` to check if any active mesh still needs a cluster before deleting its operator ManifestWork
- Integration tests use envtest with Ginkgo/Gomega; test helpers are in `test/util/`

## Common Operations

### Modifying the API (types.go)

1. Edit `pkg/apis/mesh/v1alpha1/types.go`
2. Run `make gen` to regenerate deepcopy code and CRD manifests
3. Update the controller logic in `pkg/hub/mesh/controller.go`
4. Add/update integration tests
5. Run `make verify && make test-integration`

### Adding a New Watch

1. Add the watch in `RegisterController` (use `handler.EnqueueRequestsFromMapFunc` with a mapper function)
2. Add the mapper function (follow `findMeshesForCluster` / `findMeshesForClusterSet` patterns)
3. Consider adding predicates to filter irrelevant events (e.g., label-based filtering)
4. Consider adding field indexes if the mapper needs to look up resources by a spec field
5. Add integration tests that verify the watch triggers reconciliation

## Conventions

- Follow existing code patterns - check how similar functionality is implemented before adding new code
- No comments unless the WHY is non-obvious
- Default log level for operational messages is `klog.Infof`; use `klog.V(4).Infof` for debug/verbose messages
- Error messages in `fmt.Errorf` should start lowercase and describe what failed: `"failed to X: %w"`
- Log messages at `klog.Errorf` should start with capital: `"Failed to X: %v"`
- Test helpers that create resources go in `test/util/`; test-local helpers stay in the test file
- Always run `make verify` and `make test-integration` after changes
- Check for typos in all changed lines before committing
- When you see code repeated more than twice, suggest extracting it into a shared function
- Never commit secrets or credentials
- Sign commits with `-s` flag
- Never amend or rewrite existing commits unless explicitly asked to do so

## OCM Concepts

- **ManagedCluster**: A cluster registered with the hub
- **ManagedClusterSet**: A group of ManagedClusters (uses `ExclusiveClusterSetLabel` selector - one cluster per set)
- **ManifestWork**: Resources to be applied on a managed cluster (namespace = cluster name)
- **ManagedServiceAccount**: Projects a service account identity from hub to spoke
- Product claims (`product.open-cluster-management.io`) identify cluster platform (OpenShift, ROSA, ARO, etc.)
