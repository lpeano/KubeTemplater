# KubeTemplater Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/ariellpe/KubeTemplater)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE) [![GitHub release (latest by date)](https://img.shields.io/github/v/release/ariellpe/KubeTemplater)](https://github.com/ariellpe/KubeTemplater/releases) [![Built with Go](https://img.shields.io/badge/Built%20with-Go-1976D2.svg)](https://go.dev/) [![Powered by Kubernetes](https://img.shields.io/badge/Powered%20by-Kubernetes-326CE5.svg)](https://kubernetes.io/) [![Built with Kubebuilder](https://img.shields.io/badge/Built%20with-Kubebuilder-8B572A.svg)](https://book.kubebuilder.io/) [![Community](https://img.shields.io/badge/Community-Join%20Us-blueviolet)](https://github.com/ariellpe/KubeTemplater/issues) [![Documentation](https://img.shields.io/badge/Documentation-Read%20the%20Docs-blue)](https://github.com/ariellpe/KubeTemplater/blob/main/README.md) [![CI](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml) [![CD](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml) [![Code Quality](https://img.shields.io/badge/Code%20Quality-A%2B-yellowgreen)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![Sponsors](https://img.shields.io/badge/Sponsors-Donate-df4aaa.svg)](https://github.com/sponsors/ariellpe) [![Changelog](https://img.shields.io/badge/Changelog-Read%20Me-green)](CHANGELOG.md) [![Website](https://img.shields.io/badge/Website-Visit%20Us-orange)](https://github.com/ariellpe/KubeTemplater) [![Get Started](https://img.shields.io/badge/Get%20Started-Now-ff69b4)](https://github.com/ariellpe/KubeTemplater#getting-started) [![YouTube](https://img.shields.io/badge/YouTube-Watch%20Now-red)](https://www.youtube.com/channel/UC59g-n32gC94i6Ew_fC6ZOA) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe)

**KubeTemplater** is a lightweight Kubernetes operator that manages Kubernetes resources through custom resources with built-in policy enforcement.

It allows you to define multiple Kubernetes resources in a single `KubeTemplate` custom resource, with validation and security controls provided by `KubeTemplatePolicy`.

---

## 📚 Documentation

- **[User Guide](USER_GUIDE.md)** - Complete user manual with installation, configuration, best practices, and troubleshooting
- **[Getting Started](docs/getting-started.md)** - Quick start guide
- **[Performance Tuning](docs/performance.md)** - Tuning guide for different scales
- **[Examples](docs/examples.md)** - Practical usage examples
- **[Changelog](CHANGELOG.md)** - Version history and what's new

---

## ✨ What's New in v0.6.0

**Configurable Performance Tuning** - Fine-tune KubeTemplater for your specific scale and use case:

### ⚙️ Tunable Parameters
All performance parameters are now configurable via Helm values or environment variables:
- **NUM_WORKERS**: Worker pool size (1-20, default: 3)
- **CACHE_TTL**: Policy cache lifetime (60-600s, default: 300s)
- **PERIODIC_RECONCILE_INTERVAL**: Drift detection interval (30-300s, default: 60s)
- **QUEUE_MAX_RETRIES**: Max retry attempts (1-10, default: 5)
- **QUEUE_INITIAL_RETRY_DELAY**: Initial retry delay (1-10s, default: 1s)
- **QUEUE_MAX_RETRY_DELAY**: Max retry delay cap (60-600s, default: 300s)

### 📦 Pre-configured Scenarios
4 ready-to-use Helm value files for common use cases:
- **High-Throughput**: 8 workers, 3000m CPU for burst workloads
- **Fast Drift Detection**: 30s reconciliation for security-critical environments
- **Resource-Constrained**: 2 workers, 1000m CPU for cost optimization
- **Large-Scale**: 10 workers, 4000m CPU for 15,000-30,000 templates

### 🔧 Helm Integration
```bash
# Install with high-throughput profile
helm install kubetemplater ./charts/kubetemplater \
  -f charts/kubetemplater/examples/values-high-throughput.yaml

# Or customize individual parameters
helm upgrade kubetemplater ./charts/kubetemplater \
  --set tuning.numWorkers=8 \
  --set tuning.cacheTTL=240
```

See [TUNING_GUIDE.md](TUNING_GUIDE.md) for complete tuning strategies.

---

