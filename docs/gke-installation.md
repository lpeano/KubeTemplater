# Installation Guide

This guide provides instructions for deploying KubeTemplater to any Kubernetes cluster.

## Prerequisites

- Kubernetes 1.19+
- `kubectl` configured to access your cluster
- Helm v3.0.0 or higher

## Simplified Installation (All Platforms)

Thanks to the robust, self-contained certificate management system, installing KubeTemplater is now identical and simple across all Kubernetes platforms, including **AKS, EKS, GKE, and on-premise clusters**.

There is only one recommended method:

```bash
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

**That's it!**

### What Happens Automatically?

The default (and only) mode for certificate management is `self-signed`. Here's what it does automatically, with no configuration needed:

1.  **Leader Election:** The operator pods elect a leader.
2.  **Certificate Generation:** The leader pod generates a self-signed TLS certificate and stores it in a Kubernetes `Secret`.
3.  **Webhook Patching:** The leader then automatically **patches** the `ValidatingWebhookConfiguration` resource, injecting the certificate's CA bundle. This critical step ensures the Kubernetes API server trusts the webhook.
4.  **In-Memory Sync:** All other pods watch the `Secret` and load the certificate directly into memory, ready to serve traffic.

This process is fully automated and removes the need for `cert-manager` or special cloud provider integrations.

### Verify the Installation

```bash
# 1. Check that the pods are running
kubectl get pods -n kubetemplater-system

# 2. Wait a few moments for the leader to be elected and patch the webhook.
#    Then, check that the caBundle has been populated.
kubectl get validatingwebhookconfiguration kubetemplater-validating-webhook-configuration -o yaml | grep caBundle

# 3. Test the operator with a sample policy and template
kubectl apply -f config/samples/kubetemplater.io_v1alpha1_kubetemplatepolicy.yaml
kubectl apply -f config/samples/kubetemplater.io_v1alpha1_kubetemplate.yaml
```
A non-empty `caBundle` field confirms the operator has successfully started and configured itself.

---

## Configuration

The Helm chart is now much simpler. The only webhook-related values you might configure are:

```yaml
webhook:
  # Whether the validating webhook is enabled.
  enabled: true
  
  # What happens if the webhook is unavailable. Use "Fail" for production.
  failurePolicy: Fail
  
  # How long the API server should wait for the webhook to respond.
  timeoutSeconds: 10
```
All certificate-related values have been removed as they are no longer needed.

---

## Troubleshooting

### Webhook Not Becoming Ready

**Symptom:** The `caBundle` in the `ValidatingWebhookConfiguration` remains empty and webhook calls fail.

**Cause:** The leader pod may not have the required RBAC permissions to patch the `ValidatingWebhookConfiguration`.

**Debug:**
```bash
# 1. Check the logs of the leader pod for patching errors.
#    First, find the leader:
kubectl get lease -n kubetemplater-system -o jsonpath='{.spec.holderIdentity}'

#    Then check its logs:
kubectl logs -n kubetemplater-system <leader-pod-name> | grep "Failed to patch"

# 2. Verify RBAC permissions for the operator's ServiceAccount:
kubectl auth can-i patch validatingwebhookconfigurations --as=system:serviceaccount:kubetemplater-system:kubetemplater
```

---

## Uninstallation

```bash
# Uninstall the chart
helm uninstall kubetemplater --namespace kubetemplater-system

# CRDs and ClusterRoles are kept by default. Delete them manually if desired.
kubectl delete crd kubetemplates.kubetemplater.io.my.company.com
kubectl delete crd kubetemplatepolicies.kubetemplater.io.my.company.com
kubectl delete validatingwebhookconfiguration kubetemplater-validating-webhook-configuration
kubectl delete clusterrole kubetemplater-manager-role
kubectl delete clusterrolebinding kubetemplater-manager-rolebinding

# Delete the namespace
kubectl delete namespace kubetemplater-system
```
