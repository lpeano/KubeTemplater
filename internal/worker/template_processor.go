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

package worker

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/cache"
	"github.com/lpeano/KubeTemplater/internal/queue"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// TemplateProcessor processes KubeTemplate resources asynchronously
type TemplateProcessor struct {
	Client            client.Client
	Cache             *cache.PolicyCache
	Queue             *queue.WorkQueue
	OperatorNamespace string
	WorkerID          int
}

// updateStatusWithRetry updates the status with retry on conflict
func (p *TemplateProcessor) updateStatusWithRetry(ctx context.Context, kubeTemplate *kubetemplateriov1alpha1.KubeTemplate, updateFn func(*kubetemplateriov1alpha1.KubeTemplate)) error {
	log := logf.FromContext(ctx).WithName("template-processor")
	
	for retries := 0; retries < 3; retries++ {
		// Re-fetch latest version to avoid conflicts
		if err := p.Client.Get(ctx, types.NamespacedName{
			Namespace: kubeTemplate.Namespace,
			Name:      kubeTemplate.Name,
		}, kubeTemplate); err != nil {
			log.Error(err, "Failed to re-fetch KubeTemplate for status update")
			return err
		}

		// Apply the status update function
		updateFn(kubeTemplate)
		
		if err := p.Client.Status().Update(ctx, kubeTemplate); err != nil {
			if errors.IsConflict(err) && retries < 2 {
				log.V(1).Info("Status update conflict, retrying", "attempt", retries+1)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to update status after 3 retries")
}

// Start begins processing items from the queue
func (p *TemplateProcessor) Start(ctx context.Context) {
	log := logf.FromContext(ctx).WithName("template-processor").WithValues("workerID", p.WorkerID)
	log.Info("Starting template processor worker")

	for {
		select {
		case <-ctx.Done():
			log.Info("Shutting down template processor worker")
			return
		default:
			item, ok := p.Queue.Dequeue()
			if !ok {
				// Queue is shutting down
				return
			}

			if err := p.processItem(ctx, item); err != nil {
				log.Error(err, "Failed to process item", "item", item.NamespacedName, "retryCount", item.RetryCount)
				p.Queue.Requeue(item, err)
			} else {
				log.V(1).Info("Successfully processed item", "item", item.NamespacedName)
				p.Queue.Done(item)
			}
		}
	}
}

// processItem processes a single KubeTemplate
func (p *TemplateProcessor) processItem(ctx context.Context, item *queue.WorkItem) error {
	log := logf.FromContext(ctx).WithName("template-processor").WithValues("workerID", p.WorkerID)

	var kubeTemplate kubetemplateriov1alpha1.KubeTemplate
	if err := p.Client.Get(ctx, item.NamespacedName, &kubeTemplate); err != nil {
		if errors.IsNotFound(err) {
			log.Info("KubeTemplate no longer exists, skipping", "item", item.NamespacedName)
			return nil
		}
		return fmt.Errorf("failed to get KubeTemplate: %w", err)
	}

	// Update status to Processing
	if err := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
		kt.Status.ProcessingPhase = "Processing"
		kt.Status.ProcessedAt = nil
	}); err != nil {
		log.Error(err, "Failed to update status to Processing")
	}

	// Get policy from cache (fast!)
	policy, err := p.Cache.Get(ctx, kubeTemplate.Namespace, p.OperatorNamespace)
	if err != nil {
		now := metav1.Now()
		if statusErr := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
			kt.Status.ProcessingPhase = "Failed"
			kt.Status.Status = fmt.Sprintf("Error: %v", err)
			kt.Status.ProcessedAt = &now
		}); statusErr != nil {
			log.Error(statusErr, "Failed to update status")
		}
		return err
	}

	// Process each template
	for _, template := range kubeTemplate.Spec.Templates {
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(template.Object.Raw, &obj); err != nil {
			log.Error(err, "Failed to unmarshal template object")
			continue
		}

		if obj.GetNamespace() == "" {
			obj.SetNamespace(kubeTemplate.Namespace)
		}

		gvk := obj.GroupVersionKind()
		allowed := false
		var matchedRule *kubetemplateriov1alpha1.ValidationRule

		log.Info("Validating resource against policy",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
			"policyName", policy.Name)

		for i := range policy.Spec.ValidationRules {
			rule := &policy.Spec.ValidationRules[i]
			log.Info("Checking rule",
				"ruleIndex", i,
				"ruleGroup", rule.Group,
				"ruleVersion", rule.Version,
				"ruleKind", rule.Kind,
				"resourceGroup", gvk.Group,
				"resourceVersion", gvk.Version,
				"resourceKind", gvk.Kind,
				"kindMatch", rule.Kind == gvk.Kind,
				"groupMatch", rule.Group == gvk.Group,
				"versionMatch", rule.Version == gvk.Version)
			
			if rule.Kind == gvk.Kind && rule.Group == gvk.Group && rule.Version == gvk.Version {
				allowed = true
				matchedRule = rule
				log.Info("Rule matched successfully", "ruleIndex", i)
				break
			}
		}

		if !allowed {
			log.Info("Resource not allowed by policy",
				"group", gvk.Group,
				"version", gvk.Version,
				"kind", gvk.Kind,
				"policyRules", len(policy.Spec.ValidationRules))
			now := metav1.Now()
			if err := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
				kt.Status.ProcessingPhase = "Failed"
				kt.Status.Status = fmt.Sprintf("Error: Resource %s is not allowed by policy", gvk.String())
				kt.Status.ProcessedAt = &now
			}); err != nil {
				log.Error(err, "Failed to update status")
			}
			continue
		}

		if len(matchedRule.TargetNamespaces) == 0 {
			log.Info("Rule has no target namespaces", "gvk", gvk)
			now := metav1.Now()
			if err := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
				kt.Status.ProcessingPhase = "Failed"
				kt.Status.Status = fmt.Sprintf("Error: Resource %s has no target namespaces", gvk.String())
				kt.Status.ProcessedAt = &now
			}); err != nil {
				log.Error(err, "Failed to update status")
			}
			continue
		}

		if !contains(matchedRule.TargetNamespaces, obj.GetNamespace()) {
			log.Info("Namespace not in target list", "gvk", gvk, "namespace", obj.GetNamespace())
			now := metav1.Now()
			if err := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
				kt.Status.ProcessingPhase = "Failed"
				kt.Status.Status = fmt.Sprintf("Error: namespace %s not allowed for %s", obj.GetNamespace(), gvk.String())
				kt.Status.ProcessedAt = &now
			}); err != nil {
				log.Error(err, "Failed to update status")
			}
			continue
		}

		// Validate with CEL rule if present
		if matchedRule != nil && matchedRule.Rule != "" {
			if valid, err := p.validateWithCEL(matchedRule.Rule, obj.Object); err != nil {
				log.Error(err, "CEL validation error", "gvk", gvk)
			now := metav1.Now()
			if statusErr := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
				kt.Status.ProcessingPhase = "Failed"
				kt.Status.Status = fmt.Sprintf("Error: CEL validation failed for %s: %v", gvk.String(), err)
				kt.Status.ProcessedAt = &now
			}); statusErr != nil {
					log.Error(statusErr, "Failed to update status")
				}
				continue
			} else if !valid {
				log.Info("CEL validation failed", "gvk", gvk)
			now := metav1.Now()
			if statusErr := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
				kt.Status.ProcessingPhase = "Failed"
				kt.Status.Status = fmt.Sprintf("Error: Resource %s failed CEL validation", gvk.String())
				kt.Status.ProcessedAt = &now
			}); statusErr != nil {
					log.Error(statusErr, "Failed to update status")
				}
				continue
			}
		}

		// Add tracking labels to enable watch-based reconciliation
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["kubetemplater.io/template-name"] = kubeTemplate.Name
		labels["kubetemplater.io/template-namespace"] = kubeTemplate.Namespace
		obj.SetLabels(labels)

		// Add KubeTemplate as OwnerReference if referenced is true
		if template.Referenced {
			ownerRef := metav1.OwnerReference{
				APIVersion: "kubetemplater.io/v1alpha1",
				Kind:       "KubeTemplate",
				Name:       kubeTemplate.Name,
				UID:        kubeTemplate.UID,
			}
			owners := obj.GetOwnerReferences()
			owners = append(owners, ownerRef)
			obj.SetOwnerReferences(owners)
			log.Info("Added KubeTemplate as OwnerReference",
				"gvk", gvk,
				"templateName", kubeTemplate.Name,
				"templateUID", kubeTemplate.UID)
		}

		// Apply the resource
		fieldManager := "kubetemplater"
		if err := p.Client.Patch(ctx, &obj, client.Apply, client.FieldOwner(fieldManager)); err != nil {
			if errors.IsInvalid(err) && template.Replace {
				log.Info("Applying with replace", "gvk", gvk, "name", obj.GetName())
				if deleteErr := p.Client.Delete(ctx, &obj); deleteErr != nil {
					log.Error(deleteErr, "Failed to delete for replace", "gvk", gvk)
					continue
				}
				if applyErr := p.Client.Patch(ctx, &obj, client.Apply, client.FieldOwner(fieldManager)); applyErr != nil {
					log.Error(applyErr, "Failed to apply after replace", "gvk", gvk)
					continue
				}
			} else {
				log.Error(err, "Failed to apply object", "gvk", gvk)
				now := metav1.Now()
				if statusErr := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
					kt.Status.ProcessingPhase = "Failed"
					kt.Status.Status = fmt.Sprintf("Error: Failed to apply %s/%s: %v", gvk.String(), obj.GetName(), err)
					kt.Status.ProcessedAt = &now
				}); statusErr != nil {
					log.Error(statusErr, "Failed to update status")
				}
				return err
			}
		}
	}

	// Update status to Completed
	now := metav1.Now()
	if err := p.updateStatusWithRetry(ctx, &kubeTemplate, func(kt *kubetemplateriov1alpha1.KubeTemplate) {
		kt.Status.ProcessingPhase = "Completed"
		kt.Status.Status = "Completed"
		kt.Status.ProcessedAt = &now
	}); err != nil {
		log.Error(err, "Failed to update status to Completed")
		return err
	}

	return nil
}

