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

func (ht *HelperTest) SetupTest() {

}

func (ht *HelperTest) TestMakeFolder() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Equal(ht.T(), "dummy-folder/private-key", privateK)
	assert.Equal(ht.T(), "dummy-folder/public-key", publicK)

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreateRSAkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(ht.T(), CreateRSAkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestParsePrivateRSAKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(ht.T(), e)
	_, err := ParsePrivateRSAKey(privateK, "/rsa")
	assert.Nil(ht.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreateRSAToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(ht.T(), e)
	ParsedPrKey, _ := ParsePrivateRSAKey(privateK, "/rsa")
	tok, err := CreateRSAToken(ParsedPrKey, "RS256", DefaultTokenClaims)
	assert.Nil(ht.T(), err)

	set := jwk.NewSet()
	keyData, err := os.ReadFile(filepath.Join(filepath.Clean(publicK), "/rsa.pub"))
	assert.NoError(ht.T(), err)
	key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
	assert.NoError(ht.T(), err)
	err = jwk.AssignKeyID(key)
	assert.NoError(ht.T(), err)
	assert.NoError(ht.T(), set.AddKey(key))

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(ht.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreateECkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(ht.T(), CreateECkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestParsePrivateECKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(ht.T(), e)

	k, err := ParsePrivateECKey(privateK, "/ec")
	assert.Nil(ht.T(), err)
	assert.Equal(ht.T(), "EC", fmt.Sprintf("%v", k.KeyType()))

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreateECToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(ht.T(), e)
	ParsedPrKey, err := ParsePrivateECKey(privateK, "/ec")
	assert.NoError(ht.T(), err)
	_, err = CreateECToken(ParsedPrKey, "ES256", DefaultTokenClaims)
	assert.Nil(ht.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreateHSToken() {
	key := make([]byte, 256)
	tok, err := CreateHSToken(key, DefaultTokenClaims)
	assert.Nil(ht.T(), err)

	set := jwk.NewSet()
	jwtKey, err := jwk.FromRaw(key)
	assert.NoError(ht.T(), err)

	err = jwk.AssignKeyID(jwtKey)
	assert.NoError(ht.T(), err)

	assert.NoError(ht.T(), set.AddKey(jwtKey))

	fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(ht.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ht *HelperTest) TestCreatePrivateKeyFile() {
	tempDir, err := os.MkdirTemp("", "key")
	assert.NoError(ht.T(), err)
	defer os.RemoveAll(tempDir)

	// Define the key file path and passphrase
	keyFile := fmt.Sprintf("%s/c4gh.key", tempDir)
	passphrase := "test"

	// Call the function under test
	publicKey, err := CreatePrivateKeyFile(keyFile, passphrase)
	assert.NoError(ht.T(), err)

	// Verify the file was created
	_, err = os.Stat(keyFile)
	assert.NoError(ht.T(), err, "Private key file should exist")

	// Read and verify the file contents
	fileContent, err := os.ReadFile(keyFile)
	assert.NoError(ht.T(), err, "Should be able to read the private key file")
	assert.Contains(ht.T(), string(fileContent), "BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY", "File should contain a valid private key")

	// Verify the public key is not empty
	assert.NotEqual(ht.T(), [32]byte{}, publicKey, "Public key should not be empty")
}
