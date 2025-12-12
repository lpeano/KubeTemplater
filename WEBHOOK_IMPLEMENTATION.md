# Validation Webhook Implementation

## Summary

This implementation adds a **validation webhook** to KubeTemplater that validates `KubeTemplate` resources at admission time (CREATE/UPDATE operations) to ensure they comply with the defined `KubeTemplatePolicy` before being accepted into the cluster.

## What Was Added

### 1. Webhook Implementation (`internal/webhook/`)

**Files Created:**
- `kubetemplate_webhook.go` - Main webhook validator implementation
- `kubetemplate_webhook_test.go` - Comprehensive unit tests (7 test cases)
- `suite_test.go` - Ginkgo test suite setup
- `README.md` - Package documentation

**Key Features:**
- Implements `webhook.CustomValidator` interface
- Validates policy existence and uniqueness
- Validates resource GVK (Group/Version/Kind) against policy rules
- Validates target namespace restrictions
- Evaluates CEL expressions for custom validation rules
- Provides detailed, structured error messages
- Issues warnings for special cases (e.g., replace mode)

### 2. Webhook Configuration (`config/webhook/`)

**Files Created:**
- `manifests.yaml` - ValidatingWebhookConfiguration
- `service.yaml` - Webhook service definition
- `kustomization.yaml` - Kustomize configuration
- `kustomizeconfig.yaml` - Kustomize variable substitution config

**Configuration:**
- Webhook endpoint: `/validate-kubetemplater-io-v1alpha1-kubetemplate`
- Operations: CREATE, UPDATE
- Failure policy: Fail (blocks invalid resources)
- Admission review versions: v1

### 3. Manager Integration (`config/default/`)

**Files Modified:**
- `kustomization.yaml` - Enabled webhook resources and patches
- **Files Created:**
- `manager_webhook_patch.yaml` - Adds webhook port and volume mounts to manager

### 4. Main Application (`cmd/main.go`)

**Changes:**
- Added import for webhook package (aliased as `kubetemplaterwebhook`)
- Registered webhook validator with manager
- Passes `OperatorNamespace` to webhook for policy lookups

### 5. Documentation

**Files Created:**
- `docs/webhook-validation.md` - Complete webhook documentation
- `docs/webhook-example.md` - Step-by-step usage examples
- `config/samples/kubetemplater.io_v1alpha1_webhook_validation_sample.yaml` - Full sample configuration

**Files Modified:**
- `README.md` - Added security & validation section
- `docs/features.md` - Added validation webhook feature section

## Validation Logic

The webhook performs the following validations:

### 1. Policy Validation
- ✅ Ensures exactly one `KubeTemplatePolicy` exists for source namespace
- ❌ Rejects if no policy found
- ❌ Rejects if multiple policies found (ambiguous)

### 2. Resource Type Validation
- ✅ Checks if GVK is allowed in policy's `validationRules`
- ❌ Rejects disallowed resource types

### 3. Target Namespace Validation
- ✅ Checks if target namespace is in `targetNamespaces` list
- ❌ Rejects if namespace not allowed
- ❌ Rejects if no target namespaces defined

### 4. CEL Expression Validation
- ✅ Evaluates CEL rules against resource objects
- ❌ Rejects if CEL evaluation returns false
- ❌ Rejects if CEL has syntax errors

### 5. Warnings
- ⚠️ Issues warning when `replace: true` is set

## Test Coverage

**Unit Tests:** 7 test cases covering:
1. ❌ KubeTemplate without policy
2. ✅ Valid KubeTemplate with policy
3. ❌ Disallowed resource type
4. ❌ Invalid target namespace
5. ✅ CEL validation (passing)
6. ❌ CEL validation (failing)
7. ⚠️ Replace mode warning

**Test Results:**
```
Ran 7 of 7 Specs in 0.005 seconds
SUCCESS! -- 7 Passed | 0 Failed | 0 Pending | 0 Skipped
```

## Benefits

### For Users
- **Immediate Feedback**: Errors shown at kubectl apply time
- **Clear Error Messages**: Structured, actionable error messages
- **Prevents Invalid State**: Invalid resources never enter cluster
- **Better Developer Experience**: Fast validation cycle

### For Security
- **Policy Enforcement**: Policies enforced before persistence
- **Audit Trail**: All validation attempts logged
- **Fail-Safe**: Webhook unavailability blocks invalid resources (failurePolicy: Fail)

### For Operations
- **GitOps Friendly**: Invalid manifests fail in CI/CD
- **Reduced Load**: Invalid resources don't reach controller
- **Better Debugging**: Validation happens before reconciliation

