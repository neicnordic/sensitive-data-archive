package reencrypt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	client := NewClient("localhost", 50051)

	assert.NotNil(t, client)
	assert.Equal(t, "localhost", client.host)
	assert.Equal(t, 50051, client.port)
	assert.Equal(t, 10*time.Second, client.timeout) // default
}

func TestNewClient_WithTimeout(t *testing.T) {
	client := NewClient("localhost", 50051, WithTimeout(30*time.Second))

	assert.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.timeout)
}

func TestNewClient_WithTLS(t *testing.T) {
	client := NewClient("localhost", 50051, WithTLS("/path/ca.crt", "/path/client.crt", "/path/client.key"))

	assert.NotNil(t, client)
	assert.Equal(t, "/path/ca.crt", client.caCert)
	assert.Equal(t, "/path/client.crt", client.clientCert)
	assert.Equal(t, "/path/client.key", client.clientKey)
}

func TestClient_Close_WhenNotConnected(t *testing.T) {
	client := NewClient("localhost", 50051)

	// Should not error when closing without connecting
	err := client.Close()
	assert.NoError(t, err)
}

// Note: Integration tests for ReencryptHeader would require a running gRPC server
// These are better suited as integration tests in a test environment
