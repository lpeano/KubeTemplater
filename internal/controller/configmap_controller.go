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

package controller

import (
	"bytes"
	"context"
	"text/template"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// L'ANNOTAZIONE che KubeTemplater cerca per "attivarsi"
	TemplateWatchAnnotation = "kubetemplater.io"
	// Il valore che l'annotazione deve avere
	AnnotationValue = "true"

	// La chiave (key) dentro la ConfigMap.Data dove si trova lo YAML di configurazione
	ConfigDataKey = "resources.yaml"

	// Il nome del "padrone" del campo per il Server-Side Apply
	FieldManagerName = "kubetemplater-operator"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

// Reconcile è la funzione principale del controller.
// Viene chiamata solo per le ConfigMap che hanno l'annotazione corretta.
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Carica la ConfigMap
	var cm corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cm); err != nil {
		// Ignora se non trovata (probabilmente è stata cancellata)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Estrai e parsa la configurazione YAML
	// Non serve ri-controllare l'annotazione, il Predicate l'ha già fatto
	yamlData, ok := cm.Data[ConfigDataKey]
	if !ok {
		logger.Info("La ConfigMap non ha la chiave 'resources.yaml', skipping", "key", ConfigDataKey)
		return ctrl.Result{}, nil // Nessun errore, riprova solo se la CM cambia
	}

	var resources []TemplateResource
	if err := yaml.Unmarshal([]byte(yamlData), &resources); err != nil {
		logger.Error(err, "Impossibile fare unmarshal del YAML dalla ConfigMap")
		return ctrl.Result{}, err // Errore, riprova
	}

	logger.Info("ConfigMap aggiornata. Risorse da processare", "count", len(resources))

	// 4. Loop: Renderizza e Applica ogni risorsa
	for _, res := range resources {
		// 4a. Prepara i valori per il template Go (crea una map[string]string)
		templateValues := make(map[string]interface{})
		for _, v := range res.Values {
			templateValues[v.Name] = v.Value
		}

		// 4b. Renderizza il template Go
		tmpl, err := template.New(res.Name).Parse(res.Template)
		if err != nil {
			logger.Error(err, "Errore nel parsing del template", "resourceName", res.Name)
			continue // Vai al prossimo, non bloccare tutto
		}

		var renderedManifest bytes.Buffer
		if err := tmpl.Execute(&renderedManifest, templateValues); err != nil {
			logger.Error(err, "Errore nell'esecuzione del template", "resourceName", res.Name)
			continue
		}

		// 4c. Prepara l'oggetto per il Server-Side Apply
		// Usiamo "Unstructured" perché non sappiamo che tipo di risorsa è (Deployment? Service? ...)
		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(renderedManifest.Bytes(), &obj.Object); err != nil {
			// A volte il template può renderizzare "null" o YAML vuoto se i valori sono mancanti
			if string(renderedManifest.Bytes()) == "null" || len(renderedManifest.Bytes()) == 0 {
				logger.Info("Template renderizzato vuoto, skipping", "resourceName", res.Name)
				continue
			}
			logger.Error(err, "Impossibile fare unmarshal del manifest renderizzato", "resourceName", res.Name, "manifest", renderedManifest.String())
			continue
		}

		// Imposta il namespace (fondamentale!)
		// Le risorse verranno create nello stesso namespace della ConfigMap
		obj.SetNamespace(cm.Namespace)

		// 4d. Esegui il Server-Side Apply (SSA)
		logger.Info("Applicazione della risorsa (Server-Side Apply)", "name", obj.GetName(), "kind", obj.GetKind(), "namespace", obj.GetNamespace())

		// client.Apply è la patch SSA.
		// client.FieldOwner è obbligatorio per SSA e identifica il nostro operator.
		patchOptions := []client.PatchOption{
			client.ForceOwnership,
			client.FieldOwner(FieldManagerName),
		}

		if err := r.Patch(ctx, obj, client.Apply, patchOptions...); err != nil {
			logger.Error(err, "Errore durante il Server-Side Apply", "resourceName", res.Name)
		}
	}

	return ctrl.Result{}, nil
}

// checkAnnotation controlla se l'annotazione richiesta esiste e ha il valore "true"
func checkAnnotation(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	val, ok := annotations[TemplateWatchAnnotation]
	return ok && val == AnnotationValue
}

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch;create;update;patch;delete

// predicateForConfigMap crea un filtro per "guardare" solo le ConfigMap
// che hanno la nostra annotazione specifica.
func predicateForConfigMap() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return checkAnnotation(e.Object.GetAnnotations())
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Reagisce se il *nuovo* oggetto ha l'annotazione
			return checkAnnotation(e.ObjectNew.GetAnnotations())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Reagisce anche alla cancellazione
			return checkAnnotation(e.Object.GetAnnotations())
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return checkAnnotation(e.Object.GetAnnotations())
		},
	}
}

// SetupWithManager imposta il controller e aggiunge il filtro (Predicate)
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).                 // La risorsa primaria che guardiamo
		WithEventFilter(predicateForConfigMap()). // Il NOSTRO filtro
		Complete(r)
}
