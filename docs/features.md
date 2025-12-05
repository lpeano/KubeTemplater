# Advanced Features

This section covers advanced features of KubeTemplater that help you handle more complex use cases.

## Validation Webhook

### Overview

KubeTemplater includes a **validation webhook** that validates `KubeTemplate` resources at admission time, before they are persisted to the cluster. This provides immediate feedback to users and prevents invalid configurations from being accepted.

### How It Works

When you create or update a `KubeTemplate`, the Kubernetes API server sends the resource to the validation webhook. The webhook:

1. Checks that a `KubeTemplatePolicy` exists for the source namespace
2. Validates each template against the policy rules
3. Checks GVK (Group/Version/Kind) permissions
4. Validates target namespace restrictions
5. Evaluates CEL expressions if defined
6. Returns an admission decision (allow/deny) with detailed error messages

### Benefits

- **Fast Feedback**: Immediate validation errors instead of waiting for reconciliation
- **Security**: Enforces policies before resources enter the cluster
- **Better UX**: Clear, structured error messages
- **Prevents Invalid State**: Invalid resources are rejected at the gate

For detailed information, see [Webhook Validation Documentation](webhook-validation.md) and [Webhook Example](webhook-example.md).

---

## Replace Mode for Immutable Resources

### The Problem

Some Kubernetes resources contain fields that are **immutable**, meaning they cannot be changed after the resource has been created. A common example is the `selector` field in a `Service` or the `jobTemplate` within a `CronJob`.

If you try to update one of these immutable fields, the Kubernetes API server will reject the update, and the KubeTemplater operator's `Server-Side Apply` will fail. This would normally require you to manually delete the old resource and re-apply the new one.

### The Solution: `replace: true`

To solve this, KubeTemplater provides a "replace" strategy that can be enabled per template in the `KubeTemplate` spec.

To enable this feature, set the `replace: true` field on a template entry:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-immutable-cronjob
  namespace: default
spec:
  templates:
    - replace: true  # Enable replace mode
      object:
        apiVersion: batch/v1
        kind: CronJob
        metadata:
          name: my-cronjob
        spec:
          schedule: "*/5 * * * *"
          jobTemplate:
            # The 'jobTemplate' is an immutable field
            spec:
              template:
                spec:
                  containers:
                  - name: hello
                    image: busybox
                    command: ["echo", "Hello, world!"]
                  restartPolicy: OnFailure
```

### How It Works

1.  The operator first attempts to apply the manifest using the standard `Server-Side Apply`.
2.  If the Kubernetes API server returns an error indicating that an immutable field cannot be changed, the operator checks if the template has `replace: true` set.
3.  If replace mode is enabled, the operator will:
    a. **Delete** the existing resource from the cluster.
    b. **Re-create** the resource by applying the new manifest.

This automated delete-and-recreate cycle ensures that changes to immutable fields are applied successfully, keeping your infrastructure aligned with its configuration in a fully automated way.

---

## Target Namespace Control

### The Problem

By default, all resources in a `KubeTemplate` are created in their specified namespaces as defined in the manifest's `metadata.namespace` field. To enable cross-namespace resource creation, you need appropriate policy permissions.

### The Solution: Policy-Based Namespace Targeting

You control which namespaces can be targeted using the `KubeTemplatePolicy`'s `targetNamespaces` field in validation rules.

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default
  validationRules:
    - kind: Secret
      group: ""
      version: v1
      targetNamespaces: [default, kube-system]  # Allow both namespaces
```

Then in your `KubeTemplate`, specify the target namespace in the resource manifest:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-cross-namespace-secret
  namespace: default  # Source namespace
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: my-special-secret
          namespace: kube-system  # Target namespace
        type: Opaque
        stringData:
          key: value
