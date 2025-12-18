package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
)

const (
	namespaceFinalizer = "kubetemplater.io/namespace-finalizer"
)

// NamespaceReconciler reconciles Namespace objects to manage KubeTemplate cleanup
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=kubetemplater.io,resources=kubetemplates,verbs=get;list;watch;delete

// Reconcile handles namespace deletion and cleanup of associated KubeTemplates
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the namespace
	var namespace corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &namespace); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Namespace")
		return ctrl.Result{}, err
	}

	// Check if namespace is being deleted
	if namespace.DeletionTimestamp.IsZero() {
		// Namespace is not being deleted, ensure finalizer is present
		if !controllerutil.ContainsFinalizer(&namespace, namespaceFinalizer) {
			controllerutil.AddFinalizer(&namespace, namespaceFinalizer)
			if err := r.Update(ctx, &namespace); err != nil {
				log.Error(err, "Failed to add finalizer to namespace")
				return ctrl.Result{}, err
			}
			log.Info("Added finalizer to namespace", "namespace", namespace.Name)
		}
		return ctrl.Result{}, nil
	}

	// Namespace is being deleted
	if controllerutil.ContainsFinalizer(&namespace, namespaceFinalizer) {
		// Delete all KubeTemplates in this namespace
		log.Info("Namespace is being deleted, cleaning up KubeTemplates", "namespace", namespace.Name)

		templateList := &kubetemplateriov1alpha1.KubeTemplateList{}
		if err := r.List(ctx, templateList, client.InNamespace(namespace.Name)); err != nil {
			log.Error(err, "Failed to list KubeTemplates in namespace", "namespace", namespace.Name)
			return ctrl.Result{}, err
		}

		deletedCount := 0
		for i := range templateList.Items {
			template := &templateList.Items[i]
			if err := r.Delete(ctx, template); err != nil {
				if !errors.IsNotFound(err) {
					log.Error(err, "Failed to delete KubeTemplate",
						"namespace", namespace.Name,
						"templateName", template.Name)
					return ctrl.Result{}, err
				}
			} else {
				deletedCount++
				log.Info("Deleted KubeTemplate during namespace cleanup",
					"namespace", namespace.Name,
					"templateName", template.Name)
			}
		}

		log.Info("Completed KubeTemplate cleanup",
			"namespace", namespace.Name,
			"templatesDeleted", deletedCount)

		// Remove finalizer
		controllerutil.RemoveFinalizer(&namespace, namespaceFinalizer)
		if err := r.Update(ctx, &namespace); err != nil {
			log.Error(err, "Failed to remove finalizer from namespace")
			return ctrl.Result{}, err
		}
		log.Info("Removed finalizer from namespace", "namespace", namespace.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named("namespace").
		Complete(r)
}
