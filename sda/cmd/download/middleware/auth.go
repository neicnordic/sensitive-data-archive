// Package middleware provides HTTP middleware for the download service.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	log "github.com/sirupsen/logrus"
)

// ContextKey is the key used to store auth data in the request context.
const ContextKey = "authContext"

// DatasetLookup is an interface for looking up datasets by user.
// This allows the middleware to populate user datasets without depending on the full database package.
type DatasetLookup interface {
	// GetDatasetIDsByUser returns dataset IDs where the user is the data owner (submission_user).
	GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error)
}

// AuthContext holds the authenticated user's information.
type AuthContext struct {
	// Issuer is the token issuer (from JWT iss claim or configured OIDC issuer for opaque tokens)
	Issuer string
	// Subject is the user identifier from the JWT token or userinfo
	Subject string
	// OwnedDatasets are datasets where the user is the submission_user
	OwnedDatasets []string
	// VisaDatasets are datasets granted by GA4GH visas
	VisaDatasets []string
	// Datasets is the combined (deduplicated) union of OwnedDatasets and VisaDatasets
	Datasets []string
	// Token is the parsed JWT token (nil for opaque token auth)
	Token jwt.Token
	// AuthSource indicates how the user was authenticated ("jwt" or "userinfo")
	AuthSource string
}

// Authenticator validates JWT tokens.
type Authenticator struct {
	Keyset jwk.Set
	mu     sync.RWMutex
}

// SessionCache holds cached session data using ristretto.
// No mutex needed — ristretto is designed for lock-free concurrent access.
type SessionCache struct {
	cache *ristretto.Cache
}

var (
	// auth is the global authenticator instance.
	auth *Authenticator

	// sessionCache is the in-memory session cache.
	sessionCache *SessionCache

	// tokenCache is the token-keyed cache for non-cookie clients.
	tokenCache *SessionCache

	// legacyCookieName is the old cookie name for dual-read compatibility.
	legacyCookieName = "sda_session_key"
)

// InitAuthForTesting initializes the auth middleware with an empty keyset for testing.
// This allows middleware to run without valid keys, causing authentication to fail.
func InitAuthForTesting() error {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return err
	}
	sessionCache = &SessionCache{cache: cache}

	tokenCacheInst, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return err
	}
	tokenCache = &SessionCache{cache: tokenCacheInst}

	auth = &Authenticator{
		Keyset: jwk.NewSet(),
	}

	return nil
}

// InitAuth initializes the authentication middleware.
// This should be called during application startup.
// It loads JWT public keys from either a local path or remote JWKS URL.
func InitAuth() error {
	// Initialize session cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize session cache: %w", err)
	}
	sessionCache = &SessionCache{cache: cache}

	// Initialize token cache
	tokenCacheInst, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize token cache: %w", err)
	}
	tokenCache = &SessionCache{cache: tokenCacheInst}

	// Initialize authenticator with empty keyset
	auth = &Authenticator{
		Keyset: jwk.NewSet(),
	}

	// Load keys from path or URL
	pubKeyPath := config.JWTPubKeyPath()
	pubKeyURL := config.JWTPubKeyURL()

	if pubKeyPath == "" && pubKeyURL == "" {
		// When allow-opaque is enabled and no JWT keys are configured,
		// all tokens will go through userinfo. This is valid.
		if config.AuthAllowOpaque() {
			log.Warn("no JWT keys configured; all tokens will be treated as opaque")

			return nil
		}

		return errors.New("either jwt.pubkey-path or jwt.pubkey-url must be configured")
	}

	// Load from local path if configured
	if pubKeyPath != "" {
		if err := auth.loadKeysFromPath(pubKeyPath); err != nil {
			return fmt.Errorf("failed to load JWT keys from path: %w", err)
		}
		log.Infof("JWT keys loaded from path: %s", pubKeyPath)
	}

	// Load from URL if configured (can be used in addition to or instead of path)
	if pubKeyURL != "" {
		if err := auth.loadKeysFromURL(pubKeyURL); err != nil {
			return fmt.Errorf("failed to load JWT keys from URL: %w", err)
		}
		log.Infof("JWT keys loaded from URL: %s", pubKeyURL)
	}

	return nil
}