## ✨ What's New in v0.5.1

**Drift Detection and Periodic Reconciliation** - Automatic detection and correction of configuration drift:

### 🔄 Periodic Reconciliation
- **60-Second Cycles**: Continuous reconciliation for Completed templates
- **Server-Side Apply**: Uses SSA with ForceOwnership to correct drift
- **Background Worker**: Non-blocking reconciliation in separate goroutine
- **Selective Processing**: Only processes templates in Completed state

### 🎯 Generation-Based Drift Detection
- **Accurate Tracking**: Uses Kubernetes generation field (no false positives)
- **Real Change Detection**: Only tracks actual spec modifications, not metadata/status updates
- **Drift Counter**: Increments only when external changes are corrected
- **Detailed Logging**: Records generation changes and drift correction events

### 📊 Enhanced Status Reporting
- **Rich kubectl Output**: New columns in `kubectl get kubetemplate`
  - Default view: Status, Age
  - Wide view (`-o wide`): Resources synced, Last reconcile, Drift count, Last drift
- **Status Fields**: 
  - `lastReconcileTime`: Timestamp of last reconciliation
  - `resourcesTotal` / `resourcesSynced`: Resource tracking
  - `driftDetectionCount` / `lastDriftDetected`: Drift statistics
- **Policy Status**: Enhanced KubeTemplatePolicy status columns

---

## ✨ What's New in v0.3.3

**Watch-Based Certificate Discovery with Hash Verification** - Event-driven certificate management with zero race conditions:

### 🔐 Enhanced Certificate Management
- **Event-Driven Discovery**: Watch API replaces polling for instant certificate updates (<100ms latency)
- **Hash Verification**: SHA256 hash comparison prevents loading stale certificates during kubelet sync
- **Thread-Safe Operations**: All 7 race conditions resolved with atomic operations and proper locking
- **Automatic Retry**: Hybrid watch + ticker approach ensures robustness against network failures
- **Zero Downtime**: Seamless certificate transitions with graceful hot-reload

### 🚀 Performance & Reliability
- **95% Lower Latency**: Event-driven updates vs. 2-second polling intervals
- **Reduced CPU Usage**: No constant polling, only spike on certificate events
- **Watch Restart Logic**: Automatic recovery from watch failures with 5-second retry
- **Context Cancellation**: Proper shutdown handling prevents resource leaks
- **Production-Ready**: Complete failure scenario coverage and comprehensive testing

### 🛡️ Security Improvements
- **Multi-Layer Scanning**: Trivy, GoSec, golangci-lint, govulncheck for comprehensive vulnerability detection
- **SARIF Integration**: All security scan results published to Azure DevOps
- **Justified Exceptions**: GoSec warnings properly documented with security rationale
- **Continuous Monitoring**: Automated security pipeline runs on every commit

### 📚 Documentation
- [Watch-Based Certificate Discovery](docs/WATCH_BASED_CERTIFICATE_DISCOVERY.md) - Complete architecture and troubleshooting guide
- [Security Scanning](docs/SECURITY_SCANNING.md) - Multi-layer security pipeline documentation

---

## ✨ What's New in v0.3.2

**Self-Signed Certificate Management** - Zero-dependency webhook certificate automation:

### 🔐 Automatic Certificate Management
- **Leader-Based Generation**: Leader pod generates self-signed certificates on startup
- **Shared via Secret**: Certificates stored in Kubernetes Secret, accessible to all replicas
- **Automatic Renewal**: Leader renews certificates 30 days before expiration
- **No External Dependencies**: Works without cert-manager on all platforms
- **Graceful Startup**: Non-leader pods wait for certificates (180s timeout with watch)

### 🌐 Universal Platform Support
- **Azure AKS**: ✅ Works (cloud-native not supported)
- **Google GKE**: ✅ Works (alternative to cloud-native)
- **Amazon EKS**: ✅ Works (simpler than cert-manager)
- **On-Premise**: ✅ Works (no external dependencies)

### 🎯 Implementation Details
- RSA 2048-bit self-signed certificates
- 1-year validity with automatic 30-day pre-expiration renewal
- Leader election via controller-runtime's native mechanism
- Certificate sync via Kubernetes Secret volume mount
- Hot reload support via certwatcher

---

## ✨ What's New in v0.3.0

