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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/yaml"
)

// KubeTemplateReconciler reconciles a KubeTemplate object
type KubeTemplateReconciler struct {
	client.Client
	Scheme                    *runtime.Scheme
	OperatorNamespace         string
	WorkQueue                 *queue.WorkQueue
	PeriodicReconcileInterval time.Duration
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
	// 2. Failed templates - will be retried with automatic retry cycle reset after cooldown
	// DO NOT enqueue:
	// - Completed templates (avoid continuous reconciliation loop without ResourceWatcher)
	//   TODO: Re-enable when DynamicInformer is implemented for drift detection
	// Note: Failed templates now automatically retry with reset counter after 5 min cooldown

	// For completed templates, use periodic reconciliation (every 60s)
	// Apply resources with SSA to detect and correct drift without changing status
	if kubeTemplate.Status.ProcessingPhase == "Completed" {
		// Check if template is actually idle before reconciling
		// This prevents conflicts between periodic reconcile and active worker processing
		if r.WorkQueue.Contains(types.NamespacedName{
			Namespace: kubeTemplate.Namespace,
			Name:      kubeTemplate.Name,
		}) {
			log.V(1).Info("Skipping periodic reconciliation: template is queued for processing",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace)
			return ctrl.Result{RequeueAfter: r.PeriodicReconcileInterval}, nil
		}

		// Skip if recently reconciled (within last half of periodic interval)
		// This prevents tight loops when workers are still updating status
		if kubeTemplate.Status.LastReconcileTime != nil {
			timeSinceReconcile := time.Since(kubeTemplate.Status.LastReconcileTime.Time)
			minInterval := r.PeriodicReconcileInterval / 2
			if timeSinceReconcile < minInterval {
				log.V(1).Info("Skipping periodic reconciliation: recently reconciled",
					"name", kubeTemplate.Name,
					"namespace", kubeTemplate.Namespace,
					"timeSinceReconcile", timeSinceReconcile)
				return ctrl.Result{RequeueAfter: r.PeriodicReconcileInterval}, nil
			}
		}

		log.V(1).Info("Periodic reconciliation: applying template resources with SSA",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)

		// Apply resources directly with Server-Side Apply
		// This will detect drift and correct it without triggering WorkQueue processing
		if err := r.applyTemplateResources(ctx, &kubeTemplate); err != nil {
			log.Error(err, "Failed to apply resources during periodic reconciliation")
			// Don't return error - just log and continue, will retry in next cycle
		}

		// Schedule next periodic reconciliation
		// Status remains "Completed" - no WorkQueue enqueue
		return ctrl.Result{RequeueAfter: r.PeriodicReconcileInterval}, nil
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
	// Only update if drift detected or first reconcile to avoid conflicts with worker status updates
	now := metav1.Now()
	needsStatusUpdate := driftDetected || kubeTemplate.Status.LastReconcileTime == nil
	
	if needsStatusUpdate {
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
	} else {
		log.V(1).Info("Skipping status update: no drift detected",
			"template", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplate{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Ignore updates that only change status to prevent reconciliation loops
				// Status updates from worker should not trigger controller reconciliation
				if e.ObjectOld == nil || e.ObjectNew == nil {
					return true
				}
				
				oldTemplate, okOld := e.ObjectOld.(*kubetemplateriov1alpha1.KubeTemplate)
				newTemplate, okNew := e.ObjectNew.(*kubetemplateriov1alpha1.KubeTemplate)
				
				if !okOld || !okNew {
					return true
				}
				
				// Compare only spec and metadata (ignore status)
				// Trigger reconciliation only if spec or relevant metadata changed
				specChanged := !apiequality.Semantic.DeepEqual(oldTemplate.Spec, newTemplate.Spec)
				annotationsChanged := !apiequality.Semantic.DeepEqual(oldTemplate.Annotations, newTemplate.Annotations)
				labelsChanged := !apiequality.Semantic.DeepEqual(oldTemplate.Labels, newTemplate.Labels)
				
				return specChanged || annotationsChanged || labelsChanged
			},
		})).
		Named("kubetemplater.io-kubetemplate").
		Complete(r)
}
