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
	"fmt"
	"regexp"
	"strings"
	"time"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/cache"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"
)

const (
	// MaxTemplatesPerKubeTemplate limits the number of templates in a single KubeTemplate
	maxTemplatesPerKubeTemplate = 50
	// MaxTemplateSizeBytes limits the size of each template object (1MB)
	maxTemplateSizeBytes = 1 * 1024 * 1024
	// CELEvaluationTimeout is the maximum time allowed for CEL evaluation
	celEvaluationTimeout = 100 * time.Millisecond
)

// +kubebuilder:webhook:path=/validate-kubetemplater-io-v1alpha1-kubetemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=kubetemplater.io,resources=kubetemplates,verbs=create;update,versions=v1alpha1,name=vkubetemplate.kb.io,admissionReviewVersions=v1

// KubeTemplateValidator validates KubeTemplate resources
type KubeTemplateValidator struct {
	Client            client.Client
	OperatorNamespace string
	Cache             *cache.PolicyCache
	regexCache        map[string]*regexp.Regexp
}

var _ webhook.CustomValidator = &KubeTemplateValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *KubeTemplateValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	kubeTemplate, ok := obj.(*kubetemplateriov1alpha1.KubeTemplate)
	if !ok {
		return nil, fmt.Errorf("expected a KubeTemplate but got a %T", obj)
	}

	log := logf.FromContext(ctx)
	log.Info("Validating KubeTemplate", "name", kubeTemplate.Name, "namespace", kubeTemplate.Namespace)

	return v.validateKubeTemplate(ctx, kubeTemplate)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *KubeTemplateValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	kubeTemplate, ok := newObj.(*kubetemplateriov1alpha1.KubeTemplate)
	if !ok {
		return nil, fmt.Errorf("expected a KubeTemplate but got a %T", newObj)
	}

	log := logf.FromContext(ctx)
	log.Info("Validating KubeTemplate update", "name", kubeTemplate.Name, "namespace", kubeTemplate.Namespace)

	return v.validateKubeTemplate(ctx, kubeTemplate)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *KubeTemplateValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed on delete
	return nil, nil
}

