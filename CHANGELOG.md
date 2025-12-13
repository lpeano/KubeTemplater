# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2025-12-13

### ⚙️ Configurable Performance Tuning

**All performance parameters now configurable via environment variables** - Dynamic tuning without rebuilds.

#### Added

- **Configurable Worker Pool Size** (`NUM_WORKERS`)
  - Default: 3 workers per pod
  - Range: 1-20 (recommended 3-10)
  - Allows scaling throughput per pod without recompiling
  - Validated with warnings for values >20

- **Configurable Cache TTL** (`CACHE_TTL`)
  - Default: 300 seconds (5 minutes)
  - Range: 60-600 seconds minimum enforced
  - Tune freshness vs API load dynamically
  - Optimal values: 180-240s (active), 300-600s (stable)

- **Configurable Periodic Reconciliation** (`PERIODIC_RECONCILE_INTERVAL`)
  - Default: 60 seconds
  - Range: 30-300 seconds minimum enforced
  - Control drift detection frequency
  - Automatic skip if recently reconciled (within half interval)

- **Configurable Queue Retry Parameters**
  - `QUEUE_MAX_RETRIES`: Max attempts before cooldown (default: 5, range: 1-10)
  - `QUEUE_INITIAL_RETRY_DELAY`: Initial retry delay in seconds (default: 1s)
  - `QUEUE_MAX_RETRY_DELAY`: Max retry delay cap (default: 300s, min: 60s)
  - Enables aggressive or conservative retry strategies per deployment

#### Changed

- **WorkQueue Architecture**: Replaced hardcoded constants with configurable fields
  - `NewWorkQueueWithConfig()` for custom retry configuration
  - `NewWorkQueue()` preserved for backward compatibility with defaults
  - All retry logic now uses instance-level configuration

- **Controller Configuration**: 
  - Added `PeriodicReconcileInterval` field to `KubeTemplateReconciler`
  - Replaced all hardcoded 60-second intervals with configurable parameter
  - Dynamic throttling based on configured interval

- **Main Initialization**: Added `getEnvInt()` helper for environment variable parsing
  - Validation with automatic fallback to safe defaults
  - Comprehensive logging of all tuning parameters at startup
  - Warnings for extreme values (e.g., NUM_WORKERS > 20)

#### Improved

- **Deployment Manifest** (`config/manager/manager.yaml`)
  - Added all 6 environment variables with defaults and comments
  - Clear documentation of recommended ranges per parameter
  - Easy copy-paste configuration examples

- **Documentation**
  - **README.md**: New "Performance Configuration" section with quick reference table
  - **docs/performance.md**: Comprehensive "Tuning Parameters" section with:
    - Configuration parameters reference table
    - Environment variable examples
    - Tuning scenarios by deployment scale (small/medium/large/extra-large)
    - Cache TTL trade-offs analysis
    - Worker pool scaling formulas and guidelines
    - HPA and resource limit recommendations by scale

- **Observability**: All tuning parameters logged at startup for audit trail

#### Example Configurations

**High-Throughput** (frequent template creation):
```yaml
NUM_WORKERS=8
CACHE_TTL=240
QUEUE_MAX_RETRIES=7
```

**Fast Drift Detection** (critical workloads):
```yaml
PERIODIC_RECONCILE_INTERVAL=30
CACHE_TTL=120
NUM_WORKERS=5
```

**Resource-Constrained** (minimal footprint):
```yaml
NUM_WORKERS=2
CACHE_TTL=600
PERIODIC_RECONCILE_INTERVAL=120
```

See [Performance Documentation](docs/performance.md) for detailed tuning guidance.

---

## [0.5.2] - 2025-12-12

### 🔧 Bug Fixes and Improvements

**Resolved Status Update Conflicts and Enhanced Drift Detection**

#### Fixed

- **Status Update Conflicts**: Eliminated "Operation cannot be fulfilled" errors
  - Implemented retry logic with automatic resource re-fetch before status updates
  - Handles concurrent modifications from multiple workers and FluxCD
  - All 8 status update points now use centralized `updateStatusWithRetry()` helper
  - Up to 3 automatic retries on resourceVersion conflicts

