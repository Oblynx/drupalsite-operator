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

package v2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

//+kubebuilder:object:root=true

// ProjectConfig is the Schema for the projectconfigs API
type ProjectConfig struct {
	metav1.TypeMeta `json:",inline"`

	// ControllerManagerConfigurationSpec returns the contfigurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`

	SiteBuilderImage    string `json:"siteBuilderImage,omitempty"`
	NginxImage          string `json:"nginxImage,omitempty"`
	PhpFpmExporterImage string `json:"phpFpmExporterImage,omitempty"`
	WebDAVImage         string `json:"webDAVImage,omitempty"`
	SMTPHost            string `json:"smtpHost,omitempty"`
	VeleroNamespace     string `json:"veleroNamespace,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ProjectConfig{})
}
