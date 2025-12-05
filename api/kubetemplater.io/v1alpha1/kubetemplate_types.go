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
}

// KubeTemplateStatus defines the observed state of KubeTemplate.
type KubeTemplateStatus struct {
	Status string `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
