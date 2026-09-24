package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	"github.com/go-logr/zapr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/kyverno/kyverno/pkg/leaderelection"
	"github.com/kyverno/pkg/certmanager"
	tlsMgr "github.com/kyverno/pkg/tls"
	_ "github.com/notaryproject/notation-core-go/signature/cose"
	_ "github.com/notaryproject/notation-core-go/signature/jws"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/krax1337/notation-aws-verifier/internal/kubenotation"
	"github.com/krax1337/notation-aws-verifier/internal/setup"
	"github.com/krax1337/notation-aws-verifier/internal/verifier"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

var (
	Namespace      = os.Getenv("POD_NAMESPACE")
	PodName        = os.Getenv("POD_NAME")
	ServiceName    = getEnvWithFallback("SERVICE_NAME", "notation-aws-verifier-svc")
	DeploymentName = getEnvWithFallback("DEPLOYMENT_NAME", "notation-aws-verifier")

	CertRenewalInterval = 12 * time.Hour
	CAValidityDuration  = 365 * 24 * time.Hour
	TLSValidityDuration = 150 * 24 * time.Hour

	resyncPeriod = 15 * time.Minute
)

const (
	httpAddr        = ":9080"
	httpsAddr       = ":9443"
	shutdownTimeout = 20 * time.Second
)

type options struct {
	local                 bool
	noTLS                 bool
	imagePullSecrets      string
	allowInsecureRegistry bool
	pluginConfigMap       string
	debug                 bool
	maxSignatureAttempts  int
	metricsAddr           string
	probeAddr             string
	leaderElect           bool
	cacheEnabled          bool
	cacheMaxSize          int64
	cacheTTLSeconds       int64
	allowedUsers          string
	reviewKyvernoToken    bool
	tokenReviewAudiences  string
	logLevel              string
	logLevelSet           bool
	logFormat             string
	showVersion           bool
}

func parseFlags() options {
	var o options
	flag.BoolVar(&o.local, "local", false, "Use local system notation configuration")
	flag.BoolVar(&o.noTLS, "notls", false, "Do not start the TLS server")
	flag.StringVar(&o.imagePullSecrets, "imagePullSecrets", "", "Comma-separated secret resource names for image registry access credentials.")
	flag.BoolVar(&o.allowInsecureRegistry, "allowInsecureRegistry", false, "Whether to allow insecure connections to registries. Not recommended.")
	flag.StringVar(&o.pluginConfigMap, "pluginConfigMap", "notation-plugin-config", "ConfigMap with notation plugin configuration")
	flag.BoolVar(&o.debug, "debug", false, "Enable notation and plugin debug output. Implies --logLevel=debug unless --logLevel is set.")
	flag.IntVar(&o.maxSignatureAttempts, "maxSignatureAttempts", 30, "Maximum number of signature envelopes that will be processed for verification")
	flag.StringVar(&o.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&o.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&o.leaderElect, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&o.cacheEnabled, "cacheEnabled", true, "Whether to use a TTL cache for storing verified images.")
	flag.Int64Var(&o.cacheMaxSize, "cacheMaxSize", 1000, "Maximum number of entries in the TTL cache.")
	flag.Int64Var(&o.cacheTTLSeconds, "cacheTTLDurationSeconds", 3600, "TTL of a cache entry in seconds.")
	flag.BoolVar(&o.reviewKyvernoToken, "reviewKyvernoToken", true, "Checks if the Auth token in the request is a token from kyverno controllers or other allowed users.")
	flag.StringVar(&o.allowedUsers, "allowedUsers", "system:serviceaccount:kyverno:kyverno-admission-controller,system:serviceaccount:kyverno:kyverno-reports-controller", "Comma-separated list of all the allowed users and service accounts.")
	flag.StringVar(&o.tokenReviewAudiences, "tokenReviewAudiences", "", "Comma-separated audiences for the TokenReview of request tokens. Empty uses the API server's default audience.")
	flag.StringVar(&o.logLevel, "logLevel", "info", "Log level: trace, debug, info, warn, error")
	flag.StringVar(&o.logFormat, "logFormat", "text", "Log format: text or json")
	flag.BoolVar(&o.showVersion, "version", false, "Print the version and exit")
	flag.Parse()

	flag.Visit(func(f *flag.Flag) {
		if f.Name == "logLevel" {
			o.logLevelSet = true
		}
	})
	return o
}

