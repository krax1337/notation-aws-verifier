// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026:
//   - conditions are evaluated in a per-call Kyverno engine context instead of
//     one context shared (unsynchronised) by all concurrent requests;
//   - the attestation layer reader is closed (it leaked a response body per
//     verified attestation);
//   - registry work runs on the request context;
//   - credentials and plugin config are resolved once per request, the unused
//     gcr pusher is gone and the provider keychain is cached;
//   - docker auth identity/registry tokens map to the right oras fields;
//   - images are no longer skipped because the caller-controlled
//     verify-images annotation says so;
//   - informers are only started for what is configured and synced before use;
//   - logging fixes (wrong format verbs, per-request logs at debug level).

package verifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/zapr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/kyverno/kyverno/ext/wildcard"
	kyvernocfg "github.com/kyverno/kyverno/pkg/config"
	enginecontext "github.com/kyverno/kyverno/pkg/engine/context"
	"github.com/kyverno/kyverno/pkg/engine/jmespath"
	"github.com/kyverno/kyverno/pkg/engine/variables"
	"github.com/notaryproject/notation-go"
	notationlog "github.com/notaryproject/notation-go/log"
	notationregistry "github.com/notaryproject/notation-go/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/krax1337/notation-aws-verifier/internal/cache"
	"github.com/krax1337/notation-aws-verifier/internal/notationfactory"
	"github.com/krax1337/notation-aws-verifier/internal/types"
)

// informerSyncTimeout bounds how long startup waits for the secret/configmap
// informers before serving requests anyway.
const informerSyncTimeout = 30 * time.Second

