/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("KubeTemplate Webhook", func() {
	var (
		validator         *KubeTemplateValidator
		ctx               context.Context
		operatorNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		operatorNamespace = "kubetemplater-system"

		scheme := runtime.NewScheme()
		Expect(kubetemplateriov1alpha1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		validator = &KubeTemplateValidator{
			Client:            fakeClient,
			OperatorNamespace: operatorNamespace,
		}
	})

	Context("When validating a KubeTemplate without policy", func() {
		It("Should reject the resource", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`),
							},
						},
					},
				},
			}

			_, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no KubeTemplatePolicy found"))
		})
	})

	Context("When validating a KubeTemplate with valid policy", func() {
		BeforeEach(func() {
			// Create a policy
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: operatorNamespace,
				},
				Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
					SourceNamespace: "default",
					ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
						{
							Kind:             "ConfigMap",
							Group:            "",
							Version:          "v1",
							TargetNamespaces: []string{"default"},
						},
					},
				},
			}
			Expect(validator.Client.Create(ctx, policy)).To(Succeed())
		})

		It("Should accept the resource", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`),
							},
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("When validating a KubeTemplate with disallowed resource type", func() {
		BeforeEach(func() {
			// Create a policy that only allows ConfigMaps
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: operatorNamespace,
				},
				Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
					SourceNamespace: "default",
					ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
						{
							Kind:             "ConfigMap",
							Group:            "",
							Version:          "v1",
							TargetNamespaces: []string{"default"},
						},
					},
				},
			}
			Expect(validator.Client.Create(ctx, policy)).To(Succeed())
		})

		It("Should reject a Secret", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: test-secret
type: Opaque
stringData:
  key: value`),
							},
						},
					},
				},
			}

			_, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not allowed by policy"))
		})
	})

	Context("When validating a KubeTemplate with invalid target namespace", func() {
		BeforeEach(func() {
			// Create a policy that only allows creation in 'allowed-ns'
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: operatorNamespace,
				},
				Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
					SourceNamespace: "default",
					ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
						{
							Kind:             "ConfigMap",
							Group:            "",
							Version:          "v1",
							TargetNamespaces: []string{"allowed-ns"},
						},
					},
				},
			}
			Expect(validator.Client.Create(ctx, policy)).To(Succeed())
		})

		It("Should reject resource in wrong namespace", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: wrong-ns
data:
  key: value`),
							},
						},
					},
				},
			}

			_, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not in the allowed target namespaces"))
		})
	})

	Context("When validating a KubeTemplate with CEL rule", func() {
		BeforeEach(func() {
			// Create a policy with a CEL rule
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: operatorNamespace,
				},
				Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
					SourceNamespace: "default",
					ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
						{
							Kind:             "Secret",
							Group:            "",
							Version:          "v1",
							Rule:             "object.metadata.name.startsWith('allowed-')",
							TargetNamespaces: []string{"default"},
						},
					},
				},
			}
			Expect(validator.Client.Create(ctx, policy)).To(Succeed())
		})

		It("Should accept resource that passes CEL validation", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: allowed-secret
type: Opaque
stringData:
  key: value`),
							},
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject resource that fails CEL validation", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: invalid-secret
type: Opaque
stringData:
  key: value`),
							},
						},
					},
				},
			}

			_, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed CEL validation"))
		})
	})

	Context("When validating a KubeTemplate with replace enabled", func() {
		BeforeEach(func() {
			policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: operatorNamespace,
				},
				Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
					SourceNamespace: "default",
					ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
						{
							Kind:             "ConfigMap",
							Group:            "",
							Version:          "v1",
							TargetNamespaces: []string{"default"},
						},
					},
				},
			}
			Expect(validator.Client.Create(ctx, policy)).To(Succeed())
		})

		It("Should return a warning when replace is enabled", func() {
			kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
					Templates: []kubetemplateriov1alpha1.Template{
						{
							Object: runtime.RawExtension{
								Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`),
							},
							Replace: true,
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, kubeTemplate)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).NotTo(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("replace is enabled"))
		})
	})

	Context("When validating field validations", func() {
		Context("With CEL field validation", func() {
			It("Should pass when CEL expression is true", func() {
				// Create policy with CEL field validation
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-prefix-check",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeCEL,
										CEL:       "value.startsWith('prod-')",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: prod-config
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail when CEL expression is false", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-prefix-check",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeCEL,
										CEL:       "value.startsWith('prod-')",
										Message:   "ConfigMap name must start with 'prod-'",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: dev-config
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("ConfigMap name must start with 'prod-'"))
			})
		})

		Context("With Regex field validation", func() {
			It("Should pass when regex matches", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-pattern-check",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRegex,
										Regex:     "^[a-z]+-[a-z]+$",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: prod-config
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail when regex doesn't match", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-pattern-check",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRegex,
										Regex:     "^[a-z]+-[a-z]+$",
										Message:   "Name must match pattern: lowercase-lowercase",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: INVALID_NAME
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Name must match pattern"))
			})
		})

		Context("With Range field validation", func() {
			It("Should pass when value is within range", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Deployment",
								Group:            "apps",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "replicas-range-check",
										FieldPath: "spec.replicas",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRange,
										Min:       int64Ptr(1),
										Max:       int64Ptr(10),
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy
spec:
  replicas: 5
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: nginx
        image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail when value exceeds maximum", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Deployment",
								Group:            "apps",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "replicas-range-check",
										FieldPath: "spec.replicas",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRange,
										Max:       int64Ptr(10),
										Message:   "Replicas must not exceed 10",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy
spec:
  replicas: 20
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: nginx
        image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Replicas must not exceed 10"))
			})
		})

		Context("With Required field validation", func() {
			It("Should pass when required field exists", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "team-label-required",
										FieldPath: "metadata.labels.team",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRequired,
										Required:  true,
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  labels:
    team: platform
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail when required field is missing", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "ConfigMap",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "team-label-required",
										FieldPath: "metadata.labels.team",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRequired,
										Required:  true,
										Message:   "Team label is required for all ConfigMaps",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  key: value`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Team label is required"))
			})
		})

		Context("With Forbidden field validation", func() {
			It("Should pass when forbidden field is absent", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Pod",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "no-host-network",
										FieldPath: "spec.hostNetwork",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeForbidden,
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: nginx
    image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail when forbidden field is present", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Pod",
								Group:            "",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "no-host-network",
										FieldPath: "spec.hostNetwork",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeForbidden,
										Message:   "Host network is not allowed for security reasons",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  hostNetwork: true
  containers:
  - name: nginx
    image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Host network is not allowed"))
			})
		})

		Context("With multiple field validations", func() {
			It("Should pass when all validations succeed", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Deployment",
								Group:            "apps",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-prefix",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRegex,
										Regex:     "^prod-",
									},
									{
										Name:      "replicas-limit",
										FieldPath: "spec.replicas",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRange,
										Max:       int64Ptr(5),
									},
									{
										Name:      "team-label",
										FieldPath: "metadata.labels.team",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRequired,
										Required:  true,
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: prod-api
  labels:
    team: backend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
      - name: nginx
        image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).NotTo(HaveOccurred())
			})

			It("Should fail on first failing validation", func() {
				policy := &kubetemplateriov1alpha1.KubeTemplatePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-policy",
						Namespace: operatorNamespace,
					},
					Spec: kubetemplateriov1alpha1.KubeTemplatePolicySpec{
						SourceNamespace: "default",
						ValidationRules: []kubetemplateriov1alpha1.ValidationRule{
							{
								Kind:             "Deployment",
								Group:            "apps",
								Version:          "v1",
								TargetNamespaces: []string{"default"},
								FieldValidations: []kubetemplateriov1alpha1.FieldValidation{
									{
										Name:      "name-prefix",
										FieldPath: "metadata.name",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRegex,
										Regex:     "^prod-",
										Message:   "Name must start with 'prod-'",
									},
									{
										Name:      "replicas-limit",
										FieldPath: "spec.replicas",
										Type:      kubetemplateriov1alpha1.FieldValidationTypeRange,
										Max:       int64Ptr(5),
										Message:   "Too many replicas",
									},
								},
							},
						},
					},
				}
				Expect(validator.Client.Create(ctx, policy)).To(Succeed())

				kubeTemplate := &kubetemplateriov1alpha1.KubeTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-template",
						Namespace: "default",
					},
					Spec: kubetemplateriov1alpha1.KubeTemplateSpec{
						Templates: []kubetemplateriov1alpha1.Template{
							{
								Object: runtime.RawExtension{
									Raw: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: dev-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
      - name: nginx
        image: nginx`),
								},
							},
						},
					},
				}

				_, err := validator.ValidateCreate(ctx, kubeTemplate)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Name must start with 'prod-'"))
			})
		})
	})
})

// Helper function to create int64 pointers
func int64Ptr(i int64) *int64 {
	return &i
}
