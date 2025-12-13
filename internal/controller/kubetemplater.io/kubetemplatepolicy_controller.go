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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/cache"
)

// KubeTemplatePolicyReconciler reconciles a KubeTemplatePolicy object
type KubeTemplatePolicyReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	PolicyCache *cache.PolicyCache
}

//+kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplatepolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplatepolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplatepolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplates,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// This controller watches KubeTemplatePolicy changes and immediately updates the PolicyCache
// to ensure webhook validation uses the most current policies without waiting for TTL expiration.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *KubeTemplatePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the policy
	var policy kubetemplateriov1alpha1.KubeTemplatePolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if errors.IsNotFound(err) {
			// Policy was deleted - we need to invalidate cache but don't have sourceNamespace
			// Solution: Clear entire cache to ensure deleted policy is removed immediately
			// This is safe because cache will repopulate on next access
			if r.PolicyCache != nil {
				r.PolicyCache.Clear()
				log.Info("Policy deleted, cleared entire cache for immediate effect", "policy", req.Name)
			}
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KubeTemplatePolicy")
		return ctrl.Result{}, err
	}

	// Policy exists (created or updated) - update cache immediately
	if r.PolicyCache != nil {
		r.PolicyCache.Update(&policy)
		log.V(1).Info("Updated PolicyCache",
			"policy", policy.Name,
			"sourceNamespace", policy.Spec.SourceNamespace)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeTemplatePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplatePolicy{}).
		Complete(r)
}
