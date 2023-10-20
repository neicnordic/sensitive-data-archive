package main

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
	PrivateKey  *rsa.PrivateKey
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")

	viper.Set("db.host", "test")
	viper.Set("db.port", 123)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")

	conf := config.Config{}
	conf.API.Host = "localhost"
	conf.API.Port = 8080
	server := setup(&conf)

	assert.Equal(suite.T(), "localhost:8080", server.Addr)

	suite.Path = "/tmp/keys/"
	suite.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	privpath, pubpath, err := helper.MakeFolder(suite.Path)
	assert.NoError(suite.T(), err)
	suite.PrivatePath = privpath
	suite.PublicPath = pubpath
	err = helper.CreateRSAkeys(privpath, pubpath)
	assert.NoError(suite.T(), err)

}

func TestDatabasePingCheck(t *testing.T) {
	database := database.SDAdb{}
	assert.Error(t, checkDB(&database, 1*time.Second), "nil DB should fail")

	database.DB, _, err = sqlmock.New()
	assert.NoError(t, err)
	assert.NoError(t, checkDB(&database, 1*time.Second), "ping should succeed")
}

func (suite *TestSuite) TestGetUserFromURLToken() {
	// Get key set from oidc
	auth := userauth.NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := fmt.Sprintf("http://localhost:%d/jwk", OIDCport)
	err := auth.FetchJwtPubKeyURL(jwtpubkeyurl)
	assert.NoError(suite.T(), err, "failed to fetch remote JWK")
	assert.Equal(suite.T(), 3, auth.Keyset.Len())

	// Get token from oidc
	token_url := fmt.Sprintf("http://localhost:%d/tokens", OIDCport)
	resp, err := http.Get(token_url)
	assert.NoError(suite.T(), err, "Error getting token from oidc")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(suite.T(), err, "Error reading token from oidc")

	var tokens []string
	err = json.Unmarshal(body, &tokens)
	assert.NoError(suite.T(), err, "Error unmarshalling token")
	assert.GreaterOrEqual(suite.T(), len(tokens), 1)

	rawkey := tokens[0]

	// Call get files api
	url := "localhost:8080/files"
	method := "GET"
	r, err := http.NewRequest(method, url, nil)

	assert.NoError(suite.T(), err)

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %v", rawkey))

	user, err := getUserFromToken(r, auth)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "requester@demo.org", user)

}

func (suite *TestSuite) TestGetUserFromPathToken() {
	c := &config.Config{}
	ServerConf := config.ServerConfig{}
	ServerConf.Jwtpubkeypath = suite.PublicPath
	c.Server = ServerConf

	Conf = c

	auth := userauth.NewValidateFromToken(jwk.NewSet())
	err := auth.ReadJwtPubKeyPath(Conf.Server.Jwtpubkeypath)
	assert.NoError(suite.T(), err, "Error while getting key "+Conf.Server.Jwtpubkeypath)

	url := "localhost:8080/files"
	method := "GET"
	r, err := http.NewRequest(method, url, nil)
	assert.NoError(suite.T(), err)

	// Valid token
	prKeyParsed, err := helper.ParsePrivateRSAKey(suite.PrivatePath, "/rsa")
	assert.NoError(suite.T(), err)
	token, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)
	r.Header.Add("Authorization", "Bearer "+token)

	user, err := getUserFromToken(r, auth)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "dummy", user)

	// Token without authorization header
	r.Header.Del("Authorization")

	user, err = getUserFromToken(r, auth)
	assert.EqualError(suite.T(), err, "failed to get parse token: no access token supplied")

	assert.Equal(suite.T(), "", user)

	// Token without issuer
	NoIssuer := helper.DefaultTokenClaims
	NoIssuer["iss"] = ""
	log.Printf("Noissuer %v with iss %v", NoIssuer, NoIssuer["iss"])
	token, err = helper.CreateRSAToken(prKeyParsed, "RS256", NoIssuer)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	r.Header.Add("Authorization", "Bearer "+token)

	user, err = getUserFromToken(r, auth)
	assert.EqualError(suite.T(), err, "failed to get parse token: failed to get issuer from token (<nil>)")
	assert.Equal(suite.T(), "", user)

}
