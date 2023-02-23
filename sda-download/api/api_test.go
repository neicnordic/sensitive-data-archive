package api

import (
	"crypto/tls"
	"testing"

	"github.com/neicnordic/sda-download/internal/config"
)

func TestSetup(t *testing.T) {

	// Create web server app
	config.Config.App.Host = "localhost"
	config.Config.App.Port = 8080
	server := Setup()

	// Verify that TLS is configured and set for minimum suggested version
	if server.TLSConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("server TLS version is too low, expected=%d, got=%d", tls.VersionTLS12, server.TLSConfig.MinVersion)
	}

	// Verify that server address is correctly read from config
	expectedAddress := "localhost:8080"
	if server.Addr != expectedAddress {
		t.Errorf("server address was not correctly formed, expected=%s, received=%s", expectedAddress, server.Addr)
	}
}
