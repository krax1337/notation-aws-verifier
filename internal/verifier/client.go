// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: bearer token no longer logged; malformed
// Authorization headers return 401 instead of panicking; TokenReview uses the
// request context and optional audiences; bounded request body; per-request
// logs at debug level; the verification runs on the request context; Stop no
// longer deadlocks; typo fixes.

// Package verifier implements the /checkimages endpoint called by Kyverno.
package verifier

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/kyverno/kyverno/pkg/engine/jmespath"
	"go.uber.org/zap"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"github.com/krax1337/notation-aws-verifier/internal/cache"
	"github.com/krax1337/notation-aws-verifier/internal/notationfactory"
	"github.com/krax1337/notation-aws-verifier/internal/types"
)

// maxRequestBodyBytes bounds the /checkimages request body. Kyverno sends the
// images of one resource, which is a few KiB.
const maxRequestBodyBytes = 4 << 20

type Verifier interface {
	// HandleCheckImages is a handler function that takes Kyverno images variable in body and returns JSONPatch compatible object in response
	HandleCheckImages(w http.ResponseWriter, r *http.Request)

	// UpdateNotationVerifier reloads trust policies and trust stores and clears the cache.
	UpdateNotationVerifier() error

	// Stop shuts down the informers and the cache.
	Stop()
}

type verifier struct {
	logger                  *zap.SugaredLogger
	kubeClient              kubernetes.Interface
	notationVerifierFactory notationfactory.NotationVerifierFactory
	informerFactory         kubeinformers.SharedInformerFactory
	secretLister            corev1listers.SecretNamespaceLister
	configMapLister         corev1listers.ConfigMapNamespaceLister
	providerKeychain        authn.Keychain
	imagePullSecrets        string
	insecureRegistry        bool
	pluginConfigMap         string
	maxSignatureAttempts    int
	maxCacheSize            int64
	useCache                bool
	reviewToken             bool
	tokenReviewAudiences    []string
	maxCacheTTL             time.Duration
	debug                   bool
	stopCh                  chan struct{}
	jmespath                jmespath.Interface
	cache                   cache.Cache
	allowedUsers            []string
}

// Option configures the verifier.
type Option func(*verifier)

func WithImagePullSecrets(secrets string) Option {
	return func(v *verifier) {
		v.imagePullSecrets = secrets
	}
}

func WithInsecureRegistry(insecureRegistry bool) Option {
	return func(v *verifier) {
		v.insecureRegistry = insecureRegistry
	}
}

func WithPluginConfig(pluginConfigMap string) Option {
	return func(v *verifier) {
		v.pluginConfigMap = pluginConfigMap
	}
}

func WithMaxSignatureAttempts(maxSignatureAttempts int) Option {
	return func(v *verifier) {
		v.maxSignatureAttempts = maxSignatureAttempts
	}
}

func WithMaxCacheSize(maxCacheSize int64) Option {
	return func(v *verifier) {
		v.maxCacheSize = maxCacheSize
	}
}

func WithMaxCacheTTL(maxCacheTTL time.Duration) Option {
	return func(v *verifier) {
		v.maxCacheTTL = maxCacheTTL
	}
}

func WithCacheEnabled(useCache bool) Option {
	return func(v *verifier) {
		v.useCache = useCache
	}
}

func WithTokenReviewEnabled(enableTokenReview bool) Option {
	return func(v *verifier) {
		v.reviewToken = enableTokenReview
	}
}

// WithTokenReviewAudiences sets TokenReview.Spec.Audiences. Empty means the
// API server's default audience.
func WithTokenReviewAudiences(audiences []string) Option {
	return func(v *verifier) {
		v.tokenReviewAudiences = audiences
	}
}

func WithEnableDebug(debug bool) Option {
	return func(v *verifier) {
		v.debug = debug
	}
}

func WithAllowedUsers(users []string) Option {
	return func(v *verifier) {
		v.allowedUsers = users
	}
}

