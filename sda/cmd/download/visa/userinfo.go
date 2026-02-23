package visa

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	log "github.com/sirupsen/logrus"
)

const (
	maxUserinfoResponseSize  = 1 << 20 // 1MB
	maxDiscoveryResponseSize = 1 << 16 // 64KB
	httpClientTimeout        = 10 * time.Second
)

// UserinfoClient handles fetching userinfo from OIDC providers.
type UserinfoClient struct {
	httpClient  *http.Client
	userinfoURL string
	cache       *ristretto.Cache
	cacheTTL    time.Duration
	mu          sync.RWMutex
}

// NewUserinfoClient creates a new userinfo client.
// If userinfoURL is empty, it must be discovered from the OIDC issuer.
func NewUserinfoClient(userinfoURL string, cacheTTL time.Duration) (*UserinfoClient, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     10000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo cache: %w", err)
	}

	// TODO: The CheckRedirect logic below is duplicated in 3 places (here, DiscoverUserinfoURL,
	// and jwks_cache.go). If a 4th HTTP client is added, consider extracting a shared
	// restrictedRedirectPolicy() helper to reduce duplication and ensure consistent security policy.
	return &UserinfoClient{
		httpClient: &http.Client{
			Timeout: httpClientTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 0 && req.URL.Host != via[0].URL.Host {
					return fmt.Errorf("userinfo redirect to different host not allowed: %s", req.URL.Host)
				}
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}

				return nil
			},
		},
		userinfoURL: userinfoURL,
		cache:       cache,
		cacheTTL:    cacheTTL,
	}, nil
}

// FetchUserinfo calls the userinfo endpoint with the given access token.
// Results are cached by token hash.
func (uc *UserinfoClient) FetchUserinfo(accessToken string) (*UserinfoResponse, error) {
	// Check cache
	tokenHash := hashToken(accessToken)
	if val, found := uc.cache.Get(tokenHash); found {
		if resp, ok := val.(*UserinfoResponse); ok {
			log.Debug("userinfo cache hit")

			return resp, nil
		}
	}

	uc.mu.RLock()
	endpoint := uc.userinfoURL
	uc.mu.RUnlock()

	if endpoint == "" {
		return nil, errors.New("userinfo endpoint URL not configured")
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	// Check content type - only support JSON
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/jwt") {
		return nil, errors.New("unsupported userinfo content-type: application/jwt; only application/json is supported")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUserinfoResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read userinfo response: %w", err)
	}

	var userinfo UserinfoResponse
	if err := json.Unmarshal(body, &userinfo); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	if userinfo.Sub == "" {
		return nil, errors.New("userinfo response missing 'sub' claim")
	}

	// Cache the result
	uc.cache.SetWithTTL(tokenHash, &userinfo, 1, uc.cacheTTL)

	return &userinfo, nil
}

// DiscoverUserinfoURL discovers the userinfo endpoint from an OIDC issuer.
func DiscoverUserinfoURL(issuerURL string) (string, error) {
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"

	client := &http.Client{
		Timeout: httpClientTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("OIDC discovery redirect to different host not allowed: %s", req.URL.Host)
			}
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}

			return nil
		},
	}

	resp, err := client.Get(discoveryURL)
	if err != nil {
		return "", fmt.Errorf("OIDC discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OIDC discovery returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryResponseSize))
	if err != nil {
		return "", fmt.Errorf("failed to read discovery response: %w", err)
	}

	var discovery struct {
		UserinfoEndpoint string `json:"userinfo_endpoint"`
	}
	if err := json.Unmarshal(body, &discovery); err != nil {
		return "", fmt.Errorf("failed to parse discovery response: %w", err)
	}

	if discovery.UserinfoEndpoint == "" {
		return "", errors.New("discovery response missing userinfo_endpoint")
	}

	return discovery.UserinfoEndpoint, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))

	return hex.EncodeToString(h[:])
}
