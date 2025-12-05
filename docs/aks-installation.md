# Installing KubeTemplater on Azure Kubernetes Service (AKS)

This guide provides instructions for deploying KubeTemplater on Azure Kubernetes Service with native AKS webhook support.

## Prerequisites

- Azure CLI installed and configured
- kubectl configured to access your AKS cluster
- Helm v3.0.0 or higher

## AKS Native Webhook Support

**Good news!** AKS provides **built-in support for admission webhooks** without requiring cert-manager or manual certificate management.

When you deploy a ValidatingWebhookConfiguration without a `caBundle`, AKS automatically:
1. ✅ Generates TLS certificates for your webhook service
2. ✅ Injects the CA bundle into the webhook configuration
3. ✅ Manages certificate rotation automatically

This is the **simplest and recommended approach** for AKS.

---

## Installation Methods

### Method 1: AKS-Managed Certificates (Recommended) 🎯

**The default configuration uses AKS native certificate management:**

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

**That's it!** No additional configuration needed.

**What happens behind the scenes:**
- Chart uses `certificateMode: aks` by default
- No `caBundle` in ValidatingWebhookConfiguration
- AKS detects this and automatically generates certificates
- AKS injects the CA bundle into the webhook config
- Webhook is ready to use immediately

**Verify the installation:**
```bash
# Check pods are running
kubectl get pods -n kubetemplater-system

# Check webhook config - you'll see caBundle is populated by AKS
kubectl get validatingwebhookconfigurations kubetemplater-validating-webhook-configuration -o yaml | grep caBundle

# Test webhook with a sample
kubectl apply -f config/samples/kubetemplater.io_v1alpha1_kubetemplatepolicy.yaml
kubectl apply -f config/samples/kubetemplater.io_v1alpha1_kubetemplate.yaml
```

---

### Method 2: cert-manager (For non-AKS or specific needs)

If you prefer cert-manager or are deploying to non-AKS clusters:

**Step 1: Install cert-manager**
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-webhook
```

**Step 2: Install KubeTemplater with cert-manager mode**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set webhook.certificateMode=cert-manager
```

This creates:
- A self-signed Issuer
- A Certificate resource
- Automatic cert-manager CA injection

