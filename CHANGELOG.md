# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.2] - 2025-11-08

### ✨ Features

- **Immutable Field Replace Strategy**: The controller can now automatically replace resources when a `Server-Side Apply` fails due to an immutable field change. To enable this behavior, add the following annotation to your target resource templates:
  ```yaml
  metadata:
    annotations:
      kubetemplater.io/replace-enabled: "true"
  ```
  When this annotation is present, the controller will delete and immediately re-create the resource to apply the changes.

### 🐛 Fixes

- Resolved an issue in the controller's reconciliation loop that prevented the "replace" strategy from working correctly in a single cycle.
- Improved the reliability of the integration test suite by fixing test cleanup procedures that were causing timeouts.

### 📚 Documentation

- Added documentation for the new `replace-enabled` annotation to the Helm chart's `values.yaml`.

[Unreleased]: https://github.com/ariellpe/KubeTemplater/compare/v0.0.2...HEAD
[0.0.2]: https://github.com/ariellpe/KubeTemplater/compare/v0.0.1...v0.0.2