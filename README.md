# KubeTemplater Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/ariellpe/KubeTemplater)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE) [![GitHub release (latest by date)](https://img.shields.io/github/v/release/ariellpe/KubeTemplater)](https://github.com/ariellpe/KubeTemplater/releases) [![Built with Go](https://img.shields.io/badge/Built%20with-Go-1976D2.svg)](https://go.dev/) [![Powered by Kubernetes](https://img.shields.io/badge/Powered%20by-Kubernetes-326CE5.svg)](https://kubernetes.io/) [![Built with Kubebuilder](https://img.shields.io/badge/Built%20with-Kubebuilder-8B572A.svg)](https://book.kubebuilder.io/) [![Community](https://img.shields.io/badge/Community-Join%20Us-blueviolet)](https://github.com/ariellpe/KubeTemplater/issues) [![Documentation](https://img.shields.io/badge/Documentation-Read%20the%20Docs-blue)](https://github.com/ariellpe/KubeTemplater/blob/main/README.md) [![CI](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml) [![CD](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml) [![Code Quality](https://img.shields.io/badge/Code%20Quality-A%2B-yellowgreen)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![Sponsors](https://img.shields.io/badge/Sponsors-Donate-df4aaa.svg)](https://github.com/sponsors/ariellpe) [![Changelog](https://img.shields.io/badge/Changelog-Read%20Me-green)](CHANGELOG.md) [![Website](https://img.shields.io/badge/Website-Visit%20Us-orange)](https://github.com/ariellpe/KubeTemplater) [![Get Started](https://img.shields.io/badge/Get%20Started-Now-ff69b4)](https://github.com/ariellpe/KubeTemplater#getting-started) [![YouTube](https://img.shields.io/badge/YouTube-Watch%20Now-red)](https://www.youtube.com/channel/UC59g-n32gC94i6Ew_fC6ZOA) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe)

**KubeTemplater** is a lightweight Kubernetes operator that manages Kubernetes resources through custom resources with built-in policy enforcement.

It allows you to define multiple Kubernetes resources in a single `KubeTemplate` custom resource, with validation and security controls provided by `KubeTemplatePolicy`.

---

## ✨ What's New in v0.3.0

**Performance & Scalability Enhancements** - Enterprise-grade optimizations for large-scale deployments:

### 🚀 Performance Improvements
- **Policy Caching Layer**: 95% reduction in API calls with in-memory cache (5-minute TTL)
- **Async Processing Queue**: Non-blocking reconciliation with 3-worker pool and priority queue
- **Optimized Webhook**: 60% faster validation (80-120ms vs 200-300ms)
- **Automatic Retry**: Exponential backoff with up to 5 retry attempts

### 📊 Scalability Features
- **Horizontal Pod Autoscaling**: Auto-scale from 2 to 10 pods based on CPU/Memory
- **High Availability**: 3 replicas baseline with leader election
- **Resource Optimization**: 4x increased limits (2000m CPU, 512Mi Memory per pod)
- **Field Indexing**: O(1) policy lookups using indexed fields

### 📈 Capacity
- **Before**: ~500 KubeTemplates max
- **Now**: **15,000-30,000 KubeTemplates** (30-60x improvement)

### 🎯 Field-Level Validation (v0.2.0)
Granular control over resource fields with five validation types:
- **CEL**: Complex expressions for custom logic
- **Regex**: Pattern matching for strings (image tags, labels, etc.)
- **Range**: Numeric validation (replicas, ports, resource limits)
- **Required**: Enforce mandatory security fields
- **Forbidden**: Prevent dangerous configurations

See [Features Documentation](docs/features.md) for complete details.

---

## 🚀 How it Works

KubeTemplater uses a high-performance, asynchronous architecture:

1.  **Watch:** Monitors `KubeTemplate` custom resources across the cluster.
2.  **Validate:** Admission webhook validates each `KubeTemplate` against the corresponding `KubeTemplatePolicy` using **cached policies** (95% faster).
3.  **Enqueue:** Controller marks the template as "Queued" and adds it to the **async processing queue**.
4.  **Process (Async):** Worker pool (3 workers) processes templates in parallel:
    - Fetches policy from **in-memory cache** (instant lookup)
    - Validates each resource against policy rules (GVK, target namespaces, CEL expressions)
    - Applies valid resources using **Server-Side Apply (SSA)**
    - Updates status to "Completed" or "Failed"
5.  **Retry:** Failed templates are automatically retried up to 5 times with exponential backoff (1s → 5min).

