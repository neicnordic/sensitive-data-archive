package main

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// oidcRedirect serves a request to /oidc and returns the
// query of the authorization URL the user is redirected to.
func oidcRedirect(t *testing.T, oidcConf config.OIDCConfig, requestURL string) url.Values {
	t.Helper()

	authHandler := AuthHandler{
		Config: config.AuthConf{OIDC: oidcConf},
		OAuth2Config: oauth2.Config{
			ClientID:    oidcConf.ID,
			RedirectURL: oidcConf.RedirectURL,
			Endpoint:    oauth2.Endpoint{AuthURL: oidcConf.Provider + "/authorize"},
		},
	}

	res := httptest.NewRecorder()
	authHandler.getOIDC(res, httptest.NewRequest(http.MethodGet, requestURL, nil))
	require.Equal(t, http.StatusFound, res.Code, "expected a redirect to the provider")

	location, err := url.Parse(res.Header().Get("Location"))
	require.NoError(t, err)

	return location.Query()
}

func TestGetOIDCRequestsAcrValues(t *testing.T) {
	oidcConf := config.OIDCConfig{
		ID:          "client",
		Provider:    "http://provider",
		RedirectURL: "http://redirect",
		AcrValues:   []string{"https://refeds.org/profile/mfa"},
	}

	query := oidcRedirect(t, oidcConf, "/oidc")
	assert.Equal(t, "https://refeds.org/profile/mfa", query.Get("acr_values"), "acr_values was not passed to the provider")
}

func TestGetOIDCJoinsMultipleAcrValues(t *testing.T) {
	oidcConf := config.OIDCConfig{
		ID:          "client",
		Provider:    "http://provider",
		RedirectURL: "http://redirect",
		AcrValues:   []string{"https://refeds.org/profile/mfa", "https://example.org/profile/hardware"},
	}

	query := oidcRedirect(t, oidcConf, "/oidc")
	assert.Equal(t, "https://refeds.org/profile/mfa https://example.org/profile/hardware", query.Get("acr_values"), "acr_values are space separated in a single parameter")
}

func TestGetOIDCWithoutAcrValues(t *testing.T) {
	oidcConf := config.OIDCConfig{
		ID:          "client",
		Provider:    "http://provider",
		RedirectURL: "http://redirect",
	}

	query := oidcRedirect(t, oidcConf, "/oidc")
	assert.False(t, query.Has("acr_values"), "acr_values was sent although no authentication context is required")
}

func TestGetOIDCKeepsRedirectURIWithAcrValues(t *testing.T) {
	oidcConf := config.OIDCConfig{
		ID:          "client",
		Provider:    "http://provider",
		RedirectURL: "http://redirect",
		AcrValues:   []string{"https://refeds.org/profile/mfa"},
	}

	query := oidcRedirect(t, oidcConf, "/oidc?redirect_uri=http://frontend/callback")
	assert.Equal(t, "http://frontend/callback", query.Get("redirect_uri"), "the per request redirect_uri was dropped")
	assert.Equal(t, "https://refeds.org/profile/mfa", query.Get("acr_values"), "acr_values was dropped when a redirect_uri was given")
}

func TestLoginFailureMessage(t *testing.T) {
	acrErr := fmt.Errorf("%w: acr %q returned, required one of [x]", ErrAcrNotAccepted, "y")
	assert.Contains(t, loginFailureMessage(acrErr), "two factor authentication", "a rejected authentication context needs its own message")

	assert.Contains(t, loginFailureMessage(errors.New("token exchange failed")), "clear your session cookies", "unrelated failures keep the generic message")
	assert.NotContains(t, loginFailureMessage(errors.New("token exchange failed")), "two factor")
}

