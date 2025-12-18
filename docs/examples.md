# KubeTemplater Examples

This page provides practical examples of how to use KubeTemplater to manage different kinds of Kubernetes resources.

## Example 1: Basic Nginx Deployment and Service

This example shows how to deploy a simple Nginx web server and expose it with a `ClusterIP` service. Both resources are managed from a single `KubeTemplate`.

### Step 1: Create a Policy

First, create a policy that allows Deployments and Services in the default namespace:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [default]
    - kind: Service
      group: ""
      version: v1
      targetNamespaces: [default]
```

Apply the policy:
```bash
kubectl apply -f policy.yaml
```

### Step 2: Create the KubeTemplate

Create the following `KubeTemplate` and apply it to your cluster:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: nginx-template
  namespace: default
spec:
  templates:
    # Resource 1: The Nginx Deployment
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: nginx-server
          labels:
            app: nginx-server
        spec:
          replicas: 2
          selector:
            matchLabels:
              app: nginx-server
          template:
            metadata:
              labels:
                app: nginx-server
            spec:
              containers:
              - name: nginx
                image: nginx:1.23
                ports:
                - containerPort: 80

    # Resource 2: The Service to expose Nginx
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: nginx-service
        spec:
          selector:
            app: nginx-server
          ports:
            - protocol: TCP
              port: 80
              targetPort: 80
          type: ClusterIP
```

Apply the template:
```bash
kubectl apply -f kubetemplate.yaml
```

### What Happens

1.  The admission webhook validates the `KubeTemplate` against the policy before it's accepted.
2.  The controller verifies the policy exists for the `default` namespace.
3.  Each template is validated: GVK is allowed, target namespace is permitted.
4.  The Deployment and Service are applied using Server-Side Apply.
5.  You can now update the `KubeTemplate` (e.g., change replicas to 3) and the operator will automatically update the Deployment.

---

## Example 2: Managing a CronJob with an Immutable Field

This example demonstrates how to use the **replace** feature to manage a `CronJob`, which has an immutable `spec.jobTemplate`.

### Step 1: Update the Policy

Add CronJob to the allowed resources:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default
  validationRules:
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [default]
    - kind: Service
      group: ""
      version: v1
      targetNamespaces: [default]
    - kind: CronJob
      group: batch
      version: v1
      targetNamespaces: [default]
```

### Step 2: Create the KubeTemplate with Replace Mode

Here, we define a `CronJob` and enable the replace strategy:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: cronjob-template
  namespace: default
spec:
  templates:
    - replace: true  # Enable replace mode for immutable fields
      object:
        apiVersion: batch/v1
        kind: CronJob
        metadata:
          name: hello-world-cronjob
        spec:
          schedule: "*/1 * * * *"  # Runs every minute
          jobTemplate:
            spec:
              template:
                spec:
                  containers:
                  - name: hello
                    image: busybox:1.35
                    command:
                    - /bin/sh
                    - -c
                    - date; echo "Hello from KubeTemplater!"
                  restartPolicy: OnFailure
```

Apply the template:
```bash
kubectl apply -f cronjob-template.yaml
```

### How to Test the Immutable Field Update

1.  **Apply the `KubeTemplate`** above. The `CronJob` will be created.
2.  **Modify the `KubeTemplate`**: Change the `command` in the `spec.jobTemplate.spec.template.spec.containers` array. For example, change `"Hello from KubeTemplater!"` to `"A new message!"`.
3.  **Apply the change**: `kubectl apply -f cronjob-template.yaml`.

### What Happens

1.  The admission webhook validates the `KubeTemplate` and confirms the `CronJob` is allowed by the policy.
2.  The controller applies the `CronJob` using Server-Side Apply.
3.  When you modify the immutable `jobTemplate` field and reapply, the controller attempts SSA but receives an immutable field error.
4.  Because `replace: true` is set, the controller automatically **deletes** the old `CronJob` and **creates** a new one with the updated configuration.
5.  The `CronJob` is successfully updated without manual intervention.

---

## Example 3: Creating a Resource in a Different Namespace

This example shows how to use policy-based namespace targeting to manage resources across different namespaces.

### Step 1: Create a Policy Allowing Cross-Namespace Resources

First, create a policy that allows creating Secrets in `kube-system` from the `default` namespace:

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

### Step 2: Create the KubeTemplate

Create a `KubeTemplate` that defines a `Secret` in the `kube-system` namespace:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: cross-namespace-template
  namespace: default  # Source namespace
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: important-system-secret
          namespace: kube-system  # Target namespace
        type: Opaque
        stringData:
          username: admin
          password: "s3cr3t-p@ssw0rd!"
```

Apply the template:
```bash
kubectl apply -f cross-namespace-template.yaml
```

### What Happens

1.  The admission webhook validates the `KubeTemplate` against the policy.
2.  It verifies that the policy allows `Secret` resources in the `kube-system` namespace.
3.  The controller creates the `Secret` resource in the **`kube-system`** namespace.
4.  If you delete the `KubeTemplate`, the controller will automatically delete the `Secret` from `kube-system`.

---

## Example 4: Enforcing Security Policies with CEL Rules

This example demonstrates how to use a `KubeTemplatePolicy` with CEL (Common Expression Language) to enforce naming conventions and other security requirements.

### Step 1: Create a Policy with CEL Rules

Create a policy that enforces specific naming patterns for ConfigMaps:

```yaml
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
    - kind: Secret
      group: ""
      version: v1
      targetNamespaces: [default]
      rule: "object.metadata.name.startsWith('secure-')"  # CEL rule for naming convention
