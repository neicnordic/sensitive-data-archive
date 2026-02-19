// Package middleware provides HTTP middleware for the download service.
package middleware

import (
	"context"
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
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
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
	// Subject is the user identifier from the JWT token
	Subject string
	// Datasets the user has access to (populated from database based on subject)
	Datasets []string
	// Token is the parsed JWT token
	Token jwt.Token
}

// Authenticator validates JWT tokens.
type Authenticator struct {
	Keyset jwk.Set
	mu     sync.RWMutex
}

// SessionCache holds cached session data using ristretto.
type SessionCache struct {
	cache *ristretto.Cache
	mu    sync.RWMutex
}

var (
	// auth is the global authenticator instance.
	auth *Authenticator

	// sessionCache is the in-memory session cache.
	sessionCache *SessionCache
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

	// Initialize authenticator with empty keyset
	auth = &Authenticator{
		Keyset: jwk.NewSet(),
	}

	// Load keys from path or URL
	pubKeyPath := config.JWTPubKeyPath()
	pubKeyURL := config.JWTPubKeyURL()

	if pubKeyPath == "" && pubKeyURL == "" {
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
		key := iter.Pair().Value.(jwk.Key)
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

// Authenticate verifies a token and returns the parsed JWT.
func (a *Authenticator) Authenticate(r *http.Request) (jwt.Token, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.Keyset.Len() == 0 {
		return nil, errors.New("no keys configured for token validation")
	}

	// Extract token from request
	tokenStr, _, err := GetToken(r.Header)
	if err != nil {
		return nil, err
	}

	// Parse and verify token
	// Use WithRequireKid(false) to allow matching keys without requiring key ID match
	// This is necessary when loading PEM keys that don't have explicit key IDs
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

	// Validate subject exists
	iss, err := url.ParseRequestURI(token.Issuer())
	if err != nil || iss.Hostname() == "" {
		return nil, fmt.Errorf("invalid issuer in token: %v", token.Issuer())
	}

	return token, nil
}

// TokenMiddleware performs access token verification and validation.
// The authenticated user's subject is stored in the request context.
// If db is provided and allow-all-data is disabled, the user's datasets are populated from the database.
func TokenMiddleware(db DatasetLookup) gin.HandlerFunc {
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
		token, err := auth.Authenticate(c.Request)
		if err != nil {
			log.Debugf("authentication failed: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()

			return
		}

		authCtx := AuthContext{
			Subject: token.Subject(),
			Token:   token,
		}

		// Populate datasets based on user's subject (data ownership model)
		// Skip if allow-all-data is enabled (for testing) or no database provided
		if db != nil && !config.JWTAllowAllData() {
			datasets, err := db.GetDatasetIDsByUser(c.Request.Context(), token.Subject())
			if err != nil {
				log.Warnf("failed to get datasets for user %s: %v", token.Subject(), err)
				// Don't fail authentication, just leave datasets empty
			} else {
				authCtx.Datasets = datasets
				log.Debugf("user %s has access to %d datasets", token.Subject(), len(datasets))
			}
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
		log.Debugf("authentication successful for subject: %s", token.Subject())

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

// Set stores a value in the session cache with the specified TTL.
func (sc *SessionCache) Set(key string, value AuthContext, ttl time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.cache.SetWithTTL(key, value, 1, ttl)
}
