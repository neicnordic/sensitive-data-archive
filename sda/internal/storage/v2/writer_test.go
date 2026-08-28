package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockS3 answers just enough of the S3 API for findActiveBucket: listing
// buckets, creating one, and listing its contents.
type mockS3 struct{ buckets map[string]bool }

func (m *mockS3) handler(w http.ResponseWriter, req *http.Request) {
	switch {
	case strings.HasSuffix(req.RequestURI, "ListBuckets"):
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult><Buckets>`)
		for name := range m.buckets {
			fmt.Fprintf(&b, `<Bucket><Name>%s</Name></Bucket>`, name)
		}
		b.WriteString(`</Buckets></ListAllMyBucketsResult>`)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(b.String()))
	case req.Method == "PUT":
		m.buckets[strings.Trim(strings.Split(req.RequestURI, "?")[0], "/")] = true
		w.WriteHeader(http.StatusOK)
	case strings.HasSuffix(req.RequestURI, "list-type=2"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><IsTruncated>false</IsTruncated><KeyCount>0</KeyCount></ListBucketResult>`))
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// stubBroker reports every location as empty, so any location is usable.
type stubBroker struct{}

func (stubBroker) GetObjectCount(_ context.Context, _, _ string) (uint64, error) { return 0, nil }
func (stubBroker) GetSize(_ context.Context, _, _ string) (uint64, error)        { return 0, nil }
func (stubBroker) RegisterSizeAndCountFinderFunc(_ string, _ func(string) bool, _ func(context.Context, string) (uint64, uint64, error)) {
}

func loadTestConfig(t *testing.T, config string) {
	t.Helper()

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(config), 0600))

	viper.Reset()
	viper.SetConfigType("yaml")
	viper.SetConfigFile(cfgFile)
	require.NoError(t, viper.ReadInConfig())
}

func newPosixDir(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive")
	require.NoError(t, os.MkdirAll(path, 0750))

	return path
}

// A writer_disabled s3 entry is the config for a reader, so it must not count
// as an s3 writer. Pairing one with an active posix writer has to select the
// posix writer rather than report a conflict.
func TestNewWriterWriterDisabledS3WithPosixWriter(t *testing.T) {
	s3 := &mockS3{buckets: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(s3.handler))
	defer srv.Close()

	loadTestConfig(t, fmt.Sprintf(`
storage:
  test:
    s3:
    - endpoint: %s
      access_key: ak
      secret_key: sk
      bucket_prefix: legacy-archive-
      region: us-east-1
      disable_https: true
      writer_disabled: true
    posix:
    - path: %s
`, srv.URL, newPosixDir(t)))

	writer, err := NewWriter(context.Background(), "test", stubBroker{})
	require.NoError(t, err)
	assert.NotNil(t, writer)
}

// The mirror image: an active s3 writer alongside a reader-only posix entry.
func TestNewWriterWriterDisabledPosixWithS3Writer(t *testing.T) {
	s3 := &mockS3{buckets: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(s3.handler))
	defer srv.Close()

	loadTestConfig(t, fmt.Sprintf(`
storage:
  test:
    s3:
    - endpoint: %s
      access_key: ak
      secret_key: sk
      bucket_prefix: archive-
      region: us-east-1
      disable_https: true
    posix:
    - path: %s
      writer_disabled: true
`, srv.URL, newPosixDir(t)))

	writer, err := NewWriter(context.Background(), "test", stubBroker{})
	require.NoError(t, err)
	assert.NotNil(t, writer)
}

// Two active writers is still a conflict, which is the behaviour the
// writer_disabled flag exists to let operators resolve.
func TestNewWriterTwoActiveWritersConflict(t *testing.T) {
	s3 := &mockS3{buckets: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(s3.handler))
	defer srv.Close()

	loadTestConfig(t, fmt.Sprintf(`
storage:
  test:
    s3:
    - endpoint: %s
      access_key: ak
      secret_key: sk
      bucket_prefix: archive-
      region: us-east-1
      disable_https: true
    posix:
    - path: %s
`, srv.URL, newPosixDir(t)))

	_, err := NewWriter(context.Background(), "test", stubBroker{})
	require.ErrorIs(t, err, storageerrors.ErrorMultipleWritersNotSupported)
}

// Deleting counts as a write, so a reader-only endpoint is not a valid target
// for RemoveFile. This is the same-backend migration shape: one legacy s3
// endpoint kept readable next to the endpoint currently being written to.
func TestRemoveFileAtWriterDisabledEndpointNotSupported(t *testing.T) {
	legacy := &mockS3{buckets: map[string]bool{}}
	legacySrv := httptest.NewServer(http.HandlerFunc(legacy.handler))
	defer legacySrv.Close()
	current := &mockS3{buckets: map[string]bool{}}
	currentSrv := httptest.NewServer(http.HandlerFunc(current.handler))
	defer currentSrv.Close()

	loadTestConfig(t, fmt.Sprintf(`
storage:
  test:
    s3:
    - endpoint: %s
      access_key: ak
      secret_key: sk
      bucket_prefix: legacy-
      region: us-east-1
      disable_https: true
      writer_disabled: true
    - endpoint: %s
      access_key: ak
      secret_key: sk
      bucket_prefix: current-
      region: us-east-1
      disable_https: true
`, legacySrv.URL, currentSrv.URL))

	writer, err := NewWriter(context.Background(), "test", stubBroker{})
	require.NoError(t, err)

	err = writer.RemoveFile(context.Background(), legacySrv.URL+"/legacy-0", "somefile.c4gh")
	assert.ErrorIs(t, err, storageerrors.ErrorNoEndpointConfiguredForLocation)
}

// With every entry on both sides read-only there is no writer to hand back.
func TestNewWriterAllEntriesWriterDisabled(t *testing.T) {
	loadTestConfig(t, fmt.Sprintf(`
storage:
  test:
    posix:
    - path: %s
      writer_disabled: true
`, newPosixDir(t)))

	_, err := NewWriter(context.Background(), "test", stubBroker{})
	require.ErrorIs(t, err, storageerrors.ErrorNoValidWriter)
}
