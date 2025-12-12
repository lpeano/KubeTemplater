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
	"time"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/queue"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	WorkQueue         *queue.WorkQueue
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

	// Update status to Queued and enqueue for async processing
	// Enqueue templates that are:
	// 1. New (empty phase) - initial processing
	// DO NOT enqueue:
	// - Failed templates (require manual intervention to prevent infinite loops)
	// - Completed templates (avoid continuous reconciliation loop without ResourceWatcher)
	//   TODO: Re-enable when DynamicInformer is implemented for drift detection
	if kubeTemplate.Status.ProcessingPhase == "Failed" {
		log.V(1).Info("Template is in Failed state, manual intervention required to retry",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace,
			"status", kubeTemplate.Status.Status)
		return ctrl.Result{}, nil
	}

	// For completed templates, use periodic reconciliation (every 60s)
	// Apply resources with SSA to detect and correct drift without changing status
	if kubeTemplate.Status.ProcessingPhase == "Completed" {
		log.V(1).Info("Periodic reconciliation: applying template resources with SSA",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)

		// Apply resources directly with Server-Side Apply
		// This will detect drift and correct it without triggering WorkQueue processing
		if err := r.applyTemplateResources(ctx, &kubeTemplate); err != nil {
			log.Error(err, "Failed to apply resources during periodic reconciliation")
			// Don't return error - just log and continue, will retry in next cycle
		}

		// Schedule next periodic reconciliation in 60 seconds
		// Status remains "Completed" - no WorkQueue enqueue
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	// Process new templates
	if kubeTemplate.Status.ProcessingPhase == "" {
		// New template
		kubeTemplate.Status.ProcessingPhase = "Queued"
		now := metav1.Now()
		kubeTemplate.Status.QueuedAt = &now
		kubeTemplate.Status.ProcessedAt = nil
		kubeTemplate.Status.RetryCount = 0

		if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
			if !errors.IsConflict(err) {
				log.Error(err, "Failed to update KubeTemplate status to Queued")
				return ctrl.Result{}, err
			}
			log.V(1).Info("Status update conflict, another reconciliation in progress")
		}
	}

	// Only enqueue for async processing if not already Completed
	// Completed templates are handled by periodic reconciliation (RequeueAfter)
	if kubeTemplate.Status.ProcessingPhase != "Completed" {
		r.WorkQueue.Enqueue(types.NamespacedName{
			Namespace: kubeTemplate.Namespace,
			Name:      kubeTemplate.Name,
		}, 0) // Priority 0 (normal)

		log.Info("Enqueued KubeTemplate for processing", "name", kubeTemplate.Name, "namespace", kubeTemplate.Namespace)
	}

	return ctrl.Result{}, nil
}

// applyTemplateResources applies the resources defined in the template using Server-Side Apply
// This is used during periodic reconciliation to detect and correct drift
func (r *KubeTemplateReconciler) applyTemplateResources(ctx context.Context, kubeTemplate *kubetemplateriov1alpha1.KubeTemplate) error {
	log := logf.FromContext(ctx)

	totalResources := len(kubeTemplate.Spec.Templates)
	syncedResources := 0
	driftDetected := false

	for _, template := range kubeTemplate.Spec.Templates {
		// Parse the raw template object to unstructured
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(template.Object.Raw, &obj); err != nil {
			log.Error(err, "Failed to unmarshal template object")
			continue
		}

// Get current resource to compare spec before SSA
	currentObj := &unstructured.Unstructured{}
	currentObj.SetGroupVersionKind(obj.GroupVersionKind())
	getErr := r.Client.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, currentObj)

	var specBefore map[string]interface{}
	if getErr == nil {
		// Save current spec for semantic comparison
		// We only compare spec because metadata (labels, annotations, finalizers) 
		// can be legitimately modified by other operators (e.g., Keycloak operator, FluxCD)
		if spec, found, err := unstructured.NestedFieldCopy(currentObj.Object, "spec"); err == nil && found {
			if specMap, ok := spec.(map[string]interface{}); ok {
				specBefore = specMap
			}
		}
	}

	// Apply the resource with Server-Side Apply
	// Use ForceOwnership to take control from other field managers (e.g., kubectl-patch, external operators)
	fieldManager := "kubetemplater"
	if err := r.Client.Patch(ctx, &obj, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		log.Error(err, "Failed to apply object during periodic reconciliation",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
			"namespace", obj.GetNamespace())
		continue
	}

	syncedResources++

// Detect drift: compare actual spec values instead of just generation
	// This avoids false positives when generation increments without real changes
	// We only check spec because other fields (labels, annotations, finalizers) can be 
	// legitimately managed by other operators without being considered drift
	if getErr == nil && specBefore != nil {
		updatedObj := &unstructured.Unstructured{}
		updatedObj.SetGroupVersionKind(obj.GroupVersionKind())
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, updatedObj); err == nil {
			if specAfter, found, err := unstructured.NestedFieldCopy(updatedObj.Object, "spec"); err == nil && found {
				if specAfterMap, ok := specAfter.(map[string]interface{}); ok {
					// Use DeepEqual for semantic comparison of spec values
					if !apiequality.Semantic.DeepEqual(specBefore, specAfterMap) {
						driftDetected = true
						log.Info("Drift detected: spec was modified by external source",
							"kind", obj.GetKind(),
							"name", obj.GetName(),
							"namespace", obj.GetNamespace())
						// Debug: log the differences (only in verbose mode)
						log.V(1).Info("Spec comparison details",
							"specBefore", specBefore,
							"specAfter", specAfterMap)
					} else {
						log.V(2).Info("No drift: spec unchanged after SSA",
							"kind", obj.GetKind(),
							"name", obj.GetName(),
							"namespace", obj.GetNamespace())
					}
				}
			}
			}
		}

		log.V(1).Info("Applied resource during periodic reconciliation",
			"kind", obj.GetKind(),
			"name", obj.GetName(),
			"namespace", obj.GetNamespace())
	}

	// Update status with reconciliation info
	now := metav1.Now()
	kubeTemplate.Status.LastReconcileTime = &now
	kubeTemplate.Status.ResourcesTotal = totalResources
	kubeTemplate.Status.ResourcesSynced = syncedResources

	// Update drift detection status if drift was detected
	if driftDetected {
		kubeTemplate.Status.LastDriftDetected = &now
		kubeTemplate.Status.DriftDetectionCount++
		log.Info("Drift corrected via SSA",
			"template", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace,
			"totalDriftCount", kubeTemplate.Status.DriftDetectionCount)
	}

	// Update status
	if err := r.Status().Update(ctx, kubeTemplate); err != nil {
		log.Error(err, "Failed to update template status after reconciliation")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplate{}).
		Named("kubetemplater.io-kubetemplate").
		Complete(r)
}