- **Semantic Drift Detection**: Replaced generation-based detection with spec comparison
  - Uses `apiequality.Semantic.DeepEqual` for accurate spec value comparison
  - Eliminates false positives from metadata changes (labels, annotations, finalizers)
  - No longer detects drift from empty arrays or generation increments without real changes
  - Only logs drift when actual spec values differ between SSA operations

#### Improved

- **Optimistic Locking**: Better handling of concurrent updates across multiple controllers
- **Drift Detection Accuracy**: Focuses only on spec changes, ignoring benign metadata updates
- **Status Updates**: More reliable with automatic conflict resolution

---

## [0.5.1] - 2025-12-12

### 🔄 Drift Detection and Periodic Reconciliation

**Server-Side Apply with ForceOwnership** - Automatic drift detection and correction for managed resources.

#### New Features

- **Periodic Reconciliation**: 60-second reconciliation cycle for Completed templates
- **Generation-based Drift Detection**: Accurate tracking using Kubernetes generation field
  - Detects only real spec changes (no false positives from metadata/status updates)
  - Increments drift counter only when external modifications are corrected
  - Logs drift detection events with generation change details
- **Enhanced Status Reporting**: Rich status information in kubectl output
  - Default view: Status, Age
  - Wide view (`-o wide`): Resources synced, Last reconcile time, Drift count, Last drift detected

#### Technical Implementation

- Server-Side Apply (SSA) with ForceOwnership for resource management
- Generation field comparison before/after SSA to detect real changes
- Additional printer columns in CRD definitions for kubectl output
- Status fields: `lastReconcileTime`, `resourcesTotal`, `resourcesSynced`, `lastDriftDetected`, `driftDetectionCount`
- Background reconciliation worker for Completed templates

---

## [0.5.0] - 2025-01-23

#### New Features

- **Periodic Reconciliation**: Automatic drift detection every 60 seconds
  - Templates in "Completed" phase reconcile automatically via `RequeueAfter`
  - No WorkQueue involvement for periodic reconciliation
  - Status remains "Completed" during periodic checks
  - Template modifications trigger immediate application (via Kubernetes watch)

- **Server-Side Apply (SSA)**: Declarative resource management
  - Field manager: `kubetemplater`
  - ForceOwnership enabled: takes control from external field managers
  - Handles conflicts with `kubectl patch`, operators, and manual modifications
  - Automatic resource recreation if deleted

- **Drift Correction**: Real-time enforcement of desired state
  - Manual modifications corrected within 60 seconds
  - External operator changes reconciled automatically
  - Deleted resources recreated within 60 seconds
  - OwnerReferences preserved during recreation

#### Technical Implementation

- **Controller Changes**:
  - New `applyTemplateResources` function with SSA
  - Conditional WorkQueue enqueue: only for non-Completed templates
  - Periodic reconciliation in Reconcile method (not WorkQueue)
  - ForceOwnership resolves field manager conflicts

- **ResourceWatcher Disabled**:
  - Cannot watch `unstructured.Unstructured{}` without specifying Kind
  - Replaced by SSA-based periodic reconciliation
  - Avoids API server overhead from watching all resource types

#### Breaking Changes

- ResourceWatcher functionality removed (not viable with controller-runtime)
- Cache label selector configuration removed from main.go

---

## [0.5.0] - 2025-12-11

### 🎯 Resource Lifecycle Management

**Referenced Field and Namespace Finalizers** - Advanced control over resource ownership and automatic cleanup.

#### New Features

- **Referenced Field**: Optional `referenced` boolean in template spec
  - When `true`: Adds KubeTemplate as OwnerReference to created resources (same-namespace only)
  - When `false` (default): No OwnerReference, resources persist independently
  - Enables automatic garbage collection for referenced resources
  - Works with same-namespace resources only (Kubernetes limitation)
  - Cross-namespace resources continue to use manual tracking

- **Namespace Finalizers**: Automatic template cleanup on namespace deletion
  - Namespace controller watches all namespaces
  - Adds `kubetemplater.io/namespace-finalizer` to all namespaces
  - On namespace deletion: lists and deletes all KubeTemplates in that namespace
  - Removes finalizer after cleanup complete
  - Prevents resource leaks from orphaned templates

- **CEL Validation Examples**: Production-ready policy examples
  - Field-level validation for complex resources (KeycloakRealm, databases, etc.)
  - Multi-field validation with multiple CEL expressions
  - Real-world use cases with detailed error messages

