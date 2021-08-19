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

package controllers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"path"
	"time"

	"github.com/go-logr/logr"
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"

	dbodv1a1 "gitlab.cern.ch/drupal/paas/dbod-operator/api/v1alpha1"
	webservicesv1a1 "gitlab.cern.ch/drupal/paas/drupalsite-operator/apis/drupal.webservices/v1alpha1"
	authz "gitlab.cern.ch/paas-tools/operators/authz-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchbeta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

// Const vars
const (
	// Variable used to define Default WebDAV login Username
	webDAVDefaultLogin string = "admin"
)

var (
	// BuildResources are the resource requests/limits for the image builds. Set during initEnv()
	BuildResources corev1.ResourceRequirements
)

// execToServerPod executes a command to the first running server pod of the Drupal site.
//
// Commands are interpreted similar to how kubectl does it, eg to do "drush cr" either of these will work:
// - "drush", "cr"
// - "sh", "-c", "drush cr"
// The last syntax allows passing an entire bash script as a string.
//
// Example:
// ````
//	sout, serr, err := r.execToServerPod(ctx, drp, "php-fpm", nil, "sh", "-c", "drush version; ls")
//	sout, serr, err := r.execToServerPod(ctx, drp, "php-fpm", nil, "drush", "version")
//	if err != nil {
//		log.Error(err, "Error while exec into pod")
//	}
//	log.Info("EXEC", "stdout", sout, "stderr", serr)
// ````
func (r *DrupalSiteReconciler) execToServerPod(ctx context.Context, d *webservicesv1a1.DrupalSite, containerName string, stdin io.Reader, command ...string) (stdout string, stderr string, err error) {
	pod, err := r.getRunningPodForVersion(ctx, d, releaseID(d))
	if err != nil {
		return "", "", err
	}
	return execToPodThroughAPI(containerName, pod.Name, d.Namespace, stdin, command...)
}

// getRunningPodForVersion fetches the list of the running pods for the current deployment and returns the first one from the list
func (r *DrupalSiteReconciler) getRunningPodForVersion(ctx context.Context, d *webservicesv1a1.DrupalSite, releaseID string) (corev1.Pod, reconcileError) {
	podList := corev1.PodList{}
	podLabels, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"drupalSite": d.Name, "app": "drupal"},
	})
	if err != nil {
		return corev1.Pod{}, newApplicationError(err, ErrFunctionDomain)
	}
	options := client.ListOptions{
		LabelSelector: podLabels,
		Namespace:     d.Namespace,
	}
	err = r.List(ctx, &podList, &options)
	switch {
	case err != nil:
		return corev1.Pod{}, newApplicationError(err, ErrClientK8s)
	case len(podList.Items) == 0:
		return corev1.Pod{}, newApplicationError(fmt.Errorf("No pod found with given labels: %s", podLabels), ErrTemporary)
	}
	for _, v := range podList.Items {
		if v.Annotations["releaseID"] == releaseID {
			if v.Status.Phase == corev1.PodRunning {
				return v, nil
			} else {
				return v, newApplicationError(err, ErrPodNotRunning)
			}
		}
	}
	// iterate through the list and return the first pod that has the status condition ready
	return corev1.Pod{}, newApplicationError(err, ErrClientK8s)
}

// execToServerPodErrOnStder works like `execToServerPod`, but puts the contents of stderr in the error, if not empty
func (r *DrupalSiteReconciler) execToServerPodErrOnStderr(ctx context.Context, d *webservicesv1a1.DrupalSite, containerName string, stdin io.Reader, command ...string) (stdout string, err error) {
	stdout, stderr, err := r.execToServerPod(ctx, d, containerName, stdin, command...)
	if err != nil || stderr != "" {
		return "", fmt.Errorf("STDERR: %s \n%w", stderr, err)
	}
	return stdout, nil
}

/*
ensureResources ensures the presence of all the resources that the DrupalSite needs to serve content.
This includes BuildConfigs/ImageStreams, DB, PVC, PHP/Nginx deployment + service, site install job, Routes.
*/
func (r *DrupalSiteReconciler) ensureResources(drp *webservicesv1a1.DrupalSite, log logr.Logger) (transientErrs []reconcileError) {
	ctx := context.TODO()

	// 1. BuildConfigs and ImageStreams

	if len(drp.Spec.Configuration.ExtraConfigurationRepo) > 0 {
		if transientErr := r.ensureResourceX(ctx, drp, "is_s2i", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for S2I SiteBuilder ImageStream"))
		}
		if transientErr := r.ensureResourceX(ctx, drp, "bc_s2i", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for S2I SiteBuilder BuildConfig"))
		}
	}
	// 2. Data layer

	if transientErr := r.ensureResourceX(ctx, drp, "pvc_drupal", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for Drupal PVC"))
	}
	if transientErr := r.ensureResourceX(ctx, drp, "dbod_cr", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for DBOD resource"))
	}
	if transientErr := r.ensureResourceX(ctx, drp, "webdav_secret", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for WebDAV Secret"))
	}

	// 3. Serving layer

	if transientErr := r.ensureResourceX(ctx, drp, "cm_php", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for PHP-FPM CM"))
	}
	if transientErr := r.ensureResourceX(ctx, drp, "cm_nginx", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for Nginx CM"))
	}
	if transientErr := r.ensureResourceX(ctx, drp, "cm_settings", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for settings.php CM"))
	}
	if r.isDBODProvisioned(ctx, drp) {
		if transientErr := r.ensureResourceX(ctx, drp, "deploy_drupal", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for Drupal DC"))
		}
	}
	if transientErr := r.ensureResourceX(ctx, drp, "svc_nginx", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for Nginx SVC"))
	}
	if r.isDBODProvisioned(ctx, drp) {
		if drp.Spec.Configuration.CloneFrom == "" {
			if transientErr := r.ensureResourceX(ctx, drp, "site_install_job", log); transientErr != nil {
				transientErrs = append(transientErrs, transientErr.Wrap("%v: for site install Job"))
			}
		} else {
			if transientErr := r.ensureResourceX(ctx, drp, "clone_job", log); transientErr != nil {
				transientErrs = append(transientErrs, transientErr.Wrap("%v: for clone Job"))
			}
		}
	}
	if drp.ConditionTrue("Initialized") {
		if transientErr := r.ensureResourceX(ctx, drp, "cronjob_crontask", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for Drupal Cronjob"))
		}
	}

	// 4. Ingress

	if drp.ConditionTrue("Initialized") {
		// each function below ensures 1 route per entry in `spec.siteUrl[]`. This is understandably part of the job of "ensuring resource X".
		if transientErr := r.ensureResourceX(ctx, drp, "route", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for Route"))
		}
		if transientErr := r.ensureResourceX(ctx, drp, "oidc_return_uri", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: for OidcReturnURI"))
		}

		// each function below removes any unwanted routes
		if transientErr := r.ensureNoExtraRouteResource(ctx, drp, "drupal", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: while ensuring no extra routes"))
		}
		if transientErr := r.ensureNoExtraOidcReturnUriResource(ctx, drp, "drupal", log); transientErr != nil {
			transientErrs = append(transientErrs, transientErr.Wrap("%v: while ensuring no extra OidcReturnURIs"))
		}
	} else {
		for _, url := range drp.Spec.SiteURL {
			if transientErr := r.ensureNoRoute(ctx, drp, string(url), log); transientErr != nil {
				transientErrs = append(transientErrs, transientErr.Wrap("%v: while deleting the Route"))
			}
			if transientErr := r.ensureNoReturnURI(ctx, drp, string(url), log); transientErr != nil {
				transientErrs = append(transientErrs, transientErr.Wrap("%v: while deleting the OidcReturnURI"))
			}
		}
	}

	// 5. Cluster-scoped: Backup schedule, Tekton RBAC

	if transientErr := r.ensureResourceX(ctx, drp, "backup_schedule", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for Velero Schedule"))
	}
	if transientErr := r.ensureResourceX(ctx, drp, "tekton_extra_perm_rbac", log); transientErr != nil {
		transientErrs = append(transientErrs, transientErr.Wrap("%v: for Tekton Extra Permissions ClusterRoleBinding"))
	}

	return transientErrs
}

