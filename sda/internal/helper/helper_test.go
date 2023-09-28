package helper

import (
	"fmt"
	"os"
	"testing"
	"path/filepath"

	"github.com/stretchr/testify/assert"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func TestMakeFolder(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Equal(t, "dummy-folder/private-key", privateK)
	assert.Equal(t, "dummy-folder/public-key", publicK)

	defer os.RemoveAll("dummy-folder")
}

func TestCreateRSAkeys(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(t, CreateRSAkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func TestParsePrivateRSAKey(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(t, e)
	_, err := ParsePrivateRSAKey(privateK, "/dummy.ega.nbis.se")
	assert.Nil(t, err)

	defer os.RemoveAll("dummy-folder")
}

func TestCreateRSAToken(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(t, e)
	ParsedPrKey, _ := ParsePrivateRSAKey(privateK, "/dummy.ega.nbis.se")
	tok, err := CreateRSAToken(ParsedPrKey, "RS256", DefaultTokenClaims)
	assert.Nil(t, err)

	set := jwk.NewSet()
	keyData, err := os.ReadFile(filepath.Join(filepath.Clean(publicK), "/dummy.ega.nbis.se.pub"))
	assert.NoError(t, err)
	key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
	assert.NoError(t, err)
	err = jwk.AssignKeyID(key)
	assert.NoError(t, err)
	set.AddKey(key)

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(t, err)

	defer os.RemoveAll("dummy-folder")
}

func TestCreateECkeys(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(t, CreateECkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func TestParsePrivateECKey(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(t, e)

	k, err := ParsePrivateECKey(privateK, "/dummy.ega.nbis.se")
	assert.Nil(t, err)
	assert.Equal(t, "EC", fmt.Sprintf("%v", k.KeyType()))

	defer os.RemoveAll("dummy-folder")
}

func TestCreateECToken(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(t, e)
	ParsedPrKey, err := ParsePrivateECKey(privateK, "/dummy.ega.nbis.se")
	assert.NoError(t, err)
	_, err = CreateECToken(ParsedPrKey, "ES256", DefaultTokenClaims)
	assert.Nil(t, err)

	defer os.RemoveAll("dummy-folder")
}

func TestCreateHSToken(t *testing.T) {
	key := make([]byte, 256)
	tok, err := CreateHSToken(key, "HS256", DefaultTokenClaims)
	assert.Nil(t, err)

	set := jwk.NewSet()
	jwtKey, err := jwk.FromRaw(key)
	assert.NoError(t, err)

	err = jwk.AssignKeyID(jwtKey)
	assert.NoError(t, err)
	
	set.AddKey(jwtKey)

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(t, err)

	defer os.RemoveAll("dummy-folder")
}