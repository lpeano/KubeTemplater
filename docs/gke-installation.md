# Installing KubeTemplater on Google Kubernetes Engine (GKE)

This guide provides instructions for deploying KubeTemplater on Google Kubernetes Engine with native GKE webhook support.

## Prerequisites

- Google Cloud SDK (gcloud) installed and configured
- kubectl configured to access your GKE cluster
- Helm v3.0.0 or higher

## GKE Native Webhook Support

**Good news!** Like AKS, GKE provides **built-in support for admission webhooks** without requiring cert-manager or manual certificate management.

When you deploy a ValidatingWebhookConfiguration without a `caBundle`, GKE automatically:
1. ✅ Generates TLS certificates for your webhook service
2. ✅ Injects the CA bundle into the webhook configuration
3. ✅ Manages certificate rotation automatically

This is the **simplest and recommended approach** for GKE.

---

## Quick Installation (Recommended) 🎯

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

**That's it!** GKE handles all certificate management automatically.

**Verify installation:**
```bash
# Check pods
kubectl get pods -n kubetemplater-system

# Verify caBundle is injected by GKE
kubectl get validatingwebhookconfigurations kubetemplater-validating-webhook-configuration \
  -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d | openssl x509 -text -noout

# Test webhook
kubectl apply -f config/samples/
```

---

## Production Deployment

For production with high availability:

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-gke.yaml
```

This includes:
- 2 replicas for high availability
- Pod anti-affinity for distribution across nodes
- Prometheus annotations for monitoring
- Optimized resource limits for GKE

---

## Alternative: Using cert-manager

If you prefer cert-manager (e.g., for multi-cloud consistency):

**Step 1: Install cert-manager**
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Wait for ready
kubectl wait --for=condition=Available --timeout=300s \
  -n cert-manager deployment/cert-manager-webhook
```

**Step 2: Install KubeTemplater with cert-manager**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set webhook.certificateMode=cert-manager
```

---

## Using Google Container Registry (GCR)

**Build and push to GCR:**
```bash
# Set your project ID
export PROJECT_ID=your-gcp-project-id

# Build image
docker build -t gcr.io/$PROJECT_ID/kubetemplater:0.2.0 .

# Push to GCR
docker push gcr.io/$PROJECT_ID/kubetemplater:0.2.0

# Install with custom image
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set image.repository=gcr.io/$PROJECT_ID/kubetemplater \
  --set image.tag=0.2.0
```

---

## GKE Workload Identity (Recommended)

To use GKE Workload Identity for secure access to Google Cloud services:

**Step 1: Create GCP Service Account**
```bash
gcloud iam service-accounts create kubetemplater \
  --project=$PROJECT_ID

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member "serviceAccount:kubetemplater@$PROJECT_ID.iam.gserviceaccount.com" \
  --role "roles/monitoring.metricWriter"
```

**Step 2: Bind to Kubernetes Service Account**
```bash
gcloud iam service-accounts add-iam-policy-binding \
  kubetemplater@$PROJECT_ID.iam.gserviceaccount.com \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:$PROJECT_ID.svc.id.goog[kubetemplater-system/kubetemplater]"
```

**Step 3: Install with Workload Identity annotation**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="kubetemplater@$PROJECT_ID.iam.gserviceaccount.com"
```

---

## GKE-Specific Features

### Autopilot Clusters

KubeTemplater works on GKE Autopilot with default settings. The chart is configured to comply with Autopilot's security requirements:
- No privileged containers
- Resource requests/limits defined
- SecurityContext with runAsNonRoot

### Private GKE Clusters

For private GKE clusters, ensure firewall rules allow webhook traffic:

```bash
gcloud compute firewall-rules create allow-webhook \
  --allow tcp:9443 \
  --source-ranges=<master-cidr> \
  --target-tags=gke-node \
  --description="Allow webhook traffic from GKE control plane"
```

Replace `<master-cidr>` with your cluster's master CIDR (find with `gcloud container clusters describe`).

---

## Configuration Values

```yaml
webhook:
  # Use GKE native certificate management
  certificateMode: "cloud-native"  # Recommended for GKE
  
  service:
    port: 443
    targetPort: 9443
  
  failurePolicy: Fail
  timeoutSeconds: 10
```

---

## Troubleshooting

### Webhook Not Validating

**Check webhook configuration:**
```bash
kubectl get validatingwebhookconfigurations \
  kubetemplater-validating-webhook-configuration -o yaml
```

Verify `caBundle` is present and populated by GKE.

**Check controller logs:**
```bash
kubectl logs -n kubetemplater-system \
  deployment/kubetemplater-controller-manager -f
```

### Certificate Issues

GKE manages certificates automatically. If issues persist:

```bash
# Recreate webhook configuration
kubectl delete validatingwebhookconfigurations \
  kubetemplater-validating-webhook-configuration

helm upgrade kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system
```

### Private Cluster Webhook Failures

If webhook calls timeout on private clusters:

1. **Check firewall rules:**
```bash
gcloud compute firewall-rules list --filter="name~webhook"
```

2. **Add firewall rule if missing** (see Private GKE Clusters section above)

3. **Verify master authorized networks:**
```bash
gcloud container clusters describe <cluster-name> \
  --format="value(masterAuthorizedNetworksConfig)"
```

---

## Comparison: GKE vs AKS vs EKS

| Feature | GKE | AKS | EKS |
|---------|-----|-----|-----|
| Native cert auto-injection | ✅ Yes | ✅ Yes | ❌ No |
| Requires cert-manager | ❌ No | ❌ No | ✅ Yes |
| Recommended mode | cloud-native | cloud-native | cert-manager |
| Zero config setup | ✅ Yes | ✅ Yes | ⚠️ No |

---

## Uninstallation

```bash
# Uninstall chart
helm uninstall kubetemplater --namespace kubetemplater-system

# Remove CRDs (optional)
kubectl delete crd kubetemplates.kubetemplater.io.my.company.com
kubectl delete crd kubetemplatepolicies.kubetemplater.io.my.company.com

# Remove namespace
kubectl delete namespace kubetemplater-system
```

---

## Next Steps

- [Getting Started Guide](getting-started.md)
- [Webhook Validation Documentation](webhook-validation.md)
- [Features Documentation](features.md)
- [Examples](examples.md)
