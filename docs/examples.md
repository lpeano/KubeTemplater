# KubeTemplater Examples

This page provides practical examples of how to use KubeTemplater to manage different kinds of Kubernetes resources.

## Example 1: Basic Nginx Deployment and Service

This example shows how to deploy a simple Nginx web server and expose it with a `ClusterIP` service. Both resources are managed from a single `ConfigMap`.

### `ConfigMap` Manifest

Create the following `ConfigMap` and apply it to your cluster (`kubectl apply -f your-configmap.yaml`).

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-template
  namespace: default
  annotations:
    # This annotation activates the operator
    kubetemplater.io/enabled: "true"
data:
  resources.yaml: |
    # Resource 1: The Nginx Deployment
    - name: nginx-deployment
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: {{ .name }}
          labels:
            app: {{ .name }}
        spec:
          replicas: {{ .replicas }}
          selector:
            matchLabels:
              app: {{ .name }}
          template:
            metadata:
              labels:
                app: {{ .name }}
            spec:
              containers:
              - name: nginx
                image: "nginx:1.23"
                ports:
                - containerPort: 80
      values:
        - name: name
          value: "nginx-server"
        - name: replicas
          value: "2"

    # Resource 2: The Service to expose Nginx
    - name: nginx-service
      template: |
        apiVersion: v1
        kind: Service
        metadata:
          name: nginx-service
        spec:
          selector:
            app: nginx-server # This must match the app label in the deployment
          ports:
            - protocol: TCP
              port: 80
              targetPort: 80
          type: ClusterIP
      values: [] # No values needed for this simple service
```

### What Happens

1.  KubeTemplater detects the `ConfigMap` because of the `kubetemplater.io/enabled: "true"` annotation.
2.  It renders the `Deployment` template, replacing `{{ .name }}` with `"nginx-server"` and `{{ .replicas }}` with `"2"`.
3.  It renders the `Service` template (which has no dynamic values).
4.  It applies both manifests to the `default` namespace. You can now change the number of replicas in the `ConfigMap`, and the operator will automatically update the `Deployment`.

---

## Example 2: Managing a CronJob with an Immutable Field

This example demonstrates how to use the **force replace** feature to manage a `CronJob`, which has an immutable `spec.jobTemplate`.

### `ConfigMap` Manifest

Here, we define a `CronJob` and enable the force-replace strategy.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cronjob-template
  namespace: default
  annotations:
    kubetemplater.io/enabled: "true"
data:
  resources.yaml: |
    - name: hello-world-cronjob
      template: |
        apiVersion: batch/v1
        kind: CronJob
        metadata:
          name: hello-world-cronjob
          annotations:
            # Enable the force-replace strategy
            kubetemplater.io/replace-enabled: "true"
        spec:
          schedule: "*/1 * * * *" # Runs every minute
          jobTemplate:
            spec:
              template:
                spec:
                  containers:
                  - name: hello
                    image: busybox:1.35
                    command:
                    - /bin/sh
                    - -c
                    - date; echo "Hello from KubeTemplater!"
                  restartPolicy: OnFailure
      values: []
```

### How to Test the Immutable Field Update

1.  **Apply the `ConfigMap`** above. The `CronJob` will be created.
2.  **Modify the `ConfigMap`**: Change the `command` inside the `spec.jobTemplate.spec.template.spec.containers` array. For example, change `"Hello from KubeTemplater!"` to `"A new message!"`.
3.  **Apply the change**: `kubectl apply -f your-configmap.yaml`.

### What Happens

1.  KubeTemplater renders the new `CronJob` manifest with the updated command.
2.  It attempts a `Server-Side Apply`. The Kubernetes API server rejects this because `jobTemplate` is immutable.
3.  The operator sees the failure, and because `kubetemplater.io/replace-enabled: "true"` is present, it automatically **deletes** the old `CronJob` and **creates** the new one.
4.  The `CronJob` is successfully updated without any manual intervention.
