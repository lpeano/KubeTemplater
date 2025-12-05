# Installing KubeTemplater on Amazon Elastic Kubernetes Service (EKS)

This guide provides instructions for deploying KubeTemplater on Amazon EKS with cert-manager for webhook certificates.

## Prerequisites

- AWS CLI installed and configured
- kubectl configured to access your EKS cluster
- Helm v3.0.0 or higher
- **cert-manager** (required for EKS)

## ⚠️ Important: EKS Requires cert-manager

**Unlike AKS and GKE**, EKS does **NOT** support automatic certificate injection for webhooks.

You **MUST** use one of these approaches:
1. **cert-manager** (recommended) ✅
2. **Manual certificates** (for air-gapped environments)

---

## Installation Steps

### Step 1: Install cert-manager (Required)

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available --timeout=300s \
  -n cert-manager deployment/cert-manager-webhook

# Verify installation
kubectl get pods -n cert-manager
```

All cert-manager pods should be in `Running` state before proceeding.

---

### Step 2: Install KubeTemplater

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-eks.yaml
```

This will:
- Create a self-signed Issuer for webhook certificates
- Generate a Certificate resource managed by cert-manager
- Configure the validating webhook with cert-manager CA injection
- Deploy the controller with TLS enabled

**Verify installation:**
```bash
# Check all pods are running
kubectl get pods -n kubetemplater-system

# Check certificate is ready
kubectl get certificate -n kubetemplater-system

# Verify webhook configuration
kubectl get validatingwebhookconfigurations
```

The certificate should show `Ready=True`:
```bash
kubectl get certificate -n kubetemplater-system kubetemplater-serving-cert
```

---

## Production Deployment

For production with high availability across availability zones:

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-eks.yaml \
  --set replicaCount=3
```

This distributes pods across multiple AZs using pod anti-affinity.

---

## Using Amazon ECR

**Build and push to ECR:**

```bash
# Set variables
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export AWS_REGION=us-east-1

# Create ECR repository
aws ecr create-repository --repository-name kubetemplater --region $AWS_REGION

# Login to ECR
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com

# Build and push
docker build -t $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/kubetemplater:0.2.0 .
docker push $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/kubetemplater:0.2.0

# Install with ECR image
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-eks.yaml \
  --set image.repository=$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/kubetemplater \
  --set image.tag=0.2.0
```

---

## IAM Roles for Service Accounts (IRSA)

To grant the controller AWS permissions using IRSA:

**Step 1: Create IAM policy** (if needed for your use case)
```bash
cat > kubetemplater-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "cloudwatch:PutMetricData"
      ],
      "Resource": "*"
    }
  ]
}
EOF

aws iam create-policy \
  --policy-name KubeTemplaterPolicy \
  --policy-document file://kubetemplater-policy.json
```

**Step 2: Create IAM role with OIDC**
```bash
# Get OIDC provider
export OIDC_PROVIDER=$(aws eks describe-cluster --name <cluster-name> \
  --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")

# Create trust policy
cat > trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::$AWS_ACCOUNT_ID:oidc-provider/$OIDC_PROVIDER"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "$OIDC_PROVIDER:sub": "system:serviceaccount:kubetemplater-system:kubetemplater"
        }
      }
    }
  ]
}
EOF

# Create IAM role
aws iam create-role \
  --role-name KubeTemplaterRole \
  --assume-role-policy-document file://trust-policy.json

# Attach policy
aws iam attach-role-policy \
  --role-name KubeTemplaterRole \
  --policy-arn arn:aws:iam::$AWS_ACCOUNT_ID:policy/KubeTemplaterPolicy
```

**Step 3: Install with IRSA annotation**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f charts/kubetemplater/examples/values-eks.yaml \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"="arn:aws:iam::$AWS_ACCOUNT_ID:role/KubeTemplaterRole"
```

---

## Alternative: Manual Certificates

For air-gapped environments or custom PKI:

**Generate certificates** (same process as in general docs), then:

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

## Using AWS Private CA with cert-manager

For enterprise environments using AWS Private CA:

**Step 1: Install AWS PCA Issuer for cert-manager**
```bash
helm repo add awspca https://cert-manager.github.io/aws-privateca-issuer
helm install aws-pca-issuer awspca/aws-privateca-issuer \
  --namespace cert-manager \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"="arn:aws:iam::$AWS_ACCOUNT_ID:role/CertManagerPCARole"
```

