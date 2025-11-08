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
	"encoding/json"
	"text/template"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	// ManagedResourcesAnnotation è l'annotazione usata per tracciare le risorse create
	ManagedResourcesAnnotation = "kubetemplater.io/managed-resources"

	// ReplaceEnabledAnnotation è l'annotazione che, se presente su una risorsa,
	// abilita la strategia di "replace" (delete + create) in caso di errori su campi immutabili.
	ReplaceEnabledAnnotation = "kubetemplater.io/replace-enabled"
)

// ManagedResourceMeta contiene le informazioni minime per identificare una risorsa creata.
// Questa struct viene salvata come JSON nell'annotazione della ConfigMap.
type ManagedResourceMeta struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	UID        types.UID `json:"uid"`
}

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
		// Ignora se non trovata (probabilmente è stata cancellata).
		// Le risorse figlie verranno cancellate dal garbage collector di Kubernetes
		// grazie agli OwnerReferences che abbiamo impostato.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// --- Gestione della cancellazione ---
	// Se la ConfigMap è in fase di cancellazione, non fare nulla.
	// L'OwnerReference si occuperà della pulizia.
	if !cm.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// --- Ottieni le risorse gestite in precedenza ---
	previouslyManaged := make(map[string]ManagedResourceMeta)
	annotation := cm.Annotations[ManagedResourcesAnnotation]
	if annotation != "" {
		if err := json.Unmarshal([]byte(annotation), &previouslyManaged); err != nil {
			logger.Error(err, "Errore nel fare unmarshal dell'annotazione delle risorse gestite")
			// Non bloccare la riconciliazione, continuiamo con una lista vuota
		}
	}

	// --- Logica di templating e applicazione ---
	currentlyManaged := make(map[string]ManagedResourceMeta)

	// Se la chiave 'resources.yaml' non esiste, consideriamo la lista di risorse desiderate come vuota.
	// Questo farà sì che il codice di pulizia rimuova tutte le risorse precedentemente create.
	yamlData, ok := cm.Data[ConfigDataKey]
	if !ok {
		logger.Info("La ConfigMap non ha la chiave 'resources.yaml', le risorse precedentemente create verranno rimosse", "key", ConfigDataKey)
	} else {
		var resources []TemplateResource
		if err := yaml.Unmarshal([]byte(yamlData), &resources); err != nil {
			logger.Error(err, "Impossibile fare unmarshal del YAML dalla ConfigMap")
			return ctrl.Result{}, err
		}

		logger.Info("ConfigMap aggiornata. Risorse da processare", "count", len(resources))

		for _, res := range resources {
			templateValues := make(map[string]interface{})
			for _, v := range res.Values {
				templateValues[v.Name] = v.Value
			}

			tmpl, err := template.New(res.Name).Parse(res.Template)
			if err != nil {
				logger.Error(err, "Errore nel parsing del template", "resourceName", res.Name)
				continue
			}

			var renderedManifest bytes.Buffer
			if err := tmpl.Execute(&renderedManifest, templateValues); err != nil {
				logger.Error(err, "Errore nell'esecuzione del template", "resourceName", res.Name)
				continue
			}

			if renderedManifest.Len() == 0 || renderedManifest.String() == "null" {
				logger.Info("Template renderizzato vuoto, skipping", "resourceName", res.Name)
				continue
			}

			obj := &unstructured.Unstructured{}
			if err := yaml.Unmarshal(renderedManifest.Bytes(), &obj.Object); err != nil {
				logger.Error(err, "Impossibile fare unmarshal del manifest renderizzato", "resourceName", res.Name, "manifest", renderedManifest.String())
				continue
			}

			// --- Applica la risorsa e aggiungila alla mappa 'currentlyManaged' ---
			obj.SetNamespace(cm.Namespace)
			if err := controllerutil.SetControllerReference(&cm, obj, r.Scheme); err != nil {
				logger.Error(err, "Impossibile impostare OwnerReference", "resourceName", res.Name)
				continue
			}

			patchOptions := []client.PatchOption{client.ForceOwnership, client.FieldOwner(FieldManagerName)}
			err = r.Patch(ctx, obj, client.Apply, patchOptions...)

			// Aggiungi sempre la risorsa alla mappa delle risorse gestite,
			// in modo che un patch fallito non causi una cancellazione per "risorsa orfana".
			key := obj.GetAPIVersion() + "/" + obj.GetKind() + "/" + obj.GetName()
			currentlyManaged[key] = ManagedResourceMeta{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				Name:       obj.GetName(),
				Namespace:  obj.GetNamespace(),
				UID:        obj.GetUID(), // L'UID sarà popolato solo dopo un'applicazione riuscita
			}

			if err != nil {
				isImmutableErr := errors.IsInvalid(err)
				annotations := obj.GetAnnotations()
				replaceEnabled := annotations != nil && annotations[ReplaceEnabledAnnotation] == "true"

				if isImmutableErr && replaceEnabled {
					logger.Info("Tentativo di modifica di un campo immutabile con replace abilitato. La risorsa verrà ricreata.", "resourceName", res.Name, "error", err.Error())

					// Strategia Replace: Cancella e poi ricrea immediatamente.
					if deleteErr := r.Delete(ctx, obj); deleteErr != nil && !errors.IsNotFound(deleteErr) {
						logger.Error(deleteErr, "Errore durante la cancellazione della risorsa per la replace", "resourceName", res.Name)
						// L'errore originale (err) verrà loggato sotto
					} else {
						// Ricrea la risorsa
						logger.Info("Risorsa cancellata, si procede con la ricreazione.", "resourceName", res.Name)
						if createErr := r.Patch(ctx, obj, client.Apply, patchOptions...); createErr != nil {
							logger.Error(createErr, "Errore durante la ricreazione della risorsa dopo la replace", "resourceName", res.Name)
							err = createErr // Assegna l'errore di creazione per il logging
						} else {
							logger.Info("Risorsa ricreata con successo dopo la replace.", "resourceName", res.Name)
							err = nil // Successo, annulla l'errore
						}
					}
				}

				// Logga l'errore finale, se ancora presente
				if err != nil {
					logger.Error(err, "Errore durante il Server-Side Apply", "resourceName", res.Name)
				}
			} else {
				logger.Info("Risorsa applicata (Server-Side Apply)", "name", obj.GetName(), "kind", obj.GetKind())
			}
		}
	}

	// --- Pulizia delle risorse orfane ---
	for key, oldObj := range previouslyManaged {
		if _, found := currentlyManaged[key]; !found {
			logger.Info("Rimozione risorsa orfana", "kind", oldObj.Kind, "name", oldObj.Name)

			objToDelete := &unstructured.Unstructured{}
			objToDelete.SetAPIVersion(oldObj.APIVersion)
			objToDelete.SetKind(oldObj.Kind)
			objToDelete.SetName(oldObj.Name)
			objToDelete.SetNamespace(oldObj.Namespace)

			if err := r.Delete(ctx, objToDelete); err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Errore durante la cancellazione della risorsa orfana", "kind", oldObj.Kind, "name", oldObj.Name)
				}
			}
		}
	}

	// --- Aggiorna l'annotazione della ConfigMap ---
	originalAnnotations := cm.Annotations
	if originalAnnotations == nil {
		originalAnnotations = make(map[string]string)
	}

	newAnnotationValueBytes, err := json.Marshal(currentlyManaged)
	if err != nil {
		logger.Error(err, "Impossibile fare marshal delle risorse gestite correntemente")
		return ctrl.Result{}, err
	}
	newAnnotationValue := string(newAnnotationValueBytes)

	// Controlla se l'annotazione è cambiata prima di fare la patch
	if originalAnnotations[ManagedResourcesAnnotation] != newAnnotationValue {
		patch := client.MergeFrom(cm.DeepCopy())
		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)
		}
		cm.Annotations[ManagedResourcesAnnotation] = newAnnotationValue
		if err := r.Patch(ctx, &cm, patch); err != nil {
			logger.Error(err, "Errore nell'aggiornamento dell'annotazione delle risorse gestite")
			return ctrl.Result{}, err
		}
		logger.Info("Annotazione delle risorse gestite aggiornata.")
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
