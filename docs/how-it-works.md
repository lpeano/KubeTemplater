# How KubeTemplater Works

KubeTemplater is designed to be simple and powerful. It automates the management of Kubernetes resources using custom resources with built-in policy enforcement.

This document explains the core reconciliation logic of the operator.

## The Reconciliation Loop

The operator continuously performs the following steps:

### 1. Watch for KubeTemplate Resources

KubeTemplater watches all `KubeTemplate` custom resources across all namespaces in the Kubernetes cluster.

### 2. Admission Webhook Validation

Before a `KubeTemplate` is accepted into the cluster, the validation webhook intercepts the request and performs the following checks:

- Verifies a `KubeTemplatePolicy` exists for the source namespace
- Validates each resource's GVK (Group/Version/Kind) is allowed by the policy
- Checks target namespaces are permitted
- Evaluates CEL validation rules if defined
- Returns admission decision (allow/deny) with detailed error messages

**Webhook Certificate Management (v0.3.3)**:
- Watch-based discovery: Monitors Lease resource for certificate updates (<100ms latency)
- Hash verification: SHA256 comparison ensures synchronized certificates
- Thread-safe operations: All race conditions resolved
- Zero downtime: Seamless transitions during certificate renewal

If validation fails, the `KubeTemplate` is rejected immediately at admission time, preventing invalid configurations from entering the cluster.

### 3. Asynchronous Processing Queue (v0.3.0+)

After webhook validation, the controller enqueues the `KubeTemplate` for asynchronous processing:

- **Non-blocking**: Controller marks template as "Queued" and returns immediately
- **Worker Pool**: 3 parallel workers process templates from priority queue
- **Automatic Retry**: Failed templates retried up to 5 times with exponential backoff (1s → 5min)
- **Policy Caching**: In-memory cache reduces API calls by 95%

### 4. Policy Lookup

The worker retrieves the matching `KubeTemplatePolicy` from the in-memory cache (5-minute TTL) or Kubernetes API. The policy is matched based on the `sourceNamespace` field matching the `KubeTemplate`'s namespace.

If no policy is found or multiple policies match, the reconciliation fails with an error status.

### 4. Process Templates

For each template in `spec.templates`, the controller:

### 4. Process Templates

For each template in `spec.templates`, the controller:

- Unmarshals the `object` field (which contains a raw Kubernetes manifest)
- Sets the namespace if not specified (defaults to the `KubeTemplate`'s namespace)
- Validates the resource against the policy:
  - Checks if the GVK is allowed
  - Verifies the target namespace is in the allowed list
  - Evaluates CEL rules if present
- Applies the resource using Server-Side Apply

**Example Template:**
```yaml
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
                image: nginx:1.21
    - replace: true  # Optional: force delete/recreate for immutable fields
      object:
        apiVersion: v1
        kind: Service
        metadata:
          name: my-service
        spec:
          selector:
            app: nginx
          ports:
            - port: 80
```

### 5. Apply with Server-Side Apply (SSA)

### 5. Apply with Server-Side Apply (SSA)

After validating the resource, the operator uses **Server-Side Apply** to apply it to the cluster.

**Why Server-Side Apply?**
- **Declarative**: SSA allows the operator to declaratively state the desired end-state of the resource.
- **Ownership**: The operator becomes a "manager" of the fields it controls through the `kubetemplater` field manager.
- **Efficiency**: Only the specified fields are sent, and the API server handles merging logic.
- **Conflict Resolution**: Other controllers or users can manage different fields without conflicts.

If the resource does not exist, it will be created. If it already exists, it will be updated only if there are differences.

### 6. Handle Immutable Fields (Replace Mode)

Some Kubernetes resources have immutable fields that cannot be updated. For these cases, KubeTemplater supports a `replace` flag:

```yaml
templates:
  - replace: true  # Enable replace mode
    object:
      apiVersion: batch/v1
      kind: CronJob
      # ...
```

When `replace: true` is set:
1. The operator first attempts a normal Server-Side Apply
2. If the API returns an immutable field error, the operator deletes the resource
3. Then recreates it with the new configuration

This ensures that even resources with immutable fields can be updated through KubeTemplater.

### 7. Status Update

After processing all templates, the controller updates the `KubeTemplate.status.status` field:
- `"Completed"` - All resources applied successfully
- `"Error: ..."` - Detailed error message if something failed

This entire process ensures that the resources in your cluster are always in sync with the `KubeTemplate` definitions, while enforcing security policies through `KubeTemplatePolicy`.