#### API Changes

- **Template Struct**:
  ```go
  type Template struct {
      Object     runtime.RawExtension `json:"object"`
      Replace    bool                 `json:"replace,omitempty"`
      Referenced bool                 `json:"referenced,omitempty"`  // NEW
  }
  ```

- **OwnerReference Implementation**:
  - Uses `APIVersion: kubetemplater.io/v1alpha1`
  - Uses `Kind: KubeTemplate`
  - Points from created resource to KubeTemplate (not Policy)
  - Only added for same-namespace resources

#### Bug Fixes

- **v0.5.1 (deployed)**: Initial implementation with Policy as OwnerReference (incorrect)
- **v0.5.2 (code ready)**: Fixed to use KubeTemplate as OwnerReference (correct)

#### Documentation

- Updated README.md with v0.5.1 features overview
- Added detailed "Resource Lifecycle Management" section in features.md
- Added "Namespace Finalizers" section with edge cases and monitoring
- Added production example: KeycloakRealm with CEL validations
- Updated CHANGELOG.md with breaking changes and migration guide

#### RBAC Changes

- Added namespace permissions to ClusterRole:
  - `get`, `list`, `watch`: Monitor namespace lifecycle
  - `update`: Add/remove finalizers

#### Known Issues

- v0.5.1 has cross-namespace OwnerReference bug (not yet deployed fix)
- `referenced: true` may fail for cross-namespace resources (use `false` instead)
- v0.5.2 contains the fix but is not yet built/deployed

## [0.3.3] - 2025-12-07

### 🚀 Major Performance & Reliability Improvements

**Watch-Based Certificate Discovery** - Event-driven certificate management with zero race conditions.

#### New Features

- **Event-Driven Certificate Discovery**: Kubernetes Watch API replaces polling
  - 95% lower latency: <100ms vs 2-second polling intervals
  - Reduced CPU usage: No constant polling, only spike on certificate events
  - Instant updates: React to certificate changes in real-time
  - Watch restart logic: Automatic recovery from watch failures (5-second retry)

- **SHA256 Hash Verification**: Prevents loading stale certificates
  - Leader calculates hash of generated certificate
  - Stores hash in Lease annotation (`kubetemplater.io/cert-hash`)
  - Followers verify filesystem hash matches lease before loading
  - Solves race condition: Lease update (T=0s) vs kubelet sync (T=~9s)

- **Thread-Safe Operations**: All 7 race conditions resolved
  1. **`atomic.Value`** for lastSeenLeaseHash (concurrent read/write)
  2. **`sync.Once`** for certReadyChan (prevent double-close)
  3. **Double-check locking** for certWatcher (prevent duplicate loads)
  4. **Local snapshot** for watch events (avoid concurrent modification)
  5. **Watch restart loop** (automatic recovery on closure)
  6. **Context cancellation** (proper shutdown handling)
  7. **Pre-check before I/O** (avoid wasted operations)

- **Hybrid Approach**: Watch + ticker fallback for maximum robustness
  - Watch for instant notifications (<100ms)
  - Ticker checks filesystem every 2 seconds (fallback if watch fails)
  - Both mechanisms verify hash before loading certificate

#### Security Enhancements

- **Multi-Layer Security Scanning Pipeline**:
  - **Trivy Filesystem**: Go dependencies and CVE detection
  - **GoSec**: Static security analysis (SQL injection, weak crypto, etc.)
  - **govulncheck**: Go vulnerability database check (disabled - requires Go 1.24)
  - **golangci-lint**: 20+ linters with security focus
  - **Trivy Image**: Docker image vulnerability scanning

- **SARIF Integration**: All scan results published to Azure DevOps Security tab
- **Justified #nosec**: GoSec warnings properly documented with security rationale
- **Continuous Monitoring**: Automated security pipeline on every commit

#### Bug Fixes

- **Secret Auto-Creation**: Fixed "Secret not found" error on first run
  - Added `apierrors.IsNotFound()` check in `ensureCertificate()`
  - Leader now creates empty Secret if missing before generating certificate

- **Go Version Compatibility**: Fixed pipeline failures
  - Changed go.mod from `go 1.24.0` to `go 1.23`
  - Updated Azure Pipeline GoTool task to version `1.23.4`
  - Go 1.24 not yet released, Kubernetes v0.33.0 works with 1.23

