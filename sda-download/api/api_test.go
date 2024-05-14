package api

import (
	"testing"

	"github.com/neicnordic/sda-download/internal/config"
)

func TestSetup(t *testing.T) {

	// Create web server app
	config.Config.App.Host = "localhost"
	config.Config.App.Port = 8080
	server := Setup()

	// Verify that server address is correctly read from config
	expectedAddress := "localhost:8080"
	if server.Addr != expectedAddress {
		t.Errorf("server address was not correctly formed, expected=%s, received=%s", expectedAddress, server.Addr)
	}
}