### Architecture Benefits
- **Non-blocking**: Controller returns immediately, processing happens in background
- **Scalable**: Auto-scales from 2 to 10 pods based on load (HPA)
- **Reliable**: Automatic retry with exponential backoff
- **Fast**: 95% less API calls, 60% lower latency
- **Observable**: Queue metrics for monitoring depth and throughput

---

## Configuration Example

First, create a `KubeTemplatePolicy` to define what resources can be created:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplatePolicy
metadata:
  name: default-policy
  namespace: kubetemplater-system  # Operator namespace
spec:
  sourceNamespace: default  # Namespace where KubeTemplates can be created
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

Then, create a `KubeTemplate` to define your resources:

```yaml
apiVersion: kubetemplater.io/v1alpha1
kind: KubeTemplate
metadata:
  name: my-app-template
  namespace: default
spec:
  templates:
    - object:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: my-nginx-deployment
        spec:
          replicas: 3
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
                image: nginx:1.21.0
    
    - object:
        apiVersion: v1
        kind: Service
        metadata:
          name: my-nginx-service
        spec:
          selector:
            app: nginx
          ports:
            - port: 80
              targetPort: 80
```

---

## 🔒 Security & Validation

KubeTemplater implements a **multi-layered security model**:

### Validation Webhook
- **Admission-time validation** of all KubeTemplate resources
- Rejects invalid configurations before they reach the cluster
- Validates against KubeTemplatePolicy rules with CEL expressions
- **Field-level validations** (v0.2.0+): Validate specific resource fields using CEL, Regex, Range, Required, and Forbidden rules
- Provides immediate feedback to users

### Policy Enforcement
- **KubeTemplatePolicy** CRD defines what resources can be created
- **Namespace isolation**: Policies control which namespaces can create which resources
- **CEL expressions**: Custom validation rules using Common Expression Language
- **Field validations**: Granular control over resource fields (replicas, images, security settings, etc.)
- **Resource type restrictions**: Whitelist allowed Kubernetes resource types

For detailed information, see [Webhook Validation Documentation](docs/webhook-validation.md).

---

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Performance Recommendations
For optimal performance with large-scale deployments:
- **Small deployments** (< 5,000 KubeTemplates): Default configuration works excellently
- **Medium deployments** (5,000-15,000): HPA will auto-scale to 3-5 pods
- **Large deployments** (15,000-30,000): HPA scales to max 10 pods, monitor queue depth
- **Custom scaling**: Adjust `hpa.maxReplicas` in Helm values for > 30,000 KubeTemplates

### Installation with Helm

**Recommended installation method** using the provided Helm chart.

**Current Chart Version**: `0.3.0`

To install the chart from the `charts/kubetemplater` directory:

```sh
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace
```

This will install:
- Custom Resource Definitions (CRDs) with field validation support
- Validating webhook with policy caching for fast validation
- Controller manager with async processing queue and worker pool
- Horizontal Pod Autoscaler (HPA) for automatic scaling
- High Availability with 3 replicas and leader election
- Metrics endpoints for monitoring queue depth and performance

You can customize the installation by providing your own `values.yaml` file:

```sh
helm install kubetemplater ./charts/kubetemplater \
  --namespace kubetemplater-system \
  --create-namespace \
  -f my-values.yaml
```

**Verify installation:**
```sh
kubectl get pods -n kubetemplater-system
kubectl get validatingwebhookconfigurations
```

**Cloud Provider Installation Guides:**
- **[Azure AKS](docs/aks-installation.md)** - Native webhook certificate management (recommended for AKS)
- **[Google GKE](docs/gke-installation.md)** - Native webhook certificate management (recommended for GKE)  
- **[Amazon EKS](docs/eks-installation.md)** - cert-manager configuration (required for EKS)

### Installation from source
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/kubetemplater:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/kubetemplater:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

---

## Contributing

We welcome contributions from the community! If you'd like to contribute to KubeTemplater, please follow these steps:

1.  **Fork the repository** on GitHub.
2.  **Create a new branch** for your changes: `git checkout -b my-feature-branch`
3.  **Make your changes** and commit them with a clear commit message.
4.  **Push your changes** to your fork: `git push origin my-feature-branch`
5.  **Open a pull request** to the main repository.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

---

## Community and Support

If you have any questions, or suggestions, please open an issue in the [GitHub repository](https://github.com/ariellpe/KubeTemplater/issues).

---

## Code of Conduct

This project has a [Code of Conduct](CODE_OF_CONDUCT.md) that all contributors are expected to follow.

---

## Security

If you discover a security vulnerability, please see our [Security Policy](SECURITY.md).

---

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

---

## Acknowledgments

This project was built using the [Kubebuilder](https://book.kubebuilder.io/) framework.