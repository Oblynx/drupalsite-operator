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
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/go-logr/logr"
	appsv1 "github.com/openshift/api/apps/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/operator-framework/operator-lib/status"
	"github.com/prometheus/common/log"
	webservicesv1a1 "gitlab.cern.ch/drupal/paas/drupalsite-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// finalizerStr string that is going to added to every DrupalSiteRequest created
	finalizerStr   = "controller.drupalsiterequest.webservices.cern.ch"
	MAX_BACKOFF_MS = 10000
)

//validateSpec validates the spec against the DrupalSiteRequestSpec definition
func validateSpec(drpSpec webservicesv1a1.DrupalSiteRequestSpec) reconcileError {
	_, err := govalidator.ValidateStruct(drpSpec)
	if err != nil {
		return newApplicationError(err, ErrInvalidSpec)
	}
	return nil
}

func contains(a []string, x string) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

// ensureSpecFinalizer ensures that the spec is valid, adding extra info if necessary, and that the finalizer is there,
// then returns if it needs to be updated.
func ensureSpecFinalizer(drp *webservicesv1a1.DrupalSiteRequest) (update bool) {
	if !contains(drp.GetFinalizers(), finalizerStr) {
		drp.SetFinalizers(append(drp.GetFinalizers(), finalizerStr))
		update = true
	}
	return
}

// exponentialBackoffMillisec is the waiting time in milliseconds until retry for a given number of failed attempts,
// up to `MAX_BACKOFF_MS`ms. Adds 50ms jitter.
func exponentialBackoffMillisec(attempts int) (waitMs time.Duration) {
	attemptsDurationMs := math.Pow(2, float64(attempts)) * 10
	return time.Duration(math.Min(attemptsDurationMs, MAX_BACKOFF_MS)+50*rand.Float64()) * time.Millisecond
}

// ensureInstalled implements the site install workflow and updates the Status conditions accordingly
func (r *DrupalSiteRequestReconciler) ensureInstalled(ctx context.Context, drp *webservicesv1a1.DrupalSiteRequest) (reconcile ctrl.Result, transientErr reconcileError) {
	if transientErr := r.ensurePreInstallResources(drp); transientErr != nil {
		drp.Status.Conditions.SetCondition(status.Condition{
			Type:   "Installed",
			Status: "False",
		})
		return ctrl.Result{}, transientErr
	}
	if jobComplete := r.isDrushJobCompleted(ctx, drp); !jobComplete {
		attempts := 0
		if drp.Status.Conditions.GetCondition(status.ConditionType("Installed")) != nil {
			var err error
			attempts, err = strconv.Atoi(string(drp.Status.Conditions.GetCondition(status.ConditionType("Installed")).Reason))
			if err != nil {
				return ctrl.Result{}, newApplicationError(fmt.Errorf("Error parsing condition[\"Installed\"] as int: %v", err), ErrTemporary)
			}
		}
		drp.Status.Conditions.SetCondition(status.Condition{
			Type:   "Installed",
			Status: "False",
			Reason: status.ConditionReason(strconv.Itoa(attempts + 1)),
		})
		return ctrl.Result{
			Requeue: true, RequeueAfter: exponentialBackoffMillisec(attempts),
		}, nil
	}
	if transientErr := r.ensureDependentResources(drp); transientErr != nil {
		drp.Status.Conditions.SetCondition(status.Condition{
			Type:   "Installed",
			Status: "False",
		})
		return ctrl.Result{}, transientErr.Wrap("%v while ensuring the dependent resources")
	}
	drp.Status.Conditions.SetCondition(status.Condition{
		Type:   "Installed",
		Status: "True",
	})
	return ctrl.Result{}, nil
}