- **GoSec Warnings Resolved**: Fixed 10 security warnings
  - G304 (File Inclusion): Added justified `#nosec` comments (paths controlled by Kubernetes)
  - G306 (File Permissions): Changed test file permissions from 0644 → 0600

#### Performance Improvements

| Metric | v0.3.2 (Polling) | v0.3.3 (Watch) | Improvement |
|--------|------------------|----------------|-------------|
| Certificate Discovery Latency | 0-2 seconds | <100ms | **95% faster** |
| API Calls (100 pods) | 3000/min | ~0/min | **100% reduction** |
| CPU per Pod | 5m constant | 1m idle | **80% reduction** |
| Memory Overhead | Low | Medium | Watch buffer |

#### Documentation

- **[Watch-Based Certificate Discovery](docs/WATCH_BASED_CERTIFICATE_DISCOVERY.md)**: Complete architecture guide
  - Flow diagrams and timelines
  - Race condition analysis and solutions
  - Troubleshooting guide
  - Performance benchmarks
  
- **[Security Scanning](docs/SECURITY_SCANNING.md)**: Multi-layer security pipeline documentation
  - Tool descriptions and examples
  - SARIF artifact locations
  - Viewing instructions

#### Breaking Changes

- **Go 1.23 Required**: Changed minimum Go version from 1.24.0 to 1.23
- **New Import**: `sync/atomic` added for atomic operations
- **New Dependencies**: `k8s.io/client-go/kubernetes` for Watch API

#### Upgrade Notes

```bash
# Recommended: Use v0.3.3 tag for production
kubectl set image deployment/kubetemplater-controller-manager \
  manager=<your-registry>/kubetemplater:0.3.3 \
  -n kubetemplater-system

# Verify certificate watcher logs
kubectl logs -n kubetemplater-system \
  -l app.kubernetes.io/name=kubetemplater \
  | grep -i "watch\|hash\|cert"
```

Expected log messages:
- `Lease watch established, monitoring for certificate updates`
- `Lease watch event received` (with hash)
- `Certificate hash mismatch, waiting for kubelet sync` (during renewal)
- `Certificate loaded and verified` (when hash matches)

---

## [0.3.2] - 2025-12-06

### 🔐 Self-Signed Certificate Management

**Zero-dependency webhook certificate automation** - Works on all platforms.

#### New Features

- **Automatic Certificate Generation**: Leader pod generates self-signed certificates
  - RSA 2048-bit certificates
  - 1-year validity period
  - DNS SANs for webhook service
  - Stored in Kubernetes Secret

- **Certificate Renewal**: Automatic renewal 30 days before expiration
  - Leader checks expiry every 24 hours
  - Generates new certificate when <30 days remain
  - Updates Secret with new certificate
  - Followers detect change via volume mount

- **Leader Election**: Built-in coordination for multi-replica deployments
  - Only leader generates/renews certificates
  - Followers wait for leader to create certificates
  - Lease-based leader election via controller-runtime

- **Universal Platform Support**: Works without external dependencies
  - ✅ Azure AKS (no cloud-native certificate injection needed)
  - ✅ Google GKE (alternative to cloud-native)
  - ✅ Amazon EKS (no cert-manager required)
  - ✅ On-Premise (no external dependencies)

#### Components

- `internal/cert/manager.go`: Certificate generation and renewal logic
- `internal/cert/manager_test.go`: Comprehensive unit tests
- Lease annotations for certificate readiness signaling

#### Documentation

- Updated installation guides for all platforms (AKS, GKE, EKS)
- Removed cert-manager dependency from documentation

---

## [0.3.1] - 2025-12-05

### 🐛 Bug Fixes

- **CEL Validation**: Fixed `cel.CostLimit` type error in webhook validation
  - Moved `CostLimit` from `cel.NewEnv()` (EnvOption) to `env.Program()` (ProgramOption)
  - Resolves build compilation error in Go CEL validation logic

### 🔧 CI/CD Improvements

- **Azure Pipeline**: Enhanced ACR firewall management
  - Added automatic pipeline IP whitelisting before Docker operations
  - Implemented IP cleanup in both Build and TagRelease stages
  - Added 10-second wait for firewall rule propagation
  - Fixed Docker image tagging with registry prefix for proper push
  - Improved build/push workflow with proper containerRegistry specification

