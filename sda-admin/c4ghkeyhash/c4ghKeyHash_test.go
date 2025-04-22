package c4ghkeyhash

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
	b64String  string
	pubkeyPath string
	tempFolder string
}

type MockHelpers struct {
	mock.Mock
}

func (m *MockHelpers) GetResponseBody(url, token string) ([]byte, error) {
	args := m.Called(url, token)

	return args.Get(0).([]byte), args.Error(1)
}
func (m *MockHelpers) PostRequest(url, token string, jsonBody []byte) ([]byte, error) {
	args := m.Called(url, token, jsonBody)

	return args.Get(0).([]byte), args.Error(1)
}

func TestC4gh(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) SetupSuite() {
	ts.tempFolder = "/tmp/keys/"
	err := os.MkdirAll(ts.tempFolder, 0750)
	if err != nil {
		ts.T().FailNow()
	}

	pub, _, err := keys.GenerateKeyPair()
	if err != nil {
		ts.T().FailNow()
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, pub); err != nil {
		ts.T().FailNow()
	}

	ts.b64String = base64.StdEncoding.EncodeToString(buf.Bytes())

	pubKeyFile, err := os.Create(ts.tempFolder + "/pub.key")
	if err != nil {
		ts.T().FailNow()
	}
	ts.pubkeyPath = ts.tempFolder + "/pub.key"

	_, err = pubKeyFile.Write(buf.Bytes())
	if err != nil {
		ts.T().FailNow()
	}
}
func (ts *TestSuite) TearDownSuite() {
	os.RemoveAll(ts.tempFolder)
}

func (ts *TestSuite) TestAdd() {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }()

	expectedURL := "http://example.com/c4gh-keys/add"
	token := "test-token"

	payload := C4ghPubKey{
		PubKey:      ts.b64String,
		Description: "test description",
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		ts.T().Fail()
	}

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(`{}`), nil)

	assert.NoError(ts.T(), Add("http://example.com", token, ts.pubkeyPath, "test description"))
	mockHelpers.AssertExpectations(ts.T())
}

func (ts *TestSuite) TestDeprecate() {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }()

	expectedURL := "http://example.com/c4gh-keys/deprecate/6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507"
	token := "test-token"
	mockHelpers.On("PostRequest", expectedURL, token, []byte(`{}`)).Return([]byte(`{}`), nil)

	assert.NoError(ts.T(), Deprecate("http://example.com", token, "6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507"))
	mockHelpers.AssertExpectations(ts.T())
}

func (ts *TestSuite) TestList() {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/c4gh-keys/list", "test-token").Return([]byte(`[{"hash":"cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23","description":"this is a test key","created_at":"2009-11-10 23:00:00","deprecated_at":""}]`), nil)
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := List("http://example.com", "test-token")
	assert.NoError(ts.T(), err)
	mockHelpers.AssertExpectations(ts.T())
}