```

This policy:
- Allows `ConfigMap` resources in the `default` namespace
- Allows `Secret` resources in the `default` namespace, but **only** if their name starts with `secure-`

Apply the policy:
```bash
kubectl apply -f policy-with-cel.yaml
```

### Step 2: Create a KubeTemplate with Multiple Secrets

Create a `KubeTemplate` that attempts to create a `ConfigMap` and two `Secret` resources:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-template
  namespace: default
spec:
  templates:
    # Valid ConfigMap
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: my-configmap
        data:
          key: value

    # Valid Secret - name starts with 'secure-'
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: secure-credentials
        type: Opaque
        stringData:
          username: admin
          password: secret123

    # Invalid Secret - name doesn't start with 'secure-'
    - object:
        apiVersion: v1
        kind: Secret
        metadata:
          name: invalid-secret
        type: Opaque
        stringData:
          key: value
```

### What Happens

**At admission time (webhook validation):**
1.  You attempt to apply the `KubeTemplate` to the cluster.
2.  The validation webhook intercepts the request.
3.  It finds the policy for the `default` namespace.
4.  It validates each template:
    - `ConfigMap` with name `my-configmap`: **Allowed** ✓
    - `Secret` with name `secure-credentials`: **Allowed** ✓ (name starts with `secure-`)
    - `Secret` with name `invalid-secret`: **REJECTED** ✗ (CEL rule fails: name doesn't start with `secure-`)
5.  The webhook **rejects** the entire `KubeTemplate` with an error message explaining which resource failed validation.

To make it work, you must rename the third secret to start with `secure-`, for example `secure-app-credentials`.

This example shows how `KubeTemplatePolicy` with CEL rules can enforce fine-grained security policies and naming conventions at admission time, preventing non-compliant resources from being created.

---

## Example 5: Advanced Multi-Field Validation

This example demonstrates the full power of field validations by combining multiple validation types to enforce comprehensive policies.

### Step 1: Create a Comprehensive Policy

Create a policy with multiple field validations:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: production-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: production
  validationRules:
    # Production Deployments must follow strict rules
    - kind: Deployment
      group: apps
      version: v1
      targetNamespaces: [production]
      fieldValidations:
        # 1. Name must start with 'prod-'
        - name: "production-naming"
          fieldPath: "metadata.name"
          type: regex
          regex: "^prod-[a-z0-9-]+$"
          message: "Deployment name must start with 'prod-' and contain only lowercase letters, numbers, and hyphens"
        
        # 2. Replicas between 2 and 10 for HA
        - name: "high-availability"
          fieldPath: "spec.replicas"
          type: range
          min: 2
          max: 10
          message: "Production deployments must have 2-10 replicas for high availability"
        
        # 3. Team label is mandatory
        - name: "team-ownership"
          fieldPath: "metadata.labels.team"
          type: required
          message: "All production resources must have a 'team' label for ownership tracking"
        
        # 4. Environment label must be 'production'
        - name: "env-label-validation"
          fieldPath: "metadata.labels.env"
          type: cel
          cel: "value == 'production'"
          message: "Environment label must be set to 'production'"
        
        # 5. No privileged containers allowed
        - name: "no-privileged-containers"
          fieldPath: "spec.template.spec.containers.0.securityContext.privileged"
          type: forbidden
          message: "Privileged containers are not allowed in production for security reasons"
    
    # Production Pods must have resource limits
    - kind: Pod
      group: ""
      version: v1
      targetNamespaces: [production]
      fieldValidations:
        - name: "resource-limits-required"
          type: cel
          cel: "object.spec.containers.all(c, has(c.resources) && has(c.resources.limits))"
          message: "All containers must define resource limits"
```

### Step 2: Create a Compliant KubeTemplate

Create a `KubeTemplate` that satisfies all validations:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: production-api
  namespace: production
spec:
  templates:
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: prod-api-service
          labels:
            app: api
            team: platform
            env: production
        spec:
          replicas: 3
          selector:
            matchLabels:
              app: api
          template:
            metadata:
              labels:
                app: api
            spec:
              containers:
              - name: api
                image: myregistry/api:v1.2.3
                ports:
                - containerPort: 8080
                resources:
                  requests:
                    cpu: 100m
                    memory: 128Mi
                  limits:
                    cpu: 500m
                    memory: 512Mi
                securityContext:
                  runAsNonRoot: true
                  allowPrivilegeEscalation: false
```

### What Happens

**At admission time:**
1. ✅ Name `prod-api-service` matches regex `^prod-[a-z0-9-]+$`
2. ✅ Replicas value `3` is within range `[2, 10]`
3. ✅ Label `team: platform` exists
4. ✅ Label `env: production` has correct value
5. ✅ No `privileged: true` field exists in securityContext
6. ✅ All containers have resource limits defined

The `KubeTemplate` is accepted and resources are created.

### Testing Validation Failures

Try creating this invalid template:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-deployment
  namespace: production
spec:
  templates:
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: dev-api  # ❌ Wrong prefix
          labels:
            app: api
            env: production
            # ❌ Missing 'team' label
        spec:
          replicas: 1  # ❌ Too few replicas
          selector:
            matchLabels:
              app: api
          template:
            metadata:
              labels:
                app: api
            spec:
              containers:
              - name: api
                image: myregistry/api:latest
                securityContext:
                  privileged: true  # ❌ Forbidden!
```

**Result:** The webhook rejects it with:
```
Error: admission webhook denied the request: template[0]: fieldValidation (production-naming): 
Deployment name must start with 'prod-' and contain only lowercase letters, numbers, and hyphens
```

This example demonstrates how field validations can create comprehensive, production-ready policies that enforce naming conventions, security best practices, resource constraints, and organizational requirements—all validated at admission time before resources enter the cluster.