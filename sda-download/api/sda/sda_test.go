package sda

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/proto"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/database"
	"github.com/neicnordic/sda-download/internal/reencrypt"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/internal/storage"
)

func TestDatasets(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("datasets", session.Cache{Datasets: []string{"dataset1", "dataset2"}})

	// Test the outcomes of the handler
	Datasets(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(`["dataset1","dataset2"]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext

}

func TestFind_Found(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset2", datasets)

	// Expected results
	expectedFound := true

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestFind_NotFound(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset4", datasets)

	// Expected results
	expectedFound := false

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestGetFiles_Fail_Database(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		return nil, errors.New("something went wrong")
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 500
	expectedError := "database error"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_Database failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestGetFiles_Fail_NotFound(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset3", c)

	// Expected results
	expectedStatusCode := 404
	expectedError := "dataset not found"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
}

func TestGetFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		fileInfo := database.FileInfo{
			FileID: "file1",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, nil
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 200
	expectedFileID := "file1"

	if fileInfo[0].FileID != expectedFileID {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Success failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err != nil {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", err)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestFiles_Fail(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		return nil, 404, errors.New("dataset not found")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("dataset not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		fileInfo := database.FileInfo{
			FileID:                    "file1",
			DatasetID:                 "dataset1",
			DisplayFileName:           "file1.txt",
			FilePath:                  "dir/file1.txt",
			FileName:                  "file1.txt",
			FileSize:                  200,
			DecryptedFileSize:         100,
			DecryptedFileChecksum:     "hash",
			DecryptedFileChecksumType: "sha256",
			Status:                    "READY",
			CreatedAt:                 "a while ago",
			LastModified:              "now",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, 200, nil
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(
		`[{"fileId":"file1","datasetId":"dataset1","displayFileName":"file1.txt","filePath":` +
			`"dir/file1.txt","fileName":"file1.txt","fileSize":200,"decryptedFileSize":100,` +
			`"decryptedFileChecksum":"hash","decryptedFileChecksumType":"sha256",` +
			`"fileStatus":"READY","createdAt":"a while ago","lastModified":"now"}]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestDownload_Fail_FileNotFound(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "", errors.New("file not found")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("file not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission

}

func TestDownload_Fail_NoPermissions(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		// nolint:goconst
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 401
	expectedBody := []byte("unauthorised")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext

}

func TestDownload_Fail_GetFile(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		return nil, errors.New("database error")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("database error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_GetFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_GetFile failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile

}

func TestDownload_Fail_OpenFile(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	Backend, _ = storage.NewBackend(config.Config.Archive)

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		fileDetails := &database.FileDownload{
			ArchivePath: "non-existant-file.txt",
			ArchiveSize: 0,
			Header:      []byte{},
		}

		return fileDetails, nil
	}

	// Mock request and response holders and initialize headers
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := &http.Request{
		URL:    &url.URL{},
		Header: make(http.Header),
	}
	c.Request = req

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("archive error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_OpenFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_OpenFile failed, got '%s' expected '%s'", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile

}

func Test_getCoordinates(t *testing.T) {

	fileDetails := &database.FileDownload{
		ArchiveSize:   320028,
		DecryptedSize: 320000,
	}

	w := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(w)
	context.Request = &http.Request{}

	// Should work
	context.Request.Header = http.Header{"Range": []string{"bytes=131000-196999"}}
	c, err := getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(131000), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(197000), c[0].end, "Unexpected end coordinate")

	// Should fail - unknown unit
	context.Request.Header = http.Header{"Range": []string{"octets=191000-136999"}}
	_, err = getCoordinates(context, fileDetails)
	assert.Error(t, err, "Unexpected not error from getCoordinates")

	// Should fail - start after end
	context.Request.Header = http.Header{"Range": []string{"bytes=191000-136999"}}
	_, err = getCoordinates(context, fileDetails)
	assert.Error(t, err, "Unexpected not error from getCoordinates")

	// Start of file
	context.Request.Header = http.Header{"Range": []string{"bytes=-400"}}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(319600), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(320000), c[0].end, "Unexpected end coordinate")

	// End of file
	context.Request.Header = http.Header{"Range": []string{"bytes=315000-"}}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(315000), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(320000), c[0].end, "Unexpected end coordinate")

	// End of file
	context.Request.Header = http.Header{"Range": []string{"bytes=340000-350000"}}
	_, err = getCoordinates(context, fileDetails)
	assert.Error(t, err, "Unexpected lack error from getCoordinates when after file")

	// Now we add query params
	context.Request = &http.Request{URL: &url.URL{Path: "/somepath", RawQuery: "startCoordinate=5&endCoordinate=10"}}

	// Should work, Range overrides query params
	context.Request.Header = http.Header{"Range": []string{"bytes=131000-196999"}}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(131000), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(197000), c[0].end, "Unexpected end coordinate")

	// Should work, without Range, query params are used
	context.Request.Header = http.Header{}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(5), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(10), c[0].end, "Unexpected end coordinate")

	context, _ = gin.CreateTestContext(w)
	context.Request = &http.Request{URL: &url.URL{Path: "/somepath", RawQuery: "startCoordinate=5"}}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(5), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(320000), c[0].end, "Unexpected end coordinate")

	context, _ = gin.CreateTestContext(w)
	context.Request = &http.Request{URL: &url.URL{Path: "/somepath", RawQuery: "endCoordinate=35"}}
	c, err = getCoordinates(context, fileDetails)
	assert.NoError(t, err, "Unexpected error from getCoordinates")
	assert.Equal(t, 1, len(c), "Unexpected number of coordinates")
	assert.Equal(t, int64(0), c[0].start, "Unexpected start coordinate")
	assert.Equal(t, int64(35), c[0].end, "Unexpected end coordinate")

	context, _ = gin.CreateTestContext(w)
	context.Request = &http.Request{URL: &url.URL{Path: "/somepath", RawQuery: "endCoordinate=3500000"}}
	_, err = getCoordinates(context, fileDetails)
	assert.Error(t, err, "Unexpected lack of error from getCoordinates")

	context, _ = gin.CreateTestContext(w)
	context.Request = &http.Request{URL: &url.URL{Path: "/somepath", RawQuery: "startCoordinate=350000&endCoordinate=350100"}}
	_, err = getCoordinates(context, fileDetails)
	assert.Error(t, err, "Unexpected lack of error from getCoordinates")
}