**Step 2: Create AWSPCAClusterIssuer**
```bash
cat <<EOF | kubectl apply -f -
apiVersion: awspca.cert-manager.io/v1beta1
kind: AWSPCAClusterIssuer
metadata:
  name: aws-pca-issuer
spec:
  arn: arn:aws:acm-pca:$AWS_REGION:$AWS_ACCOUNT_ID:certificate-authority/<ca-id>
  region: $AWS_REGION
EOF
```

**Step 3: Install KubeTemplater with AWS PCA**
```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  --set webhook.certificateMode=cert-manager \
  --set webhook.certManager.issuerName=aws-pca-issuer \
  --set webhook.certManager.issuerKind=AWSPCAClusterIssuer
```

---

## EKS-Specific Configuration

### Security Groups for Pods

If using Security Groups for Pods, ensure the controller's security group allows:
- Inbound TCP 9443 from API server
- Outbound to API server

### VPC CNI Configuration

For private subnets, ensure NAT gateway or VPC endpoints are configured for:
- ECR (if using ECR for images)
- Certificate validation (if using AWS Private CA)

---

## Configuration Values for EKS

```yaml
webhook:
  # REQUIRED: EKS does not support cloud-native mode
  certificateMode: "cert-manager"
  
  certManager:
    issuerName: ""           # Empty = self-signed issuer
    issuerKind: "Issuer"     # Or "AWSPCAClusterIssuer"
  
  service:
    port: 443
    targetPort: 9443
  
  failurePolicy: Fail
  timeoutSeconds: 10
```

---

## Troubleshooting

### cert-manager Certificate Not Ready

**Check certificate status:**
```bash
kubectl describe certificate -n kubetemplater-system kubetemplater-serving-cert
```

**Common issues:**
- cert-manager webhook not ready: Wait longer or restart cert-manager
- RBAC issues: Ensure cert-manager has proper permissions

**Check cert-manager logs:**
```bash
kubectl logs -n cert-manager deployment/cert-manager -f
```

### Webhook Connection Timeouts

**For private EKS clusters:**

1. **Check security groups:**
```bash
aws eks describe-cluster --name <cluster-name> \
  --query 'cluster.resourcesVpcConfig.clusterSecurityGroupId'
```

2. **Add security group rule:**
```bash
aws ec2 authorize-security-group-ingress \
  --group-id <cluster-sg-id> \
  --protocol tcp \
  --port 9443 \
  --source-group <cluster-sg-id>
```

### Certificate Renewal Issues

cert-manager automatically renews certificates. If renewal fails:

```bash
# Force certificate renewal
kubectl delete certificate -n kubetemplater-system kubetemplater-serving-cert

# cert-manager will recreate it automatically
kubectl get certificate -n kubetemplater-system -w
```

---

## Comparison: Why EKS Needs cert-manager

| Feature | EKS | AKS | GKE |
|---------|-----|-----|-----|
| Native cert auto-injection | ❌ No | ✅ Yes | ✅ Yes |
| Requires cert-manager | ✅ Yes | ❌ No | ❌ No |
| cloud-native mode works | ❌ No | ✅ Yes | ✅ Yes |
| Recommended setup | cert-manager | cloud-native | cloud-native |

**EKS requires additional setup**, but cert-manager provides:
- ✅ Automatic certificate rotation
- ✅ Multiple CA options (self-signed, Let's Encrypt, AWS PCA)
- ✅ Enterprise-grade certificate management

---

## Uninstallation

```bash
# Uninstall KubeTemplater
helm uninstall kubetemplater --namespace kubetemplater-system

# Remove CRDs (optional)
kubectl delete crd kubetemplates.kubetemplater.io.my.company.com
kubectl delete crd kubetemplatepolicies.kubetemplater.io.my.company.com

# Remove namespace
kubectl delete namespace kubetemplater-system

# Optionally remove cert-manager
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

---

## Next Steps

- [Getting Started Guide](getting-started.md)
- [Webhook Validation Documentation](webhook-validation.md)
- [Features Documentation](features.md)
- [Examples](examples.md)
