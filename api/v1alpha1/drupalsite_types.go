/*
Copyright 2021.

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
	"github.com/operator-framework/operator-lib/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DrupalSiteSpec defines the desired state of DrupalSite
type DrupalSiteSpec struct {
	// Publish defines if the site has to be published or not
	// +kubebuilder:validation:Required
	Publish bool `json:"publish"`

	// DrupalVersion defines the version of the Drupal to install
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	DrupalVersion string `json:"drupalVersion"` // Convert to enum

	// Environment defines the drupal site environments
	// +kubebuilder:validation:Required
	Environment `json:"environment"`
}

// Environment defines the environment field in DrupalSite
type Environment struct {
	// Name specifies the environment name for the DrupalSite. The name will be used for resource lables and route name
	// +kubebuilder:validation:Pattern=[a-z0-9]([-a-z0-9]*[a-z0-9])?
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// ExtraConfigRepo passes on the git url with advanced configuration to the DrupalSite S2I functionality
	// TODO: support branches https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/28
	// +kubebuilder:validation:Pattern=`[(http(s)?):\/\/(www\.)?a-zA-Z0-9@:%._\+~#=]{2,256}\.[a-z]{2,6}\b([-a-zA-Z0-9@:%_\+.~#?&//=]*)`
	ExtraConfigRepo string `json:"extraConfigRepo,omitempty"`
	// ImageOverride overrides the image urls in the DrupalSite deployment for the fields that are set
	ImageOverride `json:"imageOverride,omitempty"`
}

// ImageOverride lets the website admin bypass the operator's buildconfigs and inject custom images.
// Envisioned primarily for the sitebuilder, this could allow an advanced developer to deploy their own
// custom version of Drupal or different PHP versions.
type ImageOverride struct {
	// Sitebuilder overrides the Sitebuilder image url in the DrupalSite deployment
	// +kubebuilder:validation:Pattern=`[a-z0-9]+(?:[\/._-][a-z0-9]+)*.`
	Sitebuilder string `json:"siteBuilder,omitempty"`

	// Note: Overrides for the nginx and php images might be added if needed
}

// DrupalSiteStatus defines the observed state of DrupalSite
type DrupalSiteStatus struct {
	// Phase aggregates the information from all the conditions and reports on the lifecycle phase of the resource
	// Enum: {Creating,Created,Deleted}
	Phase string `json:"phase,omitempty"`

	// Conditions specifies different conditions based on the DrupalSite status
	// +kubebuilder:validation:type=array
	Conditions status.Conditions `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DrupalSite is the Schema for the drupalsites API
type DrupalSite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DrupalSiteSpec   `json:"spec,omitempty"`
	Status DrupalSiteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DrupalSiteList contains a list of DrupalSite
type DrupalSiteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DrupalSite `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DrupalSite{}, &DrupalSiteList{})
}

func (drp DrupalSite) ConditionTrue(condition status.ConditionType) (update bool) {
	init := drp.Status.Conditions.GetCondition(condition)
	return init != nil && init.Status == v1.ConditionTrue
}
