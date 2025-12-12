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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var resourceWatcherLog = logf.Log.WithName("resource-watcher")

// ResourceWatcherReconciler watches resources created by KubeTemplates
type ResourceWatcherReconciler struct {
	client.Client
}

// SetupWithManager sets up watches for resources with kubetemplater.io labels
// PERFORMANCE NOTE: This watches ALL unstructured resources and filters client-side.
// This is the tradeoff for supporting dynamic resource types unknown at compile time.
// The predicate filters efficiently but ALL watch events are received from API server.
// In large clusters, consider periodically reconciling templates instead of watching all resources.
func (r *ResourceWatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate filters resources by kubetemplater.io labels
	// This runs client-side AFTER receiving events from API server
	pred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		objLabels := obj.GetLabels()
		if objLabels == nil {
			return false
		}
		// Verify both required labels are present
		_, hasName := objLabels["kubetemplater.io/template-name"]
		_, hasNamespace := objLabels["kubetemplater.io/template-namespace"]
		return hasName && hasNamespace
	})

	// Watch unstructured objects with predicate filtering
	// Trade-offs:
	// ✅ Supports ANY resource type (dynamic discovery)
	// ✅ No hardcoded GVK list to maintain
	// ✅ Automatic recreation of deleted resources
	// ⚠️ Receives ALL unstructured watch events (filtered client-side)
	// ⚠️ Higher network traffic in large clusters with many unstructured resources
	//
	// Alternative approaches if performance becomes an issue:
	// 1. Periodic reconciliation (e.g. every 5 minutes) instead of watching
	// 2. Watch only specific known GVKs (Deployment, Service, ConfigMap, etc.)
	// 3. Namespace-scoped watching (if templates create resources in specific namespaces)
	return ctrl.NewControllerManagedBy(mgr).
		For(&unstructured.Unstructured{}).
		WithEventFilter(pred).
		Named("resource-watcher").
		Complete(r)
}

// mapToKubeTemplate extracts the KubeTemplate reference from resource labels
func (r *ResourceWatcherReconciler) mapToKubeTemplate(ctx context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels == nil {
		return nil
	}

	templateName, hasName := labels["kubetemplater.io/template-name"]
	templateNamespace, hasNamespace := labels["kubetemplater.io/template-namespace"]

	if !hasName || !hasNamespace {
		return nil
	}

	resourceWatcherLog.V(1).Info("Resource changed, triggering KubeTemplate reconciliation",
		"resource", obj.GetName(),
		"resourceNamespace", obj.GetNamespace(),
		"templateName", templateName,
		"templateNamespace", templateNamespace)

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      templateName,
				Namespace: templateNamespace,
			},
		},
	}
}

// Reconcile handles the reconciliation of watched resources
func (r *ResourceWatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// This reconciler doesn't actually reconcile anything directly
	// It just triggers KubeTemplate reconciliation via the mapper function
	// The actual work is done by KubeTemplateReconciler
	
	// No requeue needed - we only react to watch events
	return ctrl.Result{}, nil
}