/*
ensurePreInstallResources ensures presence of the primary set of resources namely MySQL DeploymentConfig,
MySQL Service, PersistentVolumeClaim and Site install Kubernetes Job
*/
func (r *DrupalSiteRequestReconciler) ensurePreInstallResources(drp *webservicesv1a1.DrupalSiteRequest) (transientErr reconcileError) {
	ctx := context.TODO()
	if transientErr := r.ensureResourceX(ctx, drp, "pvc"); transientErr != nil {
		return transientErr.Wrap("%v: for PVC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "dc_mysql"); transientErr != nil {
		return transientErr.Wrap("%v: for Mysql DC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "svc_mysql"); transientErr != nil {
		return transientErr.Wrap("%v: for Mysql SVC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "site_install_job"); transientErr != nil {
		return transientErr.Wrap("%v: for Drush site-install Job")
	}
	return nil
}

/*
ensureDependentResources ensures the presence of the requested dependent resources namely PHP DeploymentConfig,
PHP Service, PHP ConfigMap, Nginx DeploymentConfig, Nginx Service, Route
*/
func (r *DrupalSiteRequestReconciler) ensureDependentResources(drp *webservicesv1a1.DrupalSiteRequest) (transientErr reconcileError) {
	ctx := context.TODO()

	if transientErr := r.ensureResourceX(ctx, drp, "dc_php"); transientErr != nil {
		return transientErr.Wrap("%v: for PHP DC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "svc_php"); transientErr != nil {
		return transientErr.Wrap("%v: for PHP SVC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "fpm_cm"); transientErr != nil {
		return transientErr.Wrap("%v: for PHP FPM CM")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "dc_nginx"); transientErr != nil {
		return transientErr.Wrap("%v: for Nginx DC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "svc_nginx"); transientErr != nil {
		return transientErr.Wrap("%v: for Nginx SVC")
	}
	if transientErr := r.ensureResourceX(ctx, drp, "route"); transientErr != nil {
		return transientErr.Wrap("%v: for Route")
	}
	return nil
}

// labelsForDrupalSiterequest returns the labels for selecting the resources
// belonging to the given drupalSiteRequest CR name.
func labelsForDrupalSiterequest(name string) map[string]string {
	return map[string]string{"CRD": "drupalSiteRequest", "drupalSiteRequest_cr": name}
}

// deploymentConfigForDrupalSiteRequestMySQL returns a DeploymentConfig object for MySQL
func deploymentConfigForDrupalSiteRequestMySQL(d *webservicesv1a1.DrupalSiteRequest) *appsv1.DeploymentConfig {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "mysql"
	objectName := "drupal-mysql-" + d.Name

	dep := &appsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName,
			Namespace: d.Namespace,
		},
		Spec: appsv1.DeploymentConfigSpec{
			Replicas: 1,
			Selector: ls,
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:           "mysql:5.7",
						Name:            "mysql",
						ImagePullPolicy: "Always",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 3306,
							Name:          "mysql",
							Protocol:      "TCP",
						}},
						Env: []corev1.EnvVar{
							{
								Name:  "MYSQL_DATABASE",
								Value: "drupal",
							},
							{
								Name: "MYSQL_ROOT_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										Key: "DB_PASSWORD",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "drupal-mysql-secret",
										},
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "mysql-persistent-storage",
							MountPath: "/var/lib/mysql",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name:         "mysql-persistent-storage",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
				},
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, dep, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(dep, asOwner(d))
	return dep
}

// deploymentConfigForDrupalSiteRequestNginx returns a DeploymentConfig for Nginx
func deploymentConfigForDrupalSiteRequestNginx(d *webservicesv1a1.DrupalSiteRequest) *appsv1.DeploymentConfig {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "nginx"

	dep := &appsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-nginx-" + d.Name,
			Namespace: d.Namespace,
		},
		Spec: appsv1.DeploymentConfigSpec{
			Replicas: 1,
			Selector: ls,
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:           "gitlab-registry.cern.ch/drupal/paas/drupalsite-operator/nginx:drupal-" + d.Spec.DrupalVersion,
						Name:            "nginx",
						ImagePullPolicy: "Always",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
							Name:          "nginx",
							Protocol:      "TCP",
						}},
						Env: []corev1.EnvVar{
							{
								Name:  "DRUPAL_SHARED_VOLUME",
								Value: "/drupal-data",
							},
						},
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "drupal-mysql-secret",
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "drupal-directory-" + d.Name,
							MountPath: "/drupal-data",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "drupal-directory-" + d.Name,
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "drupal-pv-claim-" + d.Name,
							},
						},
					}},
				},
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, dep, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(dep, asOwner(d))
	return dep
}