// loadKeysFromPath loads public keys from a directory containing PEM files.
func (a *Authenticator) loadKeysFromPath(keyPath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return filepath.Walk(keyPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			log.Debugf("Loading key from file: %s", path)

			keyData, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read key file %s: %w", path, err)
			}

			key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
			if err != nil {
				log.Warnf("failed to parse key from %s: %v (skipping)", path, err)

				return nil
			}

			if err := jwk.AssignKeyID(key); err != nil {
				return fmt.Errorf("failed to assign key ID: %w", err)
			}

			if err := a.Keyset.AddKey(key); err != nil {
				return fmt.Errorf("failed to add key to set: %w", err)
			}

			log.Debugf("Loaded key with ID: %s", key.KeyID())
		}

		return nil
	})
}

// loadKeysFromURL fetches keys from a JWKS endpoint.
func (a *Authenticator) loadKeysFromURL(jwksURL string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	parsedURL, err := url.ParseRequestURI(jwksURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("invalid JWKS URL: %s", jwksURL)
	}

	keySet, err := jwk.Fetch(context.Background(), jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Add all keys from the fetched keyset
	for iter := keySet.Keys(context.Background()); iter.Next(context.Background()); {
		key, ok := iter.Pair().Value.(jwk.Key)
		if !ok {
			log.Warn("unexpected key type in JWKS response, skipping")

			continue
		}
		if err := jwk.AssignKeyID(key); err != nil {
			log.Warnf("failed to assign key ID: %v", err)

			continue
		}
		if err := a.Keyset.AddKey(key); err != nil {
			log.Warnf("failed to add key: %v", err)

			continue
		}
	}

	return nil
}

// looksLikeJWT returns true if token has JWT structure:
// 3 dot-separated segments where segments 1+2 base64url-decode into valid JSON objects.
// This avoids misclassifying "dotty" opaque tokens (e.g., Azure AD opaque tokens
// that contain dots but aren't JWTs) as JWT-shaped.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	// Both header and payload must be valid base64url-encoded JSON objects
	for _, part := range parts[:2] {
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			return false
		}
		var obj map[string]any
		if err := json.Unmarshal(decoded, &obj); err != nil {
			return false
		}
	}

	return true
}

// verifyJWT validates a JWT token string against the loaded keyset.
func (a *Authenticator) verifyJWT(tokenStr string) (jwt.Token, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.Keyset.Len() == 0 {
		return nil, errors.New("no keys configured for token validation")
	}

	token, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKeySet(a.Keyset,
			jws.WithInferAlgorithmFromKey(true),
			jws.WithRequireKid(false),
		),
		jwt.WithValidate(true),
	)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	// Validate issuer if configured
	if issuer := config.OIDCIssuer(); issuer != "" {
		tokenIssuer := token.Issuer()
		if tokenIssuer != issuer && tokenIssuer != strings.TrimSuffix(issuer, "/") {
			return nil, fmt.Errorf("invalid issuer: got %s, expected %s", tokenIssuer, issuer)
		}
	}

	// Validate audience if configured
	if audience := config.OIDCAudience(); audience != "" {
		aud := token.Audience()
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

	// Validate issuer is a valid URI
	iss, err := url.ParseRequestURI(token.Issuer())
	if err != nil || iss.Hostname() == "" {
		return nil, fmt.Errorf("invalid issuer in token: %v", token.Issuer())
	}

	return token, nil
}

// Authenticate verifies a token and returns the parsed JWT.
// This is the legacy method that always tries JWT verification.
func (a *Authenticator) Authenticate(r *http.Request) (jwt.Token, error) {
	tokenStr, _, err := GetToken(r.Header)
	if err != nil {
		return nil, err
	}

	return a.verifyJWT(tokenStr)
}

