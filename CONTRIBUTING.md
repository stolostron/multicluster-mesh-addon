# Contributing to Multicluster Mesh Add-on

Guidelines for contributing to the OCM Service Mesh Add-on.

## Table of Contents

- [Getting Started](#getting-started)
- [Joining the Stolostron Organization](#joining-the-stolostron-organization)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Keeping Docs in Sync](#keeping-docs-in-sync)
- [Developer Certificate of Origin](#developer-certificate-of-origin)
- [Development Workflow](#development-workflow)
- [License](#license)

## Getting Started

Before contributing, please:

1. Read the [development docs](docs/dev/README.md) for setup, building, testing, and architecture
2. Review the [design document](docs/dev/design.md) for architecture and design decisions

## Joining the Stolostron Organization

To have CI run automatically on your pull requests, you need to be a member of the stolostron GitHub organization.

Contact the org [owners] to request access.

## Pull Request Guidelines

### PR Requirements

- **Approval**: Pull requests require approval from an approver (see [OWNERS](OWNERS))
- **DCO Sign-off**: All commits must be signed off (`git commit -s`)
- **GPG Signing**: It's recommended to sign your commits (`git commit --gpg-sign`)
- **Tests**: Include unit tests for new functionality
- **Documentation**: Update docs if changing user-facing behavior (see [Keeping Docs in Sync](#keeping-docs-in-sync))

### Review Process

- Approvers will respond to pull requests promptly
- Address review feedback by pushing new commits
- Avoid force-pushing during review if possible.
  Use merge commits to resolve conflicts.
- Use `/hold` to prevent a PR from merging while discussion is in progress.
  Use `/unhold` when it's ready.
  Without a hold, a PR becomes eligible for merge after a single approving review from a maintainer.

### Dependent Pull Requests

If a pull request depends on another open pull request in this repository, declare the dependency in the PR description with a full PR URL (short forms like `#123` are not supported):

```text
Depends-On: https://github.com/stolostron/multicluster-mesh-addon/pull/123
```

You can list multiple dependencies, one `Depends-On:` line each.
The **PR Dependencies / Check Dependencies** GitHub Action blocks merge until every listed PR is merged.

The check does not refresh automatically when a dependency merges.
After the parent PR lands, merge or rebase `main` into the dependent PR to re-run the check.

## Keeping Docs in Sync

When your change touches any of the following, update the corresponding docs in the same PR:

| What changed | Update |
|---|---|
| CRD fields, defaults, or validation (`types.go`) | [docs/api-reference.md](docs/api-reference.md) |
| Status conditions or reason constants (`types.go`) | [docs/api-reference.md](docs/api-reference.md), [docs/troubleshooting.md](docs/troubleshooting.md) |
| Controller behavior, lifecycle, or collision handling | [docs/architecture.md](docs/architecture.md) |
| Adding or removing sample manifests | [docs/api-reference.md](docs/api-reference.md), [docs/user-guide.md](docs/user-guide.md) |
| New prerequisites or user-facing features | [docs/user-guide.md](docs/user-guide.md), [README.md](README.md) |
| Helm chart resource names or labels (`chart/templates/`) | [docs/troubleshooting.md](docs/troubleshooting.md), [chart/README.md](chart/README.md) |
| Make targets or dev-env flow | [docs/dev/environment.md](docs/dev/environment.md) |
| Release process | [docs/dev/release-process.md](docs/dev/release-process.md) |

After running `make gen`, check whether the generated CRD in `chart/crds/` still matches the field table in `docs/api-reference.md`.

## Developer Certificate of Origin

Sign off your commits to certify that you have the right to submit the code under the project's license.
See [Developer Certificate of Origin (DCO)][DCO].

## Development Workflow

1. **Fork the repository** to your GitHub account
2. **Clone your fork** locally
3. **Create a branch** for your changes
   ```bash
   git checkout -b feature/my-feature
   ```
4. **Make your changes** following project conventions
5. **Run verification and tests**
   ```bash
   make verify && make test && make test-integration
   make dev-env && make test-e2e && make dev-clean # optional
   ```
6. **Commit with sign-off**
   ```bash
   git commit -s -m "Your commit message"
   ```
7. **Push to your fork**
   ```bash
   git push origin feature/my-feature
   ```
8. **Open a pull request** from your fork to the main repository

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

<!-- Reference links -->
[DCO]: https://developercertificate.org/
[owners]: https://github.com/orgs/stolostron/people?query=role%3Aowner
