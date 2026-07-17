# Contributing to Multicluster Mesh Add-on

Thank you for your interest in contributing to the OCM Service Mesh Add-on! This document provides guidelines for contributing to this project.

## Table of Contents

- [Getting Started](#getting-started)
- [Joining the Stolostron Organization](#joining-the-stolostron-organization)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Developer Certificate of Origin](#developer-certificate-of-origin)
- [Development Workflow](#development-workflow)
- [License](#license)

## Getting Started

Before contributing, please:

1. Review the [design documentation](docs/design.md) to understand the architecture
2. Read the [README.md](README.md) file for general project information.

## Joining the Stolostron Organization

To have CI run automatically on your pull requests, you need to be a member of the stolostron GitHub organization.

Contact the org [owners](https://github.com/orgs/stolostron/people?query=role%3Aowner) to request access.

## Pull Request Guidelines

### PR Requirements

- **Approval**: Pull requests require approval from an approver (see [OWNERS](OWNERS))
- **DCO Sign-off**: All commits must be signed off (`git commit -s`)
- **GPG Signing**: It's recommended to sign your commits (`git commit --gpg-sign`)
- **Tests**: Include unit tests for new functionality
- **Documentation**: Update docs if changing user-facing behavior

### Review Process

- Approvers will respond to pull requests promptly
- Address review feedback by pushing new commits
- Avoid force-pushing during review if possible. Use merge commits to resolve conflicts.
- Use the `/hold` and `/unhold` comments to control when a PR is eligible for merge. Contributors may use `/hold` to prevent a PR from merging while additional review or discussion is in progress. This is particularly useful for complex changes that require multiple approvals. If a PR is not on hold, it becomes eligible for merge after receiving a single approving review from a maintainer.

### Dependent Pull Requests

If a pull request depends on another open pull request in this repository, declare the dependency in the PR description using a full PR URL:

```text
Depends-On: https://github.com/stolostron/multicluster-mesh-addon/pull/123
```

You can list multiple dependencies, one `Depends-On:` line each. The **PR Dependencies / Check Dependencies** GitHub Action fails until every listed pull request is merged, which keeps Tide from merging the dependent PR early. Use `/hold` for other reasons to block merge (WIP, discussion); dependency gating is automatic when `Depends-On:` is present.

The check re-runs when the dependent PR is updated (new commits, editing description etc). It does **not** refresh automatically when a listed dependency merges. After dependencies land, re-run **PR Dependencies / Check Dependencies** on the waiting PR (or push a commit / edit the description) so the check can turn green.

## Developer Certificate of Origin

You must sign off your commits to certify that you have the right to submit the code under the project's license. This is done using the [Developer Certificate of Origin (DCO)](https://developercertificate.org/).

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
