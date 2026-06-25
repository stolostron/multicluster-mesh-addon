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

Contact existing maintainers (listed in the [OWNERS](OWNERS) file)

## Pull Request Guidelines

### PR Requirements

- **Approval**: Pull requests require approval from an approver (see [OWNERS](OWNERS))
- **DCO Sign-off**: All commits must be signed off (`git commit -s`)
- **GPG Signing**: It's recommended to sign your commits (`git commit --gpg-sign`)
- **Tests**: Include unit tests for new functionality
- **Documentation**: Update docs if changing user-facing behavior

### Review Process

- Approvers will respond to pull requests promptly
- Address review feedback by pushing new commits (don't force-push during review)
- don't force-push during review at all if possible
- `okay to merge` label is required for a PR to be merged. This allows for better control in cases where multiple reviews are needed.

## Developer Certificate of Origin

You must sign off your commits to certify that you have the right to submit the code under the project's license. This is done using the Developer Certificate of Origin (DCO).

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