// authResult holds the result of structure-based authentication.
type authResult struct {
	Issuer     string
	Subject    string
	AuthSource string
	Token      jwt.Token
}

// authenticateStructureBased performs structure-based authentication.
// JWT-shaped tokens are validated locally; opaque tokens go through userinfo.
func authenticateStructureBased(rawToken string, userinfoClient *visa.UserinfoClient) (*authResult, error) {
	if looksLikeJWT(rawToken) {
		// JWT-shaped: MUST validate locally, no userinfo fallback
		token, err := auth.verifyJWT(rawToken)
		if err != nil {
			return nil, fmt.Errorf("jwt validation failed: %w", err)
		}

		return &authResult{
			Issuer:     token.Issuer(),
			Subject:    token.Subject(),
			AuthSource: "jwt",
			Token:      token,
		}, nil
	}

	// Opaque token
	if !config.AuthAllowOpaque() {
		return nil, errors.New("opaque tokens not allowed")
	}

	// Userinfo fallback for opaque tokens only
	if userinfoClient == nil {
		return nil, errors.New("userinfo client not configured for opaque token auth")
	}

	userinfo, err := userinfoClient.FetchUserinfo(rawToken)
	if err != nil || userinfo.Sub == "" {
		if err != nil {
			return nil, fmt.Errorf("userinfo failed: %w", err)
		}

		return nil, errors.New("userinfo returned empty subject")
	}

	// For opaque tokens, issuer comes from configured OIDC issuer
	issuer := config.OIDCIssuer()

	return &authResult{
		Issuer:     issuer,
		Subject:    userinfo.Sub,
		AuthSource: "userinfo",
		Token:      nil,
	}, nil
}

