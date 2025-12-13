# Validation Webhook

KubeTemplater implements a **Validation Webhook** that validates `KubeTemplate` resources at creation and update time, ensuring they comply with the defined `KubeTemplatePolicy` before they are accepted into the cluster.

## Overview

The validation webhook provides **early validation** of KubeTemplate resources, preventing invalid configurations from being created. This shifts validation from reconciliation time to admission time, providing immediate feedback to users.

## What the Webhook Validates

The webhook performs the following validation checks:

### 1. Policy Existence

The webhook ensures that a `KubeTemplatePolicy` exists for the source namespace:

- **Checks**: A policy must exist in the operator namespace with a `sourceNamespace` matching the KubeTemplate's namespace
- **Rejects**: Resources without a matching policy
- **Rejects**: Resources with multiple matching policies (ambiguous configuration)

### 2. Resource Type Validation

For each template in the `KubeTemplate.spec.templates` array:

- **Checks**: The resource's GVK (Group/Version/Kind) is allowed by the policy's `validationRules`
- **Rejects**: Resource types not explicitly allowed in the policy

### 3. Target Namespace Validation

- **Checks**: The target namespace is in the `targetNamespaces` list for the resource type
- **Rejects**: Resources targeting namespaces not allowed by the policy
- **Rejects**: Resources when the policy has no target namespaces defined

### 4. CEL Rule Validation

If a validation rule contains a CEL expression:

- **Evaluates**: The CEL rule against the resource object
- **Rejects**: Resources that fail CEL validation (rule evaluates to false)
- **Rejects**: Resources if the CEL rule has syntax errors

### 5. Warnings

The webhook provides warnings (not rejections) for:

- **Replace Mode**: When `replace: true` is set, warning users that the resource will be deleted and recreated on immutable field changes

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│  User applies KubeTemplate                                   │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes API Server                                       │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│  Validation Webhook called                                   │
│  POST /validate-kubetemplater-io-v1alpha1-kubetemplate      │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│  Webhook Validator Logic:                                    │
│  1. Find matching KubeTemplatePolicy                        │
│  2. Validate each template:                                 │
│     - Check GVK is allowed                                  │
│     - Check target namespace is allowed                     │
│     - Evaluate CEL rules                                    │
│  3. Return admission response                               │
└─────────────────────┬───────────────────────────────────────┘
                      │
         ┌────────────┴────────────┐
         ▼                         ▼
    ┌─────────┐              ┌─────────┐
    │ Allowed │              │ Rejected│
    └────┬────┘              └────┬────┘
         │                        │
         ▼                        ▼
┌─────────────────┐      ┌─────────────────┐
│ Resource saved  │      │ Error returned  │
│ to etcd         │      │ to user         │
└─────────────────┘      └─────────────────┘
```

## Configuration

The webhook is automatically configured when you deploy KubeTemplater with the webhook enabled.

### Webhook Configuration

The `ValidatingWebhookConfiguration` is created with the following settings:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: kubetemplater-validating-webhook-configuration
webhooks:
- admissionReviewVersions: [v1]
  clientConfig:
    service:
      name: kubetemplater-webhook-service
      namespace: kubetemplater-system
      path: /validate-kubetemplater-io-v1alpha1-kubetemplate
  failurePolicy: Fail  # Reject resources if webhook is unavailable
  name: vkubetemplate.kb.io
  rules:
  - apiGroups: [kubetemplater.io]
    apiVersions: [v1alpha1]
    operations: [CREATE, UPDATE]
    resources: [kubetemplates]
  sideEffects: None
```

### Failure Policy

The webhook uses `failurePolicy: Fail`, meaning:

- If the webhook is unavailable, KubeTemplate resources will be **rejected**
- This ensures that no invalid resources can be created during webhook downtime
- For high availability, run multiple replicas of the operator

## Example Validation Scenarios

### ✅ Valid KubeTemplate

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: valid-template
  namespace: app-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: my-config
      data:
        key: value
```

**Policy:**
```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: app-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: app-namespace
  validationRules:
  - kind: ConfigMap
    group: ""
    version: v1
    targetNamespaces: [app-namespace]
```

**Result**: ✅ Accepted

---

### ❌ Invalid: No Policy

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-template
  namespace: no-policy-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: my-config
```

**Result**: ❌ Rejected
```
Error: no KubeTemplatePolicy found for source namespace no-policy-namespace
```

---

