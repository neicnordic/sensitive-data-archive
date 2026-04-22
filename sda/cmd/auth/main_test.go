package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/kataras/iris/v12"
	"github.com/kataras/iris/v12/httptest"
	"github.com/kataras/iris/v12/sessions"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
)

func newTestApp(t *testing.T, h AuthHandler) *httptest.Expect {
	t.Helper()

	app := iris.New()

	// sessions middleware required because handlers use sessions.Get(ctx)
	sess := sessions.New(sessions.Config{Cookie: "_session_id", AllowReclaim: true})
	app.Use(sess.Handler())

	app.Get("/oidc/start", h.getOIDCStart)
	app.Post("/oidc/exchange", h.postOIDCExchange)

	return httptest.New(t, app)
}

func TestOIDCStart_MissingReturnTo(t *testing.T) {
	h := AuthHandler{
		Config: config.AuthConf{
			ReturnToAllowlist:     []string{"https://portal.example.org/auth/callback"},
			AllowInsecureReturnTo: false,
		},
		OAuth2Config: oauth2.Config{ // minimal to avoid nil usage if it ever reaches getOIDC
			Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example.org/auth"},
		},
	}

	e := newTestApp(t, h)

	e.GET("/oidc/start").
		WithQuery("token_type", "raw").
		Expect().
		Status(iris.StatusBadRequest).
		Body().Contains("missing return_to")
}

func TestOIDCStart_ReturnToNotAllowed(t *testing.T) {
	h := AuthHandler{
		Config: config.AuthConf{
			ReturnToAllowlist:     []string{"https://portal.example.org/auth/callback"},
			AllowInsecureReturnTo: false,
		},
		OAuth2Config: oauth2.Config{
			Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example.org/auth"},
		},
	}

	e := newTestApp(t, h)

	e.GET("/oidc/start").
		WithQuery("return_to", "https://evil.example.org/cb").
		WithQuery("token_type", "raw").
		Expect().
		Status(iris.StatusBadRequest).
		Body().Contains("return_to not allowed")
}

func TestOIDCStart_InsecureHTTPDisallowed(t *testing.T) {
	h := AuthHandler{
		Config: config.AuthConf{
			ReturnToAllowlist:     []string{"http://portal.example.org/auth/callback"},
			AllowInsecureReturnTo: false,
		},
		OAuth2Config: oauth2.Config{
			Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example.org/auth"},
		},
	}

	e := newTestApp(t, h)

	e.GET("/oidc/start").
		WithQuery("return_to", "http://portal.example.org/auth/callback").
		WithQuery("token_type", "raw").
		Expect().
		Status(iris.StatusBadRequest).
		Body().Contains("return_to must be https")
}

func TestOIDCStart_InsecureHTTPAllowed_NotRejectedByValidation(t *testing.T) {
	h := AuthHandler{
		Config: config.AuthConf{
			ReturnToAllowlist:     []string{"http://portal.example.org/auth/callback"},
			AllowInsecureReturnTo: true,
		},
	}

	e := newTestApp(t, h)

	r := e.GET("/oidc/start").
		WithQuery("return_to", "http://portal.example.org/auth/callback").
		WithQuery("token_type", "raw").
		Expect()

	assert.NotEqual(t, iris.StatusBadRequest, r.Raw().StatusCode)
}

func TestOIDCExchange_DisabledWhenNoSecret(t *testing.T) {
	h := AuthHandler{
		Config:   config.AuthConf{ExchangeSecret: ""},
		Handoffs: NewMemoryHandoffStore(2*time.Minute, 100),
	}

	e := newTestApp(t, h)

	body := `{"code":"abc"}`
	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithBytes([]byte(body)).
		Expect().
		Status(iris.StatusNotFound)
}

func TestOIDCExchange_UnauthorizedWhenSecretMissingOrWrong(t *testing.T) {
	h := AuthHandler{
		Config:   config.AuthConf{ExchangeSecret: "supersecret"},
		Handoffs: NewMemoryHandoffStore(2*time.Minute, 100),
	}

	e := newTestApp(t, h)

	// missing header
	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithBytes([]byte(`{"code":"abc"}`)).
		Expect().
		Status(iris.StatusUnauthorized)

	// wrong header
	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithHeader("X-SDA-AUTH-EXCHANGE-SECRET", "wrong").
		WithBytes([]byte(`{"code":"abc"}`)).
		Expect().
		Status(iris.StatusUnauthorized)
}

func TestOIDCExchange_SuccessAndSingleUse(t *testing.T) {
	store := NewMemoryHandoffStore(2*time.Minute, 100)
	code, err := store.Put(HandoffItem{
		Token:     "token123",
		Exp:       "2099-01-01 00:00:00",
		Sub:       "user",
		TokenType: "raw",
		CreatedAt: time.Now().UTC(),
	})
	assert.NoError(t, err)

	h := AuthHandler{
		Config:   config.AuthConf{ExchangeSecret: "supersecret"},
		Handoffs: store,
	}

	e := newTestApp(t, h)

	req := exchangeReq{Code: code}
	b, _ := json.Marshal(req)

	// first exchange succeeds
	resp := e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithHeader("X-SDA-AUTH-EXCHANGE-SECRET", "supersecret").
		WithBytes(b).
		Expect().
		Status(iris.StatusOK).
		JSON().Object()

	resp.Value("token").IsEqual("token123")
	resp.Value("sub").IsEqual("user")
	resp.Value("token_type").IsEqual("raw")

	// second exchange with same code fails (single-use)
	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithHeader("X-SDA-AUTH-EXCHANGE-SECRET", "supersecret").
		WithBytes(b).
		Expect().
		Status(iris.StatusNotFound)
}

func TestOIDCExchange_InvalidJSON(t *testing.T) {
	h := AuthHandler{
		Config:   config.AuthConf{ExchangeSecret: "supersecret"},
		Handoffs: NewMemoryHandoffStore(2*time.Minute, 100),
	}

	e := newTestApp(t, h)

	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithHeader("X-SDA-AUTH-EXCHANGE-SECRET", "supersecret").
		WithBytes([]byte(`not-json`)).
		Expect().
		Status(iris.StatusBadRequest)
}

// Optional: if your postOIDCExchange reads JSON even when Content-Type differs,
// keep this; otherwise you can remove it.
func TestOIDCExchange_EmptyCode(t *testing.T) {
	h := AuthHandler{
		Config:   config.AuthConf{ExchangeSecret: "supersecret"},
		Handoffs: NewMemoryHandoffStore(2*time.Minute, 100),
	}

	e := newTestApp(t, h)

	empty := exchangeReq{Code: ""}
	b := new(bytes.Buffer)
	_ = json.NewEncoder(b).Encode(empty)

	e.POST("/oidc/exchange").
		WithHeader("Content-Type", "application/json").
		WithHeader("X-SDA-AUTH-EXCHANGE-SECRET", "supersecret").
		WithBytes(b.Bytes()).
		Expect().
		Status(iris.StatusBadRequest)
}
