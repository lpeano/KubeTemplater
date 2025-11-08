# Advanced Features

This section covers advanced features of KubeTemplater that help you handle more complex use cases.

## Force Replace for Immutable Resources

### The Problem

Some Kubernetes resources contain fields that are **immutable**, meaning they cannot be changed after the resource has been created. A common example is the `selector` field in a `Service` or the `jobTemplate` within a `CronJob`.

If you try to update one of these immutable fields, the Kubernetes API server will reject the update, and the KubeTemplater operator's `Server-Side Apply` will fail. This would normally require you to manually delete the old resource and re-apply the new one.

### The Solution: `kubetemplater.io/replace-enabled`

To solve this, KubeTemplater provides a "force replace" strategy that can be enabled with an annotation.

To enable this feature, add the `kubetemplater.io/replace-enabled: "true"` annotation to the resource template inside your `ConfigMap`.

```yaml
# In your resources.yaml
- name: my-immutable-cronjob
  template: |
    apiVersion: batch/v1
    kind: CronJob
    metadata:
      name: my-cronjob
      annotations:
        # This annotation enables the force-replace strategy
        kubetemplater.io/replace-enabled: "true"
    spec:
      schedule: "*/5 * * * *"
      jobTemplate:
        # The 'jobTemplate' is an immutable field
        spec:
          template:
            spec:
              containers:
              - name: hello
                image: busybox
                command: ["echo", "Hello, world!"]
              restartPolicy: OnFailure
  values: []
```

### How It Works

1.  The operator first attempts to apply the rendered manifest using the standard `Server-Side Apply`.
2.  If the Kubernetes API server returns an error indicating that an immutable field cannot be changed, the operator checks if the resource template has the `kubetemplater.io/replace-enabled: "true"` annotation.
3.  If the annotation is present, the operator will:
    a. **Delete** the existing resource from the cluster.
    b. **Re-create** the resource by applying the new manifest.

This automated delete-and-recreate cycle ensures that changes to immutable fields are applied successfully, keeping your infrastructure aligned with its configuration in a fully automated way.
