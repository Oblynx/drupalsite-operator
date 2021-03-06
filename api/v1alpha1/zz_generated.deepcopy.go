// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"github.com/operator-framework/operator-lib/status"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Backup) DeepCopyInto(out *Backup) {
	*out = *in
	if in.Date != nil {
		in, out := &in.Date, &out.Date
		*out = (*in).DeepCopy()
	}
	if in.Expires != nil {
		in, out := &in.Expires, &out.Expires
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Backup.
func (in *Backup) DeepCopy() *Backup {
	if in == nil {
		return nil
	}
	out := new(Backup)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Configuration) DeepCopyInto(out *Configuration) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Configuration.
func (in *Configuration) DeepCopy() *Configuration {
	if in == nil {
		return nil
	}
	out := new(Configuration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalProjectConfig) DeepCopyInto(out *DrupalProjectConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalProjectConfig.
func (in *DrupalProjectConfig) DeepCopy() *DrupalProjectConfig {
	if in == nil {
		return nil
	}
	out := new(DrupalProjectConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalProjectConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalProjectConfigList) DeepCopyInto(out *DrupalProjectConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DrupalProjectConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalProjectConfigList.
func (in *DrupalProjectConfigList) DeepCopy() *DrupalProjectConfigList {
	if in == nil {
		return nil
	}
	out := new(DrupalProjectConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalProjectConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalProjectConfigSpec) DeepCopyInto(out *DrupalProjectConfigSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalProjectConfigSpec.
func (in *DrupalProjectConfigSpec) DeepCopy() *DrupalProjectConfigSpec {
	if in == nil {
		return nil
	}
	out := new(DrupalProjectConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalProjectConfigStatus) DeepCopyInto(out *DrupalProjectConfigStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalProjectConfigStatus.
func (in *DrupalProjectConfigStatus) DeepCopy() *DrupalProjectConfigStatus {
	if in == nil {
		return nil
	}
	out := new(DrupalProjectConfigStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSite) DeepCopyInto(out *DrupalSite) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSite.
func (in *DrupalSite) DeepCopy() *DrupalSite {
	if in == nil {
		return nil
	}
	out := new(DrupalSite)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalSite) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteConfigOverride) DeepCopyInto(out *DrupalSiteConfigOverride) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteConfigOverride.
func (in *DrupalSiteConfigOverride) DeepCopy() *DrupalSiteConfigOverride {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteConfigOverride)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalSiteConfigOverride) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteConfigOverrideList) DeepCopyInto(out *DrupalSiteConfigOverrideList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DrupalSiteConfigOverride, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteConfigOverrideList.
func (in *DrupalSiteConfigOverrideList) DeepCopy() *DrupalSiteConfigOverrideList {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteConfigOverrideList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalSiteConfigOverrideList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteConfigOverrideSpec) DeepCopyInto(out *DrupalSiteConfigOverrideSpec) {
	*out = *in
	in.Php.DeepCopyInto(&out.Php)
	in.Nginx.DeepCopyInto(&out.Nginx)
	in.Webdav.DeepCopyInto(&out.Webdav)
	in.PhpExporter.DeepCopyInto(&out.PhpExporter)
	in.Cron.DeepCopyInto(&out.Cron)
	in.DrupalLogs.DeepCopyInto(&out.DrupalLogs)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteConfigOverrideSpec.
func (in *DrupalSiteConfigOverrideSpec) DeepCopy() *DrupalSiteConfigOverrideSpec {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteConfigOverrideSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteConfigOverrideStatus) DeepCopyInto(out *DrupalSiteConfigOverrideStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteConfigOverrideStatus.
func (in *DrupalSiteConfigOverrideStatus) DeepCopy() *DrupalSiteConfigOverrideStatus {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteConfigOverrideStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteList) DeepCopyInto(out *DrupalSiteList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DrupalSite, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteList.
func (in *DrupalSiteList) DeepCopy() *DrupalSiteList {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DrupalSiteList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteSpec) DeepCopyInto(out *DrupalSiteSpec) {
	*out = *in
	if in.SiteURL != nil {
		in, out := &in.SiteURL, &out.SiteURL
		*out = make([]Url, len(*in))
		copy(*out, *in)
	}
	out.Version = in.Version
	out.Configuration = in.Configuration
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteSpec.
func (in *DrupalSiteSpec) DeepCopy() *DrupalSiteSpec {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalSiteStatus) DeepCopyInto(out *DrupalSiteStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make(status.Conditions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	out.ReleaseID = in.ReleaseID
	if in.AvailableBackups != nil {
		in, out := &in.AvailableBackups, &out.AvailableBackups
		*out = make([]Backup, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ExpectedDeploymentReplicas != nil {
		in, out := &in.ExpectedDeploymentReplicas, &out.ExpectedDeploymentReplicas
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalSiteStatus.
func (in *DrupalSiteStatus) DeepCopy() *DrupalSiteStatus {
	if in == nil {
		return nil
	}
	out := new(DrupalSiteStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DrupalVersion) DeepCopyInto(out *DrupalVersion) {
	*out = *in
	out.ReleaseSpec = in.ReleaseSpec
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DrupalVersion.
func (in *DrupalVersion) DeepCopy() *DrupalVersion {
	if in == nil {
		return nil
	}
	out := new(DrupalVersion)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReleaseID) DeepCopyInto(out *ReleaseID) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReleaseID.
func (in *ReleaseID) DeepCopy() *ReleaseID {
	if in == nil {
		return nil
	}
	out := new(ReleaseID)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReleaseSpec) DeepCopyInto(out *ReleaseSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReleaseSpec.
func (in *ReleaseSpec) DeepCopy() *ReleaseSpec {
	if in == nil {
		return nil
	}
	out := new(ReleaseSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Resources) DeepCopyInto(out *Resources) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Resources.
func (in *Resources) DeepCopy() *Resources {
	if in == nil {
		return nil
	}
	out := new(Resources)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SupportedDrupalVersions) DeepCopyInto(out *SupportedDrupalVersions) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SupportedDrupalVersions.
func (in *SupportedDrupalVersions) DeepCopy() *SupportedDrupalVersions {
	if in == nil {
		return nil
	}
	out := new(SupportedDrupalVersions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SupportedDrupalVersions) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SupportedDrupalVersionsList) DeepCopyInto(out *SupportedDrupalVersionsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SupportedDrupalVersions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SupportedDrupalVersionsList.
func (in *SupportedDrupalVersionsList) DeepCopy() *SupportedDrupalVersionsList {
	if in == nil {
		return nil
	}
	out := new(SupportedDrupalVersionsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SupportedDrupalVersionsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SupportedDrupalVersionsSpec) DeepCopyInto(out *SupportedDrupalVersionsSpec) {
	*out = *in
	if in.Blacklist != nil {
		in, out := &in.Blacklist, &out.Blacklist
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SupportedDrupalVersionsSpec.
func (in *SupportedDrupalVersionsSpec) DeepCopy() *SupportedDrupalVersionsSpec {
	if in == nil {
		return nil
	}
	out := new(SupportedDrupalVersionsSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SupportedDrupalVersionsStatus) DeepCopyInto(out *SupportedDrupalVersionsStatus) {
	*out = *in
	if in.AvailableVersions != nil {
		in, out := &in.AvailableVersions, &out.AvailableVersions
		*out = make([]DrupalVersion, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SupportedDrupalVersionsStatus.
func (in *SupportedDrupalVersionsStatus) DeepCopy() *SupportedDrupalVersionsStatus {
	if in == nil {
		return nil
	}
	out := new(SupportedDrupalVersionsStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Version) DeepCopyInto(out *Version) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Version.
func (in *Version) DeepCopy() *Version {
	if in == nil {
		return nil
	}
	out := new(Version)
	in.DeepCopyInto(out)
	return out
}