/*
ensureResourceX ensure the requested resource is created, with the following valid values
	- pvc_drupal: PersistentVolume for the drupalsite
	- site_install_job: Kubernetes Job for the drush site-install
	- clone_job: Kubernetes Job for cloning a drupal site
	- is_base: ImageStream for sitebuilder-base
	- is_s2i: ImageStream for S2I sitebuilder
	- bc_s2i: BuildConfig for S2I sitebuilder
	- deploy_drupal: Deployment for Nginx & PHP-FPM
	- svc_nginx: Service for Nginx
	- cm_php: ConfigMap for PHP-FPM
	- cm_nginx: ConfigMap for Nginx
	- cm_settings: ConfigMap for `settings.php`
	- route: Route for the drupalsite
	- oidc_return_uri: Redirection URI for OIDC
	- dbod_cr: DBOD custom resource to establish database & respective connection for the drupalsite
	- webdav_secret: Secret with credential for WebDAV
	- backup_schedule: Velero Schedule for scheduled backups of the drupalSite
	- tekton_extra_perm_rbac: ClusterRoleBinding for tekton tasks
	- cronjob: Creates cronjob to trigger Cron tasks on Drupalsites, see: https://gitlab.cern.ch/webservices/webframeworks-planning/-/issues/437
*/
func (r *DrupalSiteReconciler) ensureResourceX(ctx context.Context, d *webservicesv1a1.DrupalSite, resType string, log logr.Logger) (transientErr reconcileError) {
	switch resType {
	case "is_s2i":
		is := &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{Name: "sitebuilder-s2i-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, is, func() error {
			return imageStreamForDrupalSiteBuilderS2I(is, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", is.TypeMeta.Kind, "Resource.Namespace", is.Namespace, "Resource.Name", is.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "bc_s2i":
		bc := &buildv1.BuildConfig{ObjectMeta: metav1.ObjectMeta{Name: "sitebuilder-s2i-" + nameVersionHash(d), Namespace: d.Namespace}}
		// We don't really benefit from udating here, because of https://docs.openshift.com/container-platform/4.6/builds/triggering-builds-build-hooks.html#builds-configuration-change-triggers_triggering-builds-build-hooks
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, bc, func() error {
			return buildConfigForDrupalSiteBuilderS2I(bc, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", bc.TypeMeta.Kind, "Resource.Namespace", bc.Namespace, "Resource.Name", bc.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "webdav_secret":
		webdav_secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "webdav-secret-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, webdav_secret, func() error {
			log.Info("Ensuring Resource", "Kind", webdav_secret.TypeMeta.Kind, "Resource.Namespace", webdav_secret.Namespace, "Resource.Name", webdav_secret.Name)
			return secretForWebDAV(webdav_secret, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", webdav_secret.TypeMeta.Kind, "Resource.Namespace", webdav_secret.Namespace, "Resource.Name", webdav_secret.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "deploy_drupal":
		deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
		err := r.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, deploy)

		// Check if a deployment exists & if any of the given conditions satisfy
		// In scenarios where, the deployment is deleted during a failed upgrade, this check is needed to bring it back
		if err == nil && (d.Annotations["updateInProgress"] == "true" || d.ConditionTrue("CodeUpdateFailed") || d.ConditionTrue("DBUpdatesFailed")) {
			return nil
		}
		if databaseSecret := databaseSecretName(d); len(databaseSecret) != 0 {
			deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
			_, err = controllerruntime.CreateOrUpdate(ctx, r.Client, deploy, func() error {
				releaseID := releaseID(d)
				return deploymentForDrupalSite(deploy, databaseSecret, d, releaseID)
			})
			if err != nil {
				log.Error(err, "Failed to ensure Resource", "Kind", deploy.TypeMeta.Kind, "Resource.Namespace", deploy.Namespace, "Resource.Name", deploy.Name)
				return newApplicationError(err, ErrClientK8s)
			}
		}
		return nil
	case "svc_nginx":
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, svc, func() error {
			return serviceForDrupalSite(svc, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", svc.TypeMeta.Kind, "Resource.Namespace", svc.Namespace, "Resource.Name", svc.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "pvc_drupal":
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pv-claim-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, pvc, func() error {
			return persistentVolumeClaimForDrupalSite(pvc, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", pvc.TypeMeta.Kind, "Resource.Namespace", pvc.Namespace, "Resource.Name", pvc.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "route":
		routeRequestList := d.Spec.SiteURL
		for _, req := range routeRequestList {
			hash := md5.Sum([]byte(req))
			route := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: d.Name + "-" + hex.EncodeToString(hash[0:4]), Namespace: d.Namespace}}
			_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, route, func() error {
				return routeForDrupalSite(route, d, string(req))
			})
			if err != nil {
				log.Error(err, "Failed to ensure Resource", "Kind", route.TypeMeta.Kind, "Resource.Namespace", route.Namespace, "Resource.Name", route.Name)
				return newApplicationError(err, ErrClientK8s)
			}
		}
		return nil
	case "oidc_return_uri":
		routeRequestList := d.Spec.SiteURL
		for _, req := range routeRequestList {
			hash := md5.Sum([]byte(req))
			OidcReturnURI := &authz.OidcReturnURI{ObjectMeta: metav1.ObjectMeta{Name: d.Name + "-" + hex.EncodeToString(hash[0:4]), Namespace: d.Namespace}}
			_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, OidcReturnURI, func() error {
				log.Info("Ensuring Resource", "Kind", OidcReturnURI.TypeMeta.Kind, "Resource.Namespace", OidcReturnURI.Namespace, "Resource.Name", OidcReturnURI.Name)
				return newOidcReturnURI(OidcReturnURI, d, string(req))
			})
			if err != nil {
				log.Error(err, "Failed to ensure Resource", "Kind", OidcReturnURI.TypeMeta.Kind, "Resource.Namespace", OidcReturnURI.Namespace, "Resource.Name", OidcReturnURI.Name)
			}
		}
		return nil
	case "site_install_job":
		databaseSecretName := databaseSecretName(d)
		if len(databaseSecretName) == 0 {
			return nil
		}
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "site-install-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, job, func() error {
			return jobForDrupalSiteInstallation(job, databaseSecretName, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", job.TypeMeta.Kind, "Resource.Namespace", job.Namespace, "Resource.Name", job.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "clone_job":
		if databaseSecret := databaseSecretName(d); len(databaseSecret) != 0 {
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "clone-" + d.Name, Namespace: d.Namespace}}
			_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, job, func() error {
				log.Info("Ensuring Resource", "Kind", job.TypeMeta.Kind, "Resource.Namespace", job.Namespace, "Resource.Name", job.Name)
				return jobForDrupalSiteClone(job, databaseSecret, d)
			})
			if err != nil {
				log.Error(err, "Failed to ensure Resource", "Kind", job.TypeMeta.Kind, "Resource.Namespace", job.Namespace, "Resource.Name", job.Name)
				return newApplicationError(err, ErrClientK8s)
			}
		}
		return nil
	case "cm_php":
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "php-fpm-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, cm, func() error {
			return updateConfigMapForPHPFPM(ctx, cm, d, r.Client)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", cm.TypeMeta.Kind, "Resource.Namespace", cm.Namespace, "Resource.Name", cm.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "cm_nginx":
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "nginx-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, cm, func() error {
			return updateConfigMapForNginx(ctx, cm, d, r.Client)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", cm.TypeMeta.Kind, "Resource.Namespace", cm.Namespace, "Resource.Name", cm.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "cm_settings":
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "site-settings-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, cm, func() error {
			return updateConfigMapForSiteSettings(ctx, cm, d, r.Client)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", cm.TypeMeta.Kind, "Resource.Namespace", cm.Namespace, "Resource.Name", cm.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "dbod_cr":
		dbod := &dbodv1a1.Database{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, dbod, func() error {
			return dbodForDrupalSite(dbod, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", dbod.TypeMeta.Kind, "Resource.Namespace", dbod.Namespace, "Resource.Name", dbod.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "backup_schedule":
		schedule := &velerov1.Schedule{ObjectMeta: metav1.ObjectMeta{Name: d.Namespace + "-" + d.Name, Namespace: VeleroNamespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, schedule, func() error {
			return scheduledBackupsForDrupalSite(schedule, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", schedule.TypeMeta.Kind, "Resource.Namespace", schedule.Namespace, "Resource.Name", schedule.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	case "tekton_extra_perm_rbac":
		// We only need one ClusterRoleBinding for a given project. Therefore the naming. It gets created by any of the sites in
		// the project if it doesn't exist. We don't delete it specifically as well, it can be handled with project deletion
		rbac := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "tektoncd-extra-permissions-" + d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, rbac, func() error {
			return clusterRoleBindingForTektonExtraPermission(rbac, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", rbac.TypeMeta.Kind, "Resource.Name", rbac.Name)
		}
		return nil
	case "cronjob_crontask":
		databaseSecret := databaseSecretName(d)
		if len(databaseSecret) == 0 {
			return nil
		}
		// This ensures we have cron function for the website, see: https://gitlab.cern.ch/webservices/webframeworks-planning/-/issues/437
		cron := &batchbeta1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cronjob-" + d.Name, Namespace: d.Namespace}}
		_, err := controllerruntime.CreateOrUpdate(ctx, r.Client, cron, func() error {
			log.Info("Ensuring Resource", "Kind", cron.TypeMeta.Kind, "Resource.Namespace", cron.Namespace, "Resource.Name", cron.Name)
			return cronjobForDrupalSite(cron, databaseSecret, d)
		})
		if err != nil {
			log.Error(err, "Failed to ensure Resource", "Kind", cron.TypeMeta.Kind, "Resource.Namespace", cron.Namespace, "Resource.Name", cron.Name)
			return newApplicationError(err, ErrClientK8s)
		}
		return nil
	default:
		return newApplicationError(nil, ErrFunctionDomain)
	}
}

func cronjobForDrupalSite(currentobject *batchbeta1.CronJob, databaseSecret string, drupalsite *webservicesv1a1.DrupalSite) error {
	var jobsHistoryLimit int32 = 1
	var jobBackoffLimit int32 = 1

	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	ls := labelsForDrupalSite(drupalsite.Name)
	ls["app"] = "cronjob"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(drupalsite))
		currentobject.Spec = batchbeta1.CronJobSpec{
			// Every 30min, this is based on https://en.wikipedia.org/wiki/Cron
			Schedule: "*/30 * * * *",
			// The default is 3, last job should suffice
			SuccessfulJobsHistoryLimit: &jobsHistoryLimit,
			ConcurrencyPolicy:          batchbeta1.AllowConcurrent,
			JobTemplate: batchbeta1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: &jobBackoffLimit,
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							RestartPolicy: "Never",
							Containers: []corev1.Container{
								{
									Name:            "cronjob",
									Image:           sitebuilderImageRefToUse(drupalsite, releaseID(drupalsite)).Name,
									ImagePullPolicy: "IfNotPresent",
									Command: []string{
										"sh",
										"-c",
										"/operations/run-cron.sh -s " + drupalsite.Name,
									},
									EnvFrom: []corev1.EnvFromSource{
										{
											SecretRef: &corev1.SecretEnvSource{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: databaseSecret,
												},
											},
										},
										{
											SecretRef: &corev1.SecretEnvSource{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: oidcSecretName, //This is always set the same way
												},
											},
										},
									},
									VolumeMounts: []corev1.VolumeMount{{
										Name:      "drupal-directory-" + drupalsite.Name,
										MountPath: "/drupal-data",
									}},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "drupal-directory-" + drupalsite.Name,
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: "pv-claim-" + drupalsite.Name,
										},
									},
								},
							},
						},
					},
				},
			}}
	}
	return nil
}

