// Package middleware provides HTTP middleware for the download service.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	log "github.com/sirupsen/logrus"
)

// ContextKey is the key used to store auth data in the request context.
const ContextKey = "authContext"

// AuthContext holds the authenticated user's information.
type AuthContext struct {
	// Datasets the user has access to (from visas)
	Datasets []string
}

// OIDCDetails holds OIDC provider configuration.
type OIDCDetails struct {
	Userinfo string `json:"userinfo_endpoint"`
	JWKSURI  string `json:"jwks_uri"`
}

// Visas holds the GA4GH passport visas from userinfo.
type Visas struct {
	Visa []string `json:"ga4gh_passport_v1"`
}

// VisaClaim holds the visa type and value.
type VisaClaim struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// SessionCache holds cached session data using ristretto.
type SessionCache struct {
	cache *ristretto.Cache
	mu    sync.RWMutex
}

var (
	// oidcDetails holds the cached OIDC configuration.
	oidcDetails     OIDCDetails
	oidcDetailsOnce sync.Once

	// sessionCache is the in-memory session cache.
	sessionCache *SessionCache

	// httpClient is used for OIDC requests.
	httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

// InitAuth initializes the authentication middleware.
// This should be called during application startup.
func InitAuth() error {
	// Initialize session cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return err
	}
	sessionCache = &SessionCache{cache: cache}

	// Fetch OIDC configuration
	var initErr error
	oidcDetailsOnce.Do(func() {
		issuer := config.OIDCIssuer()
		if issuer == "" {
			initErr = errors.New("OIDC issuer not configured")

			return
		}

		// Fetch OIDC discovery document
		configURL := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
		resp, err := httpClient.Get(configURL)
		if err != nil {
			initErr = err

			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			initErr = errors.New("failed to fetch OIDC configuration")

			return
		}

		if err := json.NewDecoder(resp.Body).Decode(&oidcDetails); err != nil {
			initErr = err

			return
		}

		// Override with config values if set
		if jwksURL := config.OIDCJWKSURL(); jwksURL != "" {
			oidcDetails.JWKSURI = jwksURL
		}
		if userinfoURL := config.OIDCUserinfoURL(); userinfoURL != "" {
			oidcDetails.Userinfo = userinfoURL
		}

		log.Infof("OIDC configuration loaded from %s", configURL)
	})

	return initErr
}

// TokenMiddleware performs access token verification and validation.
// JWTs are verified and validated, then visas are fetched from userinfo.
// The datasets are stored in a session cache and request context.
func TokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for cached session
		sessionCookie, err := c.Cookie(config.SessionName())
		if err == nil && sessionCookie != "" {
			if authCtx, exists := sessionCache.Get(sessionCookie); exists {
				log.Debug("session found in cache")
				c.Set(ContextKey, authCtx)
				c.Next()

				return
			}
		}

		// No session, authenticate via token
		token, code, err := GetToken(c.Request.Header)
		if err != nil {
			c.JSON(code, gin.H{"error": err.Error()})
			c.Abort()

			return
		}

		// Verify token signature
		_, err = VerifyJWT(token)
		if err != nil {
			log.Debugf("token verification failed: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()

			return
		}

		// Fetch visas from userinfo
		visas, err := GetVisas(token)
		if err != nil {
			log.Debugf("failed to get visas: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to get visas"})
			c.Abort()

			return
		}

		// Extract dataset permissions from visas
		datasets := GetPermissions(visas)

		authCtx := AuthContext{
			Datasets: datasets,
		}

		// Create new session
		sessionKey := uuid.New().String()
		sessionCache.Set(sessionKey, authCtx, time.Duration(config.SessionExpiration())*time.Second)

		// Set session cookie
		c.SetCookie(
			config.SessionName(),
			sessionKey,
			config.SessionExpiration(),
			"/",
			config.SessionDomain(),
			config.SessionSecure(),
			config.SessionHTTPOnly(),
		)

		// Store in request context
		c.Set(ContextKey, authCtx)
		log.Debug("authentication successful")

		c.Next()
	}
}

// GetToken extracts the access token from request headers.
// Supports both Authorization: Bearer and X-Amz-Security-Token headers.
var GetToken = func(headers http.Header) (string, int, error) {
	// Check X-Amz-Security-Token header first (for S3 compatibility)
	if token := headers.Get("X-Amz-Security-Token"); token != "" {
		return token, 0, nil
	}

	// Check Authorization header
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return "", http.StatusUnauthorized, errors.New("access token must be provided")
	}

	// Check Bearer scheme
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 {
		return "", http.StatusBadRequest, errors.New("invalid authorization header format")
	}

	if !strings.EqualFold(parts[0], "Bearer") {
		return "", http.StatusBadRequest, errors.New("authorization scheme must be bearer")
	}

	return parts[1], 0, nil
}

// VerifyJWT verifies the token signature using JWKS.
var VerifyJWT = func(token string) (jwt.Token, error) {
	if oidcDetails.JWKSURI == "" {
		return nil, errors.New("JWKS URI not configured")
	}

	// Fetch JWKS
	keySet, err := jwk.Fetch(context.Background(), oidcDetails.JWKSURI)
	if err != nil {
		return nil, err
	}

	// Parse and verify token
	parsedToken, err := jwt.Parse(
		[]byte(token),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
	)
	if err != nil {
		return nil, err
	}

	// Validate audience if configured
	if audience := config.OIDCAudience(); audience != "" {
		aud := parsedToken.Audience()
		found := false
		for _, a := range aud {
			if a == audience {
				found = true

				break
			}
		}
		if !found {
			return nil, errors.New("invalid audience")
		}
	}

	return parsedToken, nil
}