// validateWithCEL validates an object using a CEL expression
func (p *TemplateProcessor) validateWithCEL(rule string, object map[string]interface{}) (bool, error) {
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("object", decls.NewMapType(decls.String, decls.Dyn)),
		),
	)
	if err != nil {
		return false, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	parsed, issues := env.Parse(rule)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("failed to parse CEL rule: %w", issues.Err())
	}

	checked, issues := env.Check(parsed)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("failed to check CEL rule: %w", issues.Err())
	}

	prg, err := env.Program(checked)
	if err != nil {
		return false, fmt.Errorf("failed to create CEL program: %w", err)
	}

	out, _, err := prg.Eval(map[string]interface{}{
		"object": object,
	})
	if err != nil {
		return false, fmt.Errorf("failed to evaluate CEL rule: %w", err)
	}

	return out.Value() == true, nil
}

func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// StartWorkers starts multiple worker goroutines
func StartWorkers(ctx context.Context, client client.Client, cache *cache.PolicyCache, queue *queue.WorkQueue, operatorNamespace string, numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		processor := &TemplateProcessor{
			Client:            client,
			Cache:             cache,
			Queue:             queue,
			OperatorNamespace: operatorNamespace,
			WorkerID:          i,
		}
		go processor.Start(ctx)
	}
}

// EnqueueKubeTemplate is a helper to enqueue a KubeTemplate for processing
func EnqueueKubeTemplate(queue *queue.WorkQueue, namespacedName types.NamespacedName) {
	// Normal priority = 0, you can adjust based on your needs
	queue.Enqueue(namespacedName, 0)
}
