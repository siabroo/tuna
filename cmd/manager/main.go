// Package main is the tuna operator entrypoint.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
	"github.com/siabroo/tuna/internal/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tunav1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr      string
		probeAddr        string
		enableLeader     bool
		prometheusURL    string
		authMode         string
		analysisInterval string
		selectorMode     string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address for the operator's own metrics endpoint.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address for readyz/healthz.")
	flag.BoolVar(&enableLeader, "leader-elect", false, "Enable leader election. Required for HA.")
	flag.StringVar(&prometheusURL, "prometheus-url", "", "PromQL HTTP API base URL. Required.")
	flag.StringVar(&authMode, "auth-mode", "none", "Auth mode for Prometheus: none | gcp-id-token.")
	flag.StringVar(&analysisInterval, "analysis-interval", "5m", "How often to re-analyze each CR.")
	flag.StringVar(&selectorMode, "selector-mode", "k8s-api", "How to resolve pod sets: k8s-api | kube-state-metrics.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeader,
		LeaderElectionID:       "tuna-manager.tuna.siabroo.github.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := controller.AddDiscoveryController(mgr); err != nil {
		setupLog.Error(err, "unable to set up DiscoveryController")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "prometheusURL", prometheusURL, "authMode", authMode)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
