# CI/CD Pipelines

This document describes the CI/CD pipelines for building and publishing KubeTemplater container images.

## GitHub Actions

The GitHub Actions workflow (`.github/workflows/build-image.yml`) automatically builds and publishes container images to GitHub Container Registry (ghcr.io).

### Triggers

- **Push to main**: Builds and pushes with `main` and `latest` tags
- **Tags**: Builds and pushes versioned images (e.g., `v0.2.0`, `0.2.0`, `0.2`, `0`)
- **Pull Requests**: Builds only (does not push)

### Image Registry

Images are published to: `ghcr.io/<github-org>/kubetemplater`

### Tags Generated

- `latest` - Latest build from main branch
- `main` - Latest build from main branch
- `v1.2.3` - Exact version tag
- `1.2.3` - Version without 'v' prefix
- `1.2` - Major.minor version
- `1` - Major version only
- `main-<sha>` - Branch with commit SHA
- `pr-<number>` - Pull request builds (not pushed)

### Multi-Architecture Support

Images are built for:
- `linux/amd64` (x86_64)
- `linux/arm64` (ARM 64-bit)

### Security Scanning

The pipeline includes Trivy vulnerability scanning:
- Scans for HIGH and CRITICAL vulnerabilities
- Results uploaded to GitHub Security tab
- Runs on all builds (except PRs)

### Setup

No additional setup required. The workflow uses `GITHUB_TOKEN` which is automatically available.

To pull images:
```bash
docker pull ghcr.io/<github-org>/kubetemplater:latest
```

---

## Azure DevOps Pipeline

The Azure Pipeline (`azure-pipelines.yml`) builds and publishes images to Azure Container Registry.

### Prerequisites

1. **Azure Container Registry**: Create an ACR instance
   ```bash
   az acr create --resource-group <rg> --name <acr-name> --sku Basic
   ```

2. **Service Connection**: Create a service connection in Azure DevOps
   - Go to Project Settings → Service connections
   - Create "Docker Registry" connection
   - Connection type: Azure Container Registry
   - Name it (e.g., `acr-connection`)
   - Select your ACR

3. **Update Pipeline Variables**: Edit `azure-pipelines.yml`
   ```yaml
   variables:
     acrServiceConnection: 'your-acr-connection-name'
     containerRegistry: 'youracr.azurecr.io'
   ```

### Triggers

- **Push to main**: Builds and pushes images
- **Tags (v*.*.*)**: Builds and pushes versioned releases
- **Pull Requests**: Builds only (validation)

### Pipeline Stages

#### Stage 1: Build
- Builds Docker image for multiple architectures
- Pushes to ACR with tags: `latest`, `<branch-name>`, `<build-id>`
- Runs Trivy security scan
- Publishes scan results as artifacts

#### Stage 2: TagRelease
- Only runs for version tags (e.g., `v1.2.3`)
- Creates additional version tags:
  - `1.2.3` (full version)
  - `1.2` (major.minor)
  - `1` (major)

### Tags Generated

For branch builds:
- `latest` - Latest from main
- `main` - Branch name
- `<build-id>` - Azure DevOps build ID

For release tags (v1.2.3):
- `1.2.3` - Full version
- `1.2` - Major.minor
- `1` - Major version

### Security Scanning

- Trivy scans for HIGH and CRITICAL vulnerabilities
- Results published as build artifacts
- Can be viewed in Azure DevOps Artifacts tab

### Usage

Pull images from ACR:
```bash
# Login to ACR
az acr login --name <acr-name>

# Pull image
docker pull <acr-name>.azurecr.io/kubetemplater:latest
```

---

## Building Locally

### Build for Single Platform
```bash
docker build -t kubetemplater:local .
```

### Build for Multiple Platforms
```bash
docker buildx create --use
docker buildx build --platform linux/amd64,linux/arm64 -t kubetemplater:multi .
```

### Build with Version Info
```bash
docker build \
  --build-arg VERSION=0.2.0 \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  -t kubetemplater:0.2.0 .
```

---

## Image Usage in Helm

### GitHub Container Registry
```bash
helm install kubetemplater ./charts/kubetemplater \
  --set image.repository=ghcr.io/<org>/kubetemplater \
  --set image.tag=0.2.0
```

### Azure Container Registry
```bash
helm install kubetemplater ./charts/kubetemplater \
  --set image.repository=<acr-name>.azurecr.io/kubetemplater \
  --set image.tag=0.2.0
```

### With Image Pull Secrets (for private registries)
```bash
# Create secret
kubectl create secret docker-registry acr-secret \
  --docker-server=<acr-name>.azurecr.io \
  --docker-username=<username> \
  --docker-password=<password> \
  --namespace kubetemplater-system

# Install with secret
helm install kubetemplater ./charts/kubetemplater \
  --set image.repository=<acr-name>.azurecr.io/kubetemplater \
  --set image.tag=0.2.0 \
  --set image.pullSecrets[0]=acr-secret
```

---

## Troubleshooting

### GitHub Actions: Permission Denied to Push
Ensure the workflow has `packages: write` permission:
```yaml
permissions:
  packages: write
```

### Azure DevOps: Service Connection Fails
- Verify service connection in Project Settings
- Check ACR name matches in variables
- Ensure service principal has `AcrPush` role

### Multi-Architecture Build Fails
Install buildx:
```bash
docker buildx create --use --name multiarch
docker buildx inspect --bootstrap
```

### Image Pull Fails on Kubernetes
- Check image name and tag are correct
- For private registries, ensure imagePullSecrets are configured
- Verify nodes can reach the registry (firewall, DNS)

---

## Security Best Practices

1. **Scan Images**: Both pipelines include Trivy scanning
2. **Use Specific Tags**: Avoid `latest` in production
3. **Multi-Stage Builds**: Dockerfile uses multi-stage for minimal image size
4. **Non-Root User**: Container runs as non-root user
5. **Minimal Base**: Uses distroless or minimal base images
6. **Sign Images**: Consider using Cosign for image signing

---

## Related Documentation

- [Dockerfile](../Dockerfile)
- [Helm Chart](../charts/kubetemplater/README.md)
- [AKS Installation](aks-installation.md)
- [GKE Installation](gke-installation.md)
- [EKS Installation](eks-installation.md)
