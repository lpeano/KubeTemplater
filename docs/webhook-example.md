# Example: Using the Validation Webhook

This example demonstrates how the validation webhook enforces policies at admission time.

## Scenario

We'll create a policy that:
1. Allows creation of `ConfigMap` and `Secret` resources
2. Restricts `Secret` names to start with `secure-`
3. Only allows creation in the `app-namespace` namespace

## Step 1: Create the Policy

First, create a `KubeTemplatePolicy` in the operator namespace:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: app-policy
  namespace: kubetemplater-system  # Operator namespace
spec:
  sourceNamespace: app-namespace  # Namespace where KubeTemplates can be created
  validationRules:
    # Allow ConfigMaps
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces:
        - app-namespace
        - app-namespace-prod
    
    # Allow Secrets with name validation
    - kind: Secret
      group: ""
      version: v1
      rule: "object.metadata.name.startsWith('secure-')"
      targetNamespaces:
        - app-namespace
```

Apply the policy:
```bash
kubectl apply -f policy.yaml
```

## Step 2: Test Valid KubeTemplate ✅

Create a valid `KubeTemplate`:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: valid-template
  namespace: app-namespace
spec:
  templates:
    # Valid ConfigMap
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: app-config
          namespace: app-namespace
        data:
          database_url: "postgresql://localhost:5432/mydb"
    
    # Valid Secret (name starts with 'secure-')
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: secure-api-key
          namespace: app-namespace
        type: Opaque
        stringData:
          api-key: "my-secret-key"
```

Apply the template:
```bash
kubectl apply -f valid-template.yaml
```

**Result**: ✅ **Accepted** - The webhook validates successfully, and resources are created.

```
kubetemplate.kubetemplater.io/valid-template created
```

## Step 3: Test Invalid KubeTemplate - Wrong Secret Name ❌

Try to create a Secret with an invalid name:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-secret-name
  namespace: app-namespace
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: bad-secret  # ❌ Doesn't start with 'secure-'
          namespace: app-namespace
        type: Opaque
        stringData:
          api-key: "my-secret-key"
```

Apply the template:
```bash
kubectl apply -f invalid-secret-name.yaml
```

**Result**: ❌ **Rejected** - The webhook blocks the resource at admission time.

```
Error from server: admission webhook "vkubetemplate.kb.io" denied the request: 
template[0]: resource /v1, Kind=Secret/bad-secret failed CEL validation rule: 
object.metadata.name.startsWith('secure-')
```

## Step 4: Test Invalid KubeTemplate - Disallowed Resource Type ❌

Try to create a Deployment (not allowed by policy):

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-resource-type
  namespace: app-namespace
spec:
  templates:
    - object:
        apiVersion: apps/v1
        kind: Deployment  # ❌ Not in the policy
        metadata:
          name: nginx
          namespace: app-namespace
        spec:
          replicas: 1
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
                  image: nginx:latest
```

Apply the template:
```bash
kubectl apply -f invalid-resource-type.yaml
```

**Result**: ❌ **Rejected** - The webhook blocks because Deployment is not allowed.

```
Error from server: admission webhook "vkubetemplate.kb.io" denied the request: 
template[0]: resource type apps/v1, Kind=Deployment is not allowed by policy app-policy
```

## Step 5: Test Invalid KubeTemplate - Wrong Namespace ❌

Try to create a resource in a disallowed namespace:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-namespace
  namespace: app-namespace
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: app-config
          namespace: forbidden-namespace  # ❌ Not in targetNamespaces
        data:
          key: value
```

Apply the template:
```bash
kubectl apply -f invalid-namespace.yaml
```

**Result**: ❌ **Rejected** - The webhook blocks because the target namespace is not allowed.

```
Error from server: admission webhook "vkubetemplate.kb.io" denied the request: 
template[0]: resource namespace forbidden-namespace is not in the allowed target namespaces 
[app-namespace app-namespace-prod] for resource type /v1, Kind=ConfigMap
```

## Step 6: Test KubeTemplate without Policy ❌

Try to create a KubeTemplate in a namespace without a policy:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: no-policy-template
  namespace: no-policy-namespace  # ❌ No policy for this namespace
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: app-config
        data:
          key: value
```

Apply the template:
```bash
kubectl apply -f no-policy-template.yaml
```

**Result**: ❌ **Rejected** - The webhook requires a policy to exist.

```
Error from server: admission webhook "vkubetemplate.kb.io" denied the request: 
no KubeTemplatePolicy found for source namespace no-policy-namespace. 
A policy must be defined in namespace kubetemplater-system
```

## Step 7: Replace Mode Warning ⚠️

Create a KubeTemplate with replace mode enabled:

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
          name: app-config
          namespace: app-namespace
        data:
          key: value
      replace: true  # ⚠️ Replace mode enabled
```

Apply the template:
```bash
kubectl apply -f template-with-replace.yaml
```

**Result**: ✅ **Accepted with Warning** - The webhook allows it but issues a warning.

```
Warning: template[0]: replace is enabled for /v1, Kind=ConfigMap/app-config. 
The resource will be deleted and recreated if immutable fields are changed
kubetemplate.kubetemplater.io/template-with-replace created
```

## Verification

### Check Webhook Configuration

```bash
# View webhook configuration
kubectl get validatingwebhookconfiguration

# Describe webhook details
kubectl describe validatingwebhookconfiguration \
  kubetemplater-validating-webhook-configuration
```

### Check Webhook Logs

```bash
# View webhook validation logs
kubectl logs -n kubetemplater-system \
  -l control-plane=controller-manager \
  --tail=50

# Follow logs in real-time
kubectl logs -n kubetemplater-system \
  -l control-plane=controller-manager \
  -f
```

### Check Applied Resources

```bash
# List KubeTemplates
kubectl get kubetemplates -n app-namespace

# Check KubeTemplate status
kubectl describe kubetemplate valid-template -n app-namespace

# View created resources
kubectl get configmap,secret -n app-namespace
```

## Summary

The validation webhook provides:

| Validation | Type | Example |
|-----------|------|---------|
| Policy existence | ❌ Rejection | No policy for namespace |
| Resource type | ❌ Rejection | Deployment not allowed |
| Target namespace | ❌ Rejection | Wrong namespace |
| CEL rules | ❌ Rejection | Secret name validation |
| Replace mode | ⚠️ Warning | Replace enabled |

All validation happens **immediately** when you apply the KubeTemplate, providing fast feedback and preventing invalid configurations from entering the cluster.