// deploymentConfigForDrupalSiteRequestPHP returns a DeploymentConfig object for PHP
func deploymentConfigForDrupalSiteRequestPHP(d *webservicesv1a1.DrupalSiteRequest) *appsv1.DeploymentConfig {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "php"

	dep := &appsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-php-" + d.Name,
			Namespace: d.Namespace,
		},
		Spec: appsv1.DeploymentConfigSpec{
			Replicas: 1,
			Selector: ls,
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:           "gitlab-registry.cern.ch/drupal/paas/drupalsite-operator/php-fpm:drupal-" + d.Spec.DrupalVersion,
						Name:            "php-fpm",
						ImagePullPolicy: "Always",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 9000,
							Name:          "php-fpm",
							Protocol:      "TCP",
						}},
						Env: []corev1.EnvVar{
							{
								Name:  "DRUPAL_SHARED_VOLUME",
								Value: "/drupal-data",
							},
						},
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "drupal-mysql-secret",
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "drupal-directory-" + d.Name,
								MountPath: "/drupal-data",
							},
							{
								Name:      "config-volume",
								MountPath: "/usr/local/etc/php-fpm.d/www.conf",
								SubPath:   "www.conf",
							}},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "drupal-directory-" + d.Name,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "drupal-pv-claim-" + d.Name,
								},
							},
						},
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "php-fpm-cm-" + d.Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, dep, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(dep, asOwner(d))
	return dep
}

// persistentVolumeClaimForDrupalSiteRequest returns a PVC object
func persistentVolumeClaimForDrupalSiteRequest(d *webservicesv1a1.DrupalSiteRequest) *corev1.PersistentVolumeClaim {
	// ls := labelsForDrupalSiterequest(d.Name)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-pv-claim-" + d.Name,
			Namespace: d.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			// Selector: &metav1.LabelSelector{
			// 	MatchLabels: ls,
			// },
			StorageClassName: pointer.StringPtr("cephfs-no-backup"),
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("5Gi"),
				},
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, pvc, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(pvc, asOwner(d))
	return pvc
}

// serviceForDrupalSiteRequestPHP returns a service object for PHP
func serviceForDrupalSiteRequestPHP(d *webservicesv1a1.DrupalSiteRequest) *corev1.Service {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "php"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "php-fpm",
			Namespace: d.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{{
				TargetPort: intstr.FromInt(9000),
				Name:       "php-fpm",
				Port:       80,
				Protocol:   "TCP",
			}},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, svc, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(svc, asOwner(d))
	return svc
}

// serviceForDrupalSiteRequestNginx returns a service object for Nginx
func serviceForDrupalSiteRequestNginx(d *webservicesv1a1.DrupalSiteRequest) *corev1.Service {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "nginx"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-nginx",
			Namespace: d.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{{
				TargetPort: intstr.FromInt(8080),
				Name:       "nginx",
				Port:       80,
				Protocol:   "TCP",
			}},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, svc, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(svc, asOwner(d))
	return svc
}

// serviceForDrupalSiteRequestMySQL returns a service object for MySQL
func serviceForDrupalSiteRequestMySQL(d *webservicesv1a1.DrupalSiteRequest) *corev1.Service {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "mysql"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-mysql",
			Namespace: d.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{{
				TargetPort: intstr.FromInt(3306),
				Name:       "mysql",
				Port:       3306,
				Protocol:   "TCP",
			}},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, svc, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(svc, asOwner(d))
	return svc
}

