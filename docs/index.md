# Welcome to KubeTemplater Documentation

KubeTemplater is a lightweight, policy-driven Kubernetes operator that dynamically applies Kubernetes resources using `KubeTemplate` custom resources with built-in validation and security policies.

**Current Version**: `0.2.0` - Now with field-level validation support!

This documentation provides a comprehensive guide to understanding, installing, and using the KubeTemplater operator.

## 📖 Table of Contents

- **[Getting Started](./getting-started.md)**: Learn how to install and set up the operator in your cluster (includes Helm v0.2.0 installation).

### ☁️ Cloud Provider Installation Guides
- **[AKS Installation (Azure)](./aks-installation.md)**: Azure Kubernetes Service with native certificate management
- **[GKE Installation (Google Cloud)](./gke-installation.md)**: Google Kubernetes Engine with native certificate management
- **[EKS Installation (AWS)](./eks-installation.md)**: Amazon EKS with cert-manager configuration

### 📚 Core Documentation
- **[How It Works](./how-it-works.md)**: Understand the core concepts and reconciliation logic of the operator.
- **[Advanced Features](./features.md)**: Discover advanced features like field validations, replace strategy, and webhook validation.
- **[Examples](./examples.md)**: Find practical examples for common use cases including field-level validations.

### 🔒 Security & Validation

- **[Validation Webhook](./webhook-validation.md)**: Complete guide to the admission webhook that validates KubeTemplate resources against policies.
- **[Webhook Examples](./webhook-example.md)**: Step-by-step examples showing webhook validation in action.
- **[Webhook Deployment](./webhook-deployment.md)**: Detailed guide for deploying and configuring the validation webhook.

### 🚀 CI/CD & Operations

- **[CI/CD Pipelines](./ci-cd.md)**: GitHub Actions and Azure DevOps pipelines for building and publishing container images.

---

For information on contributing, community support, or security, please see the main [README.md](https://github.com/ariellpe/KubeTemplater/blob/main/README.md) file.
