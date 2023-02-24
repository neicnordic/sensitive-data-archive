package helper

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
	_, err := CreateRSAToken(ParsedPrKey, "RS256", "JWT", DefaultTokenClaims)
	assert.Nil(t, err)

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
	_, err := ParsePrivateECKey(privateK, "/dummy.ega.nbis.se")
	assert.Nil(t, err)

	defer os.RemoveAll("dummy-folder")
}

func TestCreateECToken(t *testing.T) {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(t, e)
	ParsedPrKey, _ := ParsePrivateECKey(privateK, "/dummy.ega.nbis.se")
	_, err := CreateECToken(ParsedPrKey, "RS256", "JWT", DefaultTokenClaims)
	assert.Nil(t, err)

	defer os.RemoveAll("dummy-folder")
}
