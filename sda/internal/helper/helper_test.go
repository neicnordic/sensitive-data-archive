package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HelperTest struct {
	suite.Suite
}

func TestUserAuthTestSuite(t *testing.T) {
	suite.Run(t, new(HelperTest))
}

func (suite *HelperTest) SetupTest() {

}

func (suite *HelperTest) TestMakeFolder() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Equal(suite.T(), "dummy-folder/private-key", privateK)
	assert.Equal(suite.T(), "dummy-folder/public-key", publicK)

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreateRSAkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(suite.T(), CreateRSAkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestParsePrivateRSAKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(suite.T(), e)
	_, err := ParsePrivateRSAKey(privateK, "/rsa")
	assert.Nil(suite.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreateRSAToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(suite.T(), e)
	ParsedPrKey, _ := ParsePrivateRSAKey(privateK, "/rsa")
	tok, err := CreateRSAToken(ParsedPrKey, "RS256", DefaultTokenClaims)
	assert.Nil(suite.T(), err)

	set := jwk.NewSet()
	keyData, err := os.ReadFile(filepath.Join(filepath.Clean(publicK), "/rsa.pub"))
	assert.NoError(suite.T(), err)
	key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
	assert.NoError(suite.T(), err)
	err = jwk.AssignKeyID(key)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), set.AddKey(key))

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(suite.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreateECkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(suite.T(), CreateECkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestParsePrivateECKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(suite.T(), e)

	k, err := ParsePrivateECKey(privateK, "/ec")
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), "EC", fmt.Sprintf("%v", k.KeyType()))

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreateECToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(suite.T(), e)
	ParsedPrKey, err := ParsePrivateECKey(privateK, "/ec")
	assert.NoError(suite.T(), err)
	_, err = CreateECToken(ParsedPrKey, "ES256", DefaultTokenClaims)
	assert.Nil(suite.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreateHSToken() {
	key := make([]byte, 256)
	tok, err := CreateHSToken(key, DefaultTokenClaims)
	assert.Nil(suite.T(), err)

	set := jwk.NewSet()
	jwtKey, err := jwk.FromRaw(key)
	assert.NoError(suite.T(), err)

	err = jwk.AssignKeyID(jwtKey)
	assert.NoError(suite.T(), err)

	assert.NoError(suite.T(), set.AddKey(jwtKey))

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(suite.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (suite *HelperTest) TestCreatePrivateKeyFile() {
	tempDir, err := os.MkdirTemp("", "key")
	assert.NoError(suite.T(), err)
	defer os.RemoveAll(tempDir)

	// Define the key file path and passphrase
	keyFile := fmt.Sprintf("%s/c4gh.key", tempDir)
	passphrase := "test"

	// Call the function under test
	publicKey, err := CreatePrivateKeyFile(keyFile, passphrase)
	assert.NoError(suite.T(), err)

	// Verify the file was created
	_, err = os.Stat(keyFile)
	assert.NoError(suite.T(), err, "Private key file should exist")

	// Read and verify the file contents
	fileContent, err := os.ReadFile(keyFile)
	assert.NoError(suite.T(), err, "Should be able to read the private key file")
	assert.Contains(suite.T(), string(fileContent), "BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY", "File should contain a valid private key")

	// Verify the public key is not empty
	assert.NotEqual(suite.T(), [32]byte{}, publicKey, "Public key should not be empty")
}
