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

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/cache"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// PolicyCacheReconciler keeps the policy cache synchronized with cluster state
type PolicyCacheReconciler struct {
	client.Client
	Cache *cache.PolicyCache
}

// +kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplatepolicies,verbs=get;list;watch

func (r *PolicyCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var policy kubetemplateriov1alpha1.KubeTemplatePolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if errors.IsNotFound(err) {
			// Policy was deleted - invalidate cache entry
			log.Info("Policy deleted, invalidating cache", "policy", req.Name)
			r.Cache.Delete(policy.Spec.SourceNamespace)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get KubeTemplatePolicy")
		return ctrl.Result{}, err
	}

	// Policy was created or updated - update cache
	log.Info("Policy updated, refreshing cache", "policy", policy.Name, "sourceNamespace", policy.Spec.SourceNamespace)
	r.Cache.Set(policy.Spec.SourceNamespace, &policy)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *PolicyCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplatePolicy{}).
		Named("policy-cache").
		Complete(r)
}
