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
make verify              # gofmt, modules, vet
make test                # unit tests
make test-integration    # integration tests (envtest - K8s cluster API running in memory)
make test-e2e            # e2e tests (requires a running development environment)
```

For e2e tests, the addon must be deployed first (`make dev-env` or `make deploy`).
See [environment setup](environment.md) for options.
See [test/integration/README.md](../../test/integration/README.md) for test structure and adding CRDs.
