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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// KubeTemplateSpec defines the desired state of KubeTemplate.
type KubeTemplateSpec struct {
	Templates []Template `json:"templates"`
}

// Template defines a template to be rendered.
type Template struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	Object runtime.RawExtension `json:"object"`
	// +optional
	Replace bool `json:"replace,omitempty"`
	// +optional
	// Referenced determines if the created object should have the policy as OwnerReference.
	// When true, the policy will be added as an owner reference to the created resource.
	// Default: false
	Referenced bool `json:"referenced,omitempty"`
}

// KubeTemplateStatus defines the observed state of KubeTemplate.
type KubeTemplateStatus struct {
	Status              string       `json:"status,omitempty"`
	ProcessingPhase     string       `json:"processingPhase,omitempty"` // Queued, Processing, Completed, Failed, Paused
	QueuedAt            *metav1.Time `json:"queuedAt,omitempty"`
	ProcessedAt         *metav1.Time `json:"processedAt,omitempty"`
	RetryCount          int          `json:"retryCount,omitempty"`
	RetryCycle          int          `json:"retryCycle,omitempty"`
	LastReconcileTime   *metav1.Time `json:"lastReconcileTime,omitempty"`
	ResourcesTotal      int          `json:"resourcesTotal,omitempty"`
	ResourcesSynced     int          `json:"resourcesSynced,omitempty"`
	LastDriftDetected   *metav1.Time `json:"lastDriftDetected,omitempty"`
	DriftDetectionCount int          `json:"driftDetectionCount,omitempty"`
	DryRunChecks        int          `json:"dryRunChecks,omitempty"`
	// AppliedSpecHash is the SHA256 hash of the spec that was last successfully applied
	AppliedSpecHash string       `json:"appliedSpecHash,omitempty"`
	// PausedReason describes why the template is paused
	PausedReason string       `json:"pausedReason,omitempty"`
	// PausedAt is the timestamp when the template was paused
	PausedAt *metav1.Time `json:"pausedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.processingPhase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Retry Cycle",type=integer,JSONPath=`.status.retryCycle`,priority=1
// +kubebuilder:printcolumn:name="Resources",type=string,JSONPath=`.status.resourcesSynced`,priority=1
// +kubebuilder:printcolumn:name="Last Reconcile",type="date",JSONPath=`.status.lastReconcileTime`,priority=1
// +kubebuilder:printcolumn:name="Drift Count",type=integer,JSONPath=`.status.driftDetectionCount`,priority=1
// +kubebuilder:printcolumn:name="Last Drift",type="date",JSONPath=`.status.lastDriftDetected`,priority=1

// KubeTemplate is the Schema for the kubetemplates API.
type KubeTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeTemplateSpec   `json:"spec,omitempty"`
	Status KubeTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeTemplateList contains a list of KubeTemplate.
type KubeTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeTemplate{}, &KubeTemplateList{})
}