// TokenMiddleware performs access token verification and validation.
// The authenticated user's subject is stored in the request context.
// If db is provided and allow-all-data is disabled, the user's datasets are populated from the database.
// If visaValidator is provided, GA4GH visa-based access is also computed based on permission.model.
func TokenMiddleware(db DatasetLookup, visaValidator *visa.Validator, auditLogger audit.Logger) gin.HandlerFunc {
	// Determine userinfo client for opaque token support.
	var userinfoClient *visa.UserinfoClient
	if config.AuthAllowOpaque() {
		if visaValidator != nil {
			userinfoClient = visaValidator.UserinfoClient()
		} else {
			// No visa validator — create a standalone userinfo client.
			userinfoURL := config.VisaUserinfoURL()
			if userinfoURL == "" && config.OIDCIssuer() != "" {
				discovered, err := visa.DiscoverUserinfoURL(config.OIDCIssuer())
				if err == nil {
					userinfoURL = discovered
				}
			}
			if userinfoURL != "" {
				uc, err := visa.NewUserinfoClient(userinfoURL, time.Duration(config.VisaCacheUserinfoTTL())*time.Second)
				if err == nil {
					userinfoClient = uc
				}
			}
			if userinfoClient == nil {
				log.Warn("auth.allow-opaque is enabled but no userinfo client could be created (configure visa.userinfo-url or oidc.issuer)")
			}
		}
	}

	auditDenied := func(c *gin.Context, status int) {
		auditLogger.Log(c.Request.Context(), audit.Event{
			Event:         "download.denied",
			CorrelationID: c.GetString("correlationId"),
			Endpoint:      c.Request.URL.Path,
			HTTPStatus:    status,
		})
	}

	return func(c *gin.Context) {
		// Check for cached session (try configured name, then legacy name)
		sessionCookie, err := c.Cookie(config.SessionName())
		if err != nil || sessionCookie == "" {
			// Try legacy cookie name for compatibility with sda-cli
			sessionCookie, _ = c.Cookie(legacyCookieName)
		}
		if sessionCookie != "" {
			if authCtx, exists := sessionCache.Get(sessionCookie); exists {
				log.Debug("session found in cache")
				c.Set(ContextKey, authCtx)
				c.Next()

				return
			}
		}

		// Extract raw token
		rawToken, _, err := GetToken(c.Request.Header)
		if err != nil {
			c.Header("Content-Type", "application/problem+json")
			c.JSON(http.StatusUnauthorized, gin.H{
				"title":  "Unauthorized",
				"status": http.StatusUnauthorized,
				"detail": "Missing, invalid, or expired bearer token.",
			})
			auditDenied(c, http.StatusUnauthorized)
			c.Abort()

			return
		}

		// Check token cache
		tokenKey := sha256Hex(rawToken)
		if cached, exists := tokenCache.Get(tokenKey); exists {
			log.Debug("token cache hit")
			c.Set(ContextKey, cached)
			c.Next()

			return
		}

		permModel := config.PermissionModel()
		needsVisa := visaValidator != nil && (permModel == "visa" || permModel == "combined")

		// Authenticate: structure-based if visa support is active, legacy otherwise
		var authCtx AuthContext
		if needsVisa || config.AuthAllowOpaque() {
			result, authErr := authenticateStructureBased(rawToken, userinfoClient)
			if authErr != nil {
				log.Debugf("authentication failed: %v", authErr)
				c.Header("Content-Type", "application/problem+json")
				c.JSON(http.StatusUnauthorized, gin.H{
					"title":  "Unauthorized",
					"status": http.StatusUnauthorized,
					"detail": "Missing, invalid, or expired bearer token.",
				})
				auditDenied(c, http.StatusUnauthorized)
				c.Abort()

				return
			}
			authCtx = AuthContext{
				Issuer:     result.Issuer,
				Subject:    result.Subject,
				Token:      result.Token,
				AuthSource: result.AuthSource,
			}
		} else {
			// Legacy JWT-only auth
			token, authErr := auth.Authenticate(c.Request)
			if authErr != nil {
				log.Debugf("authentication failed: %v", authErr)
				c.Header("Content-Type", "application/problem+json")
				c.JSON(http.StatusUnauthorized, gin.H{
					"title":  "Unauthorized",
					"status": http.StatusUnauthorized,
					"detail": "Missing, invalid, or expired bearer token.",
				})
				auditDenied(c, http.StatusUnauthorized)
				c.Abort()

				return
			}
			authCtx = AuthContext{
				Issuer:     token.Issuer(),
				Subject:    token.Subject(),
				Token:      token,
				AuthSource: "jwt",
			}
		}

		skipCache := false

		// Populate owned datasets (ownership model)
		if db != nil && !config.JWTAllowAllData() && (permModel == "ownership" || permModel == "combined") {
			datasets, err := db.GetDatasetIDsByUser(c.Request.Context(), authCtx.Subject)
			if err != nil {
				log.Warnf("failed to get datasets for user %s: %v", authCtx.Subject, err)
			} else {
				authCtx.OwnedDatasets = datasets
			}
		}

		// Populate visa datasets
		var visaMinExpiry *time.Time
		if needsVisa {
			datasets, minExp, visaErr := processVisas(c.Request.Context(), visaValidator, authCtx, rawToken)
			if visaErr != nil {
				log.Warnf("visa processing failed: %v", visaErr)
				if permModel == "visa" {
					c.Header("Content-Type", "application/problem+json")
					c.JSON(http.StatusUnauthorized, gin.H{
						"title":  "Unauthorized",
						"status": http.StatusUnauthorized,
						"detail": "Visa validation failed.",
					})
					auditDenied(c, http.StatusUnauthorized)
					c.Abort()

					return
				}
				// Combined mode: continue with ownership only, but don't cache
				skipCache = true
			} else {
				authCtx.VisaDatasets = datasets
				visaMinExpiry = minExp
			}
		}

		// Merge datasets (deduplicated union)
		authCtx.Datasets = mergeDatasets(authCtx.OwnedDatasets, authCtx.VisaDatasets)

		// Create session and token cache entries
		if !skipCache {
			tokenExp := tokenExpiry(authCtx)
			cacheTTL := computeCacheTTL(tokenExp, visaMinExpiry)

			// Guard against zero or negative TTL (expired token/visa)
			// Ristretto's behavior with TTL=0 is undefined, and caching
			// an already-expired auth makes no sense.
			if cacheTTL <= 0 {
				log.Debug("skipping cache: computed TTL is zero or negative (token/visa already expired)")
			} else {
				sessionKey := uuid.New().String()
				sessionCache.Set(sessionKey, authCtx, cacheTTL)

				// Set session cookie
				c.SetSameSite(http.SameSiteLaxMode)
				c.SetCookie(
					config.SessionName(),
					sessionKey,
					int(cacheTTL.Seconds()),
					"/",
					config.SessionDomain(),
					config.SessionSecure(),
					config.SessionHTTPOnly(),
				)

				// Token-keyed cache
				tokenCache.Set(tokenKey, authCtx, cacheTTL)
			}
		}

		// Store in request context
		c.Set(ContextKey, authCtx)
		log.Debugf("authentication successful for subject: %s (datasets: %d)", authCtx.Subject, len(authCtx.Datasets))

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

// Set stores a value in the session cache with the specified TTL.
func (sc *SessionCache) Set(key string, value AuthContext, ttl time.Duration) {
	sc.cache.SetWithTTL(key, value, 1, ttl)
}

// mergeDatasets returns a deduplicated union of two string slices.
func mergeDatasets(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	var result []string

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// processVisas calls the visa validator and extracts datasets and the earliest visa expiry.
// Returns (datasets, minExpiry, error). minExpiry is nil if no accepted visas had an expiry.
func processVisas(ctx context.Context, vv *visa.Validator, authCtx AuthContext, rawToken string) ([]string, *time.Time, error) {
	identity := visa.Identity{Issuer: authCtx.Issuer, Subject: authCtx.Subject}

	visaResult, err := vv.GetVisaDatasets(ctx, identity, rawToken, authCtx.AuthSource)
	if err != nil {
		return nil, nil, err
	}

	if visaResult == nil {
		return nil, nil, nil
	}

	for _, w := range visaResult.Warnings {
		log.Warn(w)
	}

	var minExp *time.Time
	if !visaResult.MinExpiry.IsZero() {
		minExp = &visaResult.MinExpiry
	}

	return visaResult.Datasets, minExp, nil
}

// sha256Hex returns the hex-encoded SHA-256 hash of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))

	return hex.EncodeToString(h[:])
}

// computeCacheTTL calculates the appropriate cache TTL bounded by token and visa expiry.
//
// Per GA4GH spec, cache TTL must never outlive visa validity:
//
//	JWT + accepted visas:    min(configTTL, token.exp - now, min(visa.exp) - now)
//	JWT + no accepted visas: min(configTTL, token.exp - now)
//	Opaque + accepted visas: min(configTTL, min(visa.exp) - now)
//	Opaque + no accepted visas: configTTL

// tokenExpiry extracts the JWT expiry from the auth context, or nil for opaque tokens.
func tokenExpiry(authCtx AuthContext) *time.Time {
	if authCtx.Token == nil {
		return nil
	}

	exp := authCtx.Token.Expiration()
	if exp.IsZero() {
		return nil
	}

	return &exp
}

func computeCacheTTL(tokenExp *time.Time, visaMinExpiry *time.Time) time.Duration {
	configTTL := time.Duration(config.VisaCacheTokenTTL()) * time.Second
	if configTTL == 0 {
		configTTL = time.Duration(config.SessionExpiration()) * time.Second
	}

	minTTL := configTTL
	now := time.Now()

	// Bound by JWT token expiry if available
	if tokenExp != nil {
		remaining := tokenExp.Sub(now)
		if remaining <= 0 {
			return 0
		}

		if remaining < minTTL {
			minTTL = remaining
		}
	}

	// Bound by earliest visa expiry (GA4GH requirement: never cache beyond visa validity)
	if visaMinExpiry != nil && !visaMinExpiry.IsZero() {
		remaining := visaMinExpiry.Sub(now)
		if remaining <= 0 {
			return 0
		}

		if remaining < minTTL {
			minTTL = remaining
		}
	}

	return minTTL
}
