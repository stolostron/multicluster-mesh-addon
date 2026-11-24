# Development

## Documentation

- [environment.md](environment.md) - Development environment setup, building, deploying, running locally
- [design.md](design.md) - Architecture, design decisions, CRD internals
- [release-process.md](release-process.md) - Creating releases

## Getting Started

1. Read the [design document](design.md) to understand the architecture and design decisions.
2. Review the [contributing guide](../../CONTRIBUTING.md) for the PR process, DCO, and team conventions.
3. Set up a [development environment](environment.md).

## Running Tests

```bash
make verify                  # gofmt, modules, generated files, vet, istio-reader RBAC drift, script syntax
make test                    # unit tests
make test-integration        # integration tests (envtest - K8s cluster API running in memory)
make test-e2e                # e2e tests (requires a running development environment)
make test-e2e-multicluster   # multi-primary e2e tests (requires make dev-env)
```

For e2e tests, the addon and prerequisites must be deployed first. `make dev-env` installs them for Kind, including the Sail operator through OperatorPolicy. `make deploy` installs only the addon, so existing clusters need the [prerequisites](../user-guide.md#prerequisites) installed separately. Both e2e targets default to `PLATFORM=kind`; use `PLATFORM=openshift` with the corresponding operator installation and kubeconfigs for OpenShift.
See [environment setup](environment.md) for options.
See [test/integration/README.md](../../test/integration/README.md) for test structure and adding CRDs.

Envtest starts the mesh controller against a real API server and etcd, but does not run the Placement scheduler, cert-manager, ManagedServiceAccount agents, OCM work/ReplicaSet controllers, or garbage collector. Tests simulate those controllers' outputs. Assertions on hub resources do not establish spoke application or remote cleanup; the e2e suites exercise those integrations.

`make verify` regenerates code, CRDs, RBAC, and chart metadata before checking them against `HEAD`; it also checks RBAC drift and demo/dev-env script syntax. `make test-integration` prepares CRDs and dependencies.

`make verify` includes `make verify-istio-reader-rbac`, which fetches the upstream
Istio Helm templates and checks the embedded `pkg/hub/mesh/manifests/istio-reader-*.yaml`
for drift.
It requires network access; override the Istio ref it compares against with
`ISTIO_READER_REF` (default: `master`), or skip it in offline environments with
`ISTIO_READER_SKIP=1`.
