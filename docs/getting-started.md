# Getting Started with KubeTemplater

This guide will walk you through the installation of the KubeTemplater operator.

## Prerequisites

Before you begin, ensure you have the following tools installed and configured:

- **Go**: Version `v1.24.0` or higher.
- **Docker**: Version `17.03` or higher.
- **kubectl**: Version `v1.11.3` or higher.
- **Helm**: Version `v3.0.0` or higher.
- Access to a Kubernetes cluster (e.g., Minikube, Kind, or a cloud provider's cluster).

## Installation Methods

You can install the operator in two ways:

1.  **Using Helm (Recommended)**: For a straightforward installation into any Kubernetes environment.
2.  **From Source**: For development purposes or for building a custom version of the operator.

---

### 1. Installation with Helm

The provided Helm chart is the recommended way to install KubeTemplater.

1.  **Add the Helm Repository (if available)**
    *(Note: This step is a placeholder. If a repository is set up, update the command.)*
    ```sh
    # helm repo add kubetemplater <repository-url>
    # helm repo update
    ```

2.  **Install the Chart**
    To install the chart from the local directory, run the following command. This will create a new namespace for the operator and deploy it.

    ```sh
    helm install kubetemplater ./charts/kubetemplater \
      --namespace kubetemplater-system \
      --create-namespace
    ```

3.  **Customize the Installation**
    You can customize the deployment by creating a `my-values.yaml` file and passing it during installation:

    ```sh
    helm install kubetemplater ./charts/kubetemplater \
      --namespace kubetemplater-system \
      --create-namespace \
      -f my-values.yaml
    ```

---

### 2. Installation from Source

Follow these steps to build and deploy the operator from the source code.

1.  **Build and Push the Container Image**
    Build the operator's container image and push it to a container registry that your Kubernetes cluster can access.

    ```sh
    make docker-build docker-push IMG=<some-registry>/kubetemplater:tag
    ```
    > **Note**: Replace `<some-registry>/kubetemplater:tag` with your registry path (e.g., `docker.io/your-user/kubetemplater:latest`).

2.  **Install CRDs**
    This command installs the necessary Custom Resource Definitions into the cluster. This step is only needed once.

    ```sh
    make install
    ```

3.  **Deploy the Operator**
    Deploy the KubeTemplater controller manager to the cluster, referencing the image you pushed.

    ```sh
    make deploy IMG=<some-registry>/kubetemplater:tag
    ```
    > **Note**: If you encounter RBAC errors, you may need cluster-admin privileges.

### Uninstallation

**To uninstall with Helm:**

```sh
helm uninstall kubetemplater --namespace kubetemplater-system
```

**To uninstall from a source installation:**

1.  **Undeploy the controller:**
    ```sh
    make undeploy
    ```
2.  **Uninstall the CRDs:**
    ```sh
    make uninstall
    ```
