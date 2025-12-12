# KubeTemplater Helm Chart

Helm chart for deploying KubeTemplater operator to Kubernetes clusters.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+

## Installation

### Quick Start (Self-Signed Certificates - Recommended)

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

This installs KubeTemplater with:
- Self-signed certificate management (automatic, no dependencies)
- 1 replica (increase for production)
- Validating webhook enabled

### Production Installation

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --values values-prod.yaml
```

**values-prod.yaml example:**
```yaml
replicaCount: 2

image:
  repository: your-registry/kubetemplater
  tag: "0.3.2"

webhook:
  enabled: true
  certificateMode: "self-signed"
  failurePolicy: Fail  # Strict validation in production

resources:
  limits:
    cpu: 2000m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi

tolerations:
  - operator: Exists

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: kubetemplater
          topologyKey: kubernetes.io/hostname
```

## Certificate Management Modes

### Self-Signed (Default - Recommended)

**Automatic certificate generation and renewal:**
- No external dependencies (cert-manager not required)
- Works on all platforms (AKS, EKS, GKE, on-premise)
- Leader pod generates certificates on startup
- Automatic renewal 30 days before expiration
- Certificates shared via Kubernetes Secret

```yaml
webhook:
  enabled: true
  certificateMode: "self-signed"
```

**How it works:**
1. Leader pod checks Secret for existing certificates
2. Generates RSA 2048-bit self-signed cert if missing/expired
3. Stores in Secret (shared across all replicas)
4. Kubelet syncs Secret to pod volume mounts
5. Non-leader pods wait for certificates (60s timeout)
6. Leader checks daily for renewal

### Cloud-Native

**Provider-managed certificates (GKE only):**
- Google GKE auto-injects certificates
- ⚠️ Not supported on Azure AKS or Amazon EKS

```yaml
webhook:
  certificateMode: "cloud-native"
```

### Cert-Manager

**Managed by cert-manager (requires cert-manager installed):**

```yaml
webhook:
  certificateMode: "cert-manager"
  certManager:
    issuerName: "my-issuer"  # Leave empty for self-signed issuer created by chart
    issuerKind: "Issuer"     # or "ClusterIssuer"
```

### Manual

**Bring your own certificates:**

```yaml
webhook:
  certificateMode: "manual"
  certificate:
    caBundle: "<base64-encoded-ca-cert>"
    tlsCert: "<base64-encoded-tls-cert>"
    tlsKey: "<base64-encoded-tls-key>"
```

## Configuration

### Common Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `kubetemplater` |
| `image.tag` | Image tag | `Chart appVersion` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `webhook.enabled` | Enable validating webhook | `true` |
| `webhook.certificateMode` | Certificate mode (`self-signed`, `cloud-native`, `cert-manager`, `manual`) | `self-signed` |
| `webhook.failurePolicy` | Webhook failure policy (`Fail` or `Ignore`) | `Fail` |
| `webhook.timeoutSeconds` | Webhook timeout | `10` |

### Resource Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |

### High Availability

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tolerations` | Pod tolerations | `[]` |
| `affinity` | Pod affinity rules | `{}` |
| `podAnnotations` | Pod annotations | `{}` |

## Platform-Specific Configurations

### Azure AKS

```yaml
replicaCount: 2

webhook:
  certificateMode: "self-signed"  # cloud-native not supported on AKS
  failurePolicy: Ignore           # or Fail for production

tolerations:
  - operator: Exists  # Run on any node

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: kubetemplater
          topologyKey: kubernetes.io/hostname
```

### Google GKE

```yaml
webhook:
  certificateMode: "cloud-native"  # or "self-signed"
```

### Amazon EKS

```yaml
webhook:
  certificateMode: "self-signed"  # or "cert-manager" if installed
```

## Upgrading

### From v0.3.1 or earlier

If upgrading from a version that used `cloud-native` mode on AKS:

```bash
# Update values to use self-signed
helm upgrade kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --set webhook.certificateMode=self-signed \
  --reuse-values
```

The operator will automatically generate certificates on restart.

## Uninstalling

```bash
helm uninstall kubetemplater --namespace kubetemplater-system
```

**Note:** Cluster-scoped resources (CRDs, ClusterRoles, ValidatingWebhookConfiguration) have `helm.sh/resource-policy: keep` annotations and will be retained. Delete manually if needed:

```bash
kubectl delete crd kubetemplates.kubetemplater.io.my.company.com
kubectl delete crd kubetemplatepolicies.kubetemplater.io.my.company.com
kubectl delete validatingwebhookconfiguration kubetemplater-validating-webhook-configuration
kubectl delete clusterrole kubetemplater-manager-role
kubectl delete clusterrolebinding kubetemplater-manager-rolebinding
```

## Troubleshooting

### Pods not starting - waiting for certificates

**Symptom:** Non-leader pods log "Waiting for leader to generate certificates..."

**Solution:** This is normal. Leader pod generates certificates, other pods wait (max 60s). If timeout occurs:
1. Check leader pod logs: `kubectl logs -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater --tail=100`
2. Verify Secret created: `kubectl get secret -n kubetemplater-system kubetemplater-webhook-cert`
3. Check RBAC permissions: `kubectl auth can-i update secrets --as=system:serviceaccount:kubetemplater-system:kubetemplater -n kubetemplater-system`

### Webhook validation failing

**Symptom:** Resources rejected with "no endpoints available for service"

**Solution:**
1. Verify pods running: `kubectl get pods -n kubetemplater-system`
2. Check webhook config: `kubectl get validatingwebhookconfiguration kubetemplater-validating-webhook-configuration -o yaml`
3. Check service endpoints: `kubectl get endpoints -n kubetemplater-system kubetemplater-webhook-service`
4. Temporarily set `failurePolicy: Ignore` for debugging

### Certificate renewal not working

**Symptom:** Certificates expired, pods crashing

**Solution:**
1. Check leader pod logs for renewal attempts
2. Verify leader has RBAC permissions to update Secret
3. Manually delete Secret to force regeneration: `kubectl delete secret -n kubetemplater-system kubetemplater-webhook-cert`
4. Restart pods: `kubectl rollout restart deployment -n kubetemplater-system kubetemplater`

## Support

- GitHub Issues: https://github.com/ariellpe/KubeTemplater/issues
- Documentation: https://github.com/ariellpe/KubeTemplater/blob/main/README.md

## License

Apache License 2.0