func Test_adjustCoordinates(t *testing.T) {
	fileDetails := &database.FileDownload{
		ArchiveSize:   320140,
		DecryptedSize: 320000,
	}

	coords := []coordinates{{start: 0, end: 10}}

	del, err := adjustCoordinates(coords, fileDetails)
	assert.NoError(t, err, "Unexpected error from adjustCoordinates")
	assert.Equal(t, []uint64{0, 10}, del, "Incorrect dataeditlist returned")
	assert.Equal(t, int64(0), coords[0].start, "Incorrect start after adjustCoordinates")
	assert.Equal(t, int64(65536+28), coords[0].end, "Incorrect end after adjustCoordinates")

	coords = []coordinates{{start: 75000, end: 200000}}
	del, err = adjustCoordinates(coords, fileDetails)
	assert.NoError(t, err, "Unexpected error from adjustCoordinates")
	assert.Equal(t, []uint64{75000 - 65536, 200000 - 75000}, del, "Incorrect dataeditlist returned")
	assert.Equal(t, int64(65536+28), coords[0].start, "Incorrect start after adjustCoordinates")
	assert.Equal(t, int64(4*(65536+28)), coords[0].end, "Incorrect end after adjustCoordinates")

	coords = []coordinates{{start: 200000, end: 320000}}
	del, err = adjustCoordinates(coords, fileDetails)
	assert.NoError(t, err, "Unexpected error from adjustCoordinates")
	assert.Equal(t, []uint64{200000 - 3*65536}, del, "Incorrect dataeditlist returned")
	assert.Equal(t, int64(3*(65536+28)), coords[0].start, "Incorrect start after adjustCoordinates")
	assert.Equal(t, int64(320140), coords[0].end, "Incorrect end after adjustCoordinates")

	coords = []coordinates{{start: 319700, end: 320000}}
	del, err = adjustCoordinates(coords, fileDetails)
	assert.NoError(t, err, "Unexpected error from adjustCoordinates")
	assert.Equal(t, []uint64{57556}, del, "Incorrect dataeditlist returned")
	assert.Equal(t, int64(4*(65536+28)), coords[0].start, "Incorrect start after adjustCoordinates")
	assert.Equal(t, int64(320140), coords[0].end, "Incorrect end after adjustCoordinates")

}

type fakeGRPC struct {
	t         *testing.T
	pubkey    [32]byte
	passedDel []uint64
}

