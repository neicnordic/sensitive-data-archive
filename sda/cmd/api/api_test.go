package main

import (
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

	// apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
	token  string
	mockDb mocks.MockDatabase
	api    API
}

func (ts *TestSuite) SetupSuite() {
	// Needs to be set for config.Load() but will not be used in testing
	viper.Set("database.host", "")
	viper.Set("database.user", "")
	viper.Set("database.password", "")
	viper.Set("rbac.path", "")
	assert.NoError(ts.T(), config.Load())

	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(ts.T(), err)

	jwkKey, err := jwk.FromRaw(privatekey)
	assert.NoError(ts.T(), err)

	token, err := helper.CreateRSAToken(jwkKey, "RS256", helper.DefaultTokenClaims)
	assert.NoError(ts.T(), err)
	ts.token = token

	publicKeyBytes, err := jwk.EncodePEM(&privatekey.PublicKey)
	assert.NoError(ts.T(), err)

	auth := userauth.NewValidateFromToken(jwk.NewSet())
	err = auth.ReadJwtPubKeyBytes(publicKeyBytes)
	assert.NoError(ts.T(), err)
	ts.api.auth = auth

	ts.api.db = &mocks.MockDatabase{}
}

func (ts *TestSuite) TearDownSuite() {}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) TestGetFiles_BaseCase() {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/files", nil)
	r.Header.Add("Authorization", "Bearer "+ts.token)
	ts.api.getFiles(w, r)
	slog.Info("response", "code", w.Code)
	assert.Equal(ts.T(), http.StatusOK, w.Code)
}