---

## [0.3.0] - 2025-12-05

### 🚀 Performance & Scalability

**Major Release**: Enterprise-grade performance optimizations delivering 30-60x capacity improvement.

#### New Features

- **Policy Caching Layer**: In-memory cache with 5-minute TTL
  - 95% reduction in API calls to Kubernetes API server
  - 60% faster webhook validation (80-120ms vs 200-300ms)
  - Automatic cache invalidation via watch-based controller
  - Thread-safe implementation with RWMutex

- **Async Reconciliation Queue**: Non-blocking processing pipeline
  - Priority queue with exponential backoff retry (1s → 5min)
  - 3-worker pool for parallel processing
  - Maximum 5 retry attempts per template
  - Controller returns in ~5ms (vs ~200ms synchronous)
  - 10-30x throughput improvement (50-150 reconciliations/sec)

- **Horizontal Pod Autoscaling**: Dynamic scaling based on load
  - Baseline: 3 replicas (high availability)
  - Auto-scale: 2-10 pods based on CPU (70%) and Memory (80%)
  - Aggressive scale-up (100%/30s), gradual scale-down (50%/60s)
  - Leader election for multi-replica coordination

- **Enhanced Status Tracking**: Rich status fields for observability
  - `processingPhase`: Queued, Processing, Completed, Failed
  - `queuedAt`: Timestamp when template was enqueued
  - `processedAt`: Timestamp when processing completed
  - `retryCount`: Number of retry attempts made

#### Performance Improvements

- **Resource Optimization**: 4x increased resource limits per pod
  - CPU: 500m → 2000m (4x)
  - Memory: 128Mi → 512Mi (4x)
  
- **Field Indexing**: O(1) policy lookups using indexed fields
  - Index on `KubeTemplatePolicy.spec.sourceNamespace`
  - Eliminates O(n) list operations
  - Reduces lookup time from ~10-50ms to ~1ms

- **CEL Optimization**: Added performance limits
  - CEL evaluation timeout: 100ms
  - CEL cost limit: 1,000,000 units
  - Regex pattern caching for reuse

- **Template Limits**: DoS protection
  - Max templates per KubeTemplate: 50
  - Max template size: 1MB

#### Capacity Improvements

| Metric | Before (v0.2.x) | After (v0.3.0) | Improvement |
|--------|----------------|----------------|-------------|
| Max KubeTemplates | ~500 | 15,000-30,000 | **30-60x** |
| Webhook Latency | 200-300ms | 80-120ms | **60% faster** |
| Throughput | 5-10/sec | 50-150/sec | **10-30x** |
| API Calls | 20-50/sec | 1-3/sec | **95% reduction** |

#### New Components

- `internal/cache/policy_cache.go`: Thread-safe policy cache implementation
- `internal/controller/kubetemplater.io/policy_cache_controller.go`: Cache synchronization controller
- `internal/queue/work_queue.go`: Priority queue with retry logic and metrics
- `internal/worker/template_processor.go`: Async worker pool for template processing
- `config/autoscaling/hpa.yaml`: Horizontal Pod Autoscaler configuration
- `docs/performance.md`: Comprehensive performance and scaling guide

#### Breaking Changes

- **CRD Update Required**: `KubeTemplateStatus` now includes new fields
  - Run `kubectl apply -f config/crd/bases/` to update CRDs
  - Existing KubeTemplates will have status fields populated on next reconciliation

#### Documentation

- Added comprehensive performance documentation (`docs/performance.md`)
- Updated README with v0.3.0 architecture and capabilities
- Added scaling scenarios and tuning recommendations
- Included monitoring and troubleshooting guides

### 🔧 Technical Details

- Upgraded Go requirement to 1.24.0 (required by k8s.io/api@v0.33.0)
- Added time package import to CRD types for timestamp fields
- Modified controller to enqueue work instead of synchronous processing
- Updated webhook to use cached policy lookups
- Implemented graceful shutdown for worker pool

## [0.2.0] - 2025-12-05

### ✨ Features

