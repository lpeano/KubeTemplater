/*
Copyright 2025.

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
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	kubetemplateriov1alpha1 "github.com/lpeano/KubeTemplater/api/kubetemplater.io/v1alpha1"
	"github.com/lpeano/KubeTemplater/internal/cache"
	"github.com/lpeano/KubeTemplater/internal/cert"
	"github.com/lpeano/KubeTemplater/internal/controller"
	kubetemplateriocontroller "github.com/lpeano/KubeTemplater/internal/controller/kubetemplater.io"
	"github.com/lpeano/KubeTemplater/internal/queue"
	kubetemplaterwebhook "github.com/lpeano/KubeTemplater/internal/webhook"
	"github.com/lpeano/KubeTemplater/internal/worker"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// getEnvInt retrieves an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultValue
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		setupLog.Info("Invalid integer value for environment variable, using default",
			"key", key,
			"value", valStr,
			"default", defaultValue,
			"error", err)
		return defaultValue
	}
	return val
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(kubetemplateriov1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// isValidCertFile checks if a file contains valid PEM data
// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var webhookCertSecretName string
	var webhookServiceName string
	var webhookConfigurationName string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&webhookCertSecretName, "webhook-cert-secret-name", "", "The name of the secret containing webhook certificates (for automatic cert management).")
	flag.StringVar(&webhookServiceName, "webhook-service-name", "kubetemplater-webhook-service", "The name of the webhook service.")
	flag.StringVar(&webhookConfigurationName, "webhook-configuration-name", "kubetemplater-validating-webhook-configuration", "The name of the validating webhook configuration to patch with the CA bundle.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watcher for metrics certificate
	var metricsCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	// Setup SecretCertWatcher for webhook certificates (all pods)
	var secretCertWatcher *cert.SecretCertWatcher
	if webhookCertSecretName != "" {
		operatorNamespace := os.Getenv("POD_NAMESPACE")
		if operatorNamespace == "" {
			operatorNamespace = "kubetemplater-system"
		}

		setupLog.Info("Setting up webhook with dynamic certificate loading from secret",
			"secretName", webhookCertSecretName,
			"namespace", operatorNamespace)

		config := ctrl.GetConfigOrDie()
		k8sClientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			setupLog.Error(err, "unable to create kubernetes clientset")
			os.Exit(1)
		}

		// Note: Client will be set after manager creation (line ~270)
		secretCertWatcher = cert.NewSecretCertWatcher(
			nil, // Client set after manager creation to avoid circular dependency
			k8sClientset,
			webhookCertSecretName,
			operatorNamespace,
		)

		// Configure webhook to use SecretCertWatcher
		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = secretCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "8377a775.my.company.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// Safe for this operator because:
		// - Program exits immediately after manager stops (no cleanup operations)
		// - Enables fast voluntary leader transitions (no LeaseDuration wait)
		// - Prevents zombie leases on graceful shutdowns
		LeaderElectionReleaseOnCancel: true,
		// Aggressive lease settings for faster failover
		LeaseDuration: ptr.To(10 * time.Second), // Lease expires after 10s without renewal
		RenewDeadline: ptr.To(7 * time.Second),  // Leader must renew within 7s
		RetryPeriod:   ptr.To(2 * time.Second),  // Retry every 2s
		// Result: If leader dies, new election happens in ~10s maximum
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Add SecretCertWatcher as a Runnable (all pods)
	if secretCertWatcher != nil {
		// Set the client now that manager is created (required for future use)
		secretCertWatcher.Client = mgr.GetClient()
		
		if err := mgr.Add(secretCertWatcher); err != nil {
			setupLog.Error(err, "unable to add secret cert watcher to manager")
			os.Exit(1)
		}
	}

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		// Fallback to a default namespace if the env var is not set, for local development.
		operatorNamespace = "default"
		setupLog.Info("OPERATOR_NAMESPACE not set, falling back to 'default' namespace. For production, this should be set to the namespace where the operator is running.")
	}

	// Initialize certificate manager if secret name is provided
	var certManager *cert.Manager
	if webhookCertSecretName != "" {
		setupLog.Info("Certificate auto-management enabled",
			"secretName", webhookCertSecretName,
			"namespace", operatorNamespace,
			"serviceName", webhookServiceName)
		
		config := ctrl.GetConfigOrDie()
		k8sClientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			setupLog.Error(err, "unable to create kubernetes clientset")
			os.Exit(1)
		}
		
		certManager = cert.NewManager(
			mgr.GetClient(),
			k8sClientset,
			webhookCertSecretName,
			operatorNamespace,
			webhookServiceName,
			webhookConfigurationName,
		)

		// Add certificate manager as a Runnable that respects leader election
		if err := mgr.Add(certManager); err != nil {
			setupLog.Error(err, "unable to add certificate manager to manager")
			os.Exit(1)
		}
	}

	// Setup field indexer for KubeTemplatePolicy.Spec.SourceNamespace for efficient policy lookups
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kubetemplateriov1alpha1.KubeTemplatePolicy{}, "spec.sourceNamespace", func(obj client.Object) []string {
		policy := obj.(*kubetemplateriov1alpha1.KubeTemplatePolicy)
		return []string{policy.Spec.SourceNamespace}
	}); err != nil {
		setupLog.Error(err, "unable to create field indexer for KubeTemplatePolicy")
		os.Exit(1)
	}

	// Get tuning parameters from environment variables
	// NUM_WORKERS: Number of concurrent worker goroutines (default: 3)
	numWorkers := getEnvInt("NUM_WORKERS", 3)
	if numWorkers < 1 {
		numWorkers = 1
		setupLog.Info("NUM_WORKERS must be >= 1, using default", "value", 1)
	}
	if numWorkers > 20 {
		setupLog.Info("NUM_WORKERS > 20 may cause high resource usage", "value", numWorkers)
	}

	// CACHE_TTL: General cache time-to-live in seconds (default: 300 = 5 minutes)
	// Used for general caching operations. For policy cache, see POLICY_CACHE_TTL below.
	cacheTTLSeconds := getEnvInt("CACHE_TTL", 300)
	if cacheTTLSeconds < 60 {
		cacheTTLSeconds = 60
		setupLog.Info("CACHE_TTL must be >= 60 seconds, using minimum", "value", 60)
	}
	cacheTTL := time.Duration(cacheTTLSeconds) * time.Second

	// POLICY_CACHE_TTL: Policy cache time-to-live in seconds (default: 60 = 1 minute)
	// Configured via tuning.policyCacheTTL in Helm values
	// Used by both webhook validation and backend worker processing
	// Shorter TTL ensures fresh policy data for security-critical operations
	// Lower values = better security, higher values = better performance
	policyCacheTTLSeconds := getEnvInt("POLICY_CACHE_TTL", 60)
	if policyCacheTTLSeconds < 30 {
		policyCacheTTLSeconds = 30
		setupLog.Info("POLICY_CACHE_TTL must be >= 30 seconds, using minimum", "value", 30)
	}
	if policyCacheTTLSeconds > 600 {
		policyCacheTTLSeconds = 600
		setupLog.Info("POLICY_CACHE_TTL must be <= 600 seconds, using maximum", "value", 600)
	}
	policyCacheTTL := time.Duration(policyCacheTTLSeconds) * time.Second

	// PERIODIC_RECONCILE_INTERVAL: Interval for drift detection reconciliation in seconds (default: 60)
	periodicReconcileSeconds := getEnvInt("PERIODIC_RECONCILE_INTERVAL", 60)
	if periodicReconcileSeconds < 30 {
		periodicReconcileSeconds = 30
		setupLog.Info("PERIODIC_RECONCILE_INTERVAL must be >= 30 seconds, using minimum", "value", 30)
	}
	periodicReconcileInterval := time.Duration(periodicReconcileSeconds) * time.Second

	// QUEUE_MAX_RETRIES: Maximum retry attempts before cooldown (default: 5)
	queueMaxRetries := getEnvInt("QUEUE_MAX_RETRIES", 5)
	if queueMaxRetries < 1 {
		queueMaxRetries = 1
		setupLog.Info("QUEUE_MAX_RETRIES must be >= 1, using minimum", "value", 1)
	}

	// QUEUE_INITIAL_RETRY_DELAY: Initial retry delay in seconds (default: 1)
	queueInitialRetrySeconds := getEnvInt("QUEUE_INITIAL_RETRY_DELAY", 1)
	if queueInitialRetrySeconds < 1 {
		queueInitialRetrySeconds = 1
		setupLog.Info("QUEUE_INITIAL_RETRY_DELAY must be >= 1 second, using minimum", "value", 1)
	}
	queueInitialRetryDelay := time.Duration(queueInitialRetrySeconds) * time.Second

	// QUEUE_MAX_RETRY_DELAY: Maximum retry delay in seconds (default: 300 = 5 minutes)
	queueMaxRetrySeconds := getEnvInt("QUEUE_MAX_RETRY_DELAY", 300)
	if queueMaxRetrySeconds < 60 {
		queueMaxRetrySeconds = 60
		setupLog.Info("QUEUE_MAX_RETRY_DELAY must be >= 60 seconds, using minimum", "value", 60)
	}
	queueMaxRetryDelay := time.Duration(queueMaxRetrySeconds) * time.Second

	// QUEUE_MAX_RETRY_CYCLES: Maximum retry cycles before pausing (default: 3, 0 = unlimited)
	queueMaxRetryCycles := getEnvInt("QUEUE_MAX_RETRY_CYCLES", 3)
	if queueMaxRetryCycles < 0 {
		queueMaxRetryCycles = 0
		setupLog.Info("QUEUE_MAX_RETRY_CYCLES cannot be negative, using unlimited", "value", 0)
	}

	setupLog.Info("Tuning parameters configured",
		"numWorkers", numWorkers,
		"cacheTTL", cacheTTL,
		"policyCacheTTL", policyCacheTTL,
		"periodicReconcileInterval", periodicReconcileInterval,
		"queueMaxRetries", queueMaxRetries,
		"queueInitialRetryDelay", queueInitialRetryDelay,
		"queueMaxRetryDelay", queueMaxRetryDelay,
		"queueMaxRetryCycles", queueMaxRetryCycles)

	// Initialize policy cache with security-focused TTL (used by webhook & workers)
	policyCache := cache.NewPolicyCache(mgr.GetClient(), policyCacheTTL)
	setupLog.Info("Policy cache initialized", "ttl", policyCacheTTL)

	// Initialize work queue for async processing with configurable retry parameters
	workQueue := queue.NewWorkQueueWithConfig(queueMaxRetries, queueInitialRetryDelay, queueMaxRetryDelay, queueMaxRetryCycles)
	setupLog.Info("Work queue initialized",
		"maxRetries", queueMaxRetries,
		"initialRetryDelay", queueInitialRetryDelay,
		"maxRetryDelay", queueMaxRetryDelay,
		"maxRetryCycles", queueMaxRetryCycles)

	// Create event recorder for worker events
	eventRecorder := mgr.GetEventRecorderFor("kubetemplater-worker")
	
	// Start worker pool for processing templates
	ctx := context.Background()
	worker.StartWorkers(ctx, mgr.GetClient(), policyCache, workQueue, eventRecorder, operatorNamespace, numWorkers)
	setupLog.Info("Started template processor workers", "numWorkers", numWorkers)

	// Setup policy cache controller to keep cache in sync
	if err := (&kubetemplateriocontroller.PolicyCacheReconciler{
		Client: mgr.GetClient(),
		Cache:  policyCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PolicyCache")
		os.Exit(1)
	}

	if err := (&kubetemplateriocontroller.KubeTemplateReconciler{
		Client:                    mgr.GetClient(),
		Scheme:                    mgr.GetScheme(),
		OperatorNamespace:         operatorNamespace,
		WorkQueue:                 workQueue,
		PeriodicReconcileInterval: periodicReconcileInterval,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KubeTemplate")
		os.Exit(1)
	}
	if err := (&kubetemplateriocontroller.KubeTemplatePolicyReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		PolicyCache: policyCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KubeTemplatePolicy")
		os.Exit(1)
	}
	// NOTE: ResourceWatcher disabled due to controller-runtime limitation
	// Cannot watch unstructured.Unstructured{} without specifying Kind
	// This prevents watching all resource types dynamically
	// Continuous reconciliation still works via periodic re-enqueueing of Completed templates
	// TODO: Implement periodic reconciliation or watch specific GVKs
	/*
	if err := (&kubetemplateriocontroller.ResourceWatcherReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ResourceWatcher")
		os.Exit(1)
	}
	*/
	if err := (&controller.NamespaceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	// Setup webhook for KubeTemplate validation
	if err := (&kubetemplaterwebhook.KubeTemplateValidator{
		Client:            mgr.GetClient(),
		OperatorNamespace: operatorNamespace,
		Cache:             policyCache,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "KubeTemplate")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	
	// Add certificate readiness check if SecretCertWatcher is enabled
	if secretCertWatcher != nil {
		if err := mgr.AddReadyzCheck("certificate-ready", func(req *http.Request) error {
			if secretCertWatcher.IsReady() {
				return nil
			}
			return fmt.Errorf("certificate not loaded yet")
		}); err != nil {
			setupLog.Error(err, "unable to set up certificate readiness check")
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