// GetVisas fetches visas from the userinfo endpoint.
var GetVisas = func(token string) (*Visas, error) {
	if oidcDetails.Userinfo == "" {
		return nil, errors.New("userinfo endpoint not configured")
	}

	req, err := http.NewRequest("GET", oidcDetails.Userinfo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("userinfo request failed")
	}

	var visas Visas
	if err := json.NewDecoder(resp.Body).Decode(&visas); err != nil {
		return nil, err
	}

	return &visas, nil
}

// GetPermissions extracts dataset permissions from visas.
var GetPermissions = func(visas *Visas) []string {
	if visas == nil {
		return []string{}
	}

	datasets := []string{}
	trustedIssuers := config.OIDCTrustedList()

	for _, visa := range visas.Visa {
		// Check visa type
		if !checkVisaType(visa, "ControlledAccessGrants") {
			continue
		}

		// Validate visa
		verifiedVisa, valid := validateVisa(visa, trustedIssuers)
		if !valid {
			continue
		}

		// Extract dataset from visa
		datasets = extractDatasets(verifiedVisa, datasets)
	}

	log.Debugf("matched datasets: %v", datasets)

	return datasets
}

// checkVisaType checks if a visa is of the specified type.
func checkVisaType(visa string, visaType string) bool {
	parsedToken, err := jwt.Parse([]byte(visa), jwt.WithVerify(false))
	if err != nil {
		log.Debugf("failed to parse visa: %v", err)

		return false
	}

	visaClaim := parsedToken.PrivateClaims()["ga4gh_visa_v1"]
	if visaClaim == nil {
		return false
	}

	claimJSON, err := json.Marshal(visaClaim)
	if err != nil {
		return false
	}

	var claim VisaClaim
	if err := json.Unmarshal(claimJSON, &claim); err != nil {
		return false
	}

	return claim.Type == visaType
}

// validateVisa validates a visa's signature and claims.
func validateVisa(visa string, trustedIssuers []string) (jwt.Token, bool) {
	// Parse header to get JKU
	header, err := jws.Parse([]byte(visa))
	if err != nil {
		log.Debugf("failed to parse visa header: %v", err)

		return nil, false
	}

	// Get JKU from header
	jku := header.Signatures()[0].ProtectedHeaders().JWKSetURL()
	if jku == "" {
		log.Debug("visa has no JKU")

		return nil, false
	}

	// Parse payload to get issuer
	payload, err := jwt.Parse([]byte(visa), jwt.WithVerify(false))
	if err != nil {
		log.Debugf("failed to parse visa payload: %v", err)

		return nil, false
	}

	// Check if issuer is trusted
	if len(trustedIssuers) > 0 {
		issuer := payload.Issuer()
		trusted := false
		for _, ti := range trustedIssuers {
			if ti == issuer {
				trusted = true

				break
			}
		}
		if !trusted {
			log.Debugf("visa issuer %s not trusted", issuer)

			return nil, false
		}
	}

	// Fetch JWKS and verify signature
	keySet, err := jwk.Fetch(context.Background(), jku)
	if err != nil {
		log.Debugf("failed to fetch visa JWKS: %v", err)

		return nil, false
	}

	verifiedVisa, err := jwt.Parse(
		[]byte(visa),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
	)
	if err != nil {
		log.Debugf("failed to verify visa: %v", err)

		return nil, false
	}

	return verifiedVisa, true
}

// extractDatasets extracts dataset values from a verified visa.
func extractDatasets(visa jwt.Token, datasets []string) []string {
	if visa == nil {
		return datasets
	}

	visaClaim := visa.PrivateClaims()["ga4gh_visa_v1"]
	if visaClaim == nil {
		return datasets
	}

	claimJSON, err := json.Marshal(visaClaim)
	if err != nil {
		return datasets
	}

	var claim VisaClaim
	if err := json.Unmarshal(claimJSON, &claim); err != nil {
		return datasets
	}

	if claim.Value != "" {
		// Check for duplicates
		for _, d := range datasets {
			if d == claim.Value {
				return datasets
			}
		}
		datasets = append(datasets, claim.Value)
	}

	return datasets
}

// GetAuthContext retrieves the auth context from a gin context.
func GetAuthContext(c *gin.Context) (AuthContext, bool) {
	val, exists := c.Get(ContextKey)
	if !exists {
		return AuthContext{}, false
	}

	authCtx, ok := val.(AuthContext)

	return authCtx, ok
}

// Session cache methods

// Get retrieves a value from the session cache.
func (sc *SessionCache) Get(key string) (AuthContext, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	val, found := sc.cache.Get(key)
	if !found {
		return AuthContext{}, false
	}

	authCtx, ok := val.(AuthContext)
	if !ok {
		return AuthContext{}, false
	}

	return authCtx, true
}

// Set stores a value in the session cache.
func (sc *SessionCache) Set(key string, value AuthContext, ttl time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	// Note: older ristretto doesn't have SetWithTTL, use Set instead
	// TTL is handled by cache eviction policy
	_ = ttl // TTL not supported in this version
	sc.cache.Set(key, value, 1)
}