func (f *fakeGRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Poor person's grpc server

	w.Header().Add("Content-type", "application/grpc+proto")
	w.Header().Add("Grpc-encoding", "identity")
	w.Header().Add("Trailer", "Grpc-status")

	var inLength int32
	compressed := make([]byte, 1)
	_, err := r.Body.Read(compressed)
	assert.NoError(f.t, err, "Could not read compressed flag")
	assert.Equal(f.t, compressed[0], byte(0), "Unexpected compressed flag")

	err = binary.Read(r.Body, binary.BigEndian, &inLength)
	assert.NoError(f.t, err, "Could not read length")

	body, err := io.ReadAll(r.Body)
	assert.NoError(f.t, err, "Could not read body")

	re := reencrypt.ReencryptRequest{}
	err = proto.Unmarshal(body, &re)
	assert.NoError(f.t, err, "Could not unmarshal request")

	f.passedDel = re.Dataeditlist

	rr := reencrypt.ReencryptResponse{Header: re.GetOldheader()}
	response, err := proto.Marshal(&rr)
	assert.NoError(f.t, err, "Could not marshal response")

	w.WriteHeader(200)
	_, err = w.Write([]byte{0})
	assert.NoError(f.t, err, "Could not write response flag")

	err = binary.Write(w, binary.BigEndian, int32(len(response)))
	assert.NoError(f.t, err, "Could not write response length")

	_, err = w.Write(response)
	assert.NoError(f.t, err, "Could not write response")
	w.Header().Add("Grpc-status", "0")
}

