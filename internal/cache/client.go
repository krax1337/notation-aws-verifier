// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: migrated to ristretto/v2; the cache is now
// bounded by maxSize entries (it used MaxCost=1GiB with per-item cost 0, so
// --cacheMaxSize never limited anything); a dropped Set is no longer an error;
// per-lookup logging moved to debug level.

// Package cache is a bounded TTL cache for verification outcomes.
package cache

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"go.uber.org/zap"

	"github.com/krax1337/notation-aws-verifier/internal/types"
)

type Cache interface {
	AddImage(trustPolicy string, imageRef string, result types.Image) error

	GetImage(trustPolicy string, imageRef string) (*types.Image, bool)

	AddAttestation(trustPolicy string, imageRef string, attestationType string, conditions []kyvernov1.AnyAllConditions) error

	GetAttestation(trustPolicy string, imageRef string, attestationType string, conditions []kyvernov1.AnyAllConditions) bool

	Clear()

	// Close stops the background goroutines of the cache.
	Close()
}

const (
	defaultMaxSize = 1000
	defaultTTL     = time.Hour
)

// attestationVerified is the value stored for verified attestations.
type attestationVerified struct{}

type cache struct {
	log       *zap.SugaredLogger
	useCache  bool
	ttl       time.Duration
	maxSize   int64
	ristretto *ristretto.Cache[string, any]
}

type Option = func(*cache) error

func New(options ...Option) (Cache, error) {
	c := &cache{
		ttl:     defaultTTL,
		maxSize: defaultMaxSize,
	}
	for _, opt := range options {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if c.maxSize <= 0 {
		c.maxSize = defaultMaxSize
	}
	if c.ttl <= 0 {
		c.ttl = defaultTTL
	}
	if c.log == nil {
		c.log = zap.NewNop().Sugar()
	}

	// Every entry costs 1, so MaxCost is the maximum number of entries.
	r, err := ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters:        c.maxSize * 10,
		MaxCost:            c.maxSize,
		BufferItems:        64,
		IgnoreInternalCost: true,
	})
	if err != nil {
		return nil, err
	}
	c.ristretto = r

	return c, nil
}

func WithCacheEnabled(b bool) Option {
	return func(c *cache) error {
		c.useCache = b
		return nil
	}
}

func WithMaxSize(s int64) Option {
	return func(c *cache) error {
		c.maxSize = s
		return nil
	}
}

func WithTTLDuration(t time.Duration) Option {
	return func(c *cache) error {
		c.ttl = t
		return nil
	}
}

func WithLogger(l *zap.SugaredLogger) Option {
	return func(c *cache) error {
		c.log = l
		return nil
	}
}

func (c *cache) AddImage(trustPolicy string, imageRef string, result types.Image) error {
	if !c.useCache {
		return nil
	}

	key := createImageKey(trustPolicy, imageRef)
	c.log.Debugw("adding image to the cache", "key", key)
	if ok := c.ristretto.SetWithTTL(key, result, 1, c.ttl); !ok {
		// ristretto drops sets under contention; that only costs a future re-verification.
		c.log.Debugw("cache entry dropped", "key", key)
	}
	return nil
}

func (c *cache) GetImage(trustPolicy string, imageRef string) (*types.Image, bool) {
	if !c.useCache {
		return nil, false
	}

	key := createImageKey(trustPolicy, imageRef)
	entry, ok := c.ristretto.Get(key)
	if !ok {
		c.log.Debugw("image not found in the cache", "key", key)
		return nil, false
	}

	val, ok := entry.(types.Image)
	if !ok {
		c.log.Debugw("invalid image entry in the cache", "key", key)
		return nil, false
	}
	c.log.Debugw("image found in the cache", "key", key)
	return &val, true
}

func (c *cache) AddAttestation(trustPolicy string, imageRef string, attestationType string, conditions []kyvernov1.AnyAllConditions) error {
	if !c.useCache {
		return nil
	}

	key, err := createAttestationKey(trustPolicy, imageRef, attestationType, conditions)
	if err != nil {
		return fmt.Errorf("failed to create attestation cache key: %w", err)
	}

	c.log.Debugw("adding attestation to the cache", "key", key)
	if ok := c.ristretto.SetWithTTL(key, attestationVerified{}, 1, c.ttl); !ok {
		c.log.Debugw("cache entry dropped", "key", key)
	}
	return nil
}

func (c *cache) GetAttestation(trustPolicy string, imageRef string, attestationType string, conditions []kyvernov1.AnyAllConditions) bool {
	if !c.useCache {
		return false
	}

	key, err := createAttestationKey(trustPolicy, imageRef, attestationType, conditions)
	if err != nil {
		c.log.Debugw("failed to create attestation cache key", "error", err)
		return false
	}
	_, found := c.ristretto.Get(key)
	c.log.Debugw("attestation cache lookup", "key", key, "found", found)
	return found
}

func (c *cache) Clear() {
	c.ristretto.Clear()
}

func (c *cache) Close() {
	c.ristretto.Close()
}

func createImageKey(trustPolicy string, imageRef string) string {
	return trustPolicy + ";" + imageRef
}

func createAttestationKey(trustPolicy string, imageRef string, attestationType string, conditions []kyvernov1.AnyAllConditions) (string, error) {
	c, err := json.Marshal(conditions)
	if err != nil {
		return "", err
	}
	return trustPolicy + ";" + imageRef + ";" + attestationType + ";" + string(c), nil
}