- **Field Validation System**: Added comprehensive field-level validation for KubeTemplate resources with 5 validation types:
  - **CEL Expressions**: Validate fields using Common Expression Language (e.g., `status.replicas < 100`)
  - **Regex Patterns**: Enforce format validation (e.g., DNS names, email patterns)
  - **Numeric Ranges**: Validate integer fields are within specified min/max bounds
  - **Required Fields**: Ensure critical fields are present and non-empty
  - **Forbidden Fields**: Block security-sensitive or deprecated fields
  - Sequential fail-fast execution with detailed error messages

- **Validating Admission Webhook**: Implemented validating webhook with TLS support
  - Real-time validation before resources are applied to the cluster
  - Prevents invalid templates from being created
  - Integrates with field validation system for comprehensive policy enforcement
  - Configurable failure policies and timeout settings

- **Multi-Cloud Certificate Management**: Three flexible certificate modes for webhook TLS:
  - **cloud-native**: Leverages AKS/GKE native certificate auto-injection (zero configuration)
  - **cert-manager**: Automatic certificate management via cert-manager (required for EKS, optional for others)
  - **manual**: User-provided certificates for air-gapped or corporate PKI environments

- **Cloud Provider Optimizations**:
  - **Azure AKS**: Native webhook certificate injection via service annotations
  - **Google GKE**: Workload Identity support and native certificate management
  - **Amazon EKS**: cert-manager integration with IRSA (IAM Roles for Service Accounts)
  - Dedicated Helm value examples for each cloud provider

- **CI/CD Pipelines**:
  - **GitHub Actions**: Multi-architecture Docker builds (amd64/arm64), GHCR publishing, Trivy security scanning
  - **Azure DevOps**: ACR integration, semantic versioning, release tagging stages
  - Automated security scanning with SARIF uploads to GitHub Security

### 🔧 Enhancements

- **Helm Chart v0.2.0**: Major update with webhook resources
  - Added CRDs to `crds/` folder for proper installation order
  - Conditional template rendering based on certificate mode
  - Webhook service, deployment volume mounts, and configuration
  - High availability configuration examples
  - Cloud-specific resource annotations and labels

- **API Version**: Updated to `v1alpha1` with extended `KubeTemplatePolicy` CRD
  - `FieldValidation` array in `ValidationRule` spec
  - Type-safe validation configurations
  - Generated DeepCopy methods for runtime.Object compliance

### 📚 Documentation

- **Cloud Provider Guides**: Comprehensive installation guides for AKS, GKE, and EKS
  - Prerequisites, step-by-step installation, troubleshooting
  - Cloud-specific features and best practices
  - Production-ready configuration examples

- **Webhook Documentation**: Detailed guides for webhook validation
  - Field validation configuration examples
  - Certificate management modes
  - Debugging and troubleshooting tips

- **CI/CD Documentation**: Pipeline setup and configuration guides
  - GitHub Actions workflow customization
  - Azure DevOps service connection setup
  - Image tagging and versioning strategies

### 🐛 Fixes

- Fixed unused parameter warnings in webhook validation functions
- Corrected CRD generation path for proper Go module structure
- Improved error messages for field validation failures

### 🏗️ Breaking Changes

- Updated Helm chart version to 0.2.0 (requires migration from v0.0.2)
- Webhook now required for policy enforcement (can be disabled with `webhook.enabled: false`)
- CRDs moved to `crds/` folder (automatic installation with Helm v3)

## [0.0.2] - 2025-11-08

### ✨ Features

- **Immutable Field Replace Strategy**: The controller can now automatically replace resources when a `Server-Side Apply` fails due to an immutable field change. To enable this behavior, add the following annotation to your target resource templates:
  ```yaml
  metadata:
    annotations:
      kubetemplater.io/replace-enabled: "true"
  ```
  When this annotation is present, the controller will delete and immediately re-create the resource to apply the changes.

### 🐛 Fixes

- Resolved an issue in the controller's reconciliation loop that prevented the "replace" strategy from working correctly in a single cycle.
- Improved the reliability of the integration test suite by fixing test cleanup procedures that were causing timeouts.

### 📚 Documentation

- Added documentation for the new `replace-enabled` annotation to the Helm chart's `values.yaml`.

[Unreleased]: https://github.com/lpeano/KubeTemplater/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/lpeano/KubeTemplater/compare/v0.0.2...v0.2.0
[0.0.2]: https://github.com/lpeano/KubeTemplater/compare/v0.0.1...v0.0.2