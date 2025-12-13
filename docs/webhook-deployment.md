# Deploying KubeTemplater with Validation Webhook

This guide explains how to deploy KubeTemplater with the validation webhook enabled.

## Prerequisites

- Kubernetes cluster 1.16+ with admission webhooks enabled
- `kubectl` configured to access your cluster
- Cluster admin permissions
- **Option 1**: cert-manager installed (recommended for production)
- **Option 2**: Manual TLS certificates

## Quick Start

### 1. Install cert-manager (Recommended)

If you don't have cert-manager installed:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=available --timeout=300s \
  deployment/cert-manager -n cert-manager
kubectl wait --for=condition=available --timeout=300s \
  deployment/cert-manager-webhook -n cert-manager
kubectl wait --for=condition=available --timeout=300s \
  deployment/cert-manager-cainjector -n cert-manager
```

### 2. Deploy KubeTemplater

#### Option A: Using Helm (Recommended)

```bash
# Install from local chart
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace

# Or with custom values
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --values my-values.yaml
```

#### Option B: Using Make/Kustomize

```bash
# Build and push the image (optional, if building from source)
export IMG=<your-registry>/kubetemplater:latest
make docker-build docker-push IMG=$IMG

# Install CRDs
make install

# Deploy the operator with webhook enabled
make deploy IMG=$IMG
```

### 3. Verify Webhook Installation

```bash
# Check webhook configuration
kubectl get validatingwebhookconfigurations

# Verify webhook service
kubectl get svc -n kubetemplater-system kubetemplater-webhook-service

# Check operator pods
kubectl get pods -n kubetemplater-system

# View operator logs
kubectl logs -n kubetemplater-system \
  -l control-plane=controller-manager \
  --tail=50
```

### 4. Create a Test Policy

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: test-policy
  namespace: kubetemplater-system
spec:
  sourceNamespace: default
  validationRules:
    - kind: ConfigMap
      group: ""
      version: v1
      targetNamespaces:
        - default
EOF
```

### 5. Test the Webhook

#### Test Valid KubeTemplate ✅

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: test-template
  namespace: default
spec:
  templates:
    - object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: test-config
        data:
          key: value
EOF

# Should succeed and create the ConfigMap
kubectl get configmap test-config -n default
```

#### Test Invalid KubeTemplate ❌

```bash
# This should be rejected by the webhook
cat <<EOF | kubectl apply -f -
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: invalid-template
  namespace: default
spec:
  templates:
    - object:
        apiVersion: v1
        kind: Secret  # Not allowed by policy
        metadata:
          name: test-secret
        type: Opaque
        data:
          key: dmFsdWU=
EOF

# Expected error:
# Error from server: admission webhook "vkubetemplate.kb.io" denied the request:
# template[0]: resource type /v1, Kind=Secret is not allowed by policy test-policy
```

## Advanced Configuration

### Using cert-manager (Production)

Enable cert-manager in `config/default/kustomization.yaml`:

```yaml
# Uncomment these lines
resources:
- ../certmanager

# Uncomment the replacements section for cert-manager
replacements:
- source:
    kind: Service
    version: v1
    name: webhook-service
  # ... (rest of cert-manager configuration)
```

Then deploy:

```bash
make deploy IMG=<your-registry>/kubetemplater:latest
```

### Manual TLS Certificates

If not using cert-manager, provide your own certificates:

1. **Generate certificates**:

```bash
# Create a certificate signing request
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout tls.key -out tls.crt \
  -days 365 \
  -subj "/CN=kubetemplater-webhook-service.kubetemplater-system.svc"
```

2. **Create Secret**:

```bash
kubectl create secret tls webhook-server-cert \
  --cert=tls.crt \
  --key=tls.key \
  -n kubetemplater-system
```

3. **Update webhook configuration** with CA bundle:

```bash
# Get the CA bundle
CA_BUNDLE=$(cat tls.crt | base64 -w 0)

# Patch the webhook configuration
kubectl patch validatingwebhookconfiguration \
  kubetemplater-validating-webhook-configuration \
  --type='json' \
  -p="[{'op': 'add', 'path': '/webhooks/0/clientConfig/caBundle', 'value':'${CA_BUNDLE}'}]"
```

### High Availability

For production, run multiple replicas:

```yaml
# In your Helm values or kustomization
replicas: 3

# Or patch the deployment
kubectl scale deployment kubetemplater-controller-manager \
  -n kubetemplater-system \
  --replicas=3
```

### Custom Webhook Port

If you need to change the webhook port (default 9443):

Edit `config/default/manager_webhook_patch.yaml`:

```yaml
containers:
- name: manager
  ports:
  - containerPort: 9444  # Your custom port
    name: webhook-server
    protocol: TCP