// routeForDrupalSiteRequest returns a route object
func routeForDrupalSiteRequest(d *webservicesv1a1.DrupalSiteRequest) *routev1.Route {
	// ls := labelsForDrupalSiterequest(d.Name)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drupal-mysql" + d.Name,
			Namespace: d.Namespace,
		},
		Spec: routev1.RouteSpec{
			Host: d.Name + "." + os.Getenv("CLUSTER_NAME") + ".cern.ch",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   "drupal-nginx",
				Weight: pointer.Int32Ptr(100),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(8080),
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, svc, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(route, asOwner(d))
	return route
}

// jobForDrupalSiteRequestDrush returns a job object thats runs drush
func jobForDrupalSiteRequestDrush(d *webservicesv1a1.DrupalSiteRequest) *batchv1.Job {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["job"] = "drush"

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "drush-job-" + d.Name,
			Namespace: d.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Image:           "bash",
						Name:            "pvc-init",
						ImagePullPolicy: "Always",
						Command:         []string{"bash", "-c", "mkdir -p $DRUPAL_SHARED_VOLUME/{files,private,modules,themes}"},
						Env: []corev1.EnvVar{
							{
								Name:  "DRUPAL_SHARED_VOLUME",
								Value: "/drupal-data",
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "drupal-directory-" + d.Name,
							MountPath: "/drupal-data",
						}},
					}},
					RestartPolicy: "Never",
					Containers: []corev1.Container{{
						Image:           "gitlab-registry.cern.ch/drupal/paas/drupalsite-operator/drush:drupal-" + d.Spec.DrupalVersion,
						Name:            "drush",
						ImagePullPolicy: "Always",
						Command:         siteInstallJobForDrupalSite(),
						Env: []corev1.EnvVar{
							{
								Name:  "DRUPAL_SHARED_VOLUME",
								Value: "/drupal-data",
							},
						},
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "drupal-mysql-secret",
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "drupal-directory-" + d.Name,
							MountPath: "/drupal-data",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "drupal-directory-" + d.Name,
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "drupal-pv-claim-" + d.Name,
							},
						},
					}},
				},
			},
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, dep, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(job, asOwner(d))
	return job
}

// configMapForPHPFPM returns a job object thats runs drush
func configMapForPHPFPM(d *webservicesv1a1.DrupalSiteRequest) *corev1.ConfigMap {
	ls := labelsForDrupalSiterequest(d.Name)
	ls["app"] = "php"

	content, err := ioutil.ReadFile("config/www.conf")
	if err != nil {
		log.Errorf("read failed: %v", err)
		return nil
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "php-fpm-cm-" + d.Name,
			Namespace: d.Namespace,
		},
		Data: map[string]string{
			"www.conf": string(content),
		},
	}
	// Set DrupalSiteRequest instance as the owner and controller
	// ctrl.SetControllerReference(d, dep, r.Scheme)
	// Add owner reference
	addOwnerRefToObject(cm, asOwner(d))
	return cm
}

// createResource creates a given resource passed as an argument
func createResource(ctx context.Context, res runtime.Object, name string, namespace string, r *DrupalSiteRequestReconciler) (transientErr reconcileError) {
	deleteRecreate := func() error {
		err := r.Delete(ctx, res)
		if err != nil {
			return err
		}
		return r.Create(ctx, res)
	}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, res)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating Resource", "Resource.Namespace", namespace, "Resource.Name", name)
		err := r.Create(ctx, res)
		if err != nil {
			switch {
			case apierrors.IsAlreadyExists(err):
				err = deleteRecreate()
				if err != nil {
					log.Error(err, "Failed to create new Resource", "Resource.Namespace", namespace, "Resource.Name", name)
					return newApplicationError(err, ErrClientK8s)
				}
			default:
				return newApplicationError(err, ErrClientK8s)
			}
		}
	}
	return nil
}

