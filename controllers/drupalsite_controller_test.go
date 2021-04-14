/*


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

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/operator-framework/operator-lib/status"
	dbodv1a1 "gitlab.cern.ch/drupal/paas/dbod-operator/go/api/v1alpha1"
	drupalwebservicesv1alpha1 "gitlab.cern.ch/drupal/paas/drupalsite-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Ginkgo makes it easy to write expressive specs that describe the behavior of your code in an organized manner.
// Describe and Context containers are used organize the tests and It is used to define the test result
// Describe is a top-level container that defines the test
// Context defines the circumstance i.e parameters or specifications
// It defines what the test result should be
// By defines smaller tests under a given it and in case of failure, the 'By' text is printed out
// Ref: http://onsi.github.io/ginkgo/

var _ = Describe("DrupalSite controller", func() {
	const (
		Name      = "test"
		Namespace = "default"

		timeout  = time.Second * 30
		duration = time.Second * 30
		interval = time.Millisecond * 250
	)
	var (
		drupalSiteObject = &drupalwebservicesv1alpha1.DrupalSite{}
	)

	ctx := context.Background()
	key := types.NamespacedName{
		Name:      Name,
		Namespace: Namespace,
	}

	BeforeEach(func() {
		drupalSiteObject = &drupalwebservicesv1alpha1.DrupalSite{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "drupal.webservices.cern.ch/v1alpha1",
				Kind:       "DrupalSite",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: drupalwebservicesv1alpha1.DrupalSiteSpec{
				Publish:       true,
				DrupalVersion: "8.9.13",
				DiskSize:      "10Gi",
				Environment: drupalwebservicesv1alpha1.Environment{
					Name:      "dev",
					QoSClass:  "standard",
					DBODClass: "test",
				},
			},
		}
	})

	Describe("Creating drupalSite object", func() {
		Context("With basic spec", func() {
			It("All dependent resources should be created", func() {
				By("By creating a new drupalSite")
				Eventually(func() error {
					return k8sClient.Create(ctx, drupalSiteObject)
				}, timeout, interval).Should(Succeed())

				By("Expecting drupalSite object created")
				cr := drupalwebservicesv1alpha1.DrupalSite{}
				Eventually(func() error {
					return k8sClient.Get(ctx, key, &cr)
				}, timeout, interval).Should(Succeed())
				trueVar := true
				expectedOwnerReference := v1.OwnerReference{
					APIVersion: "drupal.webservices.cern.ch/v1alpha1",
					Kind:       "DrupalSite",
					Name:       Name,
					UID:        cr.UID,
					Controller: &trueVar,
				}
				configmap := corev1.ConfigMap{}
				svc := corev1.Service{}
				pvc := corev1.PersistentVolumeClaim{}
				job := batchv1.Job{}
				deploy := appsv1.Deployment{}
				is := imagev1.ImageStream{}
				bc := buildv1.BuildConfig{}
				dbod := dbodv1a1.DBODRegistration{}
				route := routev1.Route{}

				// Check DBOD resource creation
				By("Expecting DBOD resource created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &dbod)
					return dbod.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Update DBOD resource status field
				By("Updating secret name in DBOD resource status")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &dbod)
					dbod.Status.DbCredentialsSecret = "test"
					return k8sClient.Status().Update(ctx, &dbod)
				}, timeout, interval).Should(Succeed())

				By("Expecting the drupal deployment to have the EnvFrom secret field set correctly")
				Eventually(func() bool {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					if len(deploy.Spec.Template.Spec.Containers) < 2 || len(deploy.Spec.Template.Spec.Containers[0].EnvFrom) == 0 || len(deploy.Spec.Template.Spec.Containers[1].EnvFrom) == 0 {
						return false
					}
					if deploy.Spec.Template.Spec.Containers[0].EnvFrom[0].SecretRef.Name == "test" && deploy.Spec.Template.Spec.Containers[1].EnvFrom[0].SecretRef.Name == "test" {
						return true
					}
					return false
				}, timeout, interval).Should(BeTrue())

				By("Expecting the drush job to have the EnvFrom secret field set correctly")
				Eventually(func() bool {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					if len(job.Spec.Template.Spec.Containers) == 0 || len(job.Spec.Template.Spec.Containers[0].EnvFrom) == 0 {
						return false
					}
					if job.Spec.Template.Spec.Containers[0].EnvFrom[0].SecretRef.Name == "test" {
						return true
					}
					return false
				}, timeout, interval).Should(BeTrue())

				// Check PHP-FPM configMap creation
				By("Expecting PHP_FPM configmaps created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "php-fpm-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx configMap creation
				By("Expecting Nginx configmaps created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal service
				By("Expecting Drupal service created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return svc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check drupal persistentVolumeClaim
				By("Expecting drupal persistentVolumeClaim created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "pv-claim-" + key.Name, Namespace: key.Namespace}, &pvc)
					return pvc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal deployments
				By("Expecting Drupal deployments created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					return deploy.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx imageStream
				By("Expecting Nginx imageStream created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &is)
					return is.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drush job
				By("Expecting Drush job created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					return job.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx buildConfig
				By("Expecting Nginx buildConfigs created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + nameVersionHash(drupalSiteObject), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Update drupalSite custom resource status fields to allow route conditions
				By("Updating 'installed' and 'ready' status fields in drupalSite resource")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &cr)
					cr.Status.Conditions.SetCondition(status.Condition{Type: "Ready", Status: "True"})
					cr.Status.Conditions.SetCondition(status.Condition{Type: "Installed", Status: "True"})
					return k8sClient.Status().Update(ctx, &cr)
				}, timeout, interval).Should(Succeed())

				// Check Route
				// Route is supposed to be created, but since the 'ReadyReplicas' status in deployment can't be made persistent with the reconcile loop, route won't be created
				By("Expecting Route to be created since publish is true")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &route)
					return route.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(Not(ContainElement(expectedOwnerReference)))

				// Switch "publish: false"
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &cr)
					cr.Spec.Publish = false
					return k8sClient.Update(ctx, &cr)
				}, timeout, interval).Should(Succeed())

				By("Expecting Route to be removed after switching publish to false.")
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &route)
				}, timeout, interval).Should(Not(Succeed()))
			})
		})
	})

	Describe("Deleting dependent objects", func() {
		Context("Of the basic drupalSite", func() {
			It("All dependent resources should be recreated successfully", func() {
				By("Expecting drupalSite object created")
				cr := drupalwebservicesv1alpha1.DrupalSite{}
				Eventually(func() error {
					return k8sClient.Get(ctx, key, &cr)
				}, timeout, interval).Should(Succeed())
				trueVar := true
				expectedOwnerReference := v1.OwnerReference{
					APIVersion: "drupal.webservices.cern.ch/v1alpha1",
					Kind:       "DrupalSite",
					Name:       key.Name,
					UID:        cr.UID,
					Controller: &trueVar,
				}
				configmap := corev1.ConfigMap{}
				svc := corev1.Service{}
				pvc := corev1.PersistentVolumeClaim{}
				job := batchv1.Job{}
				deploy := appsv1.Deployment{}
				is := imagev1.ImageStream{}
				bc := buildv1.BuildConfig{}
				route := routev1.Route{}

				// Check PHP-FPM configMap recreation
				By("Expecting PHP_FPM configmaps recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "php-fpm-" + key.Name, Namespace: key.Namespace}, &configmap)
					return k8sClient.Delete(ctx, &configmap)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "php-fpm-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx configMap creation
				By("Expecting Nginx configmaps recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &configmap)
					return k8sClient.Delete(ctx, &configmap)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal service
				By("Expecting Drupal service recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return k8sClient.Delete(ctx, &svc)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return svc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check drupal persistentVolumeClaim
				By("Expecting drupal persistentVolumeClaim recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "pv-claim-" + key.Name, Namespace: key.Namespace}, &pvc)
					return k8sClient.Delete(ctx, &pvc)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "pv-claim-" + key.Name, Namespace: key.Namespace}, &pvc)
					return pvc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal deployments
				By("Expecting Drupal deployments recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					return k8sClient.Delete(ctx, &deploy)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					return deploy.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx imageStream
				By("Expecting Nginx imageStream recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &is)
					return k8sClient.Delete(ctx, &is)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &is)
					return is.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drush job
				By("Expecting Drush job recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					return k8sClient.Delete(ctx, &job)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					return job.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx buildConfig
				By("Expecting Nginx buildConfigs recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + nameVersionHash(drupalSiteObject), Namespace: key.Namespace}, &bc)
					return k8sClient.Delete(ctx, &bc)
				}, timeout, interval).Should(Succeed())
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + nameVersionHash(drupalSiteObject), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Route
				// Since we switch the publish field to 'false' in the last test case, there shouldn't be a route that exists
				By("Expecting Route recreated")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &route)
					return k8sClient.Delete(ctx, &route)
				}, timeout, interval).Should(Not(Succeed()))
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &route)
					return route.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(Not(ContainElement(expectedOwnerReference)))
			})
		})
	})

	Describe("Updating a child object", func() {
		Context("Without admin annotations", func() {
			It("Should not be updated successfully", func() {
				By("By updating service object")
				svc := corev1.Service{}
				k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
				svc.Labels["app"] = "testUpdateLabel"
				Eventually(func() error {
					return k8sClient.Update(ctx, &svc)
				}, timeout, interval).Should(Succeed())

				time.Sleep(1 * time.Second)

				Eventually(func() map[string]string {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return svc.GetLabels()
				}, timeout, interval).ShouldNot(HaveKeyWithValue("app", "testUpdateLabel"))
			})
		})
	})

	Describe("Updating a child object", func() {
		Context("With admin annotations", func() {
			It("Should be updated successfully", func() {
				const adminAnnotation = "drupal.cern.ch/admin-custom-edit"
				By("By updating service object with admin annotation")
				svc := corev1.Service{}
				k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
				if len(svc.GetAnnotations()) == 0 {
					svc.Annotations = map[string]string{}
				}
				svc.Annotations[adminAnnotation] = "true"
				Eventually(func() error {
					return k8sClient.Update(ctx, &svc)
				}, timeout, interval).Should(Succeed())

				svc.Labels["app"] = "testUpdateLabel"
				Eventually(func() error {
					return k8sClient.Update(ctx, &svc)
				}, timeout, interval).Should(Succeed())

				Eventually(func() map[string]string {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return svc.GetLabels()
				}, timeout, interval).Should(HaveKeyWithValue("app", "testUpdateLabel"))
			})
		})
	})

	Describe("Deleting the drupalsite object", func() {
		Context("With basic spec", func() {
			It("Should be deleted successfully", func() {
				By("Expecting to delete successfully")
				Eventually(func() error {
					return k8sClient.Delete(context.Background(), drupalSiteObject)
				}, timeout, interval).Should(Succeed())

				By("Expecting to delete finish")
				Eventually(func() error {
					return k8sClient.Get(ctx, key, drupalSiteObject)
				}, timeout, interval).ShouldNot(Succeed())
			})
		})
	})

	Describe("Creating a drupalSite object", func() {
		Context("With s2i source", func() {
			It("All dependent resources should be created", func() {
				key = types.NamespacedName{
					Name:      Name + "-advanced",
					Namespace: "advanced",
				}
				drupalSiteObject = &drupalwebservicesv1alpha1.DrupalSite{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "drupal.webservices.cern.ch/v1alpha1",
						Kind:       "DrupalSite",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      key.Name,
						Namespace: key.Namespace,
					},
					Spec: drupalwebservicesv1alpha1.DrupalSiteSpec{
						Publish:       false,
						DrupalVersion: "8.9.13",
						DiskSize:      "10Gi",
						Environment: drupalwebservicesv1alpha1.Environment{
							Name:            "dev",
							QoSClass:        "standard",
							ExtraConfigRepo: "https://gitlab.cern.ch/rvineetr/test-ravineet-d8-containers-buildconfig.git",
						},
					},
				}

				By("By creating the testing namespace")
				Eventually(func() error {
					return k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
						Name: key.Namespace},
					})
				}, timeout, interval).Should(Succeed())

				By("By creating a new drupalSite")
				Eventually(func() error {
					return k8sClient.Create(ctx, drupalSiteObject)
				}, timeout, interval).Should(Succeed())

				// Create drupalSite object
				By("Expecting drupalSite object created")
				cr := drupalwebservicesv1alpha1.DrupalSite{}
				Eventually(func() error {
					return k8sClient.Get(ctx, key, &cr)
				}, timeout, interval).Should(Succeed())
				trueVar := true
				expectedOwnerReference := v1.OwnerReference{
					APIVersion: "drupal.webservices.cern.ch/v1alpha1",
					Kind:       "DrupalSite",
					Name:       key.Name,
					UID:        cr.UID,
					Controller: &trueVar,
				}
				configmap := corev1.ConfigMap{}
				svc := corev1.Service{}
				pvc := corev1.PersistentVolumeClaim{}
				job := batchv1.Job{}
				deploy := appsv1.Deployment{}
				is := imagev1.ImageStream{}
				bc := buildv1.BuildConfig{}
				dbod := dbodv1a1.DBODRegistration{}
				route := routev1.Route{}

				// Check DBOD resource creation
				By("Expecting DBOD resource created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &dbod)
					return dbod.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Update DBOD resource status field
				By("Updating secret name in DBOD resource status")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &dbod)
					dbod.Status.DbCredentialsSecret = "test"
					return k8sClient.Status().Update(ctx, &dbod)
				}, timeout, interval).Should(Succeed())

				By("Expecting the drupal deployment to have the EnvFrom secret field set correctly")
				Eventually(func() bool {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					if len(deploy.Spec.Template.Spec.Containers) < 2 || len(deploy.Spec.Template.Spec.Containers[0].EnvFrom) == 0 || len(deploy.Spec.Template.Spec.Containers[1].EnvFrom) == 0 {
						return false
					}
					if deploy.Spec.Template.Spec.Containers[0].EnvFrom[0].SecretRef.Name == "test" && deploy.Spec.Template.Spec.Containers[1].EnvFrom[0].SecretRef.Name == "test" {
						return true
					}
					return false
				}, timeout, interval).Should(BeTrue())

				By("Expecting the drush job to have the EnvFrom secret field set correctly")
				Eventually(func() bool {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					if len(job.Spec.Template.Spec.Containers) == 0 || len(job.Spec.Template.Spec.Containers[0].EnvFrom) == 0 {
						return false
					}
					if job.Spec.Template.Spec.Containers[0].EnvFrom[0].SecretRef.Name == "test" {
						return true
					}
					return false
				}, timeout, interval).Should(BeTrue())

				// Check PHP-FPM configMap creation
				By("Expecting PHP_FPM configmaps created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "php-fpm-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx configMap creation
				By("Expecting Nginx configmaps created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &configmap)
					return configmap.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal service
				By("Expecting Drupal service created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &svc)
					return svc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check drupal persistentVolumeClaim
				By("Expecting drupal persistentVolumeClaim created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "pv-claim-" + key.Name, Namespace: key.Namespace}, &pvc)
					return pvc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drupal deployments
				By("Expecting Drupal deployments created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &deploy)
					return deploy.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx imageStream
				By("Expecting Nginx imageStream created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + key.Name, Namespace: key.Namespace}, &is)
					return is.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check sitebuilder-s2i imageStream
				By("Expecting sitebuilder-s2i imageStream created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-builder-s2i-" + key.Name, Namespace: key.Namespace}, &is)
					return is.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Drush job
				By("Expecting Drush job created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-install-" + key.Name, Namespace: key.Namespace}, &job)
					return job.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check S2I buildConfig
				By("Expecting S2I buildConfig created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-builder-s2i-" + nameVersionHash(drupalSiteObject), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx buildConfig
				By("Expecting Nginx buildConfigs created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + nameVersionHash(drupalSiteObject), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Update drupalSite custom resource status fields to allow route conditions
				By("Updating 'installed' and 'ready' status fields in drupalSite resource")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &cr)
					cr.Status.Conditions.SetCondition(status.Condition{Type: "Ready", Status: "True"})
					cr.Status.Conditions.SetCondition(status.Condition{Type: "Installed", Status: "True"})
					return k8sClient.Status().Update(ctx, &cr)
				}, timeout, interval).Should(Succeed())

				// Check Route
				By("Expecting Route to not be created since publish is false")
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &route)
				}, timeout, interval).Should(Not(Succeed()))
			})
		})
	})

	Describe("Update the drupalsite object", func() {
		Context("With a different drupal Version", func() {
			It("Should be updated successfully", func() {
				key = types.NamespacedName{
					Name:      Name + "-advanced",
					Namespace: "advanced",
				}
				drupalSiteObject = &drupalwebservicesv1alpha1.DrupalSite{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "drupal.webservices.cern.ch/v1alpha1",
						Kind:       "DrupalSite",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      key.Name,
						Namespace: key.Namespace,
					},
					Spec: drupalwebservicesv1alpha1.DrupalSiteSpec{
						Publish:       false,
						DrupalVersion: "8.9.13",
						DiskSize:      "10Gi",
						Environment: drupalwebservicesv1alpha1.Environment{
							Name:            "dev",
							QoSClass:        "standard",
							ExtraConfigRepo: "https://gitlab.cern.ch/rvineetr/test-ravineet-d8-containers-buildconfig.git",
						},
					},
				}
				newVersion := "8.9.13-new"

				// Create drupalSite object
				cr := drupalwebservicesv1alpha1.DrupalSite{}
				By("Expecting drupalSite object created")
				Eventually(func() error {
					return k8sClient.Get(ctx, key, &cr)
				}, timeout, interval).Should(Succeed())

				trueVar := true
				expectedOwnerReference := v1.OwnerReference{
					APIVersion: "drupal.webservices.cern.ch/v1alpha1",
					Kind:       "DrupalSite",
					Name:       key.Name,
					UID:        cr.UID,
					Controller: &trueVar,
				}
				bc := buildv1.BuildConfig{}
				By("Updating the version")
				Eventually(func() error {
					k8sClient.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, &cr)
					drupalSiteObject.Spec.DrupalVersion = newVersion
					return k8sClient.Update(ctx, &cr)
				}, timeout, interval).Should(Succeed())

				By("Expecting new S2I buildConfig to be updated")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "site-builder-s2i-" + nameVersionHash(&cr), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check Nginx buildConfig
				By("Expecting new Nginx buildConfigs created")
				Eventually(func() []v1.OwnerReference {
					k8sClient.Get(ctx, types.NamespacedName{Name: "nginx-" + nameVersionHash(&cr), Namespace: key.Namespace}, &bc)
					return bc.ObjectMeta.OwnerReferences
				}, timeout, interval).Should(ContainElement(expectedOwnerReference))

				// Check if the drupalSiteObject status has UpdateNeeded set
				By("Expecting 'UpdateNeeded' set on the drupalSiteObject")
				Eventually(func() bool {
					k8sClient.Get(ctx, key, &cr)
					// The condition for UpdateNeeded would be set to Unknown and the reason would be ExecError
					return cr.Status.Conditions.GetCondition("UpdateNeeded").Status != corev1.ConditionStatus(v1.ConditionFalse)
				}, timeout, interval).Should(BeTrue())

				// Further tests need to be implemented, if we can bypass ExecErr with envtest
			})
		})
	})

	Describe("Deleting the drupalsite object", func() {
		Context("With s2i spec", func() {
			It("Should be deleted successfully", func() {
				key = types.NamespacedName{
					Name:      Name + "-advanced",
					Namespace: "advanced",
				}
				drupalSiteObject = &drupalwebservicesv1alpha1.DrupalSite{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "drupal.webservices.cern.ch/v1alpha1",
						Kind:       "DrupalSite",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      key.Name,
						Namespace: key.Namespace,
					},
					Spec: drupalwebservicesv1alpha1.DrupalSiteSpec{
						Publish:       false,
						DrupalVersion: "8.9.13",
						DiskSize:      "10Gi",
						Environment: drupalwebservicesv1alpha1.Environment{
							Name:            "dev",
							QoSClass:        "standard",
							ExtraConfigRepo: "https://gitlab.cern.ch/rvineetr/test-ravineet-d8-containers-buildconfig.git",
						},
					},
				}
				By("Expecting to delete successfully")
				Eventually(func() error {
					return k8sClient.Delete(ctx, drupalSiteObject)
				}, timeout, interval).Should(Succeed())

				By("Expecting to delete finish")
				Eventually(func() error {
					return k8sClient.Get(ctx, key, drupalSiteObject)
				}, timeout, interval).ShouldNot(Succeed())
			})
		})
	})
})
