package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func (ts *HelperTest) SetupTest() {

}

func (ts *HelperTest) TestMakeFolder() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Equal(ts.T(), "dummy-folder/private-key", privateK)
	assert.Equal(ts.T(), "dummy-folder/public-key", publicK)

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreateRSAkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(ts.T(), CreateRSAkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestParsePrivateRSAKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(ts.T(), e)
	_, err := ParsePrivateRSAKey(privateK, "/rsa")
	assert.Nil(ts.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreateRSAToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateRSAkeys(privateK, publicK)
	assert.Nil(ts.T(), e)
	parsedPrKey, _ := ParsePrivateRSAKey(privateK, "/rsa")
	tok, err := CreateRSAToken(parsedPrKey, "RS256", DefaultTokenClaims)
	assert.Nil(ts.T(), err)

	set := jwk.NewSet()
	keyData, err := os.ReadFile(filepath.Join(filepath.Clean(publicK), "/rsa.pub"))
	assert.NoError(ts.T(), err)
	key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
	assert.NoError(ts.T(), err)
	err = jwk.AssignKeyID(key)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), set.AddKey(key))

	_, _ = fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(ts.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreateECkeys() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	assert.Nil(ts.T(), CreateECkeys(privateK, publicK))

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestParsePrivateECKey() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(ts.T(), e)

	k, err := ParsePrivateECKey(privateK, "/ec")
	assert.Nil(ts.T(), err)
	assert.Equal(ts.T(), "EC", fmt.Sprintf("%v", k.KeyType()))

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreateECToken() {
	privateK, publicK, _ := MakeFolder("dummy-folder")
	e := CreateECkeys(privateK, publicK)
	assert.Nil(ts.T(), e)
	parsedPrKey, err := ParsePrivateECKey(privateK, "/ec")
	assert.NoError(ts.T(), err)
	_, err = CreateECToken(parsedPrKey, "ES256", DefaultTokenClaims)
	assert.Nil(ts.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreateHSToken() {
	key := make([]byte, 256)
	tok, err := CreateHSToken(key, DefaultTokenClaims)
	assert.Nil(ts.T(), err)

	set := jwk.NewSet()
	jwtKey, err := jwk.FromRaw(key)
	assert.NoError(ts.T(), err)

	err = jwk.AssignKeyID(jwtKey)
	assert.NoError(ts.T(), err)

	assert.NoError(ts.T(), set.AddKey(jwtKey))

	_, _ = fmt.Println(tok)
	_, err = jwt.Parse([]byte(tok), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	assert.NoError(ts.T(), err)

	defer os.RemoveAll("dummy-folder")
}

func (ts *HelperTest) TestCreatePrivateKeyFile() {
	tempDir, err := os.MkdirTemp("", "key")
	assert.NoError(ts.T(), err)
	defer os.RemoveAll(tempDir)

	// Define the key file path and passphrase
	keyFile := fmt.Sprintf("%s/c4gh.key", tempDir)
	passphrase := "test"

	// Call the function under test
	publicKey, err := CreatePrivateKeyFile(keyFile, passphrase)
	assert.NoError(ts.T(), err)

	// Verify the file was created
	_, err = os.Stat(keyFile)
	assert.NoError(ts.T(), err, "Private key file should exist")

	// Read and verify the file contents
	fileContent, err := os.ReadFile(keyFile)
	assert.NoError(ts.T(), err, "Should be able to read the private key file")
	assert.Contains(ts.T(), string(fileContent), "BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY", "File should contain a valid private key")

	// Verify the public key is not empty
	assert.NotEqual(ts.T(), [32]byte{}, publicKey, "Public key should not be empty")
}

func (ts *HelperTest) TestAnonymizeFilepath() {
	filePath := "main_folder/sub_folder/file.name"
	userName := "test.user@demo.org"
	newPath := AnonymizeFilepath(filepath.Join(strings.Replace(userName, "@", "_", 1), filePath), userName)
	assert.Equal(ts.T(), filePath, newPath)
}

func (ts *HelperTest) TestAnonymizeFilepath_noUser() {
	filePath := "main_folder/sub_folder/file.name"
	userName := "test.user@demo.org"
	newPath := AnonymizeFilepath(filePath, userName)
	assert.Equal(ts.T(), filePath, newPath)
}

func (ts *HelperTest) TestUnanonymizeFilepath() {
	filePath := "main_folder/sub_folder/file.name"
	userName := "test.user@demo.org"
	newPath := UnanonymizeFilepath(filePath, userName)
	assert.Equal(ts.T(), filepath.Join(strings.Replace(userName, "@", "_", 1), filePath), newPath)
}
func (ts *HelperTest) TestUnanonymizeFilepath_oldMessage() {
	filePath := "test.user_demo.org/main_folder/sub_folder/file.name"
	userName := "test.user@demo.org"
	newPath := UnanonymizeFilepath(filePath, userName)
	assert.Equal(ts.T(), filePath, newPath)
}

func (ts *HelperTest) TestUnanonymizeFilepath_leadingSeparator() {
	// A leading "/" must not drop the username directory: Go's filepath.Join concatenates and
	// cleans, it does not treat a leading "/" in a later element as an absolute path. So an
	// anonymized "/files/x" still resolves under the user directory.
	userName := "test.user@demo.org"
	assert.Equal(ts.T(), "test.user_demo.org/files/x.raw.enc",
		UnanonymizeFilepath("/files/x.raw.enc", userName))
}

func (ts *HelperTest) TestResolveInboxPath_stockDefault_prependsNormalizedUser() {
	// With no project code ResolveInboxPath defers to the stock UnanonymizeFilepath: the first
	// "@" becomes "_" and the username directory is prepended unless the path already starts with
	// it. These assertions keep existing deployments pinned.
	stockDefault := InboxProjectConfig{}
	user := "test.user@demo.org"
	assert.Equal(ts.T(), "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("files/x.raw.enc", user, stockDefault))
	// Already-prefixed path is returned unchanged.
	assert.Equal(ts.T(), "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("test.user_demo.org/files/x.raw.enc", user, stockDefault))
}

func (ts *HelperTest) TestResolveInboxPath_stockDefault_leadingSeparator() {
	// Stock branch (no project code): a leading "/" on the anonymized path is harmless. The userDir
	// prefix is still applied, refuting the assumption that filepath.Join treats a leading "/" as
	// absolute and drops the prefix.
	stockDefault := InboxProjectConfig{}
	user := "test.user@demo.org"
	assert.Equal(ts.T(), "test.user_demo.org/files/x.raw.enc",
		ResolveInboxPath("/files/x.raw.enc", user, stockDefault))
}

func (ts *HelperTest) TestResolveInboxPath_projectCode_reconstructsRawUserDir() {
	// FEGA-Norway: anonymized "/files/x" is rebuilt under the project-code-prefixed RAW username
	// (the "@" is not normalized to "_" when a project code is configured).
	cfg := InboxProjectConfig{Code: "p11", Delimiter: "-"}
	got := ResolveInboxPath("/files/x.raw.enc", "dummy@elixir-europe.org", cfg)
	assert.Equal(ts.T(), "p11-dummy@elixir-europe.org/files/x.raw.enc", got)
}

func (ts *HelperTest) TestResolveInboxPath_projectCode_alreadyPrefixed_unchanged() {
	// An already-resolved path (e.g. on reprocessing) is returned as-is, not double-prefixed.
	cfg := InboxProjectConfig{Code: "p11", Delimiter: "-"}
	fp := "p11-dummy@elixir-europe.org/files/x.raw.enc"
	assert.Equal(ts.T(), fp, ResolveInboxPath(fp, "dummy@elixir-europe.org", cfg))
}

func (ts *HelperTest) TestResolveInboxPath_projectCode_leadingSeparator_notDoubled() {
	// Older proxy formats can send an already-resolved path with a leading "/". The leading
	// separator must be normalized away, not cause the user directory to be prepended a second time
	// ("/p11-user/files/x" -> "p11-user/p11-user/files/x").
	cfg := InboxProjectConfig{Code: "p11", Delimiter: "-"}
	assert.Equal(ts.T(), "p11-dummy@elixir-europe.org/files/x.raw.enc",
		ResolveInboxPath("/p11-dummy@elixir-europe.org/files/x.raw.enc", "dummy@elixir-europe.org", cfg))
	// All leading separators are stripped, so repeated slashes cannot sneak past the
	// already-resolved check either.
	assert.Equal(ts.T(), "p11-dummy@elixir-europe.org/files/x.raw.enc",
		ResolveInboxPath("//p11-dummy@elixir-europe.org/files/x.raw.enc", "dummy@elixir-europe.org", cfg))
}

func (ts *HelperTest) TestResolveInboxPath_projectCode_segmentBoundary() {
	// The already-resolved check holds on a path-segment boundary: "p11-user2/..." belongs to a
	// different user and must not be treated as already under the "p11-user" directory.
	cfg := InboxProjectConfig{Code: "p11", Delimiter: "-"}
	assert.Equal(ts.T(), "p11-user/p11-user2/files/x.raw.enc",
		ResolveInboxPath("p11-user2/files/x.raw.enc", "user", cfg))
}