// ensureNoExtraRouteResource uses the current SiteURL resource as reference and deletes any extra route
func (r *DrupalSiteReconciler) ensureNoExtraRouteResource(ctx context.Context, d *webservicesv1a1.DrupalSite, label string, log logr.Logger) (transientErr reconcileError) {
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	ls["route"] = label
	existingRoutes := &routev1.RouteList{}
	routeLabels, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: ls,
	})
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	options := client.ListOptions{
		Namespace:     d.Namespace,
		LabelSelector: routeLabels,
	}
	err = r.Client.List(context.TODO(), existingRoutes, &options)
	if err != nil {
		log.Error(err, "Couldn't query routes with the given labels")
		return newApplicationError(err, ErrClientK8s)
	}
	routeRequestList := d.Spec.SiteURL
	routesToRemove := []webservicesv1a1.Url{}
	for _, route := range existingRoutes.Items {
		flag := false
		for _, req := range routeRequestList {
			if label == "webdav" {
				req = "webdav-" + req
			}
			if string(req) == route.Spec.Host {
				flag = true
				continue
			}
		}
		if !flag {
			routesToRemove = append(routesToRemove, webservicesv1a1.Url(route.Spec.Host))
		}
	}
	for _, route := range routesToRemove {
		if transientErr := r.ensureNoRoute(ctx, d, string(route), log); transientErr != nil {
			return transientErr
		}
	}
	return nil
}

// ensureNoExtraOidcReturnUriResource uses the current SiteURL resource as reference and deletes any extra oidcReturnURI
func (r *DrupalSiteReconciler) ensureNoExtraOidcReturnUriResource(ctx context.Context, d *webservicesv1a1.DrupalSite, label string, log logr.Logger) (transientErr reconcileError) {
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	ls["oidcReturnURI"] = label
	existingOidcReturnUris := &authz.OidcReturnURIList{}
	oidcReturnUriLabels, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: ls,
	})
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	options := client.ListOptions{
		Namespace:     d.Namespace,
		LabelSelector: oidcReturnUriLabels,
	}
	err = r.Client.List(context.TODO(), existingOidcReturnUris, &options)
	if err != nil {
		log.Error(err, "Couldn't query oidcReturnUris with the given labels")
		return newApplicationError(err, ErrClientK8s)
	}
	oidcReturnUriRequestList := d.Spec.SiteURL
	oidcReturnUrisToRemove := []string{}
	for _, route := range existingOidcReturnUris.Items {
		flag := false
		for _, req := range oidcReturnUriRequestList {
			url, err := url.Parse(route.Spec.RedirectURI)
			if err != nil {
				return newApplicationError(err, ErrFunctionDomain)
			}
			if string(req) == url.Host {
				flag = true
				continue
			}
		}
		if !flag {
			url, err := url.Parse(route.Spec.RedirectURI)
			if err != nil {
				return newApplicationError(err, ErrFunctionDomain)
			}
			oidcReturnUrisToRemove = append(oidcReturnUrisToRemove, url.Host)
		}
	}
	for _, oidcReturnURI := range oidcReturnUrisToRemove {
		if transientErr := r.ensureNoReturnURI(ctx, d, oidcReturnURI, log); transientErr != nil {
			return transientErr
		}
	}
	return nil
}