### ❌ Invalid: Disallowed Resource Type

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-template
  namespace: app-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: Secret  # Not allowed by policy
      metadata:
        name: my-secret
```

**Policy** (only allows ConfigMaps):
```yaml
spec:
  sourceNamespace: app-namespace
  validationRules:
  - kind: ConfigMap
    group: ""
    version: v1
    targetNamespaces: [app-namespace]
```

**Result**: ❌ Rejected
```
Error: template[0]: resource type /v1, Kind=Secret is not allowed by policy
```

---

### ❌ Invalid: Wrong Target Namespace

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-template
  namespace: app-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: my-config
        namespace: forbidden-namespace  # Not in allowed list
```

**Policy**:
```yaml
spec:
  sourceNamespace: app-namespace
  validationRules:
  - kind: ConfigMap
    group: ""
    version: v1
    targetNamespaces: [app-namespace, allowed-namespace]  # forbidden-namespace not here
```

**Result**: ❌ Rejected
```
Error: template[0]: resource namespace forbidden-namespace is not in the allowed target namespaces
```

---

### ❌ Invalid: Failed CEL Validation

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-template
  namespace: app-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: Secret
      metadata:
        name: bad-name  # Doesn't start with 'secure-'
      type: Opaque
      data:
        key: dmFsdWU=
```

**Policy** (requires name to start with 'secure-'):
```yaml
spec:
  sourceNamespace: app-namespace
  validationRules:
  - kind: Secret
    group: ""
    version: v1
    rule: "object.metadata.name.startsWith('secure-')"
    targetNamespaces: [app-namespace]
```

**Result**: ❌ Rejected
```
Error: template[0]: resource /v1, Kind=Secret/bad-name failed CEL validation rule
```

---

### ⚠️ Warning: Replace Enabled

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: template-with-replace
  namespace: app-namespace
spec:
  templates:
  - object:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: my-config
    replace: true  # Replace mode enabled
```

**Result**: ✅ Accepted with warning
```
Warning: template[0]: replace is enabled for /v1, Kind=ConfigMap/my-config. 
The resource will be deleted and recreated if immutable fields are changed
```

## Benefits of Webhook Validation

1. **Fast Feedback**: Users get immediate validation errors instead of waiting for reconciliation
2. **Prevents Invalid State**: Invalid resources never enter the cluster
3. **Better UX**: Clear, structured error messages at creation time
4. **Security**: Enforces policies before resources are persisted
5. **Audit**: All validation attempts are logged
6. **GitOps Friendly**: Invalid resources fail in CI/CD pipelines before deployment

## Debugging Webhook Issues

### Check Webhook Status

```bash
kubectl get validatingwebhookconfigurations
kubectl describe validatingwebhookconfiguration kubetemplater-validating-webhook-configuration
```

### Check Webhook Service

```bash
kubectl get svc -n kubetemplater-system kubetemplater-webhook-service
kubectl get endpoints -n kubetemplater-system kubetemplater-webhook-service
```

### Check Webhook Logs

```bash
kubectl logs -n kubetemplater-system -l control-plane=controller-manager -f
```

### Test Webhook Manually

```bash
# Try to create a KubeTemplate
kubectl apply -f my-template.yaml

# Check for validation errors in the output
```

## Integration with Controller

The webhook and controller work together:

1. **Webhook** (admission time): Validates structure and policy compliance
2. **Controller** (reconciliation time): Applies validated resources to the cluster

This separation ensures:
- Invalid resources never reach the controller
- The controller can focus on resource application, not validation
- Users get immediate feedback on policy violations

## Webhook vs Controller Validation

| Aspect | Webhook | Controller |
|--------|---------|------------|
| **When** | At admission (CREATE/UPDATE) | During reconciliation |
| **Speed** | Immediate | Asynchronous |
| **Feedback** | Synchronous error to user | Status field update |
| **Blocks Resource** | Yes (on failure) | No (resource created) |
| **Use Case** | Policy enforcement | Resource application |

## Requirements

- Kubernetes 1.16+ (for admissionregistration.k8s.io/v1)
- Valid TLS certificates for webhook endpoint
- Network connectivity from API server to webhook service

## Certificate Management

The webhook requires TLS certificates. Options:

1. **cert-manager** (recommended): Automatically manages and rotates certificates
2. **Manual certificates**: Provide your own certificates via Secret
3. **Self-signed**: Controller-runtime generates self-signed certificates (dev only)

To enable cert-manager, uncomment the `[CERTMANAGER]` sections in `config/default/kustomization.yaml`.