```

### How It Works

1.  The admission webhook validates that the target namespace is allowed in the policy's `targetNamespaces` list.
2.  The controller creates or updates the resource in the specified namespace.
3.  If the target namespace is not in the allowed list, the validation fails and the `KubeTemplate` is rejected.

#### Garbage Collection

*   **For same-namespace resources**: KubeTemplater sets an `OwnerReference` on the created resource. If you delete the `KubeTemplate`, Kubernetes will automatically delete the child resource.
*   **For cross-namespace resources**: KubeTemplater **cannot** set an `OwnerReference` (Kubernetes limitation). The operator tracks these resources and deletes them when the `KubeTemplate` is removed.
---

## Security Policies

### The Problem

In a multi-tenant environment, it is important to control what resources can be created in each namespace. For example, you might want to prevent users from creating `ClusterRole` or `ClusterRoleBinding` resources, or you might want to restrict the creation of resources to specific namespaces.

### The Solution: `KubeTemplatePolicy`

KubeTemplater introduces the `KubeTemplatePolicy` custom resource, which allows you to define fine-grained security policies based on the source namespace.

A `KubeTemplatePolicy` defines which resources can be created by `KubeTemplate` resources from a specific namespace.

Here is an example of a `KubeTemplatePolicy`:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default  # Policy applies to KubeTemplates in the 'default' namespace
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces: [default]
    - kind: Secret
      group: ""
      version: v1
      targetNamespaces: [default]
      rule: "object.metadata.name.startsWith('secure-')"  # CEL expression
```

This policy:
- Applies to `KubeTemplate` resources in the `default` namespace
- Allows the creation of `ConfigMap` resources in the `default` namespace
- Allows the creation of `Secret` resources in the `default` namespace, but **only** if the name starts with `secure-`

### How It Works

**At admission time (validation webhook):**
1.  When a `KubeTemplate` is created or updated, the validation webhook finds the policy matching the source namespace.
2.  It validates each template against the policy rules:
    - Checks if the resource GVK (Group/Version/Kind) is allowed
    - Verifies the target namespace is in the `targetNamespaces` list
    - Evaluates any CEL rules if present
3.  If validation fails, the `KubeTemplate` is rejected with a detailed error message.

**During reconciliation (controller):**
1.  The controller performs the same validation to ensure consistency.
2.  Only validated resources are applied to the cluster using Server-Side Apply.

### Validation Types

The `fieldValidations` array supports multiple validation types for granular control:

#### 1. CEL Expressions

Use CEL (Common Expression Language) for complex validation logic:

```yaml
fieldValidations:
  - name: "name-prefix-check"
    fieldPath: "metadata.name"
    type: cel
    cel: "value.startsWith('prod-')"
    message: "Resource name must start with 'prod-'"
```

For object-level validation, omit `fieldPath`:

```yaml
fieldValidations:
  - name: "replicas-and-resources"
    type: cel
    cel: "object.spec.replicas <= 10 && object.spec.template.spec.containers.all(c, has(c.resources))"
    message: "Max 10 replicas and all containers must define resources"
```

#### 2. Regex Validation

Validate string fields against regex patterns:

```yaml
fieldValidations:
  - name: "dns-compliant-name"
    fieldPath: "metadata.name"
    type: regex
    regex: "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
    message: "Name must be DNS-compliant"
```

#### 3. Range Validation

Enforce numeric ranges:

```yaml
fieldValidations:
  - name: "replicas-limit"
    fieldPath: "spec.replicas"
    type: range
    min: 1
    max: 10
    message: "Replicas must be between 1 and 10"
```

#### 4. Required Fields

Enforce presence of required fields:

```yaml
fieldValidations:
  - name: "team-label-required"
    fieldPath: "metadata.labels.team"
    type: required
    message: "All resources must have a 'team' label"
```

#### 5. Forbidden Fields

Prohibit specific fields for security:

```yaml
fieldValidations:
  - name: "no-host-network"
    fieldPath: "spec.hostNetwork"
    type: forbidden
    message: "Host network is not allowed for security reasons"
```

### Multiple Validations

Combine multiple validations for comprehensive policy enforcement:

```yaml
validationRules:
  - kind: Deployment
    group: apps
    version: v1
    targetNamespaces: [production]
    fieldValidations:
      - name: "prod-prefix"
        fieldPath: "metadata.name"
        type: regex
        regex: "^prod-"
      - name: "max-replicas"
        fieldPath: "spec.replicas"
        type: range
        max: 5
      - name: "team-label"
        fieldPath: "metadata.labels.team"
        type: required
      - name: "no-privileged"
        fieldPath: "spec.template.spec.containers.0.securityContext.privileged"
        type: forbidden
```

All validations must pass for the resource to be accepted. This mechanism provides a powerful way to enforce security and compliance policies in your cluster, with validation happening at admission time to prevent invalid configurations from entering the system.