**Using a custom issuer (e.g., Let's Encrypt):**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set webhook.certificateMode=cert-manager \
  --set webhook.certManager.issuerName=letsencrypt-prod \
  --set webhook.certManager.issuerKind=ClusterIssuer
```

---

### Method 3: Manual Certificates (For air-gapped/corporate PKI)

For complete control over certificates:

**Step 1: Generate certificates**

```bash
# Generate CA
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -subj "/CN=kubetemplater-ca" -days 365 -out ca.crt

# Generate webhook server key
openssl genrsa -out tls.key 2048
openssl req -new -key tls.key -subj "/CN=kubetemplater-webhook-service.kubetemplater-system.svc" -out tls.csr

# Create SAN config
cat > san.cnf <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
[req_distinguished_name]
[v3_req]
subjectAltName = @alt_names
[alt_names]
DNS.1 = kubetemplater-webhook-service.kubetemplater-system.svc
DNS.2 = kubetemplater-webhook-service.kubetemplater-system.svc.cluster.local
EOF

# Sign certificate
openssl x509 -req -in tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out tls.crt -days 365 -extensions v3_req -extfile san.cnf
```

**Step 2: Base64 encode**

Linux/Mac:
```bash
CA_BUNDLE=$(cat ca.crt | base64 | tr -d '\n')
TLS_CERT=$(cat tls.crt | base64 | tr -d '\n')
TLS_KEY=$(cat tls.key | base64 | tr -d '\n')
```

Windows PowerShell:
```powershell
$CA_BUNDLE = [Convert]::ToBase64String([IO.File]::ReadAllBytes("ca.crt"))
$TLS_CERT = [Convert]::ToBase64String([IO.File]::ReadAllBytes("tls.crt"))
$TLS_KEY = [Convert]::ToBase64String([IO.File]::ReadAllBytes("tls.key"))
```

**Step 3: Install with manual certificates**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set webhook.certificateMode=manual \
  --set webhook.certificate.caBundle="$CA_BUNDLE" \
  --set webhook.certificate.tlsCert="$TLS_CERT" \
  --set webhook.certificate.tlsKey="$TLS_KEY"
```

---

## Why AKS Mode is Best for AKS

| Feature | AKS | GKE | EKS |
|---------|-----|-----|-----|
| Native cert auto-injection | ✅ Yes | ✅ Yes | ❌ No |
| Requires cert-manager | ❌ No | ❌ No | ✅ Yes |
| cloud-native mode works | ✅ Yes | ✅ Yes | ❌ No |
| Recommended setup | cloud-native | cloud-native | cert-manager |
| Zero config | ✅ Yes | ✅ Yes | ⚠️ No |

**Use cloud-native mode unless you have specific requirements for cert-manager or manual certificates.**

See also: [GKE Installation Guide](gke-installation.md) | [EKS Installation Guide](eks-installation.md)

---

## Configuration Values

```yaml
webhook:
  # Certificate management mode
  # "aks" = AKS native (recommended for AKS)
  # "cert-manager" = Use cert-manager
  # "manual" = Provide your own certificates
  certificateMode: "aks"
  
  # cert-manager settings (only when certificateMode=cert-manager)
  certManager:
    issuerName: ""         # Empty = chart creates self-signed issuer
    issuerKind: "Issuer"   # "Issuer" or "ClusterIssuer"
  
  # Manual certificate settings (only when certificateMode=manual)
  certificate:
    caBundle: ""    # base64-encoded CA certificate
    tlsCert: ""     # base64-encoded TLS certificate
    tlsKey: ""      # base64-encoded TLS private key
  
  # Service configuration
  service:
    port: 443
    targetPort: 9443
  
  # Failure policy
  failurePolicy: Fail    # "Fail" or "Ignore"
  
  # Timeout
  timeoutSeconds: 10
```

---

## Example: Production AKS Deployment

See `charts/kubetemplater/examples/values-aks.yaml` for a production-ready configuration:

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-aks.yaml
```

This includes:
- 2 replicas for high availability
- Pod anti-affinity for distribution
- Prometheus annotations for monitoring
- Optimized resource limits

---

## Troubleshooting

### Webhook Not Validating

**Check webhook configuration:**
```bash
kubectl get validatingwebhookconfigurations kubetemplater-validating-webhook-configuration -o yaml
```

Verify that `caBundle` is present and populated. If using AKS mode, this should be automatically injected.

**Check controller logs:**
```bash
kubectl logs -n kubetemplater-system deployment/kubetemplater-controller-manager -f
```

**Test webhook endpoint:**
```bash
kubectl run test-webhook --image=curlimages/curl -it --rm -- \
  curl -k https://kubetemplater-webhook-service.kubetemplater-system.svc:443/healthz
```

### Certificate Issues

**For AKS mode:**
- AKS manages certificates automatically
- If issues persist, try recreating the webhook configuration:
  ```bash
  kubectl delete validatingwebhookconfigurations kubetemplater-validating-webhook-configuration
  helm upgrade kubetemplater ./charts/kubetemplater --namespace kubetemplater-system
  ```

**For cert-manager mode:**
```bash
# Check certificate status
kubectl describe certificate -n kubetemplater-system

# Check certificate secret
kubectl get secret -n kubetemplater-system kubetemplater-webhook-server-cert
```

**For manual mode:**
```bash
# Verify secret exists
kubectl get secret -n kubetemplater-system kubetemplater-webhook-manual-cert

# Check secret contents
kubectl get secret kubetemplater-webhook-manual-cert -n kubetemplater-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

### AKS-Specific Issues

**Network Policies:**
- Ensure network policies allow traffic to webhook service on port 443
- Default AKS allows all traffic within cluster

**Pod Security:**
- AKS enforces restricted pod security standards
- The chart is configured to comply with these requirements

**RBAC:**
- Ensure your AKS cluster has RBAC enabled (default in modern AKS)
- Verify you have cluster-admin privileges for installation

---

## Uninstallation

```bash
# Uninstall chart
helm uninstall kubetemplater --namespace kubetemplater-system

# Remove CRDs (optional - they persist by default)
kubectl delete crd kubetemplates.kubetemplater.io.my.company.com
kubectl delete crd kubetemplatepolicies.kubetemplater.io.my.company.com

# Remove namespace
kubectl delete namespace kubetemplater-system
```

---

## Next Steps

- [Getting Started Guide](getting-started.md) - General installation and usage
- [Webhook Validation Documentation](webhook-validation.md) - Understanding webhook validation
- [Features Documentation](features.md) - Field validations and advanced features
- [Examples](examples.md) - Practical use cases
