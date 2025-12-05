# Webhook Package

This package implements the validation webhook for KubeTemplater.

## Overview

The webhook validates `KubeTemplate` resources at admission time (CREATE/UPDATE operations) to ensure they comply with the defined `KubeTemplatePolicy` before being accepted into the cluster.

## Files

- `kubetemplate_webhook.go` - Main webhook implementation
- `kubetemplate_webhook_test.go` - Unit tests for webhook validation
- `suite_test.go` - Ginkgo test suite setup

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  KubeTemplateValidator                                   │
│  Implements: webhook.CustomValidator                     │
├─────────────────────────────────────────────────────────┤
│  + Client: client.Client                                 │
│  + OperatorNamespace: string                            │
├─────────────────────────────────────────────────────────┤
│  + ValidateCreate(ctx, obj) (Warnings, error)           │
│  + ValidateUpdate(ctx, old, new) (Warnings, error)      │
│  + ValidateDelete(ctx, obj) (Warnings, error)           │
│  - validateKubeTemplate(ctx, kt) (Warnings, error)      │
│  - validateCELRule(ctx, rule, obj, idx, gvk) error      │
└─────────────────────────────────────────────────────────┘
```

## Validation Flow

1. **Policy Lookup**: Find matching `KubeTemplatePolicy` for source namespace
2. **Policy Validation**: Ensure exactly one policy exists (no duplicates)
3. **For each template**:
   - Parse YAML to unstructured object
   - Validate GVK (Group/Version/Kind) is allowed
   - Validate target namespace is allowed
   - Evaluate CEL rule if present
   - Add warnings for special cases (e.g., replace mode)

## Kubebuilder Marker

```go
// +kubebuilder:webhook:path=/validate-kubetemplater-io-v1alpha1-kubetemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubetemplater.io,resources=kubetemplates,verbs=create;update,versions=v1alpha1,name=vkubetemplate.kb.io,admissionReviewVersions=v1
```

This marker generates the `ValidatingWebhookConfiguration` manifest.

## Usage

The webhook is automatically registered with the manager in `cmd/main.go`:

```go
if err := (&kubetemplaterwebhook.KubeTemplateValidator{
    Client:            mgr.GetClient(),
    OperatorNamespace: operatorNamespace,
}).SetupWebhookWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create webhook")
    os.Exit(1)
}
```

## Testing

Run tests with:

```bash
go test ./internal/webhook/... -v
```

Test coverage includes:
- ✅ Valid KubeTemplate with policy
- ❌ KubeTemplate without policy
- ❌ Disallowed resource types
- ❌ Invalid target namespaces
- ✅ CEL validation (pass)
- ❌ CEL validation (fail)
- ⚠️ Replace mode warnings

## Error Messages

The webhook provides detailed, structured error messages:

| Scenario | Error Format |
|----------|--------------|
| No policy | `no KubeTemplatePolicy found for source namespace {ns}` |
| Multiple policies | `multiple KubeTemplatePolicies found for source namespace {ns}: {names}` |
| Disallowed GVK | `template[{idx}]: resource type {gvk} is not allowed by policy {policy}` |
| No target namespaces | `template[{idx}]: resource type {gvk} has no target namespaces defined` |
| Invalid namespace | `template[{idx}]: resource namespace {ns} is not in the allowed target namespaces {list}` |
| CEL parse error | `template[{idx}]: failed to parse CEL rule: {error}` |
| CEL validation failed | `template[{idx}]: resource {gvk}/{name} failed CEL validation rule: {rule}` |

## Integration with Controller

The webhook and controller work together but have distinct responsibilities:

| Component | Responsibility | Timing |
|-----------|---------------|--------|
| **Webhook** | Validate policy compliance | Admission time (synchronous) |
| **Controller** | Apply resources to cluster | Reconciliation time (asynchronous) |

This separation ensures:
- Invalid resources never reach the controller
- Users get immediate feedback
- Controller focuses on resource application

## Dependencies

- `controller-runtime/pkg/webhook` - Webhook framework
- `google/cel-go` - CEL expression evaluation
- `controller-runtime/pkg/client` - Kubernetes client
- `sigs.k8s.io/yaml` - YAML parsing

## Future Enhancements

Possible improvements:
- [ ] Cache policies to reduce API calls
- [ ] Add mutation webhook for defaults
- [ ] Support for dry-run validation
- [ ] Metrics for validation results
- [ ] Audit logging for policy violations