// effectiveLogLevel keeps --debug working as it did before --logLevel existed.
func (o options) effectiveLogLevel() string {
	if o.debug && !o.logLevelSet {
		return "debug"
	}
	return o.logLevel
}

func (o options) cacheTTL() (time.Duration, error) {
	if o.cacheTTLSeconds <= 0 || o.cacheTTLSeconds > math.MaxInt64/int64(time.Second) {
		return 0, fmt.Errorf("invalid --cacheTTLDurationSeconds %d", o.cacheTTLSeconds)
	}
	return time.Duration(o.cacheTTLSeconds) * time.Second, nil
}

func main() {
	o := parseFlags()
	if o.showVersion {
		fmt.Println(version)
		return
	}

	logger, err := newLogger(o.logFormat, o.effectiveLogLevel())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	// Route klog (client-go) and the standard library logger through zap so
	// that every line honours --logFormat.
	klog.SetLogger(zapr.NewLogger(logger).WithName("klog"))
	undoStdLog := zap.RedirectStdLog(logger.Named("stdlog"))

	err = run(logger, o)
	undoStdLog()
	if err != nil {
		logger.Error("exiting", zap.Error(err))
		_ = logger.Sync()
		os.Exit(1)
	}
	_ = logger.Sync()
}

func run(logger *zap.Logger, o options) error {
	slog := logger.Sugar()
	slog.Infow("starting notation-aws-verifier", "version", version)

	cacheTTL, err := o.cacheTTL()
	if err != nil {
		return err
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes cluster config: %w", err)
	}
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to initialize kube client: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tlsMgrConfig := &tlsMgr.Config{
		ServiceName: ServiceName,
		Namespace:   Namespace,
	}

	caInformer := NewSecretInformer(kubeClient, Namespace, tlsMgr.GenerateRootCASecretName(tlsMgrConfig), resyncPeriod)
	go caInformer.Informer().Run(ctx.Done())

	tlsInformer := NewSecretInformer(kubeClient, Namespace, tlsMgr.GenerateTLSPairSecretName(tlsMgrConfig), resyncPeriod)
	go tlsInformer.Informer().Run(ctx.Done())

	le, err := leaderelection.New(
		zapr.NewLogger(logger).WithName("leader-election"),
		DeploymentName,
		Namespace,
		kubeClient,
		PodName,
		2*time.Second,
		func(context.Context) {
			certRenewer := tlsMgr.NewCertRenewer(
				zapr.NewLogger(logger).WithName("tls").WithValues("pod", PodName),
				kubeClient.CoreV1().Secrets(Namespace),
				CertRenewalInterval,
				CAValidityDuration,
				TLSValidityDuration,
				"",
				tlsMgrConfig,
			)

			certManager := certmanager.NewController(
				zapr.NewLogger(logger).WithName("certmanager").WithValues("pod", PodName),
				caInformer,
				tlsInformer,
				certRenewer,
				tlsMgrConfig,
			)

			leaderControllers := []Controller{NewController("cert-manager", certManager, 1)}

			// start leader controllers
			var wg sync.WaitGroup
			for _, controller := range leaderControllers {
				controller.Run(ctx, zapr.NewLogger(logger).WithName("controllers"), &wg)
			}
			// wait all controllers shut down
			wg.Wait()
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize leader election: %w", err)
	}
	go le.Run(ctx)

	if !o.local {
		if err := setup.Local(slog); err != nil {
			return err
		}
	}

	crdSetup, err := kubenotation.Setup(zapr.NewLogger(logger), o.metricsAddr, o.probeAddr, o.leaderElect)
	if err != nil {
		return fmt.Errorf("failed to initialize crds: %w", err)
	}

	// Not ready until the verifier serves requests, so Services do not route
	// admission calls to a pod that would refuse them.
	var ready atomic.Bool
	if err := crdSetup.CRDManager.AddReadyzCheck("verifier", func(*http.Request) error {
		if !ready.Load() {
			return errors.New("verifier not serving")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to add readiness check: %w", err)
	}

	slog.Info("Starting CRD Manager")
	errsMgr := make(chan error, 1)
	go func() {
		errsMgr <- crdSetup.CRDManager.Start(ctx)
	}()

	v, err := verifier.NewVerifier(slog,
		verifier.WithImagePullSecrets(o.imagePullSecrets),
		verifier.WithInsecureRegistry(o.allowInsecureRegistry),
		verifier.WithPluginConfig(o.pluginConfigMap),
		verifier.WithMaxSignatureAttempts(o.maxSignatureAttempts),
		verifier.WithEnableDebug(o.debug),
		verifier.WithProviderKeychain(authn.NewKeychainFromHelper(ecr.NewECRHelper(ecr.WithLogger(io.Discard)))),
		verifier.WithTokenReviewEnabled(o.reviewKyvernoToken),
		verifier.WithTokenReviewAudiences(splitList(o.tokenReviewAudiences)),
		verifier.WithCacheEnabled(o.cacheEnabled),
		verifier.WithMaxCacheSize(o.cacheMaxSize),
		verifier.WithMaxCacheTTL(cacheTTL),
		verifier.WithAllowedUsers(splitList(o.allowedUsers)))
	if err != nil {
		return fmt.Errorf("failed to initialize verifier: %w", err)
	}
	defer v.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/checkimages", v.HandleCheckImages)

	errorLog := zap.NewStdLog(logger.Named("http"))
	servers := []*http.Server{newServer(httpAddr, mux, nil, errorLog)}
	if !o.noTLS {
		servers = append(servers, newServer(httpsAddr, mux, newTLSConfig(tlsInformer, tlsMgrConfig), errorLog))
	}

	errsSrv := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			var err error
			if srv.TLSConfig != nil {
				err = srv.ListenAndServeTLS("", "")
			} else {
				err = srv.ListenAndServe()
			}
			errsSrv <- fmt.Errorf("server %s: %w", srv.Addr, err)
		}()
	}

	ready.Store(true)
	slog.Info("Listening for requests...")

	var runErr error
loop:
	for {
		select {
		case <-crdSetup.CRDChangeInformer:
			slog.Info("Trust policies or trust stores changed, updating notation verifier")
			if err := v.UpdateNotationVerifier(); err != nil {
				slog.Errorf("failed to update verifier, keeping the previous one: %v", err)
			} else {
				slog.Info("Notation verifier updated")
			}
		case err := <-errsSrv:
			runErr = err
			break loop
		case err := <-errsMgr:
			if err == nil && ctx.Err() == nil {
				err = errors.New("manager stopped unexpectedly")
			}
			if err != nil {
				runErr = fmt.Errorf("problem running manager: %w", err)
			}
			break loop
		case <-ctx.Done():
			slog.Info("Shutdown signal received")
			break loop
		}
	}

	ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warnf("failed to shut down server %s: %v", srv.Addr, err)
		}
	}
	stop()
	return runErr
}

// newServer returns an HTTP server with timeouts, so slow clients cannot hold
// connections and goroutines forever.
func newServer(addr string, handler http.Handler, tlsConf *tls.Config, errorLog *log.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsConf,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          errorLog,
	}
}

// newTLSConfig serves the certificate from the TLS secret maintained by the
// cert manager, re-parsing it only when the secret changes.
func newTLSConfig(informer corev1informers.SecretInformer, cfg *tlsMgr.Config) *tls.Config {
	type parsedCert struct {
		resourceVersion string
		cert            *tls.Certificate
	}
	secretName := tlsMgr.GenerateTLSPairSecretName(cfg)
	var current atomic.Pointer[parsedCert]

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			secret, err := informer.Lister().Secrets(cfg.Namespace).Get(secretName)
			if err != nil {
				return nil, err
			}
			if secret.Type != corev1.SecretTypeTLS {
				return nil, errors.New("secret is not a TLS secret")
			}
			if c := current.Load(); c != nil && c.resourceVersion == secret.ResourceVersion {
				return c.cert, nil
			}

			cert, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
			if err != nil {
				return nil, err
			}
			current.Store(&parsedCert{resourceVersion: secret.ResourceVersion, cert: &cert})
			return &cert, nil
		},
	}
}
