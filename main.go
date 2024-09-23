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

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	infrav1beta1 "github.com/DoodleScheduling/oauth2-redirect-controller/api/v1beta1"
	"github.com/DoodleScheduling/oauth2-redirect-controller/internal/controllers"
	"github.com/DoodleScheduling/oauth2-redirect-controller/internal/proxy"
	"github.com/fluxcd/pkg/runtime/client"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/leaderelection"
	"github.com/fluxcd/pkg/runtime/logger"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

const controllerName = "oauth2-redirect-controller"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = corev1.AddToScheme(scheme)
	_ = infrav1beta1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

var (
	proxyReadTimeout        = 10 * time.Second
	proxyWriteTimeout       = 10 * time.Second
	httpAddr                = ":8080"
	metricsAddr             string
	healthAddr              string
	concurrent              int
	gracefulShutdownTimeout time.Duration
	clientOptions           client.Options
	kubeConfigOpts          client.KubeConfigOptions
	logOptions              logger.Options
	leaderElectionOptions   leaderelection.Options
	rateLimiterOptions      helper.RateLimiterOptions
	watchOptions            helper.WatchOptions
)

func main() {
	flag.StringVar(&httpAddr, "http-addr", ":8080", "The address of http server binding to.")
	flag.DurationVar(&proxyReadTimeout, "proxy-read-timeout", 10*time.Second, "Read timeout for proxy requests.")
	flag.DurationVar(&proxyWriteTimeout, "proxy-write-timeout", 10*time.Second, "Write timeout for proxy requests.")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9556",
		"The address the metric endpoint binds to.")
	flag.StringVar(&healthAddr, "health-addr", ":9557",
		"The address the health endpoint binds to.")
	flag.IntVar(&concurrent, "concurrent", 4,
		"The number of concurrent KeycloakRealm reconciles.")
	flag.DurationVar(&gracefulShutdownTimeout, "graceful-shutdown-timeout", 600*time.Second,
		"The duration given to the reconciler to finish before forcibly stopping.")

	clientOptions.BindFlags(flag.CommandLine)
	logOptions.BindFlags(flag.CommandLine)
	leaderElectionOptions.BindFlags(flag.CommandLine)
	rateLimiterOptions.BindFlags(flag.CommandLine)
	kubeConfigOpts.BindFlags(flag.CommandLine)
	watchOptions.BindFlags(flag.CommandLine)

	flag.Parse()
	logger.SetLogger(logger.NewLogger(logOptions))

	leaderElectionId := fmt.Sprintf("%s-%s", controllerName, "leader-election")
	if watchOptions.LabelSelector != "" {
		leaderElectionId = leaderelection.GenerateID(leaderElectionId, watchOptions.LabelSelector)
	}

	watchSelector, err := helper.GetWatchSelector(watchOptions)
	if err != nil {
		setupLog.Error(err, "unable to configure watch label selector for manager")
		os.Exit(1)
	}

	opts := ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:        healthAddr,
		LeaderElection:                leaderElectionOptions.Enable,
		LeaderElectionReleaseOnCancel: leaderElectionOptions.ReleaseOnCancel,
		LeaseDuration:                 &leaderElectionOptions.LeaseDuration,
		RenewDeadline:                 &leaderElectionOptions.RenewDeadline,
		RetryPeriod:                   &leaderElectionOptions.RetryPeriod,
		GracefulShutdownTimeout:       &gracefulShutdownTimeout,
		LeaderElectionID:              leaderElectionId,
		Cache: ctrlcache.Options{
			ByObject: map[ctrlclient.Object]ctrlcache.ByObject{
				&infrav1beta1.OAUTH2Proxy{}: {Label: watchSelector},
			},
		},
	}

	if !watchOptions.AllNamespaces {
		opts.Cache.DefaultNamespaces = make(map[string]ctrlcache.Config)
		opts.Cache.DefaultNamespaces[os.Getenv("RUNTIME_NAMESPACE")] = ctrlcache.Config{}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Add liveness probe
	err = mgr.AddHealthzCheck("healthz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "Could not add liveness probe")
		os.Exit(1)
	}

	// Add readiness probe
	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "Could not add readiness probe")
		os.Exit(1)
	}

	resources, err := resource.New(context.Background(),
		resource.WithFromEnv(), // pull attributes from OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment variables
		resource.WithProcess(), // This option configures a set of Detectors that discover process information
	)
	if err != nil {
		setupLog.Error(err, "failed creating OTLP resources")
		os.Exit(1)
	}

	client := otlptracegrpc.NewClient()
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		setupLog.Error(err, "failed creating OTLP trace exporter")
		os.Exit(1)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resources),
	)

	otel.SetTextMapPropagator(b3.New())
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			setupLog.Error(err, "failed to shutdown trace provider")
		}
	}()

	otel.SetTracerProvider(tp)
	proxy := proxy.New(setupLog, &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})

	wrappedHandler := otelhttp.NewHandler(proxy, "oauth2-proxy")

	readTimeout, err := time.ParseDuration(viper.GetString("proxy-read-timeout"))
	if err != nil {
		setupLog.Error(err, "failed to parse proxy read timeout")
	}

	writeTimeout, err := time.ParseDuration(viper.GetString("proxy-write-timeout"))
	if err != nil {
		setupLog.Error(err, "failed to parse proxy write timeout")
	}

	s := &http.Server{
		Addr:         httpAddr,
		Handler:      wrappedHandler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	go func() {
		if err := s.ListenAndServe(); err != nil {
			setupLog.Error(err, "HTTP server error")
		}
	}()

	realmReconciler := &controllers.OAUTH2ProxyReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("OAUTH2ProxyReconciler"),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("OAUTH2ProxyReconciler"),
		HttpProxy: proxy,
	}

	if err = realmReconciler.SetupWithManager(mgr, controllers.OAUTH2ProxyReconcilerOptions{
		MaxConcurrentReconciles: concurrent,
	}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OAUTH2ProxyReconciler")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
