package verifier

import (
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

type countingKeychain struct {
	calls int
	auth  authn.Authenticator
}

func (k *countingKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	k.calls++
	return k.auth, nil
}

func resolveUser(t *testing.T, kc authn.Keychain, registry string) string {
	t.Helper()
	reg, err := name.NewRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	a, err := kc.Resolve(reg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Username
}

func TestCachingKeychainReusesCredentialsUntilTTL(t *testing.T) {
	inner := &countingKeychain{auth: authn.FromConfig(authn.AuthConfig{Username: "AWS", Password: "token"})}
	now := time.Unix(0, 0)
	kc := newCachingKeychain(inner, time.Minute)
	kc.now = func() time.Time { return now }

	const ecrRegistry = "123456789012.dkr.ecr.eu-north-1.amazonaws.com"
	for range 3 {
		if got := resolveUser(t, kc, ecrRegistry); got != "AWS" {
			t.Fatalf("username = %q, want AWS", got)
		}
	}
	if inner.calls != 1 {
		t.Fatalf("inner keychain called %d times within TTL, want 1", inner.calls)
	}

	resolveUser(t, kc, "210987654321.dkr.ecr.us-east-1.amazonaws.com")
	if inner.calls != 2 {
		t.Fatalf("inner keychain called %d times for a second registry, want 2", inner.calls)
	}

	now = now.Add(time.Minute)
	resolveUser(t, kc, ecrRegistry)
	if inner.calls != 3 {
		t.Fatalf("inner keychain called %d times after TTL expiry, want 3", inner.calls)
	}
}

func TestCachingKeychainDoesNotCacheAnonymous(t *testing.T) {
	inner := &countingKeychain{auth: authn.Anonymous}
	kc := newCachingKeychain(inner, time.Hour)

	resolveUser(t, kc, "ghcr.io")
	resolveUser(t, kc, "ghcr.io")
	if inner.calls != 2 {
		t.Fatalf("inner keychain called %d times, want 2 (anonymous must not be cached)", inner.calls)
	}
}
