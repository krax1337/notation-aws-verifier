// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: image pull secrets are turned into a keychain
// with authn/kubernetes instead of k8schain (which appended an uncached ECR,
// GCR and ACR helper to every per-request keychain); the keychain is built
// once per request; secret names are trimmed.

package verifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	kauth "github.com/google/go-containerregistry/pkg/authn/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"oras.land/oras-go/v2/registry"
)

// getKeychain returns the keychain used for one request: the docker config of
// the process, the configured image pull secrets and the provider keychain.
func (v *verifier) getKeychain(ctx context.Context) (authn.Keychain, error) {
	keychains := []authn.Keychain{authn.DefaultKeychain}
	if v.imagePullSecrets != "" {
		secretKeychain, err := v.getKeychainFromSecret(ctx)
		if err != nil {
			return nil, err
		}
		keychains = append(keychains, secretKeychain)
	}

	if v.providerKeychain != nil {
		keychains = append(keychains, v.providerKeychain)
	}

	return authn.NewMultiKeychain(keychains...), nil
}

func (v *verifier) getKeychainFromSecret(ctx context.Context) (authn.Keychain, error) {
	v.logger.Debugf("fetching credentials from secrets %s", v.imagePullSecrets)
	var secrets []corev1.Secret
	for _, imagePullSecret := range strings.Split(v.imagePullSecrets, ",") {
		imagePullSecret = strings.TrimSpace(imagePullSecret)
		if imagePullSecret == "" {
			continue
		}
		secret, err := v.secretLister.Get(imagePullSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to get image pull secret %s: %w", imagePullSecret, err)
		}

		secrets = append(secrets, *secret)
	}

	return kauth.NewFromPullSecrets(ctx, secrets)
}

type imageResource struct {
	ref registry.Reference
}

func (ir *imageResource) String() string {
	return ir.ref.String()
}

func (ir *imageResource) RegistryStr() string {
	return ir.ref.Registry
}

func getAuthConfigFromKeychain(keychain authn.Keychain, ref registry.Reference) (*authn.AuthConfig, error) {
	authenticator, err := keychain.Resolve(&imageResource{ref})
	if err != nil {
		return nil, err
	}

	authConfig, err := authenticator.Authorization()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth config for %s: %w", ref.String(), err)
	}

	return authConfig, nil
}
