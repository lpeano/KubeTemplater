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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubeTemplatePolicySpec defines the desired state of KubeTemplatePolicy.
type KubeTemplatePolicySpec struct {
	// SourceNamespace is the namespace where KubeTemplates are allowed to use this policy.
	SourceNamespace string `json:"sourceNamespace"`

	ValidationRules []ValidationRule `json:"validationRules"`
}

// ValidationRule defines the policy for creating a specific kind of resource.
type ValidationRule struct {
	Kind    string `json:"kind"`
	Group   string `json:"group"`
	Version string `json:"version"`

	// Rule is a CEL expression that validates the entire object.
	// DEPRECATED: Use FieldValidations for more granular control.
	// This field is kept for backward compatibility.
	Rule string `json:"rule,omitempty"`

	// FieldValidations defines multiple validation rules for specific fields.
	// Each validation is evaluated independently and all must pass.
	FieldValidations []FieldValidation `json:"fieldValidations,omitempty"`

	// TargetNamespaces is a list of namespaces where resources of this kind are allowed to be created.
	// If empty, resources of this kind cannot be created in any namespace.
	TargetNamespaces []string `json:"targetNamespaces"`
}

// FieldValidation defines validation rules for a specific field in a resource.
type FieldValidation struct {
	// Name is a human-readable name for this validation (for error messages).
	Name string `json:"name"`

	// FieldPath is the JSON path to the field to validate (e.g., "metadata.name", "spec.replicas").
	// Use dot notation for nested fields. For object-level validation, use empty string or "object".
	FieldPath string `json:"fieldPath,omitempty"`

	// Type defines the type of validation to perform.
	// Valid values: "cel", "regex", "range", "required", "forbidden"
	Type FieldValidationType `json:"type"`

	// CEL is a CEL expression evaluated against the field value.
	// The variable name depends on FieldPath:
	// - For specific fields: 'value' contains the field value
	// - For empty FieldPath: 'object' contains the entire resource
	// Example: "value.startsWith('prod-')" or "object.spec.replicas <= 10"
	CEL string `json:"cel,omitempty"`

	// Regex is a regular expression pattern that the field value must match.
	// Only valid when Type is "regex".
	Regex string `json:"regex,omitempty"`

	// Min and Max define the allowed range for numeric fields.
	// Only valid when Type is "range".
	Min *int64 `json:"min,omitempty"`
	Max *int64 `json:"max,omitempty"`

	// Required specifies that the field must exist and be non-empty.
	// Only valid when Type is "required".
	Required bool `json:"required,omitempty"`

	// Message is a custom error message to display when validation fails.
	Message string `json:"message,omitempty"`
}

// FieldValidationType defines the type of field validation.
// +kubebuilder:validation:Enum=cel;regex;range;required;forbidden
type FieldValidationType string

const (
	FieldValidationTypeCEL       FieldValidationType = "cel"
	FieldValidationTypeRegex     FieldValidationType = "regex"
	FieldValidationTypeRange     FieldValidationType = "range"
	FieldValidationTypeRequired  FieldValidationType = "required"
	FieldValidationTypeForbidden FieldValidationType = "forbidden"
)

// KubeTemplatePolicyStatus defines the observed state of KubeTemplatePolicy.
type KubeTemplatePolicyStatus struct {
	Active              bool         `json:"active,omitempty"`
	TemplatesUsing      int          `json:"templatesUsing,omitempty"`
	LastValidationTime  *metav1.Time `json:"lastValidationTime,omitempty"`
	ValidationSuccesses int          `json:"validationSuccesses,omitempty"`
	ValidationFailures  int          `json:"validationFailures,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source NS",type=string,JSONPath=`.spec.sourceNamespace`
// +kubebuilder:printcolumn:name="Active",type=boolean,JSONPath=`.status.active`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Templates",type=integer,JSONPath=`.status.templatesUsing`,priority=1
// +kubebuilder:printcolumn:name="Successes",type=integer,JSONPath=`.status.validationSuccesses`,priority=1
// +kubebuilder:printcolumn:name="Failures",type=integer,JSONPath=`.status.validationFailures`,priority=1

// KubeTemplatePolicy is the Schema for the kubetemplatepolicies API.
type KubeTemplatePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeTemplatePolicySpec   `json:"spec,omitempty"`
	Status KubeTemplatePolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeTemplatePolicyList contains a list of KubeTemplatePolicy.
type KubeTemplatePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeTemplatePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeTemplatePolicy{}, &KubeTemplatePolicyList{})
}