func (r *DrupalSiteReconciler) ensureNoRoute(ctx context.Context, d *webservicesv1a1.DrupalSite, Url string, log logr.Logger) (transientErr reconcileError) {
	hash := md5.Sum([]byte(Url))
	route := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: d.Name + "-" + hex.EncodeToString(hash[0:4]), Namespace: d.Namespace}}
	if err := r.Get(ctx, types.NamespacedName{Name: route.Name, Namespace: route.Namespace}, route); err != nil {
		switch {
		case k8sapierrors.IsNotFound(err):
			return nil
		default:
			return newApplicationError(err, ErrClientK8s)
		}
	}
	if err := r.Delete(ctx, route); err != nil {
		return newApplicationError(err, ErrClientK8s)
	}
	return nil
}

func (r *DrupalSiteReconciler) ensureNoReturnURI(ctx context.Context, d *webservicesv1a1.DrupalSite, Url string, log logr.Logger) (transientErr reconcileError) {
	hash := md5.Sum([]byte(Url))
	oidc_return_uri := &authz.OidcReturnURI{}
	if err := r.Get(ctx, types.NamespacedName{Name: d.Name + "-" + hex.EncodeToString(hash[0:4]), Namespace: d.Namespace}, oidc_return_uri); err != nil {
		switch {
		case k8sapierrors.IsNotFound(err):
			return nil
		default:
			return newApplicationError(err, ErrClientK8s)
		}
	}
	if err := r.Delete(ctx, oidc_return_uri); err != nil {
		return newApplicationError(err, ErrClientK8s)
	}
	return nil
}

func (r *DrupalSiteReconciler) ensureNoSchedule(ctx context.Context, d *webservicesv1a1.DrupalSite, log logr.Logger) (transientErr reconcileError) {
	schedule := &velerov1.Schedule{}
	if err := r.Get(ctx, types.NamespacedName{Name: d.Namespace + "-" + d.Name, Namespace: VeleroNamespace}, schedule); err != nil {
		switch {
		case k8sapierrors.IsNotFound(err):
			return nil
		default:
			return newApplicationError(err, ErrClientK8s)
		}
	}
	if err := r.Delete(ctx, schedule); err != nil {
		return newApplicationError(err, ErrClientK8s)
	}
	return nil
}

func (r *DrupalSiteReconciler) checkNewBackups(ctx context.Context, d *webservicesv1a1.DrupalSite, log logr.Logger) (backups []velerov1.Backup, reconcileErr reconcileError) {
	backupList := velerov1.BackupList{}
	backups = make([]velerov1.Backup, 0)
	hash := md5.Sum([]byte(d.Namespace))
	backupLabels, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"drupal.webservices.cern.ch/projectHash": hex.EncodeToString(hash[:])},
	})
	if err != nil {
		reconcileErr = newApplicationError(err, ErrFunctionDomain)
		return
	}
	options := client.ListOptions{
		LabelSelector: backupLabels,
		Namespace:     VeleroNamespace,
	}
	err = r.List(ctx, &backupList, &options)
	switch {
	case err != nil:
		reconcileErr = newApplicationError(err, ErrClientK8s)
	case len(backupList.Items) == 0:
		log.V(5).Info("No backup found with given labels " + backupLabels.String())
	default:
		for i := range backupList.Items {
			if backupList.Items[i].Status.Phase == velerov1.BackupPhaseCompleted {
				backups = append(backups, backupList.Items[i])
			}
		}
	}
	return
}

// labelsForDrupalSite returns the labels for selecting the resources
// belonging to the given drupalSite CR name.
func labelsForDrupalSite(name string) map[string]string {
	return map[string]string{"drupalSite": name}
}

// releaseID is the image tag to use depending on the version and releaseSpec
func releaseID(d *webservicesv1a1.DrupalSite) string {
	return d.Spec.Version.Name + "-" + d.Spec.Version.ReleaseSpec
}

// sitebuilderImageRefToUse returns which base image to use, depending on whether the field `ExtraConfigurationRepo` is set.
// If yes, the S2I buildconfig will be used; sitebuilderImageRefToUse returns the output of imageStreamForDrupalSiteBuilderS2I().
// Otherwise, returns the sitebuilder base
func sitebuilderImageRefToUse(d *webservicesv1a1.DrupalSite, releaseID string) corev1.ObjectReference {
	if len(d.Spec.Configuration.ExtraConfigurationRepo) > 0 {
		return corev1.ObjectReference{
			Kind: "ImageStreamTag",
			Name: "sitebuilder-s2i-" + d.Name + ":" + releaseID,
		}
	}
	return corev1.ObjectReference{
		Kind: "DockerImage",
		Name: SiteBuilderImage + ":" + releaseID,
	}
}

// imageStreamForDrupalSiteBuilderS2I returns a ImageStream object for Drupal SiteBuilder S2I
func imageStreamForDrupalSiteBuilderS2I(currentobject *imagev1.ImageStream, d *webservicesv1a1.DrupalSite) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Spec.LookupPolicy.Local = true
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "sitebuilder"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	return nil
}

// buildConfigForDrupalSiteBuilderS2I returns a BuildConfig object for Drupal SiteBuilder S2I
func buildConfigForDrupalSiteBuilderS2I(currentobject *buildv1.BuildConfig, d *webservicesv1a1.DrupalSite) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Spec = buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				Resources:                 BuildResources,
				CompletionDeadlineSeconds: pointer.Int64Ptr(1200),
				Source: buildv1.BuildSource{
					Git: &buildv1.GitBuildSource{
						// TODO: support branches https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/28
						Ref: "master",
						URI: d.Spec.Configuration.ExtraConfigurationRepo,
					},
				},
				Strategy: buildv1.BuildStrategy{
					SourceStrategy: &buildv1.SourceBuildStrategy{
						From: corev1.ObjectReference{
							Kind: "DockerImage",
							Name: SiteBuilderImage + ":" + releaseID(d),
						},
					},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{
						Kind: "ImageStreamTag",
						Name: "sitebuilder-s2i-" + d.Name + ":" + releaseID(d),
					},
				},
			},
			Triggers: []buildv1.BuildTriggerPolicy{
				{
					Type: buildv1.ConfigChangeBuildTriggerType,
				},
			},
		}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "sitebuilder"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	return nil
}

// dbodForDrupalSite returns a DBOD resource for the the Drupal Site
func dbodForDrupalSite(currentobject *dbodv1a1.Database, d *webservicesv1a1.DrupalSite) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		dbID := md5.Sum([]byte(d.Namespace + "-" + d.Name))
		currentobject.Spec = dbodv1a1.DatabaseSpec{
			DatabaseClass: string(d.Spec.Configuration.DatabaseClass),
			DbName:        hex.EncodeToString(dbID[1:10]),
			DbUser:        hex.EncodeToString(dbID[1:10]),
			ExtraLabels: map[string]string{
				"drupalSite": d.Name,
			},
		}
	}
	// Enforce only the drupalsite labels on the resource on every iteration
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "dbod"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	return nil
}

