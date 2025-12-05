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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	if kubeTemplate.Status.ProcessingPhase == "" || kubeTemplate.Status.ProcessingPhase == "Failed" {
		kubeTemplate.Status.ProcessingPhase = "Queued"
		now := time.Now()
		kubeTemplate.Status.QueuedAt = &now
		kubeTemplate.Status.ProcessedAt = nil
		kubeTemplate.Status.RetryCount = 0

		if err := r.Status().Update(ctx, &kubeTemplate); err != nil {
			log.Error(err, "Failed to update KubeTemplate status to Queued")
			return ctrl.Result{}, err
		}

		// Enqueue the KubeTemplate for async processing
		r.WorkQueue.Enqueue(types.NamespacedName{
			Namespace: kubeTemplate.Namespace,
			Name:      kubeTemplate.Name,
		}, 0) // Priority 0 (normal)

		log.Info("Enqueued KubeTemplate for processing", "name", kubeTemplate.Name, "namespace", kubeTemplate.Namespace)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetemplateriov1alpha1.KubeTemplate{}).
		Named("kubetemplater.io-kubetemplate").
		Complete(r)
}