// WithProviderKeychain sets the keychain used for registries without an image
// pull secret (e.g. the ECR credential helper). Resolved credentials are
// cached per registry, see newCachingKeychain.
func WithProviderKeychain(keychain authn.Keychain) Option {
	return func(v *verifier) {
		v.providerKeychain = keychain
	}
}

func NewVerifier(logger *zap.SugaredLogger, opts ...Option) (Verifier, error) {
	var v *verifier
	initVerifier := func() error {
		var err error
		v, err = newVerifier(logger, opts...)
		if err != nil {
			logger.Errorf("verifier initialization failed, retrying: %v", err)
		}
		return err
	}

	if err := backoff.Retry(initVerifier, backoff.NewExponentialBackOff()); err != nil {
		return nil, err
	}

	return v, nil
}

func (v *verifier) HandleCheckImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	if v.reviewToken {
		if status, msg := v.authenticate(ctx, r.Header.Get("Authorization")); status != http.StatusOK {
			http.Error(w, msg, status)
			return
		}
	}

	var requestData types.RequestData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&requestData); err != nil {
		v.logger.Infof("failed to decode request body: %v", err)
		http.Error(w, "failed to decode request body: "+err.Error(), http.StatusNotAcceptable)
		return
	}
	v.logger.Debugf("Request received with data=%+v", requestData)

	verificationPayload, err := processRequestData(&requestData)
	if err != nil {
		v.logger.Infof("Missing required data: %v", err)
		http.Error(w, err.Error(), http.StatusNotAcceptable)
		return
	}

	responseData, err := v.verifyImagesAndAttestations(ctx, verificationPayload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !responseData.Verified {
		http.Error(w, responseData.ErrorMessage, http.StatusNotAcceptable)
		return
	}

	data, err := json.MarshalIndent(responseData, "  ", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v.logger.Debugf("Sending response %s", data)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		v.logger.Debugf("failed to write response: %v", err)
	}
}

// authenticate reviews the bearer token and returns http.StatusOK when the
// caller is authenticated and allowed, otherwise the status and message to send.
// The token itself is never logged.
func (v *verifier) authenticate(ctx context.Context, header string) (int, string) {
	if header == "" {
		return http.StatusUnauthorized, "Authorization header not supplied"
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	token = strings.TrimSpace(token)
	if !ok || token == "" {
		return http.StatusUnauthorized, "Authorization header must be of the form \"Bearer <token>\""
	}

	tr := authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token:     token,
			Audiences: v.tokenReviewAudiences,
		},
	}

	result, err := v.kubeClient.AuthenticationV1().TokenReviews().Create(ctx, &tr, metav1.CreateOptions{})
	if err != nil {
		v.logger.Errorf("failed to review auth token: %v", err)
		return http.StatusInternalServerError, "failed to review auth token"
	}

	username := result.Status.User.Username
	if !result.Status.Authenticated {
		v.logger.Infow("token is not authenticated", "error", result.Status.Error)
		return http.StatusUnauthorized, "Token is not authenticated"
	}
	if !v.isAllowed(username) {
		v.logger.Infow("token is not authorized", "username", username)
		return http.StatusForbidden, "Token is not authorized"
	}

	v.logger.Debugw("token is authorized", "username", username)
	return http.StatusOK, ""
}

func (v *verifier) UpdateNotationVerifier() error {
	if err := v.notationVerifierFactory.RefreshVerifiers(); err != nil {
		v.logger.Errorf("notation verifier creation failed, not updating verifiers: %v", err)
		return err
	}

	v.cache.Clear()
	return nil
}

func (v *verifier) Stop() {
	// Informers only return once stopCh is closed; Shutdown waits for them.
	close(v.stopCh)
	v.informerFactory.Shutdown()
	v.cache.Close()
	_ = v.logger.Sync()
}

func (v *verifier) isAllowed(username string) bool {
	return slices.Contains(v.allowedUsers, username)
}
