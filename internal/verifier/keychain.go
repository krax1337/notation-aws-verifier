// Copyright 2026 krax1337. Licensed under the Apache License, Version 2.0.

package verifier

import (
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
)

const (
	// providerCredentialsTTL is how long credentials resolved by the provider
	// keychain are reused. ECR authorization tokens are valid for 12 hours.
	providerCredentialsTTL = 15 * time.Minute

	// maxCachedRegistries bounds the credential cache.
	maxCachedRegistries = 256
)

type cachedCredential struct {
	config  authn.AuthConfig
	expires time.Time
}

// cachingKeychain memoizes the credentials an underlying keychain resolves for
// a registry.
//
// The ECR credential helper builds a new AWS config, credential provider chain,
// HTTP transport and ECR client on every Resolve, which means an STS/IMDS round
// trip plus new TLS connections (kept idle for 90s) for every registry lookup.
// Admission requests resolve credentials several times each, so without this
// cache memory and connections grow with the request rate.
type cachingKeychain struct {
	inner authn.Keychain
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]cachedCredential
}

func newCachingKeychain(inner authn.Keychain, ttl time.Duration) *cachingKeychain {
	return &cachingKeychain{
		inner:   inner,
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]cachedCredential),
	}
}

func (k *cachingKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	key := target.RegistryStr()
	now := k.now()

	k.mu.Lock()
	entry, ok := k.entries[key]
	k.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return authn.FromConfig(entry.config), nil
	}

	authenticator, err := k.inner.Resolve(target)
	if err != nil {
		return nil, err
	}
	// Anonymous means "no credentials for this registry"; do not pin that, the
	// next keychain or a later attempt may succeed.
	if authenticator == authn.Anonymous {
		return authenticator, nil
	}
	config, err := authenticator.Authorization()
	if err != nil {
		return nil, err
	}

	k.mu.Lock()
	if len(k.entries) >= maxCachedRegistries {
		for r, e := range k.entries {
			if !now.Before(e.expires) {
				delete(k.entries, r)
			}
		}
		if len(k.entries) >= maxCachedRegistries {
			clear(k.entries)
		}
	}
	k.entries[key] = cachedCredential{config: *config, expires: now.Add(k.ttl)}
	k.mu.Unlock()

	return authn.FromConfig(*config), nil
}
