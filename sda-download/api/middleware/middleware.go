package middleware

import (
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/pkg/auth"
	log "github.com/sirupsen/logrus"
)

// requestContextKey holds a name for the request context storage key
// which is used to store and get the permissions after passing middleware
const requestContextKey = "requestContextKey"

// TokenMiddleware performs access token verification and validation
// JWTs are verified and validated by the app, opaque tokens are sent to AAI for verification
// Successful auth results in list of authorised datasets.
// The datasets are stored into a session cache for subsequent requests, and also
// to the current request context for use in the endpoints.
func TokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if dataset permissions are cached to session
		sessionCookie, err := c.Cookie(config.Config.Session.Name)
		if err != nil {
			log.Debugf("no session cookie received")
		}
		var cache session.Cache
		var exists bool
		if sessionCookie != "" {
			log.Debug("session cookie received")
			cache, exists = session.Get(sessionCookie)
		}

		if !exists {
			log.Debug("no session found, create new session")

			// Check that a token is provided
			token, code, err := auth.GetToken(c.Request.Header)
			if err != nil {
				c.String(code, err.Error())
				c.AbortWithStatus(code)

				return
			}

			// Verify token by attempting to retrieve visas from AAI
			visas, err := auth.GetVisas(auth.Details, token)
			if err != nil {
				log.Debug("failed to validate token at AAI")
				c.String(http.StatusUnauthorized, "get visas failed")
				c.AbortWithStatus(code)

				return
			}

			// Get permissions
			// This used to cause a "404 no datasets found", but now the error has been moved deeper:
			// 200 OK with [] empty dataset list, when listing datasets (use case for sda-filesystem download tool)
			// 404 dataset not found, when listing files from a dataset
			// 401 unauthorised, when downloading a file
			cache.Datasets = auth.GetPermissions(*visas)

			// Start a new session and store datasets under the session key
			key := session.NewSessionKey()
			session.Set(key, cache)
			c.SetCookie(config.Config.Session.Name, // name
				key, // value
				int(config.Config.Session.Expiration)/1e9, // max age
				"/",                            // path
				config.Config.Session.Domain,   // domain
				config.Config.Session.Secure,   // secure
				config.Config.Session.HTTPOnly, // httpOnly
			)
			log.Debug("authorization check passed")
		}

		// Store dataset list to request context, for use in the endpoint handlers
		log.Debugf("storing %v to request context", cache)
		c.Set(requestContextKey, cache)
	}
}

// ClientVersionMiddleware checks for the required "SDA-Client-Version" header.
// It aborts the request with 412 (Precondition Failed) if the header is missing or
// if the version does not meet the minimum required version.
func ClientVersionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		const headerName = "SDA-Client-Version"
		clientVersionStr := c.GetHeader(headerName)

		// 1. Check if the header is present
		if clientVersionStr == "" {
			errorMessage := fmt.Sprintf(
				"Error: Missing required header '%s'. Please ensure you are using the latest sda-cli client.",
				headerName,
			)
			log.Warnf("request blocked (412): Missing required header '%s'", headerName)
			c.String(http.StatusPreconditionFailed, errorMessage)
			c.AbortWithStatus(http.StatusPreconditionFailed)

			return
		}

		// Parse the client's provided version (using the processed string)
		clientVersion, err := semver.NewVersion(clientVersionStr)
		if err != nil {
			log.Warnf("request blocked (412): processed client version header '%s' is not a valid semantic version: %v", clientVersionStr, err)
			errorMessage := fmt.Sprintf(
				"Error: Your sda-cli client version '%s' is invalid. Required minimum version is '%s'.",
				clientVersionStr,
				config.Config.App.MinimalCliVersionStr,
			)
			c.String(http.StatusPreconditionFailed, errorMessage)
			c.AbortWithStatus(http.StatusPreconditionFailed)

			return
		}

		// 2. Check if the client version is sufficient (clientVersion >= requiredVersion)
		if clientVersion.LessThan(config.Config.App.MinimalCliVersion) {
			errorMessage := fmt.Sprintf(
				"Error: Your sda-cli client version '%s' is insufficient. Please update to at least version '%s' to proceed.",
				clientVersionStr,
				config.Config.App.MinimalCliVersionStr,
			)
			log.Warnf("request blocked (412): Insufficient client version '%s'. Required minimum '%s'", clientVersionStr, config.Config.App.MinimalCliVersionStr)
			c.String(http.StatusPreconditionFailed, errorMessage)
			c.AbortWithStatus(http.StatusPreconditionFailed)

			return
		}

		// Version is correct, proceed to the next handler/middleware
		log.Debugf("client version check passed: %s", clientVersionStr)
	}
}

// ChainDefaultMiddleware chains the TokenMiddleware and ClientVersionMiddleware,
// prioritizing authentication before checking client version requirements.
// It is intended to be the default composite middleware set for the application.
func ChainDefaultMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Run the Token Middleware. This will abort if authentication fails (HTTP 401)
		TokenMiddleware()(c)

		if c.IsAborted() {
			return
		}

		// Run the Client Version Check (only if token check passed). This will abort if version is invalid/missing (HTTP 412)
		ClientVersionMiddleware()(c)

		if c.IsAborted() {
			return
		}

		c.Next()
	}
}

// GetCacheFromContext is a helper function that endpoints can use to get data
// stored to the *current* request context (not the session storage).
// The request context was populated by the middleware, which in turn uses the session storage.
var GetCacheFromContext = func(c *gin.Context) session.Cache {
	var cache session.Cache
	cached, exists := c.Get(requestContextKey)
	if exists {
		cache = cached.(session.Cache)
	}
	log.Debugf("returning %v from request context", cached)

	return cache
}
