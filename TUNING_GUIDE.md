# KubeTemplater Tuning Guide

## Quick Reference: Configurable Parameters

All performance parameters are configurable via environment variables in `config/manager/manager.yaml`.

### Parameters Overview

| Parameter | Default | Min | Max | Purpose |
|-----------|---------|-----|-----|---------|
| `NUM_WORKERS` | 3 | 1 | 20 | Worker pool size |
| `CACHE_TTL` | 300s | 60s | 600s | Policy cache lifetime |
| `PERIODIC_RECONCILE_INTERVAL` | 60s | 30s | 300s | Drift detection interval |
| `QUEUE_MAX_RETRIES` | 5 | 1 | 10 | Retry attempts before cooldown |
| `QUEUE_INITIAL_RETRY_DELAY` | 1s | 1s | 10s | Initial retry delay |
| `QUEUE_MAX_RETRY_DELAY` | 300s | 60s | 600s | Maximum retry delay cap |

## Configuration by Deployment Scale

### 🟢 Small Deployment (< 5,000 templates)

**Default configuration is optimal**

```yaml
env:
  - name: NUM_WORKERS
    value: "3"
  - name: CACHE_TTL
    value: "300"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "60"
  - name: QUEUE_MAX_RETRIES
    value: "5"
```

**Expected Performance:**
- Queue depth: < 50 items
- Webhook latency: ~80ms
- API calls: 50-100/min
- Status: ⭐⭐⭐⭐⭐ Excellent

### 🟡 Medium Deployment (5,000-15,000 templates)

**Increase workers for better throughput**

```yaml
env:
  - name: NUM_WORKERS
    value: "5"
  - name: CACHE_TTL
    value: "240"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "50"
  - name: QUEUE_MAX_RETRIES
    value: "5"
```

**Additional Settings:**
- HPA will auto-scale to 3-5 pods
- Monitor queue depth (alert if > 300)

**Expected Performance:**
- Queue depth: 50-200 items (peak)
- Webhook latency: ~100ms
- API calls: 200-400/min
- Status: ⭐⭐⭐⭐ Optimal

### 🟠 Large Deployment (15,000-30,000 templates)

**Maximize throughput with higher worker count**

```yaml
env:
  - name: NUM_WORKERS
    value: "8"
  - name: CACHE_TTL
    value: "180"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "45"
  - name: QUEUE_MAX_RETRIES
    value: "7"
  - name: QUEUE_MAX_RETRY_DELAY
    value: "180"
```

**Additional Settings:**
- Increase HPA max replicas to 15
- Increase resources:
  ```yaml
  resources:
    limits:
      cpu: 4000m
      memory: 1Gi
  ```

**Expected Performance:**
- Queue depth: 200-500 items (peak)
- Webhook latency: ~120ms
- API calls: 500-1000/min
- Status: ⭐⭐⭐ Good (near capacity)

### 🔴 Extra Large Deployment (> 30,000 templates)

**Scale beyond single operator limits**

```yaml
env:
  - name: NUM_WORKERS
    value: "10"
  - name: CACHE_TTL
    value: "120"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "30"
  - name: QUEUE_MAX_RETRIES
    value: "10"
  - name: QUEUE_MAX_RETRY_DELAY
    value: "120"
```

**Critical Requirements:**
- HPA 15-20 pods minimum
- Resources: CPU 4000m, Memory 2Gi
- Dedicated node pool with taints
- Consider namespace sharding (multiple operators)
- Prometheus alerts on queue depth > 500

## Use Case Scenarios

### High-Throughput (Burst Creation)

For deployments with frequent template creation bursts:

```yaml
env:
  - name: NUM_WORKERS
    value: "8"
  - name: CACHE_TTL
    value: "240"
  - name: QUEUE_MAX_RETRIES
    value: "7"
```

**Best for:** CI/CD pipelines, automated deployments

### Fast Drift Detection (Critical Workloads)

For workloads requiring immediate drift correction:

```yaml
env:
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "30"
  - name: CACHE_TTL
    value: "120"
  - name: NUM_WORKERS
    value: "5"
```

**Best for:** Security-critical resources, compliance workloads

### Resource-Constrained Environment

For minimal resource footprint:

```yaml
env:
  - name: NUM_WORKERS
    value: "2"
  - name: CACHE_TTL
    value: "600"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "120"
```

```yaml
resources:
  limits:
    cpu: 1000m
    memory: 256Mi
```

**Best for:** Development clusters, cost optimization

### Stable Production Environment

For stable production with rare policy changes:

```yaml
env:
  - name: NUM_WORKERS
    value: "3"
  - name: CACHE_TTL
    value: "600"
  - name: PERIODIC_RECONCILE_INTERVAL
    value: "90"
  - name: QUEUE_MAX_RETRIES
    value: "5"
```