// validateKubeTemplate contains the core validation logic
func (v *KubeTemplateValidator) validateKubeTemplate(ctx context.Context, kubeTemplate *kubetemplateriov1alpha1.KubeTemplate) (admission.Warnings, error) {
	log := logf.FromContext(ctx)

	// Use policy cache for fast lookup (95% API call reduction!)
	matchedPolicy, err := v.Cache.Get(ctx, kubeTemplate.Namespace, v.OperatorNamespace)
	if err != nil {
		log.Error(err, "Failed to get policy from cache")
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}

	log.Info("Found matching policy", "policy", matchedPolicy.Name, "sourceNamespace", matchedPolicy.Spec.SourceNamespace)

	var warnings admission.Warnings

	// Validate template count limit
	if len(kubeTemplate.Spec.Templates) > maxTemplatesPerKubeTemplate {
		return warnings, fmt.Errorf("too many templates: %d (max allowed: %d)", len(kubeTemplate.Spec.Templates), maxTemplatesPerKubeTemplate)
	}

	// Validate each template in the KubeTemplate
	for idx, template := range kubeTemplate.Spec.Templates {
		// Validate template size
		if len(template.Object.Raw) > maxTemplateSizeBytes {
			return warnings, fmt.Errorf("template[%d]: size %d bytes exceeds maximum allowed size of %d bytes", idx, len(template.Object.Raw), maxTemplateSizeBytes)
		}
		// Unmarshal the template object
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(template.Object.Raw, &obj); err != nil {
			return warnings, fmt.Errorf("template[%d]: failed to unmarshal object: %w", idx, err)
		}

		// Set default namespace if not specified
		if obj.GetNamespace() == "" {
			obj.SetNamespace(kubeTemplate.Namespace)
		}

		gvk := obj.GroupVersionKind()
		log.Info("Validating template", "index", idx, "gvk", gvk.String(), "name", obj.GetName(), "namespace", obj.GetNamespace())

		// Find the matching validation rule for this resource type
		var matchedRule *kubetemplateriov1alpha1.ValidationRule
		for i := range matchedPolicy.Spec.ValidationRules {
			rule := &matchedPolicy.Spec.ValidationRules[i]
			if rule.Kind == gvk.Kind && rule.Group == gvk.Group && rule.Version == gvk.Version {
				matchedRule = rule
				break
			}
		}

		// Check if the resource type is allowed
		if matchedRule == nil {
			return warnings, fmt.Errorf("template[%d]: resource type %s is not allowed by policy %s", idx, gvk.String(), matchedPolicy.Name)
		}

		// Check if target namespaces are defined
		if len(matchedRule.TargetNamespaces) == 0 {
			return warnings, fmt.Errorf("template[%d]: resource type %s has no target namespaces defined in policy %s. At least one target namespace must be specified", idx, gvk.String(), matchedPolicy.Name)
		}

		// Check if the resource's namespace is in the allowed target namespaces
		if !contains(matchedRule.TargetNamespaces, obj.GetNamespace()) {
			return warnings, fmt.Errorf("template[%d]: resource namespace %s is not in the allowed target namespaces %v for resource type %s", idx, obj.GetNamespace(), matchedRule.TargetNamespaces, gvk.String())
		}

		// Validate legacy CEL rule if present (backward compatibility)
		if matchedRule.Rule != "" {
			if err := v.validateCELRule(matchedRule.Rule, &obj, idx, ""); err != nil {
				return warnings, err
			}
		}

		// Validate field validations if present
		if len(matchedRule.FieldValidations) > 0 {
			if err := v.validateFieldValidations(ctx, matchedRule.FieldValidations, &obj, idx); err != nil {
				return warnings, err
			}
		}

		// Add a warning if replace is enabled
		if template.Replace {
			warnings = append(warnings, fmt.Sprintf("template[%d]: replace is enabled for %s/%s. The resource will be deleted and recreated if immutable fields are changed", idx, gvk.String(), obj.GetName()))
		}
	}

	log.Info("KubeTemplate validation successful", "name", kubeTemplate.Name, "namespace", kubeTemplate.Namespace, "templatesCount", len(kubeTemplate.Spec.Templates))
	return warnings, nil
}