// deploymentForDrupalSite defines the server runtime deployment of a DrupalSite
func deploymentForDrupalSite(currentobject *appsv1.Deployment, databaseSecret string, d *webservicesv1a1.DrupalSite, releaseID string) error {
	nginxResources, err := ResourceRequestLimit("10Mi", "20m", "20Mi", "500m")
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	phpfpmexporterResources, err := ResourceRequestLimit("10Mi", "1m", "15Mi", "5m")
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	phpfpmResources, err := ResourceRequestLimit("100Mi", "60m", "270Mi", "1800m")
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	//TODO: Check best resource consumption
	webDAVResources, err := ResourceRequestLimit("10Mi", "5m", "100Mi", "100m")
	if err != nil {
		return newApplicationError(err, ErrFunctionDomain)
	}
	ls := labelsForDrupalSite(d.Name)
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls["app"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Annotations = map[string]string{}
		currentobject.Annotations["alpha.image.policy.openshift.io/resolve-names"] = "*"
		currentobject.Spec.Template.ObjectMeta.Annotations = map[string]string{}
		currentobject.Spec.Template.Spec.Containers = []corev1.Container{{Name: "nginx"}, {Name: "php-fpm"}, {Name: "php-fpm-exporter"}, {Name: "webdav"}}

		// This annotation is required to trigger new rollout, when the imagestream gets updated with a new image for the given tag. Without this, deployments might start running with
		// a wrong image built from a different build, that is left out on the node
		// NOTE: Removing this annotation temporarily, as it is causing indefinite rollouts with some sites
		// ref: https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/54
		// currentobject.Annotations["image.openshift.io/triggers"] = "[{\"from\":{\"kind\":\"ImageStreamTag\",\"name\":\"nginx-" + d.Name + ":" + releaseID + "\",\"namespace\":\"" + d.Namespace + "\"},\"fieldPath\":\"spec.template.spec.containers[?(@.name==\\\"nginx\\\")].image\",\"pause\":\"false\"}]"

		currentobject.Spec.Replicas = pointer.Int32Ptr(1)
		currentobject.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: ls,
		}
		currentobject.Spec.Template.ObjectMeta.Labels = ls

		if _, bool := d.Annotations["nodeSelectorLabel"]; bool {
			if _, bool = d.Annotations["nodeSelectorValue"]; bool {
				currentobject.Spec.Template.Spec.NodeSelector = map[string]string{
					d.Annotations["nodeSelectorLabel"]: d.Annotations["nodeSelectorValue"],
				}
			}
		}

		currentobject.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "drupal-directory-" + d.Name,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pv-claim-" + d.Name,
					},
				}},
			{
				Name: "php-config-volume",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "php-fpm-" + d.Name,
						},
					},
				},
			},
			{
				Name: "nginx-config-volume",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "nginx-" + d.Name,
						},
					},
				},
			},
			{
				Name: "site-settings-php",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "site-settings-" + d.Name,
						},
					},
				},
			},
			{
				Name:         "empty-dir",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			},
			{
				Name: "webdav-volume",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "webdav-secret-" + d.Name,
						Items: []corev1.KeyToPath{
							// Unecessary but garantees no other secrets are mounted
							{
								Key:  "htdigest",
								Path: "htdigest",
							},
						},
					},
				},
			},
		}

		currentobject.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Name:            "nginx-init",
			ImagePullPolicy: "Always",
			Command:         []string{"/bin/sh", "-c", "cp -r /app /var/run/"},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      "empty-dir",
				MountPath: "/var/run/",
			}},
		}}

		for i, container := range currentobject.Spec.Template.Spec.Containers {
			switch container.Name {
			case "nginx":
				// Set to always due to https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/54
				currentobject.Spec.Template.Spec.Containers[i].ImagePullPolicy = "Always"
				currentobject.Spec.Template.Spec.Containers[i].Ports = []corev1.ContainerPort{{
					ContainerPort: 8080,
					Name:          "nginx",
					Protocol:      "TCP",
				}}
				currentobject.Spec.Template.Spec.Containers[i].Env = []corev1.EnvVar{
					{
						Name:  "DRUPAL_SHARED_VOLUME",
						Value: "/drupal-data",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "drupal-directory-" + d.Name,
						MountPath: "/drupal-data",
					},
					{
						Name:      "nginx-config-volume",
						MountPath: "/etc/nginx/custom.conf",
						SubPath:   "custom.conf",
						ReadOnly:  true,
					},
					{
						Name:      "empty-dir",
						MountPath: "/var/run/",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].Resources = nginxResources
				currentobject.Spec.Template.Spec.Containers[i].ReadinessProbe = &v1.Probe{
					Handler: v1.Handler{
						HTTPGet: &v1.HTTPGetAction{
							Path: "/user/login",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 40,
					TimeoutSeconds:      15,
				}
				currentobject.Spec.Template.Spec.Containers[i].LivenessProbe = &v1.Probe{
					Handler: v1.Handler{
						HTTPGet: &v1.HTTPGetAction{
							Path: "/user/login",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 300,
					TimeoutSeconds:      200,
				}

			case "php-fpm":
				currentobject.Spec.Template.Spec.Containers[i].Command = []string{"php-fpm"}
				// Set to always due to https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/54
				currentobject.Spec.Template.Spec.Containers[i].ImagePullPolicy = "Always"
				currentobject.Spec.Template.Spec.Containers[i].Ports = []corev1.ContainerPort{{
					ContainerPort: 9000,
					Name:          "php-fpm",
					Protocol:      "TCP",
				}}
				currentobject.Spec.Template.Spec.Containers[i].Env = []corev1.EnvVar{
					{
						Name:  "DRUPAL_SHARED_VOLUME",
						Value: "/drupal-data",
					},
					{
						Name:  "SMTPHOST",
						Value: SMTPHost,
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].EnvFrom = []corev1.EnvFromSource{
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: databaseSecret,
							},
						},
					},
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: oidcSecretName, //This is always set the same way
							},
						},
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "drupal-directory-" + d.Name,
						MountPath: "/drupal-data",
					},
					{
						Name:      "php-config-volume",
						MountPath: "/usr/local/etc/php-fpm.d/zz-docker.conf",
						SubPath:   "zz-docker.conf",
						ReadOnly:  true,
					},
					{
						Name:      "empty-dir",
						MountPath: "/var/run/",
					},
					{
						Name:      "site-settings-php",
						MountPath: "/app/web/sites/default/settings.php",
						SubPath:   "settings.php",
						ReadOnly:  true,
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].Resources = phpfpmResources

			case "php-fpm-exporter":
				// Set to always due to https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/54
				currentobject.Spec.Template.Spec.Containers[i].ImagePullPolicy = "Always"
				// Port on which to expose metrics
				currentobject.Spec.Template.Spec.Containers[i].Ports = []corev1.ContainerPort{{
					ContainerPort: 9253,
					Name:          "php-fpm-metrics",
					Protocol:      "TCP",
				}}
				currentobject.Spec.Template.Spec.Containers[i].Env = []corev1.EnvVar{
					{
						Name:  "PHP_FPM_SCRAPE_URI",
						Value: "unix:///var/run/drupal.sock;/_site/_php-fpm-status",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "empty-dir",
						MountPath: "/var/run/",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].Resources = phpfpmexporterResources

			case "webdav":
				currentobject.Spec.Template.Spec.Containers[i].Command = []string{"php-fpm"}
				// Set to always due to https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/54
				currentobject.Spec.Template.Spec.Containers[i].ImagePullPolicy = "Always"
				currentobject.Spec.Template.Spec.Containers[i].Ports = []corev1.ContainerPort{{
					ContainerPort: 8008,
					Name:          "webdav",
					Protocol:      "TCP",
				}}
				//TODO: mount password as file
				currentobject.Spec.Template.Spec.Containers[i].Env = []corev1.EnvVar{
					{
						Name:  "DRUPAL_SHARED_VOLUME",
						Value: "/drupal-data",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "drupal-directory-" + d.Name,
						MountPath: "/drupal-data",
					},
					{
						Name:      "webdav-volume",
						MountPath: "/webdav/htdigest",
						ReadOnly:  true,
					},
					{
						Name:      "empty-dir",
						MountPath: "/var/run/",
					},
				}
				currentobject.Spec.Template.Spec.Containers[i].Resources = webDAVResources
			}
		}

	}

	_, annotExists := currentobject.Spec.Template.ObjectMeta.Annotations["releaseID"]
	if !annotExists || d.Status.ReleaseID.Failsafe == "" || currentobject.Spec.Template.ObjectMeta.Annotations["releaseID"] != releaseID {
		for i, container := range currentobject.Spec.Template.Spec.Containers {
			switch container.Name {
			case "nginx":
				currentobject.Spec.Template.Spec.Containers[i].Image = NginxImage + ":" + releaseID
			case "php-fpm":
				currentobject.Spec.Template.Spec.Containers[i].Image = sitebuilderImageRefToUse(d, releaseID).Name
				currentobject.Spec.Template.Spec.InitContainers[0].Image = sitebuilderImageRefToUse(d, releaseID).Name
			case "php-fpm-exporter":
				currentobject.Spec.Template.Spec.Containers[i].Image = PhpFpmExporterImage
			case "webdav":
				currentobject.Spec.Template.Spec.Containers[i].Image = WebDAVImage
			}
		}
	}
	// Add an annotation to be able to verify what releaseID of pod is running. Did not use labels, as it will affect the labelselector for the deployment and might cause downtime
	currentobject.Spec.Template.ObjectMeta.Annotations["releaseID"] = releaseID
	currentobject.Spec.Template.ObjectMeta.Annotations["pre.hook.backup.velero.io/container"] = "php-fpm"
	currentobject.Spec.Template.ObjectMeta.Annotations["pre.hook.backup.velero.io/command"] = "[\"sh\",\"-c\", \"/operations/database-backup.sh -f database_backup.sql\"]"
	// Since we have varying sizes of databases, the timeout needs to be large enough. Else the backups will fail.
	// Ref: https://gitlab.cern.ch/drupal/paas/drupalsite-operator/-/issues/71
	currentobject.Spec.Template.ObjectMeta.Annotations["pre.hook.backup.velero.io/timeout"] = "90m"
	currentobject.Spec.Template.ObjectMeta.Annotations["backup.velero.io/backup-volumes"] = "drupal-directory-" + d.Name

	currentobject.Spec.Replicas = d.Status.ExpectedDeploymentReplicas

	return nil
}