// elixirLoginResponse serves a request to the oidc callback and returns the
// response, with the state cookie set to match so that the state check passes.
func elixirLoginResponse(t *testing.T, requestURL, state string) *httptest.ResponseRecorder {
	t.Helper()

	authHandler := AuthHandler{Config: config.AuthConf{OIDC: config.OIDCConfig{ID: "client"}}}

	req := httptest.NewRequest(http.MethodGet, requestURL, nil)
	req.AddCookie(&http.Cookie{Name: "state", Value: state}) // #nosec G124 -- request built by the unit test, no browser involved
	res := httptest.NewRecorder()
	authHandler.elixirLogin(res, req)

	return res
}

func TestElixirLoginReportsProviderError(t *testing.T) {
	// A provider that refuses the authorization request redirects back with an
	// error and no code. Exchanging the empty code instead loses what it said.
	res := elixirLoginResponse(t,
		"/oidc/login?state=s&error=invalid_request&error_description=More+than+one+entity+found",
		"s")

	assert.Contains(t, res.Body.String(), "invalid_request", "the provider's error was not reported")
	assert.NotContains(t, res.Body.String(), "clear your session cookies", "a refused request is not a stale cookie")
	assert.NotContains(t, res.Body.String(), "More than one entity found", "the description may carry provider internals and belongs in the log only")
}

// testRouter returns the router of an AuthHandler that serves the real
// templates and static files.
func testRouter(t *testing.T, corsConf config.CORSConfig) (http.Handler, AuthHandler) {
	t.Helper()

	authHandler := AuthHandler{
		htmlDir:   "frontend/templates",
		staticDir: "frontend/static",
		sessions:  newSessionStore(time.Minute),
	}
	var err error
	authHandler.templates, err = template.ParseGlob(filepath.Join(authHandler.htmlDir, "*.html"))
	require.NoError(t, err)

	return authHandler.router(corsConf), authHandler
}

func serve(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	return res
}