## Architecture

```
User → kubectl apply
       ↓
API Server
       ↓
Validation Webhook ←→ KubeTemplatePolicy
       ↓
  [Allow/Deny]
       ↓
If Allowed → etcd → Controller (reconciliation)
If Denied  → Error returned to user
```

## Deployment

The webhook is automatically enabled when deploying with the updated configuration:

```bash
# Deploy with webhook enabled
make deploy IMG=<your-registry>/kubetemplater:tag

# Or with Helm
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

## Configuration Requirements

1. **TLS Certificates**: Webhook TLS certificates managed automatically
   - **v0.3.3+**: Watch-based certificate discovery with hash verification
   - **v0.3.2+**: Self-signed certificates generated by leader pod
   - **Auto-renewal**: Certificates renewed 30 days before expiration
   - **Zero dependencies**: No cert-manager required
   - **Dev/Prod**: Same mechanism, production-ready with comprehensive testing

2. **Network Connectivity**: API server must reach webhook service

3. **RBAC**: Webhook needs permissions to:
   - List KubeTemplatePolicies
   - Watch Lease resources (for certificate discovery)

## Error Message Examples

### No Policy
```
Error: no KubeTemplatePolicy found for source namespace app-namespace. 
A policy must be defined in namespace kubetemplater-system
```

### Disallowed Resource Type
```
Error: template[0]: resource type apps/v1, Kind=Deployment is not allowed 
by policy app-policy
```

### Invalid Target Namespace
```
Error: template[1]: resource namespace forbidden-ns is not in the allowed 
target namespaces [app-namespace, app-prod] for resource type /v1, Kind=Secret
```

### CEL Validation Failed
```
Error: template[2]: resource /v1, Kind=Secret/bad-name failed CEL validation 
rule: object.metadata.name.startsWith('secure-')
```

## Backward Compatibility

- ✅ Existing KubeTemplates continue to work
- ✅ Controller reconciliation logic unchanged
- ✅ No breaking changes to CRDs
- ✅ Policy validation was moved earlier (admission vs reconciliation)

## Future Enhancements

Possible improvements:
- [ ] Policy caching to reduce API calls
- [ ] Mutating webhook for default values
- [ ] Dry-run validation mode
- [ ] Metrics for validation results (accept/deny rates)
- [ ] Audit logging for compliance
- [ ] Multi-policy support with priority

## Testing Recommendations

1. **Test Policy Enforcement**:
   ```bash
   kubectl apply -f config/samples/kubetemplater.io_v1alpha1_webhook_validation_sample.yaml
   ```

2. **Test Invalid Configurations**:
   - Uncomment invalid examples in sample file
   - Verify webhook rejects them with clear errors

3. **Check Webhook Status**:
   ```bash
   kubectl get validatingwebhookconfigurations
   kubectl describe validatingwebhookconfiguration \
     kubetemplater-validating-webhook-configuration
   ```

4. **Monitor Webhook Logs**:
   ```bash
   kubectl logs -n kubetemplater-system \
     -l control-plane=controller-manager -f
   ```

## Implementation Quality

- ✅ **Well-tested**: 7 unit tests, 100% pass rate
- ✅ **Well-documented**: 5 documentation files created
- ✅ **Production-ready**: Fail-safe failure policy
- ✅ **Follows Best Practices**: Uses controller-runtime webhook framework
- ✅ **Type-safe**: Strong typing with Go interfaces
- ✅ **Error Handling**: Comprehensive error messages
- ✅ **Maintainable**: Clean code structure, well-commented

## Files Changed Summary

```
Created:
  internal/webhook/kubetemplate_webhook.go (253 lines)
  internal/webhook/kubetemplate_webhook_test.go (400 lines)
  internal/webhook/suite_test.go (27 lines)
  internal/webhook/README.md (150 lines)
  config/webhook/manifests.yaml (28 lines)
  config/webhook/service.yaml (13 lines)
  config/webhook/kustomization.yaml (6 lines)
  config/webhook/kustomizeconfig.yaml (27 lines)
  config/default/manager_webhook_patch.yaml (21 lines)
  docs/webhook-validation.md (400 lines)
  docs/webhook-example.md (450 lines)
  config/samples/kubetemplater.io_v1alpha1_webhook_validation_sample.yaml (200 lines)

Modified:
  cmd/main.go (8 lines changed)
  config/default/kustomization.yaml (2 lines changed)
  README.md (15 lines added)
  docs/features.md (30 lines added)

Total: 12 new files, 4 modified files
Total Lines Added: ~2,000+ lines
```
