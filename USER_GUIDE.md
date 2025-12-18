# KubeTemplater - User Guide

**Version**: 0.6.2  
**Last Updated**: December 18, 2025

Complete user guide for KubeTemplater - Policy-driven Kubernetes resource management with validation webhook.

---

## Table of Contents

1. [Introduction](#introduction)
2. [Quick Start](#quick-start)
3. [Core Concepts](#core-concepts)
4. [Installation](#installation)
5. [Basic Usage](#basic-usage)
6. [Policy Management](#policy-management)
7. [Field-Level Validation](#field-level-validation)
8. [Performance Tuning](#performance-tuning)
9. [Monitoring and Troubleshooting](#monitoring-and-troubleshooting)
10. [Best Practices](#best-practices)
11. [Advanced Topics](#advanced-topics)
12. [FAQ](#faq)

---

## Introduction

### What is KubeTemplater?

KubeTemplater is a Kubernetes operator that simplifies resource management by allowing you to define multiple resources in a single custom resource (`KubeTemplate`) while enforcing security policies through validation webhooks.

### Key Features

- ✅ **Multi-Resource Templates**: Define multiple Kubernetes resources in one YAML file
- ✅ **Policy Enforcement**: Validate resources against policies before creation
- ✅ **Field-Level Validation**: CEL, Regex, Range, Required, and Forbidden field rules
- ✅ **Drift Detection**: Automatic detection and correction of configuration drift
- ✅ **High Performance**: 15,000-30,000 templates capacity with auto-scaling
- ✅ **Async Processing**: Non-blocking reconciliation with retry logic
- ✅ **Self-Signed Certificates**: Zero external dependencies for webhook TLS

### Architecture Overview

```
User
  │
  ├─► Create KubeTemplate ──► Webhook Validation (80-120ms)
  │                              │
  │                              ├─► Policy Cache Lookup
  │                              └─► Field Validation (CEL/Regex)
  │
  └─► Approved ──► Controller ──► Work Queue ──► Worker Pool (3-10 workers)
                       │                              │
                       │                              ├─► Apply Resources (SSA)
                       │                              ├─► Update Status
                       │                              └─► Retry on Failure
                       │
                       └─► Periodic Reconciliation (drift detection every 60s)
```

---

## Quick Start

### Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- kubectl configured

### 5-Minute Setup

```bash
# 1. Install KubeTemplater
helm install kubetemplater \
  https://github.com/ariellpe/KubeTemplater/releases/download/v0.6.0/kubetemplater-0.6.0.tgz \
  --namespace kubetemplater-system \
  --create-namespace

# 2. Verify installation
kubectl get pods -n kubetemplater-system
kubectl get crd | grep kubetemplater

# 3. Create a policy (allow ConfigMaps in default namespace)
cat <<EOF | kubectl apply -f -
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces: [default]
EOF

# 4. Create your first template
cat <<EOF | kubectl apply -f -
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-first-template
  namespace: default
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: my-config
        data:
          greeting: "Hello KubeTemplater!"
EOF

# 5. Check status
kubectl get kubetemplate my-first-template -o wide
kubectl get configmap my-config
```

**Expected Output**:
```
NAME                STATUS      AGE
my-first-template   Completed   10s

NAME        DATA   AGE
my-config   1      10s
```

---

## Core Concepts

### 1. KubeTemplate

A `KubeTemplate` is a custom resource that wraps one or more Kubernetes resources.

**Basic Structure**:
```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: example-template
  namespace: default
spec:
  templates:
    - object:
        # Any Kubernetes resource (Deployment, Service, etc.)
```

**Lifecycle Phases**:
- `""` (empty): New template, not yet processed
- `Queued`: Enqueued for processing
- `Processing`: Being processed by worker
- `Completed`: Successfully applied
- `Failed`: Processing failed (will retry)

### 2. KubeTemplatePolicy

A `KubeTemplatePolicy` defines what resources can be created from which namespaces.

**Structure**:
```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: my-policy
  namespace: kubetemplater-system  # Always in operator namespace
spec:
  sourceNamespace: app-namespace   # Which namespace can use this policy
  validationRules:
    - kind: Deployment              # Allowed resource type
      group: apps
      version: v1
      targetNamespaces:             # Where resources can be created
        - app-namespace
        - app-namespace-prod
```

**Key Rules**:
- One policy per source namespace
- Policies must be in operator namespace (default: `kubetemplater-system`)
- Resources are validated against the policy for their source namespace

### 3. Validation Webhook

The webhook validates templates before they're created:
- Checks if a policy exists for the source namespace
- Validates resource types against policy rules
- Enforces field-level validations (CEL, Regex, etc.)
- Returns errors immediately (80-120ms latency)

---

## Installation

### Method 1: Helm (Recommended)

**Default Installation** (3 replicas, 3 workers, auto-scaling):
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

**Custom Configuration**:
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set tuning.numWorkers=5 \
  --set tuning.cacheTTL=240 \
  --set resources.limits.cpu=3000m \
  --set rbac.allowClusterResources=false  # Secure by default (v0.6.2+)
```

**RBAC Security Configuration** (v0.6.2+):
```bash
# Multi-tenant environment (secure, default)
helm install kubetemplater ./charts/kubetemplater \
  --set rbac.allowClusterResources=false \
  --namespace kubetemplater-system

# Platform/infrastructure management (more permissive)
helm install kubetemplater ./charts/kubetemplater \
  --set rbac.allowClusterResources=true \
  --namespace kubetemplater-system
```

| Setting | Allowed Resources | Use Case |
|---------|------------------|----------|
| `false` (default) | Only namespaced (Deployments, Services, ConfigMaps, Secrets, etc.) | Multi-tenant, standard applications |
| `true` | Namespaced + cluster-scoped (ClusterRoles, PV, Namespaces, CRDs) | Platform operators, infrastructure management |

**Pre-configured Scenarios**:
```bash
# High-Throughput (8 workers, 3000m CPU)
helm install kubetemplater ./charts/kubetemplater \
  -f charts/kubetemplater/examples/values-high-throughput.yaml \
  --namespace kubetemplater-system \
  --create-namespace

# Fast Drift Detection (30s interval)
helm install kubetemplater ./charts/kubetemplater \
  -f charts/kubetemplater/examples/values-fast-drift-detection.yaml \
  --namespace kubetemplater-system \
  --create-namespace

# Resource-Constrained (2 workers, 1000m CPU)
helm install kubetemplater ./charts/kubetemplater \
  -f charts/kubetemplater/examples/values-resource-constrained.yaml \
  --namespace kubetemplater-system \
  --create-namespace
```

### Method 2: From Source

```bash
# Clone repository
git clone https://github.com/ariellpe/KubeTemplater.git
cd KubeTemplater

# Install CRDs
make install

# Deploy operator
make deploy IMG=your-registry/kubetemplater:v0.6.0
```

### Cloud Provider Guides

- **Azure AKS**: [aks-installation.md](docs/aks-installation.md)
- **Google GKE**: [gke-installation.md](docs/gke-installation.md)
- **Amazon EKS**: [eks-installation.md](docs/eks-installation.md)

---

## Basic Usage

### Creating Your First Template

**Step 1: Create a Policy**

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: myapp-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: myapp
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [myapp]
    - kind: Service
      group: ""
      version: v1
      targetNamespaces: [myapp]
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces: [myapp]
```

**Step 2: Create a Template**

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: myapp
  namespace: myapp
spec:
  templates:
    # ConfigMap with configuration
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: myapp-config
        data:
          database_url: "postgresql://db:5432/myapp"
          log_level: "info"
    
    # Deployment
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
        spec:
          replicas: 3
          selector:
            matchLabels:
              app: myapp
          template:
            metadata:
              labels:
                app: myapp
            spec:
              containers:
              - name: myapp
                image: myapp:v1.0.0
                env:
                - name: DATABASE_URL
                  valueFrom:
                    configMapKeyRef:
                      name: myapp-config
                      key: database_url
    
    # Service
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: myapp
        spec:
          selector:
            app: myapp
          ports:
          - port: 80
            targetPort: 8080
```

**Step 3: Apply and Verify**

```bash
# Apply template
kubectl apply -f myapp-template.yaml

# Check status
kubectl get kubetemplate myapp -o wide
# NAME    STATUS      RESOURCES   SYNCED   AGE
# myapp   Completed   3           3        30s

# Verify resources
kubectl get deployment,service,configmap -l managed-by=kubetemplater
```

### Updating Templates

```bash
# Edit template
kubectl edit kubetemplate myapp

# Or update via file
kubectl apply -f myapp-template-v2.yaml

# Watch status
kubectl get kubetemplate myapp -w
```

### Deleting Templates

```bash
# Delete template (resources are retained by default)
kubectl delete kubetemplate myapp

# To delete resources with template, use referenced field (see Advanced Topics)
```

---

## Policy Management

### Policy Structure

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: policy-name
  namespace: kubetemplater-system  # MUST be in operator namespace
spec:
  sourceNamespace: app-namespace   # Namespace that uses this policy
  validationRules:
    - kind: ResourceKind
      group: "api-group"             # "" for core resources
      version: v1
      targetNamespaces:              # Where resources can be created
        - namespace1
        - namespace2
      rule: "optional CEL expression"
      fieldValidations:              # Optional field-level rules
        - path: "spec.replicas"
          type: Range
          range:
            min: 1
            max: 10
```

### Common Policy Patterns

#### 1. Restrict to Single Namespace

```yaml
spec:
  sourceNamespace: myapp
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [myapp]  # Only myapp namespace
```

#### 2. Multi-Namespace Access

```yaml
spec:
  sourceNamespace: platform
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces:
        - platform-dev
        - platform-staging
        - platform-prod
```

#### 3. Read-Only Resources

```yaml
spec:
  sourceNamespace: monitoring
  validationRules:
    - kind: ServiceMonitor
      group: monitoring.coreos.com
      version: v1
      targetNamespaces: [monitoring]
    # No Deployment allowed = read-only ServiceMonitors
```

### Policy Management Commands

```bash
# List policies
kubectl get kubetemplatepolicy -n kubetemplater-system

# View policy details
kubectl describe kubetemplatepolicy myapp-policy -n kubetemplater-system

# Update policy
kubectl edit kubetemplatepolicy myapp-policy -n kubetemplater-system

# Delete policy (does NOT delete templates)
kubectl delete kubetemplatepolicy myapp-policy -n kubetemplater-system
```

---

## Field-Level Validation

Field-level validations provide granular control over resource fields.

### Validation Types

#### 1. CEL (Common Expression Language)

**Use case**: Complex custom logic

```yaml
fieldValidations:
  - path: "spec.replicas"
    type: CEL
    cel:
      expression: "self >= 1 && self <= 10"
      message: "Replicas must be between 1 and 10"
```

**Advanced CEL Examples**:

```yaml
# Image from allowed registries
- path: "spec.template.spec.containers[*].image"
  type: CEL
  cel:
    expression: "self.startsWith('myregistry.io/')"
    message: "Images must come from myregistry.io"

# Resource limits enforced
- path: "spec.template.spec.containers[*].resources.limits.memory"
  type: CEL
  cel:
    expression: "self != null"
    message: "Memory limits are required"

# Label validation
- path: "metadata.labels"
  type: CEL
  cel:
    expression: "has(self.team) && has(self.environment)"
    message: "Must have 'team' and 'environment' labels"
```

#### 2. Regex (Pattern Matching)

**Use case**: String validation (names, tags, labels)

```yaml
# Image tag must be semantic version
- path: "spec.template.spec.containers[*].image"
  type: Regex
  regex:
    pattern: "^.*:v[0-9]+\\.[0-9]+\\.[0-9]+$"
    message: "Image tag must be semantic version (e.g., v1.2.3)"

# DNS-compliant names
- path: "metadata.name"
  type: Regex
  regex:
    pattern: "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
    message: "Name must be DNS-compliant"
```

#### 3. Range (Numeric Validation)

**Use case**: Replicas, ports, resource limits

```yaml
# Replica limits
- path: "spec.replicas"
  type: Range
  range:
    min: 1
    max: 10

# Port range
- path: "spec.template.spec.containers[*].ports[*].containerPort"
  type: Range
  range:
    min: 1024
    max: 65535
```

#### 4. Required (Mandatory Fields)

**Use case**: Enforce security fields

```yaml
# Security context required
- path: "spec.template.spec.securityContext"
  type: Required
  required:
    message: "Security context is mandatory"

# Resource limits required
- path: "spec.template.spec.containers[*].resources.limits"
  type: Required
  required:
    message: "Resource limits must be specified"
```

#### 5. Forbidden (Blocked Fields)

**Use case**: Prevent dangerous configurations

```yaml
# No privileged containers
- path: "spec.template.spec.containers[*].securityContext.privileged"
  type: Forbidden
  forbidden:
    message: "Privileged containers are not allowed"

# No host network
- path: "spec.template.spec.hostNetwork"
  type: Forbidden
  forbidden:
    message: "Host network access is forbidden"
```

### Complete Field Validation Example

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: secure-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: production
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [production]
      fieldValidations:
        # Replicas between 2-10
        - path: "spec.replicas"
          type: Range
          range:
            min: 2
            max: 10
        
        # Images from approved registry
        - path: "spec.template.spec.containers[*].image"
          type: Regex
          regex:
            pattern: "^myregistry\\.io/.*:v[0-9]+\\.[0-9]+\\.[0-9]+$"
            message: "Images must be from myregistry.io with semver tag"
        
        # Security context required
        - path: "spec.template.spec.securityContext"
          type: Required
          required:
            message: "Pod security context is mandatory"
        
        # No privileged containers
        - path: "spec.template.spec.containers[*].securityContext.privileged"
          type: Forbidden
          forbidden:
            message: "Privileged mode is not allowed in production"
        
        # Resource limits required
        - path: "spec.template.spec.containers[*].resources.limits.memory"
          type: Required
          required:
            message: "Memory limits are required"
        
        # CPU limits reasonable
        - path: "spec.template.spec.containers[*].resources.limits.cpu"
          type: CEL
          cel:
            expression: "self.matches('[0-9]+m$') && int(self.replace('m', '')) <= 4000"
            message: "CPU limit must not exceed 4000m"
```

---

## Performance Tuning

KubeTemplater v0.6.0 introduces configurable performance parameters.

### Tuning Parameters

| Parameter | Default | Range | Description |
|-----------|---------|-------|-------------|
| `NUM_WORKERS` | 3 | 1-20 | Worker pool size |
| `CACHE_TTL` | 300s | 60-600s | General cache lifetime |
| `POLICY_CACHE_TTL` | 60s | 30-600s | Policy cache (webhook & workers) |
| `PERIODIC_RECONCILE_INTERVAL` | 60s | 30-300s | Drift detection interval |
| `QUEUE_MAX_RETRIES` | 5 | 1-10 | Max retry attempts |
| `QUEUE_INITIAL_RETRY_DELAY` | 1s | 1-10s | Initial retry delay |
| `QUEUE_MAX_RETRY_DELAY` | 300s | 60-600s | Max retry delay cap |
| `QUEUE_MAX_RETRY_CYCLES` | 3 | 0-10 | Max retry cycles before pause (0=unlimited) |

### Configuration Methods

#### Via Helm Values

```yaml
# values.yaml
tuning:
  numWorkers: 5
  cacheTTL: 240
  policyCacheTTL: 60  # Security-sensitive, used by webhook & workers
  periodicReconcileInterval: 45
  queue:
    maxRetries: 7
    maxRetryDelay: 180
```

```bash
helm upgrade kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --values my-tuning.yaml
```

#### Via Helm Set

```bash
helm upgrade kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --set tuning.numWorkers=8 \
  --set tuning.cacheTTL=180 \
  --reuse-values
```

### Tuning by Scale

#### Small Deployment (< 5,000 templates)

**Default values are optimal**
- Workers: 3
- Cache TTL: 300s
- Expected: ⭐⭐⭐⭐⭐ Excellent (80ms latency)

#### Medium Deployment (5,000-15,000 templates)

```yaml
tuning:
  numWorkers: 5
  cacheTTL: 240
  periodicReconcileInterval: 50
```

Expected: ⭐⭐⭐⭐ Optimal (100ms latency)

#### Large Deployment (15,000-30,000 templates)

```yaml
tuning:
  numWorkers: 8
  cacheTTL: 180
  periodicReconcileInterval: 45
  queue:
    maxRetries: 7
    maxRetryDelay: 180

resources:
  limits:
    cpu: 4000m
    memory: 1Gi
```

Expected: ⭐⭐⭐ Good (120ms latency)

**Additional requirements**:
- Increase HPA max replicas to 15
- Monitor queue depth (alert if > 500)

### Use Case Tuning

#### High-Throughput (Burst Creation)

```yaml
tuning:
  numWorkers: 8
  cacheTTL: 240
  queue:
    maxRetries: 7
```

**Best for**: CI/CD pipelines, automated deployments

#### Fast Drift Detection

```yaml
tuning:
  numWorkers: 5
  cacheTTL: 120
  periodicReconcileInterval: 30
```

**Best for**: Security-critical workloads, compliance

#### Resource-Constrained

```yaml
tuning:
  numWorkers: 2
  cacheTTL: 600
  periodicReconcileInterval: 120

resources:
  limits:
    cpu: 1000m
    memory: 256Mi
```

**Best for**: Development clusters, cost optimization

See [Performance Documentation](docs/performance.md) for detailed tuning guide.

---

## Monitoring and Troubleshooting

### Monitoring Templates

#### Check Status

```bash
# List all templates with status
kubectl get kubetemplate --all-namespaces -o wide

# Detailed status (includes events)
kubectl describe kubetemplate myapp -n default

# Watch for changes
kubectl get kubetemplate myapp -w

# View Kubernetes events (NEW in v0.6.2)
kubectl get events --field-selector involvedObject.name=myapp
kubectl get events -n default | grep myapp
```

#### Status Fields

```yaml
status:
  processingPhase: "Completed"       # Queued|Processing|Completed|Failed|Paused
  status: "All resources applied"    # Detailed message
  queuedAt: "2025-12-13T10:00:00Z"
  processedAt: "2025-12-13T10:00:05Z"
  resourcesTotal: 3
  resourcesSynced: 3
  retryCount: 0
  retryCycle: 0                       # Current retry cycle (v0.6.1+)
  appliedSpecHash: "a3f5e8..."       # SHA256 hash of applied spec
  pausedAt: null                      # Timestamp when paused (if applicable)
  pausedReason: ""                    # Reason for pause
  lastReconcileTime: "2025-12-13T10:05:00Z"
  driftDetectionCount: 0
  dryRunChecks: 5                     # Counter for periodic dry-run checks
  lastDriftDetected: null
```

#### Kubernetes Events (v0.6.2+)

KubeTemplater emits Kubernetes Warning events for important operational events:

```bash
# View events for a specific template
kubectl describe kubetemplate myapp | grep Events: -A 10

# Example event output:
# Events:
#   Type     Reason           Age   From                    Message
#   ----     ------           ----  ----                    -------
#   Warning  TemplatePaused   5m    kubetemplater-worker   Template automatically paused after 3 failed retry cycles. Manual intervention required.
```

**Event Types**:
- `TemplatePaused`: Template auto-paused after max retry cycles exceeded

### Common Issues

#### 1. Template Stuck in Queued

**Symptom**: Template remains in "Queued" state

**Causes**:
- High queue depth (worker overload)
- All workers busy

**Solutions**:
```bash
# Check queue depth
kubectl get kubetemplate --all-namespaces | grep -c Queued

# If > 100, increase workers
helm upgrade kubetemplater ./charts/kubetemplater \
  --set tuning.numWorkers=8 \
  --reuse-values

# Check operator logs
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater --tail=50
```

#### 2. Template Failed

**Symptom**: processingPhase = "Failed"

**Causes**:
- Policy violation
- Resource creation error
- API server issues

**Solutions**:
```bash
# Check status message
kubectl get kubetemplate myapp -o jsonpath='{.status.status}'

# Check operator logs
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater | grep myapp

# Common errors:
# - "no KubeTemplatePolicy found" → Create policy
# - "Resource X is not allowed" → Update policy
# - "namespace Y not allowed" → Add namespace to policy
```

#### 3. Template Paused

**Symptom**: processingPhase = "Paused"

**Causes**:
- Reached max retry cycles (default: 3 cycles ≈ 15 minutes)
- Persistent errors preventing template processing

**Solutions**:
```bash
# Check pause reason
kubectl get kubetemplate myapp -o jsonpath='{.status.pausedReason}'

# Check when paused
kubectl get kubetemplate myapp -o jsonpath='{.status.pausedAt}'

# Fix underlying issue, then resume
kubectl annotate kubetemplate myapp kubetemplater.io/resume=true

# Template will be requeued and processing resumes
# If issue persists, it will pause again after max cycles
```

**Note**: Paused templates require manual intervention. Fix the underlying issue before resuming to avoid immediate re-pause.

#### 4. Webhook Validation Failing

**Symptom**: Template rejected immediately on creation

**Causes**:
- No policy for namespace
- Resource type not allowed
- Field validation failure

**Solutions**:
```bash
# Error shows in kubectl output:
# Error: admission webhook denied: no KubeTemplatePolicy found for namespace default

# Create policy
kubectl apply -f policy.yaml

# Verify policy exists
kubectl get kubetemplatepolicy -n kubetemplater-system

# Check webhook is running
kubectl get validatingwebhookconfiguration
kubectl get pods -n kubetemplater-system
```

#### 4. Drift Not Detected

**Symptom**: Manual changes not corrected

**Causes**:
- Template not in "Completed" state
- Periodic reconciliation interval too long
- Only metadata changed (not drift)

**Solutions**:
```bash
# Check template status
kubectl get kubetemplate myapp -o jsonpath='{.status.processingPhase}'
# Must be "Completed"

# Reduce reconciliation interval
helm upgrade kubetemplater ./charts/kubetemplater \
  --set tuning.periodicReconcileInterval=30 \
  --reuse-values

# Force reconciliation (edit and save)
kubectl annotate kubetemplate myapp force-reconcile="$(date)"
```

### Operator Logs

```bash
# View live logs
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater -f

# Filter by template
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater | grep "template-name"

# Check tuning parameters
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater | grep "Tuning parameters"
```

Expected output:
```
Tuning parameters configured numWorkers=3 cacheTTL=5m0s periodicReconcileInterval=1m0s ...
```

### Metrics (Prometheus)

If Prometheus is enabled:

```promql
# Queue depth
kubetemplater_queue_depth

# Processing rate
rate(kubetemplater_processed_total[5m])

# Cache hit rate
kubetemplater_cache_hits / (kubetemplater_cache_hits + kubetemplater_cache_misses)

# Webhook latency P95
histogram_quantile(0.95, rate(kubetemplater_webhook_duration_seconds_bucket[5m]))
```

---

## Best Practices

### 1. Policy Design

✅ **DO**:
- One policy per namespace
- Use descriptive policy names (`app-prod-policy` not `policy-1`)
- Document allowed resources in annotations
- Test policies in dev before prod

❌ **DON'T**:
- Create multiple policies for same namespace
- Grant overly permissive target namespaces
- Skip field validations for production

### 2. Template Organization

✅ **DO**:
- Group related resources in one template
- Use meaningful template names
- Add labels for filtering (`app`, `team`, `environment`)
- Version your templates (annotations)

❌ **DON'T**:
- Create huge templates (>50 resources)
- Mix unrelated resources
- Use generic names (`template-1`)

**Example**:
```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: myapp-v1.2.0  # Versioned
  namespace: production
  labels:
    app: myapp
    team: platform
    environment: production
  annotations:
    description: "MyApp deployment with database and cache"
    version: "1.2.0"
spec:
  templates:
    # Related resources: app, db, cache
```

### 3. Security

✅ **DO**:
- Enable field validations in production
- Require security contexts
- Forbid privileged containers
- Enforce resource limits
- Use image tag restrictions

❌ **DON'T**:
- Use `failurePolicy: Ignore` in production
- Allow `latest` tags
- Skip resource limit validation

### 4. Performance

✅ **DO**:
- Tune workers based on load
- Monitor queue depth
- Use appropriate cache TTL
- Enable HPA for large deployments

❌ **DON'T**:
- Over-provision workers (>10 per pod)
- Set very low cache TTL (<60s)
- Ignore high queue depth

### 5. Operational

✅ **DO**:
- Test in dev/staging first
- Monitor drift detection metrics
- Set up alerts on queue depth
- Document your policies
- Use GitOps for policies and templates

❌ **DON'T**:
- Apply untested templates to production
- Ignore failed templates
- Deploy without monitoring

---

## Advanced Topics

### Drift Detection

KubeTemplater automatically detects and corrects configuration drift:

**How it works**:
1. Every 60 seconds (configurable), completed templates are reconciled
2. Resources are re-applied using Server-Side Apply (SSA)
3. Changes from external sources are detected and corrected
4. Drift counter increments on correction

**View drift statistics**:
```bash
kubectl get kubetemplate myapp -o jsonpath='{.status.driftDetectionCount}'
kubectl get kubetemplate myapp -o jsonpath='{.status.lastDriftDetected}'
```

**Configure interval**:
```bash
helm upgrade kubetemplater ./charts/kubetemplater \
  --set tuning.periodicReconcileInterval=30 \
  --reuse-values
```

### Resource Lifecycle Management

Control whether resources are deleted with template:

```yaml
spec:
  templates:
    - referenced: true  # Resources deleted with template (default: false)
      object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
```

**Behavior**:
- `referenced: true`: OwnerReference added, resources deleted with template
- `referenced: false`: No OwnerReference, resources survive template deletion

### Replace Strategy

Handle immutable field updates:

```yaml
metadata:
  annotations:
    kubetemplater.io/replace-enabled: "true"
spec:
  templates:
    - object:
        # Resource with immutable fields
```

**Behavior**: If update fails due to immutable field, resource is deleted and recreated.

**Warning**: Use with caution - causes downtime!

### Multi-Namespace Templates

Create resources in multiple namespaces (if policy allows):

```yaml
# Policy allows multiple target namespaces
spec:
  sourceNamespace: platform
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces:
        - namespace-a
        - namespace-b
```

```yaml
# Template creates resources in different namespaces
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: config
          namespace: namespace-a  # Explicit namespace
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: config
          namespace: namespace-b  # Different namespace
```

### GitOps Integration

**Argo CD Example**:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp-templates
spec:
  source:
    repoURL: https://github.com/myorg/myapp
    path: templates/
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: myapp
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

**Flux Example**:
```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: myapp-templates
spec:
  interval: 1m
  url: https://github.com/myorg/myapp
  ref:
    branch: main
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: myapp-templates
spec:
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: myapp-templates
  path: ./templates
  prune: true
```

---

## FAQ

### General

**Q: Can I use KubeTemplater with other operators?**  
A: Yes! KubeTemplater works alongside other operators. Drift detection only affects fields managed by the template.

**Q: What happens if I delete a policy?**  
A: Existing templates continue working, but new templates will be rejected. Update templates to use new policy or recreate policy.

**Q: Can I update templates?**  
A: Yes, edit and apply. Resources will be updated. Failed updates trigger retry with exponential backoff.

**Q: How many templates can KubeTemplater handle?**  
A: 15,000-30,000 templates with default configuration. See [Performance Documentation](docs/performance.md) for scaling guidance.

### Performance

**Q: Why is my template stuck in Queued?**  
A: High queue depth. Increase workers or check operator logs for errors.

**Q: How do I reduce API server load?**  
A: Increase `CACHE_TTL` (reduces policy lookups) and `PERIODIC_RECONCILE_INTERVAL` (reduces drift checks).

**Q: When should I increase workers?**  
A: When queue depth consistently >100 or processing is slow. Start with 5, max 10 per pod.

### Security

**Q: Are field validations enforced for updates?**  
A: Yes, every template change is validated by the webhook.

**Q: Can I bypass validation?**  
A: No, webhook validation is mandatory. This ensures policy compliance.

**Q: What if webhook is down?**  
A: By default (`failurePolicy: Fail`), templates are rejected. For development, you can set `failurePolicy: Ignore`.

### Troubleshooting

**Q: Template shows "Failed" - how do I debug?**  
A: Check status message: `kubectl get kubetemplate myapp -o jsonpath='{.status.status}'`. Check operator logs for detailed error.

**Q: Drift not being corrected?**  
A: Ensure template is "Completed". Drift detection only works on completed templates. Check `periodicReconcileInterval` setting.

**Q: How do I force reconciliation?**  
A: Add annotation: `kubectl annotate kubetemplate myapp force-reconcile="$(date)"`

---

## Additional Resources

### Documentation
- [Architecture](docs/architecture.md)
- [How It Works](docs/how-it-works.md)
- [Performance Guide](docs/performance.md)
- [Features](docs/features.md)
- [Examples](docs/examples.md)

### Guides
- [Tuning Guide](TUNING_GUIDE.md)
- [Getting Started](docs/getting-started.md)
- [Webhook Validation](docs/webhook-validation.md)

### Source Code
- [GitHub Repository](https://github.com/ariellpe/KubeTemplater)
- [Issue Tracker](https://github.com/ariellpe/KubeTemplater/issues)
- [Releases](https://github.com/ariellpe/KubeTemplater/releases)

### Community
- [Discussions](https://github.com/ariellpe/KubeTemplater/discussions)
- [Contributing Guide](CONTRIBUTING.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)

---

## Version History

- **v0.6.0** (2025-12-13): Configurable performance tuning
- **v0.5.2** (2025-12-12): Semantic drift detection, status update improvements
- **v0.5.1** (2025-12-12): Drift detection with periodic reconciliation
- **v0.3.3** (2025-12-xx): Watch-based certificate discovery
- **v0.3.2** (2025-12-xx): Self-signed certificate management
- **v0.3.0** (2025-12-xx): Performance optimizations (30-60x improvement)
- **v0.2.0** (2025-12-xx): Field-level validation support

---

**Need Help?**

- 📖 Read the [docs](docs/)
- 💬 Join [discussions](https://github.com/ariellpe/KubeTemplater/discussions)
- 🐛 Report [issues](https://github.com/ariellpe/KubeTemplater/issues)
- 📧 Contact: [GitHub Issues](https://github.com/ariellpe/KubeTemplater/issues)

**Happy Templating! 🚀**
