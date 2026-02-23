package visa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/lestrrat-go/jwx/v2/jwk"
	log "github.com/sirupsen/logrus"
)

const maxJWKSResponseSize = 512 * 1024 // 512KB

// JWKSCache caches JWKS fetched from trusted JKU URLs.
// It enforces the trusted issuer allowlist and limits on JKU fetches per request.
type JWKSCache struct {
	trusted    []TrustedIssuer
	cache      *ristretto.Cache
	cacheTTL   time.Duration
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewJWKSCache creates a new JWKS cache with the given trusted issuers.
func NewJWKSCache(trusted []TrustedIssuer, cacheTTL time.Duration) (*JWKSCache, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     10000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS cache: %w", err)
	}

	return &JWKSCache{
		trusted:  trusted,
		cache:    cache,
		cacheTTL: cacheTTL,
		httpClient: &http.Client{
			Timeout: httpClientTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 0 && req.URL.Host != via[0].URL.Host {
					return fmt.Errorf("JWKS redirect to different host not allowed: %s", req.URL.Host)
				}
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}

				return nil
			},
		},
	}, nil
}

// GetKeySet returns the JWKS for the given issuer+JKU pair.
// It validates the pair is trusted, checks the cache, and fetches if needed.
// The jkuTracker is used to enforce per-request limits on distinct JKU fetches.
func (jc *JWKSCache) GetKeySet(issuer, jkuURL string, jkuTracker map[string]bool, maxJKU int) (jwk.Set, error) {
	// Validate the issuer+JKU pair is trusted
	if !IsTrusted(jc.trusted, issuer, jkuURL) {
		return nil, fmt.Errorf("untrusted issuer+JKU pair: iss=%s, jku=%s", issuer, jkuURL)
	}

	normalizedJKU := NormalizeURL(jkuURL)

	// Check cache
	if val, found := jc.cache.Get(normalizedJKU); found {
		if ks, ok := val.(jwk.Set); ok {
			log.Debugf("JWKS cache hit for %s", normalizedJKU)

			return ks, nil
		}
	}

	// Check per-request JKU limit and track attempt BEFORE fetching.
	// This prevents unlimited retry attempts when fetches fail, as failed
	// attempts still count against the limit.
	if !jkuTracker[normalizedJKU] {
		if len(jkuTracker) >= maxJKU {
			return nil, fmt.Errorf("exceeded max JWKS fetches per request (%d)", maxJKU)
		}
		// Mark as attempted before fetch to count against limit even on failure
		jkuTracker[normalizedJKU] = true
	}

	// Fetch JWKS
	keySet, err := jc.fetchJWKS(normalizedJKU)
	if err != nil {
		return nil, err
	}

	// Cache the result
	jc.cache.SetWithTTL(normalizedJKU, keySet, 1, jc.cacheTTL)

	return keySet, nil
}

func (jc *JWKSCache) fetchJWKS(jkuURL string) (jwk.Set, error) {
	jc.mu.RLock()
	defer jc.mu.RUnlock()

	log.Debugf("fetching JWKS from %s", jkuURL)

	req, err := http.NewRequestWithContext(context.Background(), "GET", jkuURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := jc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("JWKS fetch failed for %s: %w", jkuURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS fetch returned status %d for %s", resp.StatusCode, jkuURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response from %s: %w", jkuURL, err)
	}

	keySet, err := jwk.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWKS from %s: %w", jkuURL, err)
	}

	log.Debugf("fetched JWKS from %s with %d keys", jkuURL, keySet.Len())

	return keySet, nil
}