func newVerifier(logger *zap.SugaredLogger, opts ...Option) (*verifier, error) {
	v := &verifier{
		logger: logger,
	}
	for _, o := range opts {
		o(v)
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	v.kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	v.notationVerifierFactory = notationfactory.NewNotationVerifierFactory(logger)
	if err := v.notationVerifierFactory.RefreshVerifiers(); err != nil {
		return nil, fmt.Errorf("failed to create notation verifiers: %w", err)
	}
	v.logger.Info("notation verifier created")

	if v.providerKeychain != nil {
		v.providerKeychain = newCachingKeychain(v.providerKeychain, providerCredentialsTTL)
	}

	v.logger.Infof("Initializing cache cacheEnabled=%v, maxSize=%v, maxTTL=%v", v.useCache, v.maxCacheSize, v.maxCacheTTL)
	v.cache, err = cache.New(cache.WithCacheEnabled(v.useCache),
		cache.WithMaxSize(v.maxCacheSize),
		cache.WithTTLDuration(v.maxCacheTTL),
		cache.WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("failed to create cache client: %w", err)
	}

	v.jmespath = jmespath.New(kyvernocfg.NewDefaultConfiguration(false))

	namespace := os.Getenv("POD_NAMESPACE")
	v.informerFactory = kubeinformers.NewSharedInformerFactoryWithOptions(v.kubeClient, 15*time.Minute, kubeinformers.WithNamespace(namespace))
	// Only watch what is configured, so no RBAC is needed for unused features.
	if v.imagePullSecrets != "" {
		v.secretLister = v.informerFactory.Core().V1().Secrets().Lister().Secrets(namespace)
	}
	if v.pluginConfigMap != "" {
		v.configMapLister = v.informerFactory.Core().V1().ConfigMaps().Lister().ConfigMaps(namespace)
	}

	v.stopCh = make(chan struct{})
	v.informerFactory.Start(v.stopCh)
	v.waitForInformers()

	v.logger.Infow("initialized", "namespace", namespace, "secrets", v.imagePullSecrets,
		"insecureRegistry", v.insecureRegistry)

	return v, nil
}

// waitForInformers waits for the listers used on the request path; before the
// sync they answer "not found".
func (v *verifier) waitForInformers() {
	timeout := make(chan struct{})
	timer := time.AfterFunc(informerSyncTimeout, func() { close(timeout) })
	defer timer.Stop()

	for informerType, synced := range v.informerFactory.WaitForCacheSync(timeout) {
		if !synced {
			v.logger.Warnf("informer cache for %v not synced after %v, continuing", informerType, informerSyncTimeout)
		}
	}
}

func (v *verifier) verifyImagesAndAttestations(ctx context.Context, requestData *types.VerificationRequest) (types.ResponseData, error) {
	response := NewResponse(v.logger)
	images := requestData.Images
	trustPolicy := v.getTrustPolicy(requestData)

	notationVerifier, err := v.notationVerifierFactory.GetVerifier(requestData)
	if err != nil {
		v.logger.Errorf("failed to create notation verifier: %s", err.Error())
		return response.VerificationFailed(fmt.Sprintf("failed to create notation verifier: %s", err.Error()))
	}

	keychain, err := v.getKeychain(ctx)
	if err != nil {
		v.logger.Errorf("failed to retrieve credentials: %s", err.Error())
		return response.VerificationFailed(fmt.Sprintf("failed to get remote options: failed to retrieve credentials: %s", err.Error()))
	}

	remoteOpts, err := v.getRemoteOpts(ctx, keychain)
	if err != nil {
		v.logger.Errorf("failed to get remote options: %s", err.Error())
		return response.VerificationFailed(fmt.Sprintf("failed to get remote options: %s", err.Error()))
	}

	pluginConfig, err := v.getPluginConfig()
	if err != nil {
		v.logger.Errorf("failed to get plugin config: %s", err.Error())
		return response.VerificationFailed(err.Error())
	}

	rv := &requestVerifier{
		verifier:         v,
		notationVerifier: notationVerifier,
		trustPolicy:      trustPolicy,
		keychain:         keychain,
		remoteOpts:       remoteOpts,
		pluginConfig:     pluginConfig,
	}

	groups := []struct {
		kind   string
		images map[string]types.ImageInfo
	}{
		{"container", images.Containers},
		{"init container", images.InitContainers},
		{"ephemeral container", images.EphemeralContainers},
	}
	for _, group := range groups {
		verified := 0
		for _, image := range group.images {
			if !matchImageReferences(requestData.ImageReferences, image.String()) {
				v.logger.Debugf("Skipping image %s", image.String())
				continue
			}
			result, err := rv.verifyImageInfo(ctx, image)
			if err != nil {
				v.logger.Errorf("failed to verify %s %s: %s", group.kind, image.Name, err.Error())
				return response.VerificationFailed(fmt.Sprintf("failed to verify %s %s: %v", group.kind, image.Name, err.Error()))
			}
			if len(result.Digest) == 0 {
				v.logger.Infof("Image reference has been skipped in the trust policy, image=%s", image.String())
				continue
			}
			response.AddImage(image.String(), result)
			verified++
		}
		v.logger.Debugf("verified %d %ss", verified, group.kind)
	}

	if err := response.BuildAttestationList(requestData.Attestations); err != nil {
		return response.VerificationFailed(fmt.Sprintf("failed to create attestation list: %v", err.Error()))
	}
	v.logger.Debugf("built attestation list %v", response.GetImageList())

	if err := rv.verifyAttestations(ctx, response); err != nil {
		return response.VerificationFailed(fmt.Sprintf("failed to verify attestations: %v", err.Error()))
	}

	return response.VerificationSucceeded("")
}

// requestVerifier carries what is resolved once per request.
type requestVerifier struct {
	*verifier
	notationVerifier *notation.Verifier
	trustPolicy      string
	keychain         authn.Keychain
	remoteOpts       []gcrremote.Option
	pluginConfig     map[string]string
}

func (rv *requestVerifier) verifyAttestations(ctx context.Context, response Response) error {
	rv.logger.Debugf("verifying attestations %v", response.GetImageList())
	for image, list := range response.GetImageList() {
		if err := rv.verifyAttestation(ctx, image, list); err != nil {
			return fmt.Errorf("failed to verify attestations: %w", err)
		}
	}
	return nil
}

func (rv *requestVerifier) verifyAttestation(ctx context.Context, image string, attestationList types.AttestationList) error {
	rv.logger.Debugf("verifying attestation, image=%s; attestations=%v", image, attestationList)
	if len(attestationList) == 0 {
		return nil
	}

	if found := rv.checkAllAttestationsForImage(image, attestationList); found {
		return nil
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		return fmt.Errorf("failed to parse image reference: %s: %w", image, err)
	}

	refDesc, err := gcrremote.Head(ref, rv.remoteOpts...)
	if err != nil {
		return fmt.Errorf("failed to get gcr remote head: %w", err)
	}

	referrers, err := gcrremote.Referrers(ref.Context().Digest(refDesc.Digest.String()), rv.remoteOpts...)
	if err != nil {
		return fmt.Errorf("failed to get gcr remote referrers: %w", err)
	}

	referrersDescs, err := referrers.IndexManifest()
	if err != nil {
		return err
	}

	if err := matchAttestations(image, attestationList, referrersDescs.Manifests); err != nil {
		return err
	}

	for _, referrer := range referrersDescs.Manifests {
		conditions, found := attestationList[referrer.ArtifactType]
		if !found {
			continue
		}

		if rv.cache.GetAttestation(rv.trustPolicy, image, referrer.ArtifactType, conditions) {
			rv.logger.Debugf("Entry for the attestation found in cache, skipping attestation image=%s; type=%s", image, referrer.ArtifactType)
			continue
		}

		rv.logger.Debugf("verifying attestation, image=%s; type=%s", image, referrer.ArtifactType)
		referrerRef := getReference(referrer.Digest.String(), ref)
		digest, err := rv.verifyReferences(ctx, referrerRef)
		if err != nil {
			return fmt.Errorf("failed to get referrer of artifact type %s %s %s: %w", ref.String(), referrer.Digest.String(), referrer.ArtifactType, err)
		}
		if len(digest) == 0 {
			rv.logger.Infof("Image reference has been skipped in the trust policy, image=%s", image)
			continue
		}

		if len(conditions) != 0 {
			if err := rv.verifyConditions(ref, referrer, conditions); err != nil {
				return fmt.Errorf("failed to verify conditions %s %s: %w", ref.String(), referrer.Digest.String(), err)
			}
		}

		if err := rv.cache.AddAttestation(rv.trustPolicy, image, referrer.ArtifactType, conditions); err != nil {
			return fmt.Errorf("failed to add attestation to the cache image=%s, digest=%s: %w", ref.String(), referrer.Digest.String(), err)
		}
	}

	return nil
}

func (rv *requestVerifier) verifyConditions(repoRef name.Reference, desc v1.Descriptor, conditions []kyvernov1.AnyAllConditions) error {
	rv.logger.Debugf("verifying conditions %s", repoRef.String())

	payload, err := rv.fetchAndExtractPayload(repoRef, desc)
	if err != nil {
		return err
	}

	// A fresh context per evaluation: Kyverno contexts are not safe for
	// concurrent use and checkpoints keep full copies of every payload.
	engineContext := enginecontext.NewContext(rv.jmespath)
	if err := enginecontext.AddJSONObject(engineContext, payload); err != nil {
		return fmt.Errorf("failed to add Statement to the context: %w", err)
	}

	val, msg, err := variables.EvaluateAnyAllConditions(zapr.NewLogger(rv.logger.Desugar()), engineContext, conditions)
	if err != nil {
		return err
	}
	if !val {
		return fmt.Errorf("failed to evaluate conditions: %s", msg)
	}
	rv.logger.Debugf("successfully verified condition for image %s", repoRef.String())
	return nil
}

func (rv *requestVerifier) fetchAndExtractPayload(repoRef name.Reference, desc v1.Descriptor) (map[string]interface{}, error) {
	ref := repoRef.Context().Digest(desc.Digest.String())

	manifestDesc, err := gcrremote.Get(ref, rv.remoteOpts...)
	if err != nil {
		return nil, fmt.Errorf("error in fetching statement: %w", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestDesc.Manifest, &manifest); err != nil {
		return nil, err
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no predicate found: %+v", manifest)
	}
	if len(manifest.Layers) > 1 {
		return nil, fmt.Errorf("multiple layers in predicate not supported: %+v", manifest)
	}
	payloadDesc := manifest.Layers[0]

	layer, err := gcrremote.Layer(repoRef.Context().Digest(payloadDesc.Digest.String()), rv.remoteOpts...)
	if err != nil {
		return nil, err
	}
	ioPredicate, err := layer.Uncompressed()
	if err != nil {
		return nil, err
	}
	defer func() { _ = ioPredicate.Close() }()

	predicate := make(map[string]interface{})
	if err := json.NewDecoder(ioPredicate).Decode(&predicate); err != nil {
		return nil, err
	}
	rv.logger.Debugf("successfully extracted payload for image %s", repoRef.String())
	return predicate, nil
}

func (rv *requestVerifier) verifyImageInfo(ctx context.Context, image types.ImageInfo) (*types.ImageInfo, error) {
	imgRef := image.String()

	if img, found := rv.cache.GetImage(rv.trustPolicy, imgRef); found {
		rv.logger.Debugf("Entry for the image found in cache, skipping image=%s; trustpolicy=%s", imgRef, rv.trustPolicy)
		image.Image = *img
		return &image, nil
	}

	rv.logger.Debugf("Entry not found in the cache, verifying image=%s", imgRef)
	digest, err := rv.verifyReferences(ctx, imgRef)
	if err != nil {
		rv.logger.Errorf("verification failed for image %s: %v", imgRef, err)
		return nil, fmt.Errorf("failed to verify image %s: %w", imgRef, err)
	}

	image.Digest = digest
	if err := rv.cache.AddImage(rv.trustPolicy, imgRef, image.Image); err != nil {
		return nil, fmt.Errorf("failed to add image to the cache %s: %w", imgRef, err)
	}

	return &image, nil
}

func (rv *requestVerifier) verifyReferences(ctx context.Context, image string) (string, error) {
	rv.logger.Debugf("verifying image %s", image)
	repo, reference, err := rv.parseReferenceAndResolveDigest(image)
	if err != nil {
		return "", fmt.Errorf("failed to resolve digest: %w", err)
	}

	opts := notation.VerifyOptions{
		ArtifactReference:    reference.String(),
		MaxSignatureAttempts: rv.maxSignatureAttempts,
		PluginConfig:         rv.pluginConfig,
	}

	nctx := notationlog.WithLogger(ctx, notationlog.Discard)
	if rv.debug {
		nctx = notationlog.WithLogger(ctx, rv.logger)
	}

	desc, outcomes, err := notation.Verify(nctx, *rv.notationVerifier, repo, opts)
	if err != nil {
		rv.logger.Infof("Verification failed %v", err)
		return "", err
	}

	var errs []error
	for _, o := range outcomes {
		if o.Error != nil {
			errs = append(errs, o.Error)
		}
	}
	if len(errs) > 0 {
		return "", errors.Join(errs...)
	}

	rv.logger.Infof("successfully verified image %s digest %s", image, desc.Digest.String())
	return desc.Digest.String(), nil
}

// getPluginConfig returns a fresh copy of the notation plugin configuration.
func (v *verifier) getPluginConfig() (map[string]string, error) {
	pluginConfig := map[string]string{}
	if v.pluginConfigMap != "" {
		cm, err := v.configMapLister.Get(v.pluginConfigMap)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch plugin configmap %s: %w", v.pluginConfigMap, err)
		}

		for k, val := range cm.Data {
			pluginConfig[k] = val
		}
	}
	if v.debug {
		pluginConfig["debug"] = "true"
	}
	return pluginConfig, nil
}

func (v *verifier) getRemoteOpts(ctx context.Context, keychain authn.Keychain) ([]gcrremote.Option, error) {
	remoteOpts := []gcrremote.Option{
		gcrremote.WithAuthFromKeychain(keychain),
		gcrremote.WithContext(ctx),
	}

	// Share token exchanges between the calls of this request.
	puller, err := gcrremote.NewPuller(remoteOpts...)
	if err != nil {
		return nil, err
	}
	return append(remoteOpts, gcrremote.Reuse(puller)), nil
}

func (rv *requestVerifier) parseReferenceAndResolveDigest(ref string) (notationregistry.Repository, registry.Reference, error) {
	if !strings.Contains(ref, "/") {
		ref = "docker.io/library/" + ref
	}

	// Only look for a tag/digest in the last path element: "host:5000/repo" has no tag.
	if !strings.ContainsAny(ref[strings.LastIndex(ref, "/")+1:], ":@") {
		ref += ":latest"
	}

	parsedRef, err := registry.ParseReference(ref)
	if err != nil {
		return nil, registry.Reference{}, fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	authClient, err := rv.getAuthClient(parsedRef)
	if err != nil {
		return nil, registry.Reference{}, fmt.Errorf("failed to retrieve credentials: %w", err)
	}

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, registry.Reference{}, fmt.Errorf("failed to initialize repository: %w", err)
	}
	if authClient != nil {
		repo.Client = authClient
	}
	repository := notationregistry.NewRepository(repo)

	parsedRef, err = rv.resolveDigest(parsedRef)
	if err != nil {
		return nil, registry.Reference{}, fmt.Errorf("failed to resolve digest: %w", err)
	}

	return repository, parsedRef, nil
}

func (rv *requestVerifier) getAuthClient(ref registry.Reference) (*auth.Client, error) {
	authConfig, err := getAuthConfigFromKeychain(rv.keychain, ref)
	if err != nil {
		return nil, err
	}

	if authConfig == nil {
		return nil, nil
	}

	credential := auth.Credential{
		Username: authConfig.Username,
		Password: authConfig.Password,
		// docker's identitytoken is an OAuth2 refresh token, registrytoken a bearer access token.
		RefreshToken: authConfig.IdentityToken,
		AccessToken:  authConfig.RegistryToken,
	}

	authClient := &auth.Client{
		Credential: auth.StaticCredential(ref.Registry, credential),
		Cache:      auth.NewCache(),
		ClientID:   "notation",
	}

	authClient.SetUserAgent("kyverno.io")
	return authClient, nil
}

func (rv *requestVerifier) resolveDigest(ref registry.Reference) (registry.Reference, error) {
	if isDigestReference(ref.String()) {
		return ref, nil
	}

	// Resolve tag reference to digest reference.
	digest, err := rv.getDigest(ref.String())
	if err != nil {
		return registry.Reference{}, err
	}

	ref.Reference = digest
	return ref, nil
}

func isDigestReference(reference string) bool {
	parts := strings.SplitN(reference, "/", 2)
	if len(parts) == 1 {
		return false
	}

	index := strings.Index(parts[1], "@")
	return index != -1
}

func (rv *requestVerifier) getDigest(imageRef string) (string, error) {
	parsedRef, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %s, error: %w", imageRef, err)
	}
	desc, err := gcrremote.Get(parsedRef, rv.remoteOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to fetch image reference: %s, error: %w", imageRef, err)
	}
	if _, ok := parsedRef.(name.Digest); ok && parsedRef.Identifier() != desc.Digest.String() {
		return "", fmt.Errorf("digest mismatch, expected: %s, received: %s", parsedRef.Identifier(), desc.Digest.String())
	}
	return desc.Digest.String(), nil
}

