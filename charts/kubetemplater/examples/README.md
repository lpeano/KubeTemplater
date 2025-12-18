# KubeTemplater Helm Chart - Configuration Examples

This directory contains pre-configured `values.yaml` examples for different deployment scenarios.

## Available Examples

### 1. High-Throughput Configuration
**File**: [`values-high-throughput.yaml`](values-high-throughput.yaml)

**Use case**: Frequent template creation bursts, CI/CD pipelines

**Characteristics**:
- 8 workers for parallel processing
- 4-minute cache TTL (balance freshness/performance)
- 7 max retries with fast recovery (3 minutes)
- 3000m CPU, 768Mi memory

**Install**:
```bash
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f values-high-throughput.yaml
```

**Expected capacity**: 10,000-20,000 templates

---

### 2. Fast Drift Detection Configuration
**File**: [`values-fast-drift-detection.yaml`](values-fast-drift-detection.yaml)

**Use case**: Security-critical workloads, compliance requirements

**Characteristics**:
- 30-second drift detection interval (minimum)
- 2-minute cache TTL (very fresh policies)
- 5 workers (balanced)
- Strict webhook failurePolicy

**Install**:
```bash
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f values-fast-drift-detection.yaml
```

**Expected capacity**: 5,000-10,000 templates with fast drift correction

---

### 3. Resource-Constrained Configuration
**File**: [`values-resource-constrained.yaml`](values-resource-constrained.yaml)

**Use case**: Development clusters, cost optimization

**Characteristics**:
- 2 workers (minimal)
- 10-minute cache TTL (maximize cache efficiency)
- 2-minute reconciliation interval
- 1000m CPU, 256Mi memory (minimal footprint)
- 2 replicas

**Install**:
```bash
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f values-resource-constrained.yaml
```

**Expected capacity**: < 5,000 templates

---

### 4. Large-Scale Configuration
**File**: [`values-large-scale.yaml`](values-large-scale.yaml)

**Use case**: Enterprise deployments with 15,000-30,000 templates

**Characteristics**:
- 10 workers (maximum)
- 3-minute cache TTL
- 45-second reconciliation interval
- 10 max retries with fast recovery (2 minutes)
- 4000m CPU, 1Gi memory
- 5 replicas baseline
- Zone-aware anti-affinity

**Install**:
```bash
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f values-large-scale.yaml
```

**Expected capacity**: 15,000-30,000 templates

**Additional recommendations**:
- Use HPA to scale from 5 to 15-20 replicas
- Consider dedicated node pool
- Monitor queue depth (alert if > 500)

---

## Customization

You can combine these examples with your own overrides:

```bash
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f values-high-throughput.yaml \
  --set image.repository=my-registry/kubetemplater \
  --set image.tag=v0.6.0
```

Or create a custom values file by copying one of the examples:

```bash
cp values-high-throughput.yaml my-custom-values.yaml
# Edit my-custom-values.yaml
helm install kubetemplater ../. \
  --namespace kubetemplater-system \
  --create-namespace \
  -f my-custom-values.yaml
```

## Parameter Reference

See the main [Chart README](../README.md#performance-tuning) for detailed parameter descriptions.

| Parameter | Default | High-Throughput | Fast-Drift | Resource-Constrained | Large-Scale |
|-----------|---------|-----------------|------------|----------------------|-------------|
| `tuning.numWorkers` | 3 | 8 | 5 | 2 | 10 |
| `tuning.cacheTTL` | 300s | 240s | 120s | 600s | 180s |
| `tuning.periodicReconcileInterval` | 60s | 50s | 30s | 120s | 45s |
| `tuning.queue.maxRetries` | 5 | 7 | 5 | 5 | 10 |
| `tuning.queue.maxRetryDelay` | 300s | 180s | 300s | 300s | 120s |
| `replicaCount` | 3 | 3 | 3 | 2 | 5 |
| `resources.limits.cpu` | 2000m | 3000m | 2000m | 1000m | 4000m |
| `resources.limits.memory` | 512Mi | 768Mi | 512Mi | 256Mi | 1Gi |

## Monitoring

After installation, monitor these metrics to validate configuration:

```bash
# Queue depth (should stay < 500)
kubectl get kubetemplate --all-namespaces -o wide

# Pod resource usage
kubectl top pods -n kubetemplater-system

# Logs for tuning parameters
kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater --tail=20 | grep "Tuning parameters"
```

Expected log output:
```
Tuning parameters configured numWorkers=8 cacheTTL=4m0s periodicReconcileInterval=50s ...
```

## Troubleshooting

### High Queue Depth

If queue depth exceeds 500 consistently:
1. Increase `tuning.numWorkers`
2. Increase `replicaCount` or HPA maxReplicas
3. Check for slow API server responses

### High Memory Usage

If pods are OOMKilled:
1. Increase `resources.limits.memory`
2. Reduce `tuning.numWorkers`
3. Increase `tuning.cacheTTL` to reduce cache churn

### Slow Drift Detection

If drift takes too long to correct:
1. Reduce `tuning.periodicReconcileInterval` (min 30s)
2. Reduce `tuning.cacheTTL` for fresher policies
3. Check if drift detection is actually enabled

## Additional Resources

- [Performance Documentation](../../../docs/performance.md)
- [Main README](../../../README.md)
- [Tuning Guide](../../../TUNING_GUIDE.md)