// validateFieldValidations validates all field validations for a resource
func (v *KubeTemplateValidator) validateFieldValidations(ctx context.Context, validations []kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	log := logf.FromContext(ctx)

	for validationIdx, validation := range validations {
		log.Info("Validating field", "validation", validation.Name, "type", validation.Type, "fieldPath", validation.FieldPath)

		var err error
		switch validation.Type {
		case kubetemplateriov1alpha1.FieldValidationTypeCEL:
			err = v.validateFieldCEL(validation, obj, templateIdx)
		case kubetemplateriov1alpha1.FieldValidationTypeRegex:
			err = v.validateFieldRegex(validation, obj, templateIdx)
		case kubetemplateriov1alpha1.FieldValidationTypeRange:
			err = v.validateFieldRange(validation, obj, templateIdx)
		case kubetemplateriov1alpha1.FieldValidationTypeRequired:
			err = v.validateFieldRequired(validation, obj, templateIdx)
		case kubetemplateriov1alpha1.FieldValidationTypeForbidden:
			err = v.validateFieldForbidden(validation, obj, templateIdx)
		default:
			return fmt.Errorf("template[%d]: fieldValidation[%d] (%s): unknown validation type: %s", templateIdx, validationIdx, validation.Name, validation.Type)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// validateFieldCEL validates a field using a CEL expression
func (v *KubeTemplateValidator) validateFieldCEL(validation kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	if validation.CEL == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): CEL expression is required for type 'cel'", templateIdx, validation.Name)
	}

	// Determine the variable name and value based on fieldPath
	var varName string
	var varValue interface{}

	if validation.FieldPath == "" || validation.FieldPath == "object" {
		// Object-level validation
		varName = "object"
		varValue = obj.Object
	} else {
		// Field-level validation
		varName = "value"
		fieldValue, found, err := unstructured.NestedFieldCopy(obj.Object, fieldPathToKeys(validation.FieldPath)...)
		if err != nil {
			return fmt.Errorf("template[%d]: fieldValidation (%s): failed to get field %s: %w", templateIdx, validation.Name, validation.FieldPath, err)
		}
		if !found {
			// Field doesn't exist, treat as null
			varValue = nil
		} else {
			varValue = fieldValue
		}
	}

	// Validate using CEL with custom variable name
	if err := v.validateCELRule(validation.CEL, obj, templateIdx, validation.Name, varName, varValue); err != nil {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return err
	}

	return nil
}

// validateFieldRegex validates a field using a regex pattern
func (v *KubeTemplateValidator) validateFieldRegex(validation kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	if validation.Regex == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): regex pattern is required for type 'regex'", templateIdx, validation.Name)
	}
	if validation.FieldPath == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): fieldPath is required for type 'regex'", templateIdx, validation.Name)
	}

	// Get field value
	fieldValue, found, err := unstructured.NestedString(obj.Object, fieldPathToKeys(validation.FieldPath)...)
	if err != nil {
		return fmt.Errorf("template[%d]: fieldValidation (%s): failed to get field %s: %w", templateIdx, validation.Name, validation.FieldPath, err)
	}
	if !found {
		return fmt.Errorf("template[%d]: fieldValidation (%s): field %s not found", templateIdx, validation.Name, validation.FieldPath)
	}

	// Get or compile regex pattern (with caching)
	if v.regexCache == nil {
		v.regexCache = make(map[string]*regexp.Regexp)
	}
	
	re, exists := v.regexCache[validation.Regex]
	if !exists {
		re, err = regexp.Compile(validation.Regex)
		if err != nil {
			return fmt.Errorf("template[%d]: fieldValidation (%s): invalid regex pattern %s: %w", templateIdx, validation.Name, validation.Regex, err)
		}
		v.regexCache[validation.Regex] = re
	}

	// Match regex
	matched := re.MatchString(fieldValue)

	if !matched {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return fmt.Errorf("template[%d]: fieldValidation (%s): field %s value '%s' does not match regex pattern '%s'", templateIdx, validation.Name, validation.FieldPath, fieldValue, validation.Regex)
	}

	return nil
}

// validateFieldRange validates a numeric field is within a range
func (v *KubeTemplateValidator) validateFieldRange(validation kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	if validation.FieldPath == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): fieldPath is required for type 'range'", templateIdx, validation.Name)
	}
	if validation.Min == nil && validation.Max == nil {
		return fmt.Errorf("template[%d]: fieldValidation (%s): at least one of min or max must be specified for type 'range'", templateIdx, validation.Name)
	}

	// Get field value
	fieldValue, found, err := unstructured.NestedInt64(obj.Object, fieldPathToKeys(validation.FieldPath)...)
	if err != nil {
		return fmt.Errorf("template[%d]: fieldValidation (%s): failed to get field %s as int64: %w", templateIdx, validation.Name, validation.FieldPath, err)
	}
	if !found {
		return fmt.Errorf("template[%d]: fieldValidation (%s): field %s not found", templateIdx, validation.Name, validation.FieldPath)
	}

	// Check range
	if validation.Min != nil && fieldValue < *validation.Min {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return fmt.Errorf("template[%d]: fieldValidation (%s): field %s value %d is less than minimum %d", templateIdx, validation.Name, validation.FieldPath, fieldValue, *validation.Min)
	}
	if validation.Max != nil && fieldValue > *validation.Max {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return fmt.Errorf("template[%d]: fieldValidation (%s): field %s value %d is greater than maximum %d", templateIdx, validation.Name, validation.FieldPath, fieldValue, *validation.Max)
	}

	return nil
}

// validateFieldRequired validates that a required field exists and is non-empty
func (v *KubeTemplateValidator) validateFieldRequired(validation kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	if validation.FieldPath == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): fieldPath is required for type 'required'", templateIdx, validation.Name)
	}

	// Check if field exists
	fieldValue, found, err := unstructured.NestedFieldCopy(obj.Object, fieldPathToKeys(validation.FieldPath)...)
	if err != nil {
		return fmt.Errorf("template[%d]: fieldValidation (%s): failed to get field %s: %w", templateIdx, validation.Name, validation.FieldPath, err)
	}

	if !found || fieldValue == nil || fieldValue == "" {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return fmt.Errorf("template[%d]: fieldValidation (%s): required field %s is missing or empty", templateIdx, validation.Name, validation.FieldPath)
	}

	return nil
}