**Performance & Scalability Enhancements** - Enterprise-grade optimizations for large-scale deployments:

### 🚀 Performance Improvements
- **Policy Caching Layer**: 95% reduction in API calls with in-memory cache (5-minute TTL)
- **Async Processing Queue**: Non-blocking reconciliation with 3-worker pool and priority queue
- **Optimized Webhook**: 60% faster validation (80-120ms vs 200-300ms)
- **Automatic Retry**: Exponential backoff with up to 5 retry attempts

### 📊 Scalability Features
- **Horizontal Pod Autoscaling**: Auto-scale from 2 to 10 pods based on CPU/Memory
- **High Availability**: 3 replicas baseline with leader election
- **Resource Optimization**: 4x increased limits (2000m CPU, 512Mi Memory per pod)
- **Field Indexing**: O(1) policy lookups using indexed fields

### 📈 Capacity
- **Before**: ~500 KubeTemplates max
- **Now**: **15,000-30,000 KubeTemplates** (30-60x improvement)

### 🎯 Field-Level Validation (v0.2.0)
Granular control over resource fields with five validation types:
- **CEL**: Complex expressions for custom logic
- **Regex**: Pattern matching for strings (image tags, labels, etc.)
- **Range**: Numeric validation (replicas, ports, resource limits)
- **Required**: Enforce mandatory security fields
- **Forbidden**: Prevent dangerous configurations

See [Features Documentation](docs/features.md) for complete details.

---

## 🚀 How it Works

KubeTemplater uses a high-performance, asynchronous architecture:

1.  **Watch:** Monitors `KubeTemplate` custom resources across the cluster.
2.  **Validate:** Admission webhook validates each `KubeTemplate` against the corresponding `KubeTemplatePolicy` using **cached policies** (95% faster).
3.  **Enqueue:** Controller marks the template as "Queued" and adds it to the **async processing queue**.
4.  **Process (Async):** Worker pool (3 workers) processes templates in parallel:
    - Fetches policy from **in-memory cache** (instant lookup)
    - Validates each resource against policy rules (GVK, target namespaces, CEL expressions)
    - Applies valid resources using **Server-Side Apply (SSA)**
    - Updates status to "Completed" or "Failed"
5.  **Retry:** Failed templates are automatically retried up to 5 times with exponential backoff (1s → 5min).

### Architecture Benefits
- **Non-blocking**: Controller returns immediately, processing happens in background
- **Scalable**: Auto-scales from 2 to 10 pods based on load (HPA)
- **Reliable**: Automatic retry with exponential backoff
- **Fast**: 95% less API calls, 60% lower latency
- **Observable**: Queue metrics for monitoring depth and throughput

---

## Configuration Example

First, create a `KubeTemplatePolicy` to define what resources can be created:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system  # Operator namespace
spec:
  sourceNamespace: default  # Namespace where KubeTemplates can be created
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [default]
    - kind: Service
      group: ""
      version: v1
      targetNamespaces: [default]
```

Then, create a `KubeTemplate` to define your resources:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-app-template
  namespace: default
spec:
  templates:
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: my-nginx-deployment
        spec:
          replicas: 3
          selector:
            matchLabels:
              app: nginx
          template:
            metadata:
              labels:
                app: nginx
            spec:
              containers:
              - name: nginx
                image: nginx:1.21.0
    
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: my-nginx-service
        spec:
          selector:
            app: nginx
          ports:
            - port: 80
              targetPort: 80
```

---

## 🔒 Security & Validation

KubeTemplater implements a **multi-layered security model**:

### Validation Webhook
- **Admission-time validation** of all KubeTemplate resources
- Rejects invalid configurations before they reach the cluster
- Validates against KubeTemplatePolicy rules with CEL expressions
- **Field-level validations** (v0.2.0+): Validate specific resource fields using CEL, Regex, Range, Required, and Forbidden rules
- Provides immediate feedback to users

### Policy Enforcement
- **KubeTemplatePolicy** CRD defines what resources can be created
- **Namespace isolation**: Policies control which namespaces can create which resources
- **CEL expressions**: Custom validation rules using Common Expression Language
- **Field validations**: Granular control over resource fields (replicas, images, security settings, etc.)
- **Resource type restrictions**: Whitelist allowed Kubernetes resource types

For detailed information, see [Webhook Validation Documentation](docs/webhook-validation.md).

