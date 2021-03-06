/*
Copyright 2021 CERN.

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
)

// DrupalProjectConfigSpec defines the desired state of DrupalProjectConfig
type DrupalProjectConfigSpec struct {
	// PrimarySiteName defines the primary DrupalSite instance of a project
	// +optional
	PrimarySiteName string `json:"primarySiteName,omitempty"`
}

// DrupalProjectConfigStatus defines the observed state of DrupalProjectConfig
type DrupalProjectConfigStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DrupalProjectConfig is the Schema for the drupalprojectconfigs API
type DrupalProjectConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DrupalProjectConfigSpec   `json:"spec,omitempty"`
	Status DrupalProjectConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DrupalProjectConfigList contains a list of DrupalProjectConfig
type DrupalProjectConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DrupalProjectConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DrupalProjectConfig{}, &DrupalProjectConfigList{})
}