/*
ensureResourceX ensure the requested resource is created, with the following valid values
	- dc_mysql: DeploymentConfig for MySQL
	- svc_mysql: Service for MySQL
	- pvc: PersistentVolume for the drupalsite
	- site_install_job: Kubernetes Job for the drush site-install
	- dc_php: DeploymentConfig for PHP
	- dc_ngxin: DeploymentConfig for Nginx
	- svc_php: Service for PHP
	- svc_nginx: Service for Nginx
	- fpm_cm: ConfigMap for PHP-FPM
	- route: Route for the drupalsite
*/
func (r *DrupalSiteRequestReconciler) ensureResourceX(ctx context.Context, d *webservicesv1a1.DrupalSiteRequest, resType string) (transientErr reconcileError) {
	switch resType {
	case "dc_mysql":
		res := deploymentConfigForDrupalSiteRequestMySQL(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "dc_php":
		res := deploymentConfigForDrupalSiteRequestPHP(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "dc_nginx":
		res := deploymentConfigForDrupalSiteRequestNginx(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "svc_mysql":
		res := serviceForDrupalSiteRequestMySQL(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "svc_php":
		res := serviceForDrupalSiteRequestPHP(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "svc_nginx":
		res := serviceForDrupalSiteRequestNginx(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "pvc":
		res := persistentVolumeClaimForDrupalSiteRequest(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "route":
		res := routeForDrupalSiteRequest(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "site_install_job":
		res := jobForDrupalSiteRequestDrush(d)
		return createResource(ctx, res, res.Name, res.Namespace, r)
	case "fpm_cm":
		res := configMapForPHPFPM(d)
		if res == nil {
			return newApplicationError(nil, ErrFunctionDomain)
		}
		return createResource(ctx, res, res.Name, res.Namespace, r)
	default:
		return newApplicationError(nil, ErrFunctionDomain)
	}
}

// updateCRorFailReconcile tries to update the Custom Resource and logs any error
func (r *DrupalSiteRequestReconciler) updateCRorFailReconcile(ctx context.Context, log logr.Logger, drp *webservicesv1a1.DrupalSiteRequest) (
	reconcile.Result, error) {
	if err := r.Update(ctx, drp); err != nil {
		log.Error(err, fmt.Sprintf("%v failed to update the application", ErrClientK8s))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// updateCRStatusorFailReconcile tries to update the Custom Resource Status and logs any error
func (r *DrupalSiteRequestReconciler) updateCRStatusorFailReconcile(ctx context.Context, log logr.Logger, drp *webservicesv1a1.DrupalSiteRequest) (
	reconcile.Result, error) {
	if err := r.Status().Update(ctx, drp); err != nil {
		log.Error(err, fmt.Sprintf("%v failed to update the application status", ErrClientK8s))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// addOwnerRefToObject appends the desired OwnerReference to the object
func addOwnerRefToObject(obj metav1.Object, ownerRef metav1.OwnerReference) {
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
}

// asOwner returns an OwnerReference set as the memcached CR
func asOwner(d *webservicesv1a1.DrupalSiteRequest) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: d.APIVersion,
		Kind:       d.Kind,
		Name:       d.Name,
		UID:        d.UID,
		Controller: &trueVar,
	}
}

// isDrushJobCompleted checks if the drush job is successfully completed
func (r *DrupalSiteRequestReconciler) isDrushJobCompleted(ctx context.Context, d *webservicesv1a1.DrupalSiteRequest) bool {
	found := &batchv1.Job{}
	jobObject := jobForDrupalSiteRequestDrush(d)
	err := r.Get(ctx, types.NamespacedName{Name: jobObject.Name, Namespace: jobObject.Namespace}, found)
	if err == nil {
		if found.Status.Succeeded != 0 {
			return true
		}
	}
	return false
}

// siteInstallJobForDrupalSite outputs the command needed for jobForDrupalSiteRequestDrush
func siteInstallJobForDrupalSite() []string {
	return []string{"sh", "-c", "drush site-install -y --config-dir=../config/sync --account-name=admin --account-pass=pass --account-mail=admin@example.com"}
}
