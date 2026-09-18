package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kataras/iris/v12"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// oidcRedirect serves a request to /oidc through an iris app and returns the
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

	app := iris.New()
	app.Get("/oidc", authHandler.getOIDC)
	require.NoError(t, app.Build())

	res := httptest.NewRecorder()
	app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, requestURL, nil))
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