**Best for:** Production with established templates

## Parameter Details

### NUM_WORKERS

**Purpose:** Controls parallel processing capacity

**Formula:**
```
Throughput = NUM_WORKERS × items_per_second_per_worker
items_per_second_per_worker ≈ 15-50 (depends on template complexity)
```

**Guidelines:**
- 1-3 workers: Resource-constrained environments
- 3-5 workers: **Recommended for most deployments**
- 5-8 workers: High throughput requirements
- 8-10 workers: Maximum before diminishing returns
- 10+ workers: Only with increased CPU limits (4+ cores)

**Warning:** More workers = more concurrent API calls

### CACHE_TTL

**Purpose:** Controls policy freshness vs API load

**Trade-offs:**

| TTL | Accuracy | API Load | Use Case |
|-----|----------|----------|----------|
| 60s | Highest | High | Rapidly changing policies |
| 180s | High | Medium | Active policy management |
| 300s | Good | Low | **Recommended default** |
| 600s | Lower | Very Low | Stable environments |

**When to reduce:**
- Frequent policy updates (multiple per hour)
- Security-critical enforcement
- Development/testing with rapid iterations

**When to increase:**
- Stable production with rare policy changes
- API server under load
- Cost optimization

### PERIODIC_RECONCILE_INTERVAL

**Purpose:** Controls drift detection frequency

**Impact:**
- Lower values = Faster drift detection but more CPU
- Higher values = Less CPU but slower drift correction

**Recommendations:**
- Critical workloads: 30-45s
- Normal workloads: 60s (default)
- Low-priority: 120s

**Note:** Automatically skips reconciliation if processed within half the interval to prevent tight loops.

### QUEUE_MAX_RETRIES

**Purpose:** Controls retry persistence

**Impact:**
- Higher values = More persistent but longer queue retention
- Lower values = Faster failure but less resilience

**Recommendations:**
- Standard: 5 (default)
- High reliability: 7-10
- Fast fail: 3

**Behavior:** After max retries, item enters cooldown period (QUEUE_MAX_RETRY_DELAY) before retry counter resets.

### QUEUE_INITIAL_RETRY_DELAY

**Purpose:** Controls exponential backoff starting point

**Formula:** `delay = INITIAL_DELAY × 2^retry_count`

**Example progression (1s initial):**
- Retry 1: 1s
- Retry 2: 2s
- Retry 3: 4s
- Retry 4: 8s
- Retry 5: 16s

**Recommendations:**
- Aggressive: 1s (default)
- Conservative: 2-5s (for rate-limited APIs)

### QUEUE_MAX_RETRY_DELAY

**Purpose:** Caps exponential backoff maximum

**Impact:**
- Lower = Faster recovery cycles
- Higher = More conservative during persistent failures

**Recommendations:**
- Fast recovery: 120-180s
- Normal: 300s (default)
- Conservative: 600s

## Monitoring and Alerts

### Recommended Metrics

Monitor these metrics for optimal performance:

1. **Queue Depth**
   - Alert if > 500 for 5+ minutes
   - Indicates worker pool saturation

2. **Cache Hit Rate**
   - Alert if < 90%
   - Indicates potential TTL tuning needed

3. **Processing Time (P95)**
   - Alert if > 30 seconds
   - Indicates complex templates or API slowness

4. **Retry Rate**
   - Alert if > 5%
   - Indicates underlying issues

### Prometheus Queries

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

## Troubleshooting

### High Queue Depth (> 500)

**Solutions:**
1. Increase NUM_WORKERS
2. Increase HPA max replicas
3. Check for slow API server responses
4. Verify templates are not too complex

### High API Call Rate

**Solutions:**
1. Verify cache is working (check hit rate)
2. Increase CACHE_TTL if acceptable
3. Check for policy churn (frequent updates)

### Slow Processing (> 30s)

**Solutions:**
1. Check CEL expression complexity
2. Verify network connectivity to API server
3. Increase resource limits
4. Check for large template sizes

## Additional Resources

- [Performance Documentation](docs/performance.md) - Comprehensive performance guide
- [README.md](README.md) - Quick start and overview
- [CHANGELOG.md](CHANGELOG.md) - Version history and changes

## Summary

KubeTemplater v0.6.0+ delivers **flexible performance tuning** through:
- ✅ Configurable worker pool (1-20 workers)
- ✅ Dynamic cache TTL (60-600s)
- ✅ Adjustable drift detection (30-300s)
- ✅ Customizable retry strategy
- ✅ Zero rebuilds required for tuning

**Recommended capacity**: 15,000-30,000 KubeTemplates with excellent performance and reliability.