// secretForWebDAV returns a Secret object
func secretForWebDAV(currentobject *corev1.Secret, d *webservicesv1a1.DrupalSite) error {
	addOwnerRefToObject(currentobject, asOwner(d))
	currentobject.Type = "kubernetes.io/opaque"
	encryptedOpaquePassword := encryptBasicAuthPassword(d.Spec.Configuration.WebDAVPassword)
	currentobject.StringData = map[string]string{
		"htdigest": encryptedOpaquePassword,
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	return nil
}

// persistentVolumeClaimForDrupalSite returns a PVC object
func persistentVolumeClaimForDrupalSite(currentobject *corev1.PersistentVolumeClaim, d *webservicesv1a1.DrupalSite) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Spec = corev1.PersistentVolumeClaimSpec{
			// Selector: &metav1.LabelSelector{
			// 	MatchLabels: ls,
			// },
			StorageClassName: pointer.StringPtr("cephfs-no-backup"),
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteMany"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(d.Spec.Configuration.DiskSize),
				},
			},
		}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}
	return nil
}

// serviceForDrupalSite returns a service object
func serviceForDrupalSite(currentobject *corev1.Service, d *webservicesv1a1.DrupalSite) error {
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Spec.Selector = ls
		currentobject.Spec.Ports = []corev1.ServicePort{
			{
				TargetPort: intstr.FromInt(8080),
				Name:       "nginx",
				Port:       80,
				Protocol:   "TCP",
			},
			{
				TargetPort: intstr.FromInt(9253),
				Name:       "php-fpm-exporter",
				Port:       9253,
				Protocol:   "TCP",
			}}
	}
	return nil
}

// routeForDrupalSite returns a route object
func routeForDrupalSite(currentobject *routev1.Route, d *webservicesv1a1.DrupalSite, Url string) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Spec = routev1.RouteSpec{
			TLS: &routev1.TLSConfig{
				InsecureEdgeTerminationPolicy: "Redirect",
				Termination:                   "edge",
			},
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   d.Name,
				Weight: pointer.Int32Ptr(100),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(8080),
			},
		}
	}

	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	// Adding a new label to be able to filter and remove extra resources when there are changes in Spec.SiteURL
	ls["route"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if _, exists := d.Annotations["haproxy.router.openshift.io/ip_whitelist"]; exists {
		currentobject.Annotations["haproxy.router.openshift.io/ip_whitelist"] = d.Annotations["haproxy.router.openshift.io/ip_whitelist"]
	}
	// Route host is placed outside of currentobject.CreationTimestamp.IsZero to ensure it is updated, when the respective field in the DrupalSite CR is modified
	currentobject.Spec.Host = Url
	return nil
}

// newOidcReturnURI returns a oidcReturnURI object
func newOidcReturnURI(currentobject *authz.OidcReturnURI, d *webservicesv1a1.DrupalSite, Url string) error {
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
	}
	url, err := url.Parse(Url)
	if err != nil {
		return err
	}

	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "drupal"
	// Adding a new label to be able to filter and remove extra resources when there are changes in Spec.SiteURL
	ls["oidcReturnURI"] = "drupal"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	// This will append `/openid-connect/*` to the URL, guaranteeing all subpaths of the link can be redirected
	url.Path = path.Join(url.Path, "openid-connect")
	returnURI := "http://" + url.String() + "/*" // Hardcoded since with path.Join method creates `%2A` which will not work in the AuthzAPI, and the prefix `http`
	currentobject.Spec = authz.OidcReturnURISpec{
		RedirectURI: returnURI,
	}
	return nil
}

// jobForDrupalSiteInstallation returns a job object thats runs drush
func jobForDrupalSiteInstallation(currentobject *batchv1.Job, databaseSecret string, d *webservicesv1a1.DrupalSite) error {
	ls := labelsForDrupalSite(d.Name)
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Labels = map[string]string{}
		currentobject.Spec.Template.ObjectMeta = metav1.ObjectMeta{
			Labels: ls,
		}
		currentobject.Spec.BackoffLimit = pointer.Int32Ptr(3)
		// Increasing the limit temporarily to fix https://gitlab.cern.ch/webservices/webframeworks-planning/-/issues/479
		currentobject.Spec.Template.Spec = corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Image:           "bash",
				Name:            "pvc-init",
				ImagePullPolicy: "IfNotPresent",
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
				Image:           sitebuilderImageRefToUse(d, releaseID(d)).Name,
				Name:            "drush",
				ImagePullPolicy: "Always",
				Command:         siteInstallJobForDrupalSite(),
				Env: []corev1.EnvVar{
					{
						Name:  "DRUPAL_SHARED_VOLUME",
						Value: "/drupal-data",
					},
					{
						Name:  "SMTPHOST",
						Value: SMTPHost,
					},
				},
				EnvFrom: []corev1.EnvFromSource{
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: databaseSecret,
							},
						},
					},
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: oidcSecretName, //This is always set the same way
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
						ClaimName: "pv-claim-" + d.Name,
					},
				},
			}},
		}
		ls["app"] = "drush"
		for k, v := range ls {
			currentobject.Labels[k] = v
		}
	}
	return nil
}

