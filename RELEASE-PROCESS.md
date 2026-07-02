# Release Process

This document describes how to create a new release of the multicluster-mesh-addon.

> **Note:** This process is for dev preview releases only. The release process will change for GA (General Availability) releases.

## Prerequisites

- Write access to the GitHub repository
- Access to Quay.io registry with push permissions
- Repository secrets configured:
  - `QUAY_USERNAME` - Quay.io username
  - `QUAY_PASSWORD` - Quay.io password or robot token

## Release Steps

### 1. Prepare the Release

Ensure all changes for the release are merged to `main` and the version in `Makefile` is correct for the release you want to create.

Verify the current version on `main`:

```bash
git checkout main
git pull origin main
grep "^VERSION" Makefile
```

The version should be what you want to release (e.g., `VERSION ?= 0.2.0`).

### 2. Create Release Branch

Create a release branch from `main`:

**Via Command Line:**
```bash
git checkout main
git pull origin main
git checkout -b release-0.2
git push origin release-0.2
```

**Via GitHub UI:**
1. Go to the repository on GitHub
2. Click the branch dropdown (shows "main")
3. Type the new branch name: `release-0.2`
4. Click "Create branch: release-0.2 from main"

Branch naming convention: `release-X.Y` (e.g., `release-0.1`, `release-0.2`)

### 3. Trigger Release Workflow

1. Go to **Actions** tab in GitHub
2. Select **Release** workflow
3. Click **Run workflow**
4. Configure the workflow:
   - **Branch to release from**: Select the release branch (e.g., `release-0.2`)
   - **Create as pre-release**: Check if this is a pre-release (alpha/beta/rc)
5. Click **Run workflow**

The workflow will:
1. Extract version from `chart/Chart.yaml`
2. Check if tag already exists (fails if it does)
3. Build and push container image: `quay.io/sail-dev/multicluster-mesh-addon:v0.2.0`
4. Create and push git tag: `v0.2.0`
5. Package and publish Helm chart to `gh-pages` branch
6. Create GitHub release with auto-generated notes

The workflow takes approximately 5-10 minutes to complete.

### 4. Verify Release

After the workflow completes successfully:

#### Verify Helm Chart

```bash
helm repo add multicluster-mesh-addon https://stolostron.github.io/multicluster-mesh-addon
helm repo update
helm search repo multicluster-mesh-addon --versions
```

You should see the new version listed.

#### Verify GitHub Release

1. Go to **Releases** page
2. Verify the new release appears with correct version tag
3. Check that release notes are generated
4. Verify installation instructions are present

### 5. Bump Version on Main

After the release, update the version on `main` to start the next development cycle:

```bash
git checkout main
git pull origin main
```

Update the version in `Makefile` to the next planned version (e.g., `0.3.0`):

```makefile
VERSION ?= 0.3.0
```

Regenerate and commit:

```bash
make gen
git add Makefile chart/Chart.yaml
git commit -s -m "Bump version to 0.3.0 for next development cycle"
git push origin main
```

## Branch Strategy

- **`main`**: Active development, latest unreleased code
- **`release-X.Y`**: Release branches for each minor version (e.g., `release-0.1`, `release-0.2`)
- Release branches are created from `main` when preparing a release
- Patch releases (e.g., 0.2.1, 0.2.2) are made from the same release branch
- Release branches can receive backported fixes after the initial release
