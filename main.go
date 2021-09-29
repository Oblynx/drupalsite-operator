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

package main

import (
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	dbodv1a1 "gitlab.cern.ch/drupal/paas/dbod-operator/api/v1alpha1"
	drupalwebservicesv1alpha1 "gitlab.cern.ch/drupal/paas/drupalsite-operator/api/v1alpha1"
	"gitlab.cern.ch/drupal/paas/drupalsite-operator/controllers"
	authz "gitlab.cern.ch/paas-tools/operators/authz-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	appsv1 "k8s.io/api/apps/v1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(drupalwebservicesv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authz.AddToScheme(scheme))
	utilruntime.Must(dbodv1a1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme))
	utilruntime.Must(buildv1.AddToScheme(scheme))
	utilruntime.Must(velerov1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&controllers.SiteBuilderImage, "sitebuilder-image", "gitlab-registry.cern.ch/drupal/paas/cern-drupal-distribution/site-builder", "The sitebuilder source image name.")
	flag.StringVar(&controllers.PhpFpmExporterImage, "php-fpm-exporter-image", "gitlab-registry.cern.ch/drupal/paas/php-fpm-prometheus-exporter:RELEASE.2021.06.02T09-41-38Z", "The php-fpm-exporter source image name.")
	flag.StringVar(&controllers.WebDAVImage, "webdav-image", "gitlab-registry.cern.ch/drupal/paas/sabredav/webdav:RELEASE-2021.10.07T13-46-43Z", "The webdav source image name.")
	flag.StringVar(&controllers.SMTPHost, "smtp-host", "cernmx.cern.ch", "SMTP host used by Drupal server pods to send emails.")
	flag.StringVar(&controllers.VeleroNamespace, "velero-namespace", "openshift-cern-drupal", "The namespace of the Velero server to create backups")
	flag.StringVar(&controllers.DefaultReleaseSpec, "default-release-spec", "RELEASE-2021.10.07T14-52-56Z", "The default releaseSpec value to be passed to the DrupalSites")
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	var err error
	controllers.BuildResources, err = controllers.ResourceRequestLimit("250Mi", "250m", "300Mi", "1000m")
	if err != nil {
		setupLog.Error(err, "Invalid configuration: can't parse build resources")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "78d40201.cern.ch",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.DrupalSiteReconciler{
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("DrupalSite"),
		Scheme:                 mgr.GetScheme(),
		StartRateLimiterMillis: 500,
		MaxRateLimiterSeconds:  300, // 5 Minutes
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DrupalSite")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.V(1).Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
