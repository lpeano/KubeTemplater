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

package kubetemplaterio

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// KubeTemplateReconciler reconciles a KubeTemplate object
type KubeTemplateReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=*,verbs=get;list;watch;create;update;patch;delete

func (r *KubeTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var kubeTemplate kubetemplateriov1alpha1.KubeTemplate
	if err := r.Get(ctx, req.NamespacedName, &kubeTemplate); err != nil {
		if errors.IsNotFound(err) {
			log.Info("KubeTemplate resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KubeTemplate")
		return ctrl.Result{}, err
	}

	var policies kubetemplateriov1alpha1.KubeTemplatePolicyList
	if err := r.List(ctx, &policies, client.InNamespace(r.OperatorNamespace)); err != nil {
		log.Error(err, "Failed to list KubeTemplatePolicies")
		return ctrl.Result{}, err
	}

	var matchedPolicy *kubetemplateriov1alpha1.KubeTemplatePolicy
	for i := range policies.Items {
		p := &policies.Items[i]
		if p.Spec.SourceNamespace == kubeTemplate.Namespace {
			if matchedPolicy != nil {
				log.Info("Found multiple KubeTemplatePolicies for source namespace", "namespace", kubeTemplate.Namespace)
				kubeTemplate.Status.Status = "Error: Found multiple KubeTemplatePolicies for source namespace"
				if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
					log.Error(err, "Failed to update KubeTemplate status")
				}
				return ctrl.Result{}, fmt.Errorf("found multiple KubeTemplatePolicies for source namespace %s", kubeTemplate.Namespace)
			}
			matchedPolicy = p
		}
	}

	if matchedPolicy == nil {
		log.Info("No KubeTemplatePolicy found for source namespace", "namespace", kubeTemplate.Namespace)
		kubeTemplate.Status.Status = "Error: No KubeTemplatePolicy found for source namespace"
		if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
			log.Error(err, "Failed to update KubeTemplate status")
		}
		return ctrl.Result{}, fmt.Errorf("no KubeTemplatePolicy found for source namespace %s", kubeTemplate.Namespace)
	}

	policy := *matchedPolicy

	for _, template := range kubeTemplate.Spec.Templates {
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(template.Object.Raw, &obj); err != nil {
			log.Error(err, "Failed to unmarshal template object")
			continue
		}

		if obj.GetNamespace() == "" {
			obj.SetNamespace(req.Namespace)
		}

		gvk := obj.GroupVersionKind()
		allowed := false
		var matchedRule *kubetemplateriov1alpha1.ValidationRule
		for i := range policy.Spec.ValidationRules {
			rule := &policy.Spec.ValidationRules[i]
			if rule.Kind == gvk.Kind && rule.Group == gvk.Group && rule.Version == gvk.Version {
				allowed = true
				matchedRule = rule
				break
			}
		}

		if !allowed {
			log.Info("Resource is not allowed by KubeTemplatePolicy", "gvk", gvk)
			kubeTemplate.Status.Status = fmt.Sprintf("Error: Resource %s is not allowed by policy", gvk.String())
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				log.Error(err, "Failed to update KubeTemplate status")
			}
			continue
		}

		if len(matchedRule.TargetNamespaces) == 0 {
			log.Info("KubeTemplatePolicy rule has no target namespaces defined, refusing to create resource", "gvk", gvk)
			kubeTemplate.Status.Status = fmt.Sprintf("Error: Resource %s is not allowed by policy: no target namespaces defined", gvk.String())
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				log.Error(err, "Failed to update KubeTemplate status")
			}
			continue
		}

		if !contains(matchedRule.TargetNamespaces, obj.GetNamespace()) {
			log.Info("Resource namespace is not in the list of allowed target namespaces for this rule", "gvk", gvk, "namespace", obj.GetNamespace())
			kubeTemplate.Status.Status = fmt.Sprintf("Error: namespace %s is not an allowed target for resource %s", obj.GetNamespace(), gvk.String())
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				log.Error(err, "Failed to update KubeTemplate status")
			}
			continue
		}

		if matchedRule != nil && matchedRule.Rule != "" {
			env, err := cel.NewEnv(
				cel.Declarations(
					decls.NewVar("object", decls.NewMapType(decls.String, decls.Dyn)),
				),
			)
			if err != nil {
				log.Error(err, "Failed to create CEL environment")
				continue
			}

			parsed, issues := env.Parse(matchedRule.Rule)
			if issues != nil && issues.Err() != nil {
				log.Error(issues.Err(), "Failed to parse CEL rule")
				continue
			}

			checked, issues := env.Check(parsed)
			if issues != nil && issues.Err() != nil {
				log.Error(issues.Err(), "Failed to check CEL rule")
				continue
			}

			prg, err := env.Program(checked)
			if err != nil {
				log.Error(err, "Failed to create CEL program")
				continue
			}

			out, _, err := prg.Eval(map[string]interface{}{
				"object": obj.Object,
			})
			if err != nil {
				log.Error(err, "Failed to evaluate CEL rule")
				continue
			}

			if out.Value() != true {
				log.Info("Resource validation failed", "gvk", gvk, "rule", matchedRule.Rule)
				kubeTemplate.Status.Status = fmt.Sprintf("Error: Resource %s failed validation", gvk.String())
				if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
					log.Error(err, "Failed to update KubeTemplate status")
				}
				continue
			}
		}

		fieldManager := "kubetemplater"
		if err := r.Patch(ctx, &obj, client.Apply, client.FieldOwner(fieldManager)); err != nil {
			if errors.IsInvalid(err) && template.Replace {
				log.Info("Failed to apply object due to immutable field, replacing it", "gvk", gvk, "name", obj.GetName())
				if deleteErr := r.Delete(ctx, &obj); deleteErr != nil {
					log.Error(deleteErr, "Failed to delete object for replacement", "gvk", gvk, "name", obj.GetName())
					kubeTemplate.Status.Status = fmt.Sprintf("Error: Failed to delete object %s/%s for replacement: %v", gvk.String(), obj.GetName(), deleteErr)
					if statusErr := r.Status().Update(ctx, &kubeTemplate); statusErr != nil {
						log.Error(statusErr, "Failed to update KubeTemplate status")
					}
					continue // continue to the next template resource
				}
				// Re-apply after deletion
				if applyErr := r.Patch(ctx, &obj, client.Apply, client.FieldOwner(fieldManager)); applyErr != nil {
					log.Error(applyErr, "Failed to apply object after replacement", "gvk", gvk, "name", obj.GetName())
					kubeTemplate.Status.Status = fmt.Sprintf("Error: Failed to apply object %s/%s after replacement: %v", gvk.String(), obj.GetName(), applyErr)
					if statusErr := r.Status().Update(ctx, &kubeTemplate); statusErr != nil {
						log.Error(statusErr, "Failed to update KubeTemplate status")
					}
				}
			} else {
				log.Error(err, "Failed to apply object", "gvk", gvk, "name", obj.GetName())
				kubeTemplate.Status.Status = fmt.Sprintf("Error: Failed to apply object %s/%s: %v", gvk.String(), obj.GetName(), err)
				if statusErr := r.Status().Update(ctx, &kubeTemplate); statusErr != nil {
					log.Error(statusErr, "Failed to update KubeTemplate status")
				}
			}
		}
	}

	kubeTemplate.Status.Status = "Completed"
	if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
		log.Error(err, "Failed to update KubeTemplate status")
	}

	return ctrl.Result{}, nil
}

func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplate{}).
		Named("kubetemplater.io-kubetemplate").
		Complete(r)
}