func TestDownload_Whole_Range_Encrypted(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	archive := config.Config.Archive
	archive.Posix.Location = "."
	Backend, _ = storage.NewBackend(archive)

	// Fix to run fake GRPC server
	faker := fakeGRPC{t: t}
	server := httptest.NewUnstartedServer(&faker)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	// Figure out IP, port
	serverdetails := strings.Split(server.Listener.Addr().String(), ":")

	// Set up TLS for fake server
	keyfile, err := os.CreateTemp("", "key")
	assert.NoError(t, err, "Could not create temp file for key")
	defer os.Remove(keyfile.Name())
	privdata, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	assert.NoError(t, err, "Could not marshal private key")

	privPEM := "-----BEGIN PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString(privdata) + "\n-----END PRIVATE KEY-----\n"
	_, err = keyfile.Write([]byte(privPEM))
	assert.NoError(t, err, "Could not write private key")
	keyfile.Close()

	certfile, err := os.CreateTemp("", "cert")
	assert.NoError(t, err, "Could not create temp file for cert")
	defer os.Remove(certfile.Name())
	pubPEM := "-----BEGIN CERTIFICATE-----\n" + base64.StdEncoding.EncodeToString(server.Certificate().Raw) + "\n-----END CERTIFICATE-----\n"
	_, err = certfile.Write([]byte(pubPEM))
	assert.NoError(t, err, "Could not write public key")
	certfile.Close()

	// Configure Reencrypt to use fake server
	config.Config.Reencrypt.Host = serverdetails[0]
	port, _ := strconv.ParseInt(serverdetails[1], 10, 32)
	config.Config.Reencrypt.Port = int(port)
	config.Config.Reencrypt.CACert = certfile.Name()
	config.Config.Reencrypt.ClientCert = certfile.Name()
	config.Config.Reencrypt.ClientKey = keyfile.Name()
	config.Config.Reencrypt.Timeout = 10

	// Set up crypt4gh keys
	config.Config.App.Crypt4GHPrivateKey, config.Config.App.Crypt4GHPublicKeyB64, err = config.GenerateC4GHKey()
	assert.NoError(t, err, "Could not generate temporary keys")

	// Make a file to hold the archive file
	datafile, err := os.CreateTemp(".", "datafile.")
	assert.NoError(t, err, "Could not create datafile for test")
	datafileName := datafile.Name()
	defer os.Remove(datafileName)

	tempKey, err := base64.StdEncoding.DecodeString(config.Config.App.Crypt4GHPublicKeyB64)
	assert.NoError(t, err, "Could not decode public key envelope")

	// Decode public key
	pubKeyReader := bytes.NewReader(tempKey)
	publicKey, err := keys.ReadPublicKey(pubKeyReader)
	assert.NoError(t, err, "Could not decode public key")

	// Reader list for archive file
	readerPublicKeyList := [][chacha20poly1305.KeySize]byte{}
	readerPublicKeyList = append(readerPublicKeyList, [32]byte(publicKey))
	faker.pubkey = [32]byte(publicKey)

	bufferWriter := bytes.Buffer{}
	dataWriter, err := streaming.NewCrypt4GHWriter(&bufferWriter, config.Config.App.Crypt4GHPrivateKey, readerPublicKeyList, nil)
	assert.NoError(t, err, "Could not make crypt4gh writer for test")

	// Write some data to the file

	_, err = dataWriter.Write([]byte(strings.Repeat("data", 80000)))
	assert.NoError(t, err, "Could not write to crypt4gh writer for test")
	dataWriter.Close()

	// We have now written a crypt4gh to our buffer, prepare it for use
	// by separating out the header and write the rest to the file

	headerBytes, err := headers.ReadHeader(&bufferWriter)
	assert.NoError(t, err, "Could not get header")

	_, err = io.Copy(datafile, &bufferWriter)
	assert.NoError(t, err, "Could not write temporary file")
	datafile.Close()

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		fileDetails := &database.FileDownload{
			ArchivePath:   datafileName,
			ArchiveSize:   5*28 + 4*80000,
			DecryptedSize: 4 * 80000,
			LastModified:  "2006-01-02T15:04:05Z",
			Header:        headerBytes,
		}

		return fileDetails, nil
	}

	// Test download, should work and return the whole file
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{}}

	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	// We only check
	assert.Equal(t, []byte(strings.Repeat("data", 80000)), body, "Unexpected body from download")

	// Test download with specified coordinates, should return a small bit of the file
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/somepath", RawQuery: "startCoordinate=5&endCoordinate=10"}}
	// Test the outcomes of the handler
	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	assert.Equal(t, []byte("atada"), body, "Unexpected body from download")

	// Test encrypted download of whole file, should work and give output that is crypt4gh encrypted
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/s3-encrypted/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.App.Crypt4GHPublicKeyB64}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download, complete body length is %v", len(body))

	assert.Equal(t, 4*80000+5*28+124, len(body), "Unexpected body length from download")

	f, err := streaming.NewCrypt4GHReader(bytes.NewReader(body), config.Config.App.Crypt4GHPrivateKey, nil)
	assert.Equal(t, nil, err, "Could not create crypt4gh reader from download")

	output, err := io.ReadAll(f)
	assert.Equal(t, nil, err, "Read from crypt4gh decoder failed")
	assert.Equal(t, []byte("datadatada"), output[:10], "Unexpected decrypted content")
	assert.Equal(t, 320000, len(output), "Unexpected decrypted content length")

	// Test encrypted download with range, should work and give output that is crypt4gh encrypted
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/s3-encrypted/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.App.Crypt4GHPublicKeyB64},
		"Range": []string{"bytes=0-9"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download, complete body length is %v", len(body))

	assert.Equal(t, 65688, len(body), "Unexpected body length from download")

	f, err = streaming.NewCrypt4GHReader(bytes.NewReader(body), config.Config.App.Crypt4GHPrivateKey, nil)
	assert.Equal(t, nil, err, "Could not create crypt4gh reader from download")

	output, err = io.ReadAll(f)
	assert.Equal(t, nil, err, "Read from crypt4gh decoder failed")
	assert.Equal(t, []byte("datadatada"), output[:10], "Unexpected decrypted content")

	// Our simple grpc doesn't inject del, but we check that the correct is passed
	assert.Equal(t, []uint64{0, 10}, faker.passedDel, "Unexpected deletions passed to reencrypt")

	// Test encrypted download with a range not from start and check the data edit list
	// is correct
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/s3-encrypted/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.App.Crypt4GHPublicKeyB64},
		"Range": []string{"bytes=131000-196999"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download, complete body length is %v", len(body))
	//
	assert.Equal(t, 124+3*65564, len(body), "Unexpected body length from download")

	f, err = streaming.NewCrypt4GHReader(bytes.NewReader(body), config.Config.App.Crypt4GHPrivateKey, nil)
	assert.Equal(t, nil, err, "Could not create crypt4gh reader from download")

	output, err = io.ReadAll(f)
	assert.Equal(t, nil, err, "Read from crypt4gh decoder failed")
	assert.Equal(t, []byte("datadatada"), output[:10], "Unexpected decrypted content")

	// Our simple grpc doesn't inject del, but we check that the correct is passed
	assert.Equal(t, []uint64{65464, 66000}, faker.passedDel, "Unexpected deletions passed to reencrypt")

	// Test encrypted download with a range not from start and check the data edit list
	// is correct
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/s3-encrypted/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.App.Crypt4GHPublicKeyB64},
		"Range": []string{"bytes=-300"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	t.Logf("Body is %v bytes", len(body))
	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download, body is %v", string(body))
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download, complete body length is %v", len(body))

	assert.Equal(t, 124+(320000-4*65536)+28, len(body), "Unexpected body length from download")

	f, err = streaming.NewCrypt4GHReader(bytes.NewReader(body), config.Config.App.Crypt4GHPrivateKey, nil)
	assert.Equal(t, nil, err, "Could not create crypt4gh reader from download")

	output, err = io.ReadAll(f)
	assert.Equal(t, nil, err, "Read from crypt4gh decoder failed")
	assert.Equal(t, []byte("datadatada"), output[:10], "Unexpected decrypted content")

	// Our simple grpc doesn't inject del, but we check that the correct is passed
	assert.Equal(t, []uint64{57556}, faker.passedDel, "Unexpected deletions passed to reencrypt")

	// Test encrypted download without passing the key, should fail
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/s3-encrypted/somepath", RawQuery: "filename=somepath"}}
	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 400, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("c4gh pub"), body[:8], "Unexpected body from download")

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile

}
