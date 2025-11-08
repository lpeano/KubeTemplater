# KubeTemplater Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/ariellpe/KubeTemplater)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE) [![GitHub release (latest by date)](https://img.shields.io/github/v/release/ariellpe/KubeTemplater)](https://github.com/ariellpe/KubeTemplater/releases) [![Built with Go](https://img.shields.io/badge/Built%20with-Go-1976D2.svg)](https://go.dev/) [![Powered by Kubernetes](https://img.shields.io/badge/Powered%20by-Kubernetes-326CE5.svg)](https://kubernetes.io/) [![Built with Kubebuilder](https://img.shields.io/badge/Built%20with-Kubebuilder-8B572A.svg)](https://book.kubebuilder.io/) [![Community](https://img.shields.io/badge/Community-Join%20Us-blueviolet)](https://github.com/ariellpe/KubeTemplater/issues) [![Documentation](https://img.shields.io/badge/Documentation-Read%20the%20Docs-blue)](https://github.com/ariellpe/KubeTemplater/blob/main/README.md) [![CI](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/test.yml) [![CD](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml/badge.svg)](https://github.com/ariellpe/KubeTemplater/actions/workflows/release.yml) [![Code Quality](https://img.shields.io/badge/Code%20Quality-A%2B-yellowgreen)](https://goreportcard.com/report/github.com/ariellpe/KubeTemplater) [![Sponsors](https://img.shields.io/badge/Sponsors-Donate-df4aaa.svg)](https://github.com/sponsors/ariellpe) [![Changelog](https://img.shields.io/badge/Changelog-Read%20Me-green)](CHANGELOG.md) [![Website](https://img.shields.io/badge/Website-Visit%20Us-orange)](https://github.com/ariellpe/KubeTemplater) [![Get Started](https://img.shields.io/badge/Get%20Started-Now-ff69b4)](https://github.com/ariellpe/KubeTemplater#getting-started) [![YouTube](https://img.shields.io/badge/YouTube-Watch%20Now-red)](https://www.youtube.com/channel/UC59g-n32gC94i6Ew_fC6ZOA) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe) [![Twitter](https://img.shields.io/twitter/follow/ariellpe.svg?style=social)](https://twitter.com/ariellpe)

**KubeTemplater** is a lightweight Kubernetes operator that dynamically renders and applies Kubernetes resources using Go templates defined within your `ConfigMap`s.

It allows you to manage the state of multiple resources from a single configuration file, acting as a config-driven resource generator.

---

## 🚀 How it Works

KubeTemplater follows a simple reconciliation loop:

1.  **Watch:** It monitors all `ConfigMap` resources across the cluster.
2.  **Filter:** It only processes `ConfigMap`s that have the specific annotation: `kubetemplater.io: "true"`.
3.  **Read:** It parses a YAML array located in the `data["resources.yaml"]` key of the annotated `ConfigMap`.
4.  **Render:** For each item in the array, it renders the Go template (`template: ...`) using the provided list of `values: ...`.
5.  **Apply:** It applies the final rendered manifest to the cluster using a **Server-Side Apply (SSA)**. This ensures that the resource is only updated if the rendered template differs from the live state, or if the resource does not exist.

---

## Configuration Example

To instruct KubeTemplater to create resources, you apply a `ConfigMap` structured like this:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-app-template
  namespace: default
  annotations:
    # The annotation that activates the operator
    kubetemplater.io: "true"
data:
  # The data key containing the list of resources to render
  resources.yaml: |
    - name: my-nginx-deployment
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: {{ .deployName }}
        spec:
          replicas: {{ .replicas }}
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
                image: "nginx:1.21.0"
      values:
        - name: deployName
          value: "my-nginx-deployment"
        - name: replicas
          value: "3"

    - name: my-nginx-service
      template: |
        apiVersion: v1
        kind: Service
        metadata:
          name: my-nginx-service
        spec:
          selector:
            app: nginx
          ports:
            - port: 80
      values: []
```

---

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Installation with Helm

You can install KubeTemplater using the provided Helm chart.

To install the chart from the `charts/kubetemplater` directory, run the following command:

```sh
helm install kubetemplater ./charts/kubetemplater --namespace kubetemplater-system --create-namespace
```

You can customize the installation by providing your own `values.yaml` file:

```sh
helm install kubetemplater ./charts/kubetemplater --namespace kubetemplater-system --create-namespace -f my-values.yaml
```

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