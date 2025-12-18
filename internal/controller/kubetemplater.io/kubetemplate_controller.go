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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/queue"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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

	// Handle Paused templates with resume annotation
	if kubeTemplate.Status.ProcessingPhase == "Paused" {
		if resumeValue, hasResume := kubeTemplate.Annotations["kubetemplater.io/resume"]; hasResume && resumeValue == "true" {
			log.Info("Resume annotation detected, resetting template to Queued",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace)
			
			// Reset to Queued and clear pause info
			kubeTemplate.Status.ProcessingPhase = "Queued"
			kubeTemplate.Status.PausedReason = ""
			kubeTemplate.Status.PausedAt = nil
			kubeTemplate.Status.RetryCount = 0
			now := metav1.Now()
			kubeTemplate.Status.QueuedAt = &now
			
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				if !errors.IsConflict(err) {
					log.Error(err, "Failed to update status after resume")
					return ctrl.Result{}, err
				}
			}
			
			// Enqueue for processing
			r.WorkQueue.Enqueue(types.NamespacedName{
				Namespace: kubeTemplate.Namespace,
				Name:      kubeTemplate.Name,
			}, 0)
			
			return ctrl.Result{}, nil
		}
		
		// Paused template without resume annotation - do nothing
		return ctrl.Result{}, nil
	}

	// Handle Failed templates with spec changes - re-queue for retry
	if kubeTemplate.Status.ProcessingPhase == "Failed" {
		log.Info("Failed template detected, checking for spec changes",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)
		
		// Calculate current spec hash
		currentHash := calculateSpecHash(kubeTemplate.Spec)
		
		// Check if spec has changed since failure
		if kubeTemplate.Status.AppliedSpecHash != "" && currentHash != kubeTemplate.Status.AppliedSpecHash {
			log.Info("Spec change detected on failed template, resetting for retry",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace,
				"oldHash", kubeTemplate.Status.AppliedSpecHash,
				"newHash", currentHash)
			
			// Reset to Queued for fresh processing
			kubeTemplate.Status.ProcessingPhase = "Queued"
			kubeTemplate.Status.RetryCount = 0
			kubeTemplate.Status.RetryCycle = 0
			now := metav1.Now()
			kubeTemplate.Status.QueuedAt = &now
			kubeTemplate.Status.AppliedSpecHash = currentHash
			
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				if !errors.IsConflict(err) {
					log.Error(err, "Failed to update status after spec change on failed template")
					return ctrl.Result{}, err
				}
			}
			
			// Enqueue immediately for processing
			r.WorkQueue.Enqueue(types.NamespacedName{
				Namespace: kubeTemplate.Namespace,
				Name:      kubeTemplate.Name,
			}, 0)
			
			log.Info("Failed template re-queued after spec change",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace)
			
			return ctrl.Result{}, nil
		}
		
		// No spec change - respect the retry cooldown mechanism
		// Don't re-queue if template has already been paused due to max retry cycles
		if kubeTemplate.Status.ProcessingPhase == "Failed" && 
		   !r.WorkQueue.Contains(types.NamespacedName{
			Namespace: kubeTemplate.Namespace,
			Name:      kubeTemplate.Name,
		}) {
			// Template failed but not in queue - this is expected during cooldown period
			// or if max retry cycles reached (template will be auto-paused by worker)
			// Let the WorkQueue's retry mechanism handle it naturally
			log.V(1).Info("Failed template not in queue, waiting for retry mechanism",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace,
				"retryCount", kubeTemplate.Status.RetryCount,
				"retryCycle", kubeTemplate.Status.RetryCycle)
		}
		
		// Don't trigger continuous reconciliation for failed templates
		// The WorkQueue will handle retries with exponential backoff
		return ctrl.Result{}, nil
	}

	// For completed templates, check if spec has changed via hash comparison
	if kubeTemplate.Status.ProcessingPhase == "Completed" {
		// Calculate current spec hash
		currentHash := calculateSpecHash(kubeTemplate.Spec)
		
		// Backward compatibility: populate hash if empty (first time after upgrade)
		if kubeTemplate.Status.AppliedSpecHash == "" {
			log.V(1).Info("Populating AppliedSpecHash for existing completed template",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace)
			kubeTemplate.Status.AppliedSpecHash = currentHash
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				if !errors.IsConflict(err) {
					log.Error(err, "Failed to update AppliedSpecHash")
				}
			}
			return ctrl.Result{RequeueAfter: r.PeriodicReconcileInterval}, nil
		}
		
		// Check if spec has changed
		if currentHash != kubeTemplate.Status.AppliedSpecHash {
			log.Info("Spec change detected via hash comparison, re-queueing template",
				"name", kubeTemplate.Name,
				"namespace", kubeTemplate.Namespace,
				"oldHash", kubeTemplate.Status.AppliedSpecHash,
				"newHash", currentHash)
			
			// Reset to Queued for full reprocessing
			kubeTemplate.Status.ProcessingPhase = "Queued"
			kubeTemplate.Status.RetryCount = 0
			now := metav1.Now()
			kubeTemplate.Status.QueuedAt = &now
			kubeTemplate.Status.AppliedSpecHash = currentHash
			
			if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
				if !errors.IsConflict(err) {
					log.Error(err, "Failed to update status after spec change")
					return ctrl.Result{}, err
				}
			}
			
			// Enqueue for processing
			r.WorkQueue.Enqueue(types.NamespacedName{
				Namespace: kubeTemplate.Namespace,
				Name:      kubeTemplate.Name,
			}, 0)
			
			return ctrl.Result{}, nil
		}
		
		// No spec change - proceed with periodic drift detection
		// Check if template is actually idle before reconciling
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

		log.V(1).Info("Periodic reconciliation: applying template resources with SSA dry-run",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)

		// Apply resources with dry-run drift detection
		if err := r.applyTemplateResources(ctx, &kubeTemplate); err != nil {
			log.Error(err, "Failed to apply resources during periodic reconciliation")
		}

		// Schedule next periodic reconciliation
		return ctrl.Result{RequeueAfter: r.PeriodicReconcileInterval}, nil
	}

	// Process new templates
	if kubeTemplate.Status.ProcessingPhase == "" {
		// FASE 2 FIX: Use retry.RetryOnConflict for robust status updates
		// This prevents templates from being lost due to transient conflicts
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			// Re-fetch the latest version to avoid conflicts
			var latestTemplate kubetemplateriov1alpha1.KubeTemplate
			if err := r.Get(ctx, req.NamespacedName, &latestTemplate); err != nil {
				return err
			}

			// Update status fields
			latestTemplate.Status.ProcessingPhase = "Queued"
			now := metav1.Now()
			latestTemplate.Status.QueuedAt = &now
			latestTemplate.Status.ProcessedAt = nil
			latestTemplate.Status.RetryCount = 0

			// Attempt status update
			return r.Status().Update(ctx, &latestTemplate)
		})

		if err != nil {
			log.Error(err, "Failed to update status to Queued after retries")
			// Fallback: requeue without error to retry later
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		log.Info("Successfully updated template status to Queued",
			"name", kubeTemplate.Name,
			"namespace", kubeTemplate.Namespace)
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

// applyTemplateResources applies the resources defined in the template using Server-Side Apply with dry-run drift detection
// This is used during periodic reconciliation to detect and correct drift accurately
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

		// Step 1: Get current resource state
		currentObj := &unstructured.Unstructured{}
		currentObj.SetGroupVersionKind(obj.GroupVersionKind())
		getErr := r.Client.Get(ctx, client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, currentObj)

		// Step 2: Dry-run SSA to see what WOULD change
		dryRunObj := obj.DeepCopy()
		fieldManager := "kubetemplater"
		dryRunErr := r.Client.Patch(ctx, dryRunObj, client.Apply,
			client.FieldOwner(fieldManager),
			client.ForceOwnership,
			client.DryRunAll)

		if dryRunErr != nil {
			log.Error(dryRunErr, "Dry-run failed",
				"kind", obj.GetKind(),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace())
			continue
		}

		// Step 3: Compare dry-run result with current state
		resourceDrifted := false
		if getErr == nil {
			// Resource exists - compare to detect drift
			if hasDrift(currentObj, dryRunObj) {
				resourceDrifted = true
				driftDetected = true
				log.Info("Drift detected via dry-run comparison",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
					"namespace", obj.GetNamespace())
			} else {
				log.V(2).Info("No drift detected",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
					"namespace", obj.GetNamespace())
			}
		} else if errors.IsNotFound(getErr) {
			// Resource doesn't exist - needs creation
			resourceDrifted = true
			log.V(1).Info("Resource does not exist, will be created",
				"kind", obj.GetKind(),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace())
		} else {
			// Other error
			log.Error(getErr, "Failed to get current resource")
			continue
		}

		// Step 4: Apply for real ONLY if drift detected or resource missing
		if resourceDrifted {
			if err := r.Client.Patch(ctx, &obj, client.Apply,
				client.FieldOwner(fieldManager),
				client.ForceOwnership); err != nil {
				log.Error(err, "Failed to apply object",
					"kind", obj.GetKind(),
					"name", obj.GetName(),
					"namespace", obj.GetNamespace())
				continue
			}
			log.Info("Applied resource to correct drift",
				"kind", obj.GetKind(),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace())
		}

		syncedResources++
	}

	// Update status with reconciliation info
	// Only update if drift detected or first reconcile to avoid conflicts with worker status updates
	now := metav1.Now()
	needsStatusUpdate := driftDetected || kubeTemplate.Status.LastReconcileTime == nil
	
	if needsStatusUpdate {
		kubeTemplate.Status.LastReconcileTime = &now
		kubeTemplate.Status.ResourcesTotal = totalResources
		kubeTemplate.Status.ResourcesSynced = syncedResources
		kubeTemplate.Status.DryRunChecks += totalResources  // Track dry-run operations

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

// calculateSpecHash computes SHA256 hash of the template spec for versioning
func calculateSpecHash(spec kubetemplateriov1alpha1.KubeTemplateSpec) string {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		// If marshaling fails, return empty string (will trigger reprocessing)
		return ""
	}
	hash := sha256.Sum256(specJSON)
	return hex.EncodeToString(hash[:])
}

// hasDrift compares two objects ignoring server-managed fields to detect real drift
func hasDrift(current, desired *unstructured.Unstructured) bool {
	// Extract specs for comparison
	currentSpec, currentHasSpec, _ := unstructured.NestedFieldCopy(current.Object, "spec")
	desiredSpec, desiredHasSpec, _ := unstructured.NestedFieldCopy(desired.Object, "spec")

	// If one has spec and the other doesn't, it's drift
	if currentHasSpec != desiredHasSpec {
		return true
	}

	// Compare specs semantically
	if currentHasSpec && desiredHasSpec {
		return !apiequality.Semantic.DeepEqual(currentSpec, desiredSpec)
	}

	// For resources without spec (ConfigMap, Secret), compare data/stringData
	currentData, currentHasData, _ := unstructured.NestedFieldCopy(current.Object, "data")
	desiredData, desiredHasData, _ := unstructured.NestedFieldCopy(desired.Object, "data")

	if currentHasData != desiredHasData {
		return true
	}

	if currentHasData && desiredHasData {
		return !apiequality.Semantic.DeepEqual(currentData, desiredData)
	}

	// No drift detected
	return false
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