// jobForDrupalSiteClone returns a job object thats clones a drupalsite
func jobForDrupalSiteClone(currentobject *batchv1.Job, databaseSecret string, d *webservicesv1a1.DrupalSite) error {
	ls := labelsForDrupalSite(d.Name)
	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))
		currentobject.Labels = map[string]string{}
		currentobject.Spec.Template.ObjectMeta = metav1.ObjectMeta{
			Labels: ls,
		}
		currentobject.Spec.Template.Spec = corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Image:           sitebuilderImageRefToUse(d, releaseID(d)).Name,
					Name:            "db-backup",
					ImagePullPolicy: "Always",
					Command:         takeBackup("dbBackUp.sql"),
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
									Name: "dbcredentials-" + string(d.Spec.Configuration.CloneFrom),
								},
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "drupal-directory-" + string(d.Spec.Configuration.CloneFrom),
						MountPath: "/drupal-data",
					}},
				},
			},
			RestartPolicy: "Never",
			Containers: []corev1.Container{{
				Image:           sitebuilderImageRefToUse(d, releaseID(d)).Name,
				Name:            "clone",
				ImagePullPolicy: "Always",
				Command:         cloneSource("dbBackUp.sql"),
				Env: []corev1.EnvVar{
					{
						Name:  "DRUPAL_SHARED_VOLUME",
						Value: "/drupal-data-source",
					},
				},
				EnvFrom: []corev1.EnvFromSource{
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: databaseSecret,
							},
						},
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "drupal-directory-" + string(d.Spec.Configuration.CloneFrom),
						MountPath: "/drupal-data-source",
					},
					{
						Name:      "drupal-directory-" + d.Name,
						MountPath: "/drupal-data",
					}},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "drupal-directory-" + d.Name,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "pv-claim-" + d.Name,
						},
					},
				},
				{
					Name: "drupal-directory-" + string(d.Spec.Configuration.CloneFrom),
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "pv-claim-" + string(d.Spec.Configuration.CloneFrom),
						},
					},
				}},
		}
		ls["app"] = "clone"
		for k, v := range ls {
			currentobject.Labels[k] = v
		}
	}
	return nil
}

// scheduledBackupsForDrupalSite returns a velero Schedule object that creates scheduled backups
func scheduledBackupsForDrupalSite(currentobject *velerov1.Schedule, d *webservicesv1a1.DrupalSite) error {
	// Do not add owner references here. As this object is created in a different namespace. Instead the deletion
	// of this object is handled manually in the 'cleanupDrupalSite' function
	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}

	hash := md5.Sum([]byte(d.Namespace))
	currentobject.Labels["drupal.webservices.cern.ch/projectHash"] = hex.EncodeToString(hash[:])
	// These labels need to be removed, as annotations support longer values.
	// But this can be done only after upgrading velero to 1.5 or higher which supports propagating annotations
	// from schedules to the backups.
	// ref: https://gitlab.cern.ch/webservices/webframeworks-planning/-/issues/457
	currentobject.Labels["drupal.webservices.cern.ch/project"] = d.Namespace
	currentobject.Labels["drupal.webservices.cern.ch/drupalSite"] = d.Name

	currentobject.Annotations["drupal.webservices.cern.ch/project"] = d.Namespace
	currentobject.Annotations["drupal.webservices.cern.ch/drupalSite"] = d.Name

	currentobject.Spec = velerov1.ScheduleSpec{
		// Schedule backup at 3AM every day
		Schedule: "0 3 * * *",
		Template: velerov1.BackupSpec{
			IncludedNamespaces: []string{d.Namespace},
			IncludedResources:  []string{"pods"},
			// Add label selector to pick up the right pod and the respective PVC
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":        "drupal",
					"drupalSite": d.Name,
				},
			},
			// TTL is 7 days. The backups are deleted automatically after this duration
			TTL: metav1.Duration{
				Duration: 168 * time.Hour,
			},
		},
		UseOwnerReferencesInBackup: pointer.BoolPtr(true),
	}
	return nil
}

// clusterRoleBindingForTektonExtraPermission returns a ClusterRoleBinding object thats binds the tektoncd service account
// with the tektoncd-extra-permissions ClusterRole. This binding grants permissions to create jobs (and only that)
func clusterRoleBindingForTektonExtraPermission(currentobject *rbacv1.ClusterRoleBinding, d *webservicesv1a1.DrupalSite) error {
	currentobject.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     "tektoncd-extra-permissions",
	}

	currentobject.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "tektoncd",
			Namespace: d.Namespace,
		},
	}
	return nil
}

// updateConfigMapForPHPFPM modifies the configmap to include the php-fpm settings file.
// If the file contents change, it rolls out a new deployment.
func updateConfigMapForPHPFPM(ctx context.Context, currentobject *corev1.ConfigMap, d *webservicesv1a1.DrupalSite, c client.Client) error {
	configPath := "/tmp/runtime-config/qos-" + string(d.Spec.Configuration.QoSClass) + "/php-fpm.conf"
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return newApplicationError(fmt.Errorf("reading PHP-FPM configMap failed: %w", err), ErrFilesystemIO)
	}

	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))

		// Upstream PHP docker images use zz-docker.conf for configuration and this file gets loaded last (because of 'zz*') and overrides the default configuration loaded from www.conf
		currentobject.Data = map[string]string{
			"zz-docker.conf": string(content),
		}
	}

	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "php"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if !currentobject.CreationTimestamp.IsZero() {
		// Roll out a new deployment
		deploy := &appsv1.Deployment{}
		err = c.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, deploy)
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the PHP-FPM configMap (deployment not found): %w", err), ErrClientK8s)
		}
		updateDeploymentAnnotations := func(deploy *appsv1.Deployment, d *webservicesv1a1.DrupalSite) error {
			hash := md5.Sum([]byte(currentobject.Data["zz-docker.conf"]))
			currentHash, flag := deploy.Spec.Template.ObjectMeta.Annotations["phpfpm-configmap/hash"]
			// NOTE: the following check is unnecessary, we can always perform the action
			if flag == false || hex.EncodeToString(hash[:]) != currentHash {
				deploy.Spec.Template.ObjectMeta.Annotations["phpfpm-configmap/hash"] = hex.EncodeToString(hash[:])
			}
			return nil
		}
		_, err := controllerruntime.CreateOrUpdate(ctx, c, deploy, func() error {
			return updateDeploymentAnnotations(deploy, d)
		})
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the PHP-FPM configMap: %w", err), ErrClientK8s)
		}
	}
	return nil
}

// updateConfigMapForNginx modifies the configmap to include the Nginx settings file.
// If the file contents change, it rolls out a new deployment.
func updateConfigMapForNginx(ctx context.Context, currentobject *corev1.ConfigMap, d *webservicesv1a1.DrupalSite, c client.Client) error {
	configPath := "/tmp/runtime-config/qos-" + string(d.Spec.Configuration.QoSClass) + "/nginx.conf"
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return newApplicationError(fmt.Errorf("reading Nginx configuration failed: %w", err), ErrFilesystemIO)
	}

	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))

		currentobject.Data = map[string]string{
			"custom.conf": string(content),
		}
	}

	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "nginx"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if !currentobject.CreationTimestamp.IsZero() {
		// Roll out a new deployment
		deploy := &appsv1.Deployment{}
		err = c.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, deploy)
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the Nginx configMap (deployment not found): %w", err), ErrClientK8s)
		}
		updateDeploymentAnnotations := func(deploy *appsv1.Deployment, d *webservicesv1a1.DrupalSite) error {
			hash := md5.Sum([]byte(currentobject.Data["custom.conf"]))
			currentHash, flag := deploy.Spec.Template.ObjectMeta.Annotations["nginx-configmap/hash"]
			if flag == false || hex.EncodeToString(hash[:]) != currentHash {
				deploy.Spec.Template.ObjectMeta.Annotations["nginx-configmap/hash"] = hex.EncodeToString(hash[:])
			}
			return nil
		}
		_, err := controllerruntime.CreateOrUpdate(ctx, c, deploy, func() error {
			return updateDeploymentAnnotations(deploy, d)
		})
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the Nginx configMap: %w", err), ErrClientK8s)
		}
	}
	return nil
}