```

And update the service:

```yaml
# config/webhook/service.yaml
spec:
  ports:
  - port: 443
    targetPort: 9444  # Match your custom port
```

## Troubleshooting

### Webhook Not Responding

**Symptoms**: Timeout errors when creating KubeTemplates

**Solutions**:
1. Check operator pods are running:
   ```bash
   kubectl get pods -n kubetemplater-system
   ```

2. Check webhook service endpoints:
   ```bash
   kubectl get endpoints -n kubetemplater-system kubetemplater-webhook-service
   ```

3. Verify TLS certificates:
   ```bash
   kubectl get secret webhook-server-cert -n kubetemplater-system
   ```

4. Check operator logs:
   ```bash
   kubectl logs -n kubetemplater-system \
     -l control-plane=controller-manager
   ```

### Certificate Issues

**Symptoms**: `x509: certificate signed by unknown authority`

**Solutions**:
1. If using cert-manager, ensure it's running:
   ```bash
   kubectl get pods -n cert-manager
   ```

2. Check certificate is issued:
   ```bash
   kubectl get certificate -n kubetemplater-system
   ```

3. Verify CA bundle in webhook config:
   ```bash
   kubectl get validatingwebhookconfiguration \
     kubetemplater-validating-webhook-configuration \
     -o yaml | grep caBundle
   ```

### Policy Not Found Errors

**Symptoms**: `no KubeTemplatePolicy found for source namespace`

**Solutions**:
1. Verify policy exists in operator namespace:
   ```bash
   kubectl get kubetemplatepolicy -n kubetemplater-system
   ```

2. Check policy's `sourceNamespace` matches KubeTemplate's namespace:
   ```bash
   kubectl get kubetemplatepolicy <policy-name> \
     -n kubetemplater-system -o yaml
   ```

3. Check RBAC permissions:
   ```bash
   kubectl auth can-i list kubetemplatepolicies.kubetemplater.io \
     --as=system:serviceaccount:kubetemplater-system:kubetemplater-controller-manager \
     -n kubetemplater-system
   ```

### Webhook Timing Out API Server

**Symptoms**: Slow cluster response, timeouts

**Solutions**:
1. Increase webhook timeout (default 10s):
   ```bash
   kubectl patch validatingwebhookconfiguration \
     kubetemplater-validating-webhook-configuration \
     --type='json' \
     -p='[{"op": "add", "path": "/webhooks/0/timeoutSeconds", "value": 30}]'
   ```

2. Scale up operator replicas for better performance

3. Monitor webhook latency in logs

### Emergency: Disable Webhook

If the webhook is causing issues and you need to disable it quickly:

```bash
# Delete the webhook configuration (allows KubeTemplates to be created without validation)
kubectl delete validatingwebhookconfiguration \
  kubetemplater-validating-webhook-configuration

# Or change failure policy to Ignore (allows resources through if webhook fails)
kubectl patch validatingwebhookconfiguration \
  kubetemplater-validating-webhook-configuration \
  --type='json' \
  -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Ignore"}]'
```

## Monitoring

### Check Webhook Metrics

```bash
# Port-forward to metrics endpoint
kubectl port-forward -n kubetemplater-system \
  svc/kubetemplater-controller-manager-metrics-service 8443:8443

# Query metrics (if metrics are enabled and accessible)
curl -k https://localhost:8443/metrics | grep webhook
```

### Webhook Logs

```bash
# Follow webhook validation logs
kubectl logs -n kubetemplater-system \
  -l control-plane=controller-manager \
  -f | grep "Validating KubeTemplate"
```

### Audit Logs

If cluster audit logging is enabled, webhook decisions are logged:

```bash
# Example: Search audit logs for webhook decisions
kubectl logs -n kube-system <api-server-pod> | \
  grep "kubetemplater" | \
  grep "admit"
```

## Uninstall

To remove KubeTemplater and the webhook:

```bash
# Delete all KubeTemplates first
kubectl delete kubetemplates --all --all-namespaces

# Delete all policies
kubectl delete kubetemplatepolicies --all -n kubetemplater-system

# Uninstall with Helm
helm uninstall kubetemplater -n kubetemplater-system

# Or with Make
make undeploy
make uninstall

# Delete the namespace
kubectl delete namespace kubetemplater-system
```

## Next Steps

- Read [Webhook Validation Documentation](../docs/webhook-validation.md)
- Try [Webhook Examples](../docs/webhook-example.md)
- Review [Sample Configurations](../config/samples/)
- Learn about [Advanced Features](../docs/features.md)