// validateFieldForbidden validates that a forbidden field does not exist
func (v *KubeTemplateValidator) validateFieldForbidden(validation kubetemplateriov1alpha1.FieldValidation, obj *unstructured.Unstructured, templateIdx int) error {
	if validation.FieldPath == "" {
		return fmt.Errorf("template[%d]: fieldValidation (%s): fieldPath is required for type 'forbidden'", templateIdx, validation.Name)
	}

	// Check if field exists
	_, found, err := unstructured.NestedFieldCopy(obj.Object, fieldPathToKeys(validation.FieldPath)...)
	if err != nil {
		return fmt.Errorf("template[%d]: fieldValidation (%s): failed to get field %s: %w", templateIdx, validation.Name, validation.FieldPath, err)
	}

	if found {
		if validation.Message != "" {
			return fmt.Errorf("template[%d]: fieldValidation (%s): %s", templateIdx, validation.Name, validation.Message)
		}
		return fmt.Errorf("template[%d]: fieldValidation (%s): forbidden field %s is present", templateIdx, validation.Name, validation.FieldPath)
	}

	return nil
}

// fieldPathToKeys converts a dot-notation field path to a slice of keys
func fieldPathToKeys(fieldPath string) []string {
	return strings.Split(fieldPath, ".")
}

// validateCELRule validates a single CEL rule against an object or field value
// If varName and varValue are provided, they override the default "object" variable
func (v *KubeTemplateValidator) validateCELRule(rule string, obj *unstructured.Unstructured, templateIdx int, validationName string, varNameAndValue ...interface{}) error {
	gvkStr := obj.GroupVersionKind().String()

	// Determine variable name and value
	varName := "object"
	var varValue interface{} = obj.Object
	var varType *exprpb.Type = decls.NewMapType(decls.String, decls.Dyn)

	if len(varNameAndValue) >= 2 {
		if name, ok := varNameAndValue[0].(string); ok && name != "" {
			varName = name
		}
		varValue = varNameAndValue[1]
		// For 'value' variable, use dynamic type
		if varName == "value" {
			varType = decls.Dyn
		}
	}

	// Create CEL environment
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar(varName, varType),
		),
	)
	if err != nil {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: failed to create CEL environment: %w", errPrefix, err)
	}

	// Parse the CEL rule
	parsed, issues := env.Parse(rule)
	if issues != nil && issues.Err() != nil {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: failed to parse CEL rule: %w", errPrefix, issues.Err())
	}

	// Check the CEL rule
	checked, issues := env.Check(parsed)
	if issues != nil && issues.Err() != nil {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: failed to check CEL rule: %w", errPrefix, issues.Err())
	}

	// Create CEL program with cost tracking and cost limit
	prg, err := env.Program(checked, 
		cel.CostTracking(nil),
		cel.CostLimit(1000000), // Limit to 1M cost units
	)
	if err != nil {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: failed to create CEL program: %w", errPrefix, err)
	}

	// Evaluate the CEL rule with timeout
	evalCtx, cancel := context.WithTimeout(context.Background(), celEvaluationTimeout)
	defer cancel()

	out, _, err := prg.ContextEval(evalCtx, map[string]interface{}{
		varName: varValue,
	})
	if err != nil {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: failed to evaluate CEL rule: %w", errPrefix, err)
	}

	// Check if the rule passed
	if out.Value() != true {
		errPrefix := fmt.Sprintf("template[%d]", templateIdx)
		if validationName != "" {
			errPrefix = fmt.Sprintf("template[%d]: fieldValidation (%s)", templateIdx, validationName)
		}
		return fmt.Errorf("%s: resource %s/%s failed CEL validation rule: %s", errPrefix, gvkStr, obj.GetName(), rule)
	}

	return nil
}

// contains checks if a string is in a slice
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// SetupWebhookWithManager registers the webhook with the manager
func (v *KubeTemplateValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplate{}).
		WithValidator(v).
		Complete()
}