// updateConfigMapForSiteSettings modifies the configmap to include the file settings.php
func updateConfigMapForSiteSettings(ctx context.Context, currentobject *corev1.ConfigMap, d *webservicesv1a1.DrupalSite, c client.Client) error {
	configPath := "/tmp/runtime-config/sitebuilder/settings.php"
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return newApplicationError(fmt.Errorf("reading settings.php failed: %w", err), ErrFilesystemIO)
	}

	if currentobject.CreationTimestamp.IsZero() {
		addOwnerRefToObject(currentobject, asOwner(d))

		currentobject.Data = map[string]string{
			"settings.php": string(content),
		}

	}

	if currentobject.Labels == nil {
		currentobject.Labels = map[string]string{}
	}
	if currentobject.Annotations == nil {
		currentobject.Annotations = map[string]string{}
	}
	ls := labelsForDrupalSite(d.Name)
	ls["app"] = "nginx"
	for k, v := range ls {
		currentobject.Labels[k] = v
	}

	if !currentobject.CreationTimestamp.IsZero() {
		// Roll out a new deployment
		deploy := &appsv1.Deployment{}
		err = c.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, deploy)
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the settings.php configMap (deployment not found): %w", err), ErrClientK8s)
		}
		updateDeploymentAnnotations := func(deploy *appsv1.Deployment, d *webservicesv1a1.DrupalSite) error {
			hash := md5.Sum([]byte(currentobject.Data["settings.php"]))
			currentHash, flag := deploy.Spec.Template.ObjectMeta.Annotations["settings.php-configmap/hash"]
			if flag == false || hex.EncodeToString(hash[:]) != currentHash {
				deploy.Spec.Template.ObjectMeta.Annotations["settings.php-configmap/hash"] = hex.EncodeToString(hash[:])
			}
			return nil
		}
		_, err := controllerruntime.CreateOrUpdate(ctx, c, deploy, func() error {
			return updateDeploymentAnnotations(deploy, d)
		})
		if err != nil {
			return newApplicationError(fmt.Errorf("failed to roll out new deployment while updating the settings.php configMap: %w", err), ErrClientK8s)
		}
	}

	return nil
}

// addOwnerRefToObject appends the desired OwnerReference to the object
func addOwnerRefToObject(obj metav1.Object, ownerRef metav1.OwnerReference) {
	// If Owner already in object, we ignore
	for _, o := range obj.GetOwnerReferences() {
		if o.UID == ownerRef.UID {
			return
		}
	}
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
}

// asOwner returns an OwnerReference set as the memcached CR
func asOwner(d *webservicesv1a1.DrupalSite) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: d.APIVersion,
		Kind:       d.Kind,
		Name:       d.Name,
		UID:        d.UID,
		Controller: &trueVar,
	}
}

// siteInstallJobForDrupalSite outputs the command needed for jobForDrupalSiteDrush
func siteInstallJobForDrupalSite() []string {
	// return []string{"sh", "-c", "echo"}
	return []string{"/operations/site-install.sh"}
}

// enableSiteMaintenanceModeCommandForDrupalSite outputs the command needed to enable maintenance mode
func enableSiteMaintenanceModeCommandForDrupalSite() []string {
	return []string{"/operations/enable-maintenance-mode.sh"}
}

// disableSiteMaintenanceModeCommandForDrupalSite outputs the command needed to disable maintenance mode
func disableSiteMaintenanceModeCommandForDrupalSite() []string {
	return []string{"/operations/disable-maintenance-mode.sh"}
}

// checkUpdbStatus outputs the command needed to check if a database update is required
func checkUpdbStatus() []string {
	return []string{"/operations/check-updb-status.sh"}
}

// runUpDBCommand outputs the command needed to update the database in drupal
func runUpDBCommand() []string {
	return []string{"/operations/run-updb.sh"}
}

// takeBackup outputs the command need to take the database backup to a given filename
func takeBackup(filename string) []string {
	return []string{"/operations/database-backup.sh", "-f", filename}
}

// restoreBackup outputs the command need to restore the database backup from a given filename
func restoreBackup(filename string) []string {
	return []string{"/operations/database-restore.sh", "-f", filename}
}

// cloneSource outputs the command need to clone a drupal site
func cloneSource(filename string) []string {
	return []string{"/operations/clone.sh", "-f", filename}
}

// encryptBasicAuthPassword encrypts a password for basic authentication
// Since we are using SabreDAV, the specific format to follow: https://sabre.io/dav/authentication/#using-the-file-backend
func encryptBasicAuthPassword(password string) string {
	webdavHashPrefix := webDAVDefaultLogin + ":SabreDAV:"
	hashedPassword := md5.Sum([]byte(webdavHashPrefix + password))
	return webdavHashPrefix + hex.EncodeToString(hashedPassword[:])
}

// checkIfSiteIsInstalled outputs the command to check if a site is initialized or not
func checkIfSiteIsInstalled() []string {
	return []string{"/operations/check-if-installed.sh"}
}

// cacheReload outputs the command to reload cache on the drupalSite
func cacheReload() []string {
	return []string{"/operations/clear-cache.sh"}
}

// backupListUpdateNeeded tells whether two arrays of velero Backups elements are the same or not.
// A nil argument is equivalent to an empty slice.
func backupListUpdateNeeded(veleroBackupsList []velerov1.Backup, statusBackupsList []webservicesv1a1.Backup) bool {
	if len(veleroBackupsList) != len(statusBackupsList) {
		return true
	}
	for i, v := range veleroBackupsList {
		if v.Name != statusBackupsList[i].BackupName {
			return true
		}
	}
	return false
}

// updateBackupListStatus updates the list of backups in the status of the DrupalSite
func updateBackupListStatus(veleroBackupsList []velerov1.Backup) []webservicesv1a1.Backup {
	statusBackupsList := []webservicesv1a1.Backup{}
	for _, v := range veleroBackupsList {
		if value, bool := v.GetLabels()["drupal.webservices.cern.ch/drupalSite"]; bool {
			statusBackupsList = append(statusBackupsList, webservicesv1a1.Backup{BackupName: v.Name, DrupalSiteName: value, Date: v.Status.CompletionTimestamp, Expires: v.Status.Expiration})
		}
	}
	return statusBackupsList
}

// getCurrentNamespace checks for the given variable in the environment, if not exists
func (r *DrupalSiteReconciler) getCurrentNamespace(ctx context.Context, d *webservicesv1a1.DrupalSite) (*corev1.Namespace, error) {
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: d.Namespace}, namespace)
	return namespace, err
}

// expectedDeploymentReplicas calculates expected replicas of deployment
func expectedDeploymentReplicas(currentnamespace *corev1.Namespace) *int32 {
	_, isBlockedTimestampAnnotationSet := currentnamespace.Annotations["blocked.webservices.cern.ch/blocked-timestamp"]
	_, isBlockedReasonAnnotationSet := currentnamespace.Annotations["blocked.webservices.cern.ch/reason"]
	if isBlockedTimestampAnnotationSet && isBlockedReasonAnnotationSet {
		return pointer.Int32Ptr(0)
	} else if !isBlockedTimestampAnnotationSet && !isBlockedReasonAnnotationSet {
		return pointer.Int32Ptr(1)
	}
	return nil
}