func TestRouterServesIndex(t *testing.T) {
	router, _ := testRouter(t, config.CORSConfig{})

	res := serve(router, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "login-options")
	assert.NotEmpty(t, res.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", res.Header().Get("X-Content-Type-Options"))
	assert.Empty(t, res.Result().Cookies(), "a page view without flash messages must not start a session")
}

func TestRouterUnknownPath(t *testing.T) {
	router, _ := testRouter(t, config.CORSConfig{})

	res := serve(router, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	assert.Equal(t, http.StatusNotFound, res.Code, "the root route must not match every path")
	assert.Equal(t, "nosniff", res.Header().Get("X-Content-Type-Options"))
}

func TestRouterCSPOnlyOnPages(t *testing.T) {
	router, _ := testRouter(t, config.CORSConfig{})

	res := serve(router, httptest.NewRequest(http.MethodGet, "/ega/login", nil))
	assert.Equal(t, http.StatusOK, res.Code)
	assert.NotEmpty(t, res.Header().Get("Content-Security-Policy"))

	res = serve(router, httptest.NewRequest(http.MethodGet, "/login-options", nil))
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Empty(t, res.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", res.Header().Get("X-Content-Type-Options"))
}

func TestRouterStaticFiles(t *testing.T) {
	router, _ := testRouter(t, config.CORSConfig{})

	res := serve(router, httptest.NewRequest(http.MethodGet, "/public/login.js", nil))
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "login-options")

	res = serve(router, httptest.NewRequest(http.MethodGet, "/public/", nil))
	assert.Equal(t, http.StatusNotFound, res.Code, "the static directory must not be listed")
}

func TestRouterS3ConfDownloadIsReadOnce(t *testing.T) {
	router, authHandler := testRouter(t, config.CORSConfig{})

	res := httptest.NewRecorder()
	authHandler.sessions.SetFlash(res, httptest.NewRequest(http.MethodGet, "/", nil), "oidcInbox", map[string]string{"access_token": "token"})
	cookie := sessionCookie(res)

	req := httptest.NewRequest(http.MethodHead, "/oidc/s3conf-inbox", nil)
	req.AddCookie(cookie)
	assert.Equal(t, http.StatusMethodNotAllowed, serve(router, req).Code, "HEAD must not consume the download")

	req = httptest.NewRequest(http.MethodGet, "/oidc/s3conf-inbox", nil)
	req.AddCookie(cookie)
	res = serve(router, req)
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "attachment; filename=s3cmd-inbox.conf", res.Header().Get("Content-Disposition"))
	assert.Equal(t, "[default]\naccess_token = token\n", res.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/oidc/s3conf-inbox", nil)
	req.AddCookie(cookie)
	res = serve(router, req)
	assert.Equal(t, http.StatusFound, res.Code, "a second download must redirect home")
	assert.Equal(t, "/", res.Header().Get("Location"))
}

func TestRouterEGALoginShowsFlashMessage(t *testing.T) {
	router, authHandler := testRouter(t, config.CORSConfig{})

	res := httptest.NewRecorder()
	authHandler.sessions.SetFlash(res, httptest.NewRequest(http.MethodGet, "/", nil), "message", "Provided credentials are not valid")
	cookie := sessionCookie(res)

	req := httptest.NewRequest(http.MethodGet, "/ega/login", nil)
	req.AddCookie(cookie)
	assert.Contains(t, serve(router, req).Body.String(), "Provided credentials are not valid")

	req = httptest.NewRequest(http.MethodGet, "/ega/login", nil)
	req.AddCookie(cookie)
	assert.NotContains(t, serve(router, req).Body.String(), "Provided credentials are not valid", "the message must only be shown once")
}

func TestRouterCORS(t *testing.T) {
	request := func(method, origin string, preflight bool) *http.Request {
		req := httptest.NewRequest(method, "/info", nil)
		req.Header.Set("Origin", origin)
		if preflight {
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		}

		return req
	}

	router, _ := testRouter(t, config.CORSConfig{AllowOrigin: "https://frontend.example", AllowMethods: "get,post", AllowCredentials: true})

	res := serve(router, request(http.MethodOptions, "https://frontend.example", true))
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "https://frontend.example", res.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", res.Header().Get("Access-Control-Allow-Credentials"))

	res = serve(router, request(http.MethodGet, "https://frontend.example", false))
	assert.Equal(t, http.StatusOK, res.Code, "methods must match regardless of case")
	assert.Equal(t, "https://frontend.example", res.Header().Get("Access-Control-Allow-Origin"))

	res = serve(router, request(http.MethodOptions, "https://evil.example", true))
	assert.Equal(t, http.StatusForbidden, res.Code)
	res = serve(router, request(http.MethodGet, "https://evil.example", false))
	assert.Equal(t, http.StatusForbidden, res.Code, "a request from an origin that is not allowed must not reach the handler")

	router, _ = testRouter(t, config.CORSConfig{AllowOrigin: "https://frontend.example", AllowMethods: "POST"})
	res = serve(router, request(http.MethodGet, "https://frontend.example", false))
	assert.Equal(t, http.StatusForbidden, res.Code, "a method that is not allowed must not reach the handler")

	router, _ = testRouter(t, config.CORSConfig{AllowOrigin: "*", AllowMethods: "GET", AllowCredentials: true})
	res = serve(router, request(http.MethodGet, "https://any.example", false))
	assert.Equal(t, "https://any.example", res.Header().Get("Access-Control-Allow-Origin"), "a wildcard with credentials must echo the origin")

	router, _ = testRouter(t, config.CORSConfig{AllowOrigin: "*", AllowMethods: "GET"})
	res = serve(router, request(http.MethodGet, "https://any.example", false))
	assert.Equal(t, "*", res.Header().Get("Access-Control-Allow-Origin"))

	router, _ = testRouter(t, config.CORSConfig{})
	res = serve(router, request(http.MethodOptions, "https://frontend.example", true))
	assert.Empty(t, res.Header().Get("Access-Control-Allow-Origin"), "CORS must be off unless origins are configured")
	res = serve(router, request(http.MethodGet, "https://frontend.example", false))
	assert.Equal(t, http.StatusOK, res.Code)
}
