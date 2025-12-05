# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2025-12-05

### ✨ Features

- **Field Validation System**: Added comprehensive field-level validation for KubeTemplate resources with 5 validation types:
  - **CEL Expressions**: Validate fields using Common Expression Language (e.g., `status.replicas < 100`)
  - **Regex Patterns**: Enforce format validation (e.g., DNS names, email patterns)
  - **Numeric Ranges**: Validate integer fields are within specified min/max bounds
  - **Required Fields**: Ensure critical fields are present and non-empty
  - **Forbidden Fields**: Block security-sensitive or deprecated fields
  - Sequential fail-fast execution with detailed error messages

- **Validating Admission Webhook**: Implemented validating webhook with TLS support
  - Real-time validation before resources are applied to the cluster
  - Prevents invalid templates from being created
  - Integrates with field validation system for comprehensive policy enforcement
  - Configurable failure policies and timeout settings

- **Multi-Cloud Certificate Management**: Three flexible certificate modes for webhook TLS:
  - **cloud-native**: Leverages AKS/GKE native certificate auto-injection (zero configuration)
  - **cert-manager**: Automatic certificate management via cert-manager (required for EKS, optional for others)
  - **manual**: User-provided certificates for air-gapped or corporate PKI environments

- **Cloud Provider Optimizations**:
  - **Azure AKS**: Native webhook certificate injection via service annotations
  - **Google GKE**: Workload Identity support and native certificate management
  - **Amazon EKS**: cert-manager integration with IRSA (IAM Roles for Service Accounts)
  - Dedicated Helm value examples for each cloud provider

- **CI/CD Pipelines**:
  - **GitHub Actions**: Multi-architecture Docker builds (amd64/arm64), GHCR publishing, Trivy security scanning
  - **Azure DevOps**: ACR integration, semantic versioning, release tagging stages
  - Automated security scanning with SARIF uploads to GitHub Security

### 🔧 Enhancements

- **Helm Chart v0.2.0**: Major update with webhook resources
  - Added CRDs to `crds/` folder for proper installation order
  - Conditional template rendering based on certificate mode
  - Webhook service, deployment volume mounts, and configuration
  - High availability configuration examples
  - Cloud-specific resource annotations and labels

- **API Version**: Updated to `v1alpha1` with extended `KubeTemplatePolicy` CRD
  - `FieldValidation` array in `ValidationRule` spec
  - Type-safe validation configurations
  - Generated DeepCopy methods for runtime.Object compliance

### 📚 Documentation

- **Cloud Provider Guides**: Comprehensive installation guides for AKS, GKE, and EKS
  - Prerequisites, step-by-step installation, troubleshooting
  - Cloud-specific features and best practices
  - Production-ready configuration examples

- **Webhook Documentation**: Detailed guides for webhook validation
  - Field validation configuration examples
  - Certificate management modes
  - Debugging and troubleshooting tips

- **CI/CD Documentation**: Pipeline setup and configuration guides
  - GitHub Actions workflow customization
  - Azure DevOps service connection setup
  - Image tagging and versioning strategies

### 🐛 Fixes

- Fixed unused parameter warnings in webhook validation functions
- Corrected CRD generation path for proper Go module structure
- Improved error messages for field validation failures

### 🏗️ Breaking Changes

- Updated Helm chart version to 0.2.0 (requires migration from v0.0.2)
- Webhook now required for policy enforcement (can be disabled with `webhook.enabled: false`)
- CRDs moved to `crds/` folder (automatic installation with Helm v3)

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

[Unreleased]: https://github.com/lpeano/KubeTemplater/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/lpeano/KubeTemplater/compare/v0.0.2...v0.2.0
[0.0.2]: https://github.com/lpeano/KubeTemplater/compare/v0.0.1...v0.0.2