---

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Performance Configuration

All performance parameters are **configurable via environment variables** without rebuilding:

| Parameter | Default | Description | Tuning Guide |
|-----------|---------|-------------|--------------|
| `NUM_WORKERS` | 3 | Concurrent worker goroutines | 3-5 (normal), 5-10 (high load) |
| `CACHE_TTL` | 300s | Policy cache lifetime | 180-300s (active), 300-600s (stable) |
| `PERIODIC_RECONCILE_INTERVAL` | 60s | Drift detection interval | 45-60s (normal), 30s (critical) |
| `QUEUE_MAX_RETRIES` | 5 | Retry attempts before cooldown | 5-7 (recommended) |
| `QUEUE_INITIAL_RETRY_DELAY` | 1s | Initial retry delay | 1s (aggressive), 2-5s (conservative) |
| `QUEUE_MAX_RETRY_DELAY` | 300s | Maximum retry delay cap | 180-300s (recommended) |

**Configure in Helm values**:
```yaml
controller:
  env:
    - name: NUM_WORKERS
      value: "5"  # Increase for higher throughput
    - name: CACHE_TTL
      value: "240"  # 4 minutes for more responsive updates
```

Or edit `config/manager/manager.yaml` directly. See [Performance Documentation](docs/performance.md) for detailed tuning scenarios.

### Performance Recommendations by Scale
- **Small** (< 5,000 templates): Default config, excellent performance ⭐⭐⭐⭐⭐
- **Medium** (5,000-15,000): NUM_WORKERS=5, HPA scales 3-5 pods ⭐⭐⭐⭐
- **Large** (15,000-30,000): NUM_WORKERS=7-10, HPA max 15 pods, resources 4000m/1Gi ⭐⭐⭐
- **Extra Large** (> 30,000): Consider namespace sharding or multiple operators

### Installation with Helm

**Recommended installation method** using the provided Helm chart.

**Current Chart Version**: `0.5.1`

To install the chart from the `charts/kubetemplater` directory:

```sh
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

This will install:
- Custom Resource Definitions (CRDs) with field validation support
- Validating webhook with policy caching for fast validation
- Controller manager with async processing queue and worker pool
- Horizontal Pod Autoscaler (HPA) for automatic scaling
- High Availability with 3 replicas and leader election
- Metrics endpoints for monitoring queue depth and performance

You can customize the installation by providing your own `values.yaml` file:

```sh
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f my-values.yaml
```

**Verify installation:**
```sh
kubectl get pods -n kubetemplater-system
kubectl get validatingwebhookconfigurations
```

### Webhook Certificate Management

KubeTemplater supports multiple certificate management modes for the validating webhook:

#### 🔐 Self-Signed (Recommended - Default)
**Automatic certificate generation and renewal with leader election:**
- Leader pod generates self-signed certificates on first startup
- Certificates stored in Kubernetes Secret, shared across all replicas
- Automatic renewal 30 days before expiration
- No external dependencies (cert-manager not required)
- Works on all Kubernetes platforms (AKS, EKS, GKE, on-premise)

```yaml
webhook:
  enabled: true
  certificateMode: "self-signed"  # Default
```

**How it works:**
1. Leader pod checks if certificates exist in Secret at startup
2. If missing/expired, generates new RSA 2048-bit self-signed certificate
3. Updates Secret with certificate data
4. Kubelet syncs Secret to volume mounts on all pods
5. Non-leader pods wait for certificates to appear (max 60s)
6. Daily check by leader for renewal (if <30 days remaining)

#### ☁️ Cloud-Native
**Provider-managed certificates (GKE only):**
- Google GKE automatically injects webhook certificates
- No configuration needed on GKE
- ⚠️ **Not supported on Azure AKS or Amazon EKS**

```yaml
webhook:
  certificateMode: "cloud-native"
```

#### 📜 Cert-Manager
**Managed by cert-manager (requires cert-manager installed):**
- Certificates issued by cert-manager Certificate resource
- Automatic renewal handled by cert-manager
- Suitable for organizations with existing cert-manager infrastructure

```yaml
webhook:
  certificateMode: "cert-manager"
  certManager:
    issuerName: "my-issuer"  # Leave empty for self-signed issuer
    issuerKind: "Issuer"     # or "ClusterIssuer"