func getReference(digest string, ref name.Reference) string {
	if len(digest) == 0 {
		return ref.String()
	}
	return ref.Context().RegistryStr() + "/" + ref.Context().RepositoryStr() + "@" + digest
}

func (v *verifier) getTrustPolicy(req *types.VerificationRequest) string {
	trustPolicy := req.TrustPolicy
	if len(trustPolicy) == 0 {
		trustPolicy = os.Getenv(types.EnvDefaultTrustPolicy)
	}
	return trustPolicy
}

func (rv *requestVerifier) checkAllAttestationsForImage(image string, attestationList types.AttestationList) bool {
	for attestation, condition := range attestationList {
		if found := rv.cache.GetAttestation(rv.trustPolicy, image, attestation, condition); !found {
			return false
		}
	}
	return true
}

func matchImageReferences(imageReferences []string, image string) bool {
	if len(imageReferences) == 0 {
		return true
	}

	for _, imageRef := range imageReferences {
		if wildcard.Match(imageRef, image) {
			return true
		}
	}

	return false
}

// matchAttestations verifies that all attestations in the policy are present in the image
func matchAttestations(image string, attestationList types.AttestationList, referrers []v1.Descriptor) error {
	// create a map because referrers can have same artifact type
	refList := make(map[string]bool)
	for _, referrer := range referrers {
		refList[referrer.ArtifactType] = true
	}

	// match attestation type list and referrers list
	for att := range attestationList {
		if _, found := refList[att]; !found {
			return fmt.Errorf("failed to find attestation %s in image %s", att, image)
		}
	}

	return nil
}
