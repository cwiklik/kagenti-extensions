# Deployment Guide for ToolHive Webhook

This repository includes automated CI/CD workflows using GitHub Actions and GoReleaser to build, test, and release the ToolHive webhook.

## Overview

The deployment process consists of:

1. **CI Checks** - Automated on every pull request
2. **Release Process** - Automated on git tag pushes
3. **Docker Images** - Multi-architecture container builds
4. **Helm Charts** - Published to GitHub Container Registry

## CI/CD Workflows

### CI Workflow

**File:** [`.github/workflows/ci.yaml`](.github/workflows/ci.yaml:1)

Runs on every pull request to `main` or `release-*` branches:

- **Linting**: Runs `go fmt` on all Go modules
- **Static Analysis**: Runs `go vet` for code quality checks
- **Build**: Compiles all Go modules to verify no build errors

### Release Workflow

**File:** [`.github/workflows/goreleaser.yml`](.github/workflows/goreleaser.yml:1)

Triggered when you push a tag starting with `v` (e.g., `v1.0.0`, `v0.1.2`):

1. Builds multi-architecture binaries (Linux/Darwin, amd64/arm64)
2. Creates Docker images for `amd64` and `arm64`
3. Pushes images to `ghcr.io/kagenti/kagenti-extensions/toolhive-webhook`
4. Packages and publishes Helm chart
5. Creates GitHub release with changelog

## How to Create a Release

### 1. Prepare Your Release

Ensure all changes are merged to the `main` branch and tests pass.

### 2. Create and Push a Tag

```bash
# Tag format: v{MAJOR}.{MINOR}.{PATCH}
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### 3. Monitor the Release

1. Go to the **Actions** tab in GitHub
2. Watch the `goreleaser` workflow execute
3. Once complete, check the **Releases** page for the new release

### 4. Verify Published Artifacts

After successful release, you'll have:

- **Docker Images:**
  - `ghcr.io/kagenti/kagenti-extensions/toolhive-webhook:v1.0.0`


- **Helm Chart:**
  - `oci://ghcr.io/kagenti/kagenti-extensions/toolhive-webhook/toolhive-webhook-chart`

- **Binaries** (attached to GitHub release):
  - `toolhive-webhook_Linux_x86_64.tar.gz`
  - `toolhive-webhook_Linux_arm64.tar.gz`
  - `toolhive-webhook_Darwin_x86_64.tar.gz`
  - `toolhive-webhook_Darwin_arm64.tar.gz`

## GoReleaser Configuration

**File:** [`.goreleaser.yaml`](.goreleaser.yaml:1)

Key features:

- **Multi-architecture builds**: Linux and Darwin for amd64 and arm64
- **Docker manifest**: Combines platform-specific images
- **Versioning**: Semantic versioning with major/minor tags
- **Helm integration**: Automatic chart packaging and publishing

### Docker Image Tags

Each release creates multiple tags:

- Full version: `v1.0.0`
- Major version: `v1` (updated with each v1.x.x release)
- Minor version: `v1.0` (updated with each v1.0.x release)
- Latest: `latest` (always points to most recent release)

## Deployment

Install the latest version:

```bash
helm install toolhive-webhook \
  oci://ghcr.io/kagenti/kagenti-extensions/toolhive-webhook/toolhive-webhook-chart \
  --version 1.0.0 \
  --namespace toolhive-system \
  --create-namespace
```

### Method 2: Using kubectl with Released YAML

Download and apply the release manifest:

```bash
kubectl apply -f https://github.com/kagenti/kagenti-extensions/releases/download/v1.0.0/install.yaml
```

### Method 3: Using Docker Image Directly

Pull the image:

```bash
docker pull ghcr.io/kagenti/kagenti-extensions/toolhive-webhook:v1.0.0
```

## Configuration

### Required Secrets

The workflows require the following GitHub secrets:

- `GITHUB_TOKEN` - Automatically provided by GitHub Actions
  - Used for pushing to GHCR and creating releases

### Environment Variables

In [`.github/workflows/goreleaser.yml`](.github/workflows/goreleaser.yml:10):

```yaml
env:
  REGISTRY: ghcr.io
  REPO: kagenti/toolhive-webhook
  CHARTS_PATH: ./charts
```

## Local Testing

### Test Docker Build

```bash
cd toolhive-webhook
docker build -t toolhive-webhook:test .
```