```

#### 🔧 Manual
**Bring your own certificates:**
- For corporate PKI, air-gapped environments, or custom requirements
- You manage certificate lifecycle and renewal

```yaml
webhook:
  certificateMode: "manual"
  certificate:
    caBundle: "<base64-encoded-ca-cert>"
    tlsCert: "<base64-encoded-tls-cert>"
    tlsKey: "<base64-encoded-tls-key>"
```

**Cloud Provider Installation Guides:**
- **[Azure AKS](docs/aks-installation.md)** - Self-signed mode (recommended, cloud-native not supported)
- **[Google GKE](docs/gke-installation.md)** - Cloud-native or self-signed (both work)
- **[Amazon EKS](docs/eks-installation.md)** - Self-signed or cert-manager (cloud-native not supported)

### Installation from source
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/kubetemplater:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/kubetemplater:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

---

## ⚙️ Configuration Reference

### Environment Variables

All tuning parameters can be configured via environment variables in `config/manager/manager.yaml`:

#### Worker Pool Configuration

**NUM_WORKERS**
- **Default**: `3`
- **Range**: 1-20 (1-10 recommended)
- **Description**: Number of concurrent worker goroutines processing templates
- **Impact**: Higher values = more throughput but more CPU/memory usage
- **Tuning**:
  - Small deployments: 3 (default)
  - Medium deployments: 5
  - Large deployments: 7-10

#### Cache Configuration

**CACHE_TTL**
- **Default**: `300` (5 minutes)
- **Range**: 60-600 seconds
- **Description**: Policy cache time-to-live in seconds
- **Impact**: Lower values = fresher data but more API calls
- **Tuning**:
  - Active policy changes: 180-240s
  - Stable environments: 300-600s

#### Reconciliation Configuration

**PERIODIC_RECONCILE_INTERVAL**
- **Default**: `60` (1 minute)
- **Range**: 30-300 seconds
- **Description**: Interval for drift detection reconciliation
- **Impact**: Lower values = faster drift detection but more CPU usage
- **Tuning**:
  - Critical workloads: 30-45s
  - Normal workloads: 60s (default)

#### Work Queue Retry Configuration

**QUEUE_MAX_RETRIES**
- **Default**: `5`
- **Range**: 1-10
- **Description**: Maximum retry attempts before cooldown period

**QUEUE_INITIAL_RETRY_DELAY**
- **Default**: `1` (1 second)
- **Description**: Initial retry delay (exponential backoff starts here)

**QUEUE_MAX_RETRY_DELAY**
- **Default**: `300` (5 minutes)
- **Range**: 60-600 seconds
- **Description**: Maximum cap for retry delay

### Configuration Examples

**High-Throughput Setup** (frequent template creation):
```yaml
env:
  - name: NUM_WORKERS
    value: "8"
  - name: CACHE_TTL
    value: "240"
  - name: QUEUE_MAX_RETRIES
    value: "7"
```

**Fast Drift Detection** (critical workloads):
```yaml
env:
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "30"
  - name: CACHE_TTL
    value: "120"
  - name: NUM_WORKERS
    value: "5"
```

**Resource-Constrained** (minimal footprint):
```yaml
env:
  - name: NUM_WORKERS
    value: "2"
  - name: CACHE_TTL
    value: "600"
resources:
  limits:
    cpu: 1000m
    memory: 256Mi
```

📖 **See [Performance Documentation](docs/performance.md) for detailed tuning scenarios and capacity planning.**

---

## Contributing

We welcome contributions from the community! If you'd like to contribute to KubeTemplater, please follow these steps:

1.  **Fork the repository** on GitHub.
2.  **Create a new branch** for your changes: `git checkout -b my-feature-branch`
3.  **Make your changes** and commit them with a clear commit message.
4.  **Push your changes** to your fork: `git push origin my-feature-branch`
5.  **Open a pull request** to the main repository.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

---

## Community and Support

If you have any questions, or suggestions, please open an issue in the [GitHub repository](https://github.com/ariellpe/KubeTemplater/issues).

---

## Code of Conduct

This project has a [Code of Conduct](CODE_OF_CONDUCT.md) that all contributors are expected to follow.

---

## Security

If you discover a security vulnerability, please see our [Security Policy](SECURITY.md).

---

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

---

## Acknowledgments

This project was built using the [Kubebuilder](https://book.kubebuilder.io/) framework.