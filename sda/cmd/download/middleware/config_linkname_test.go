//go:build visas
// +build visas

package middleware

import (
	"testing"
	"unsafe"
)

//go:linkname permissionModel github.com/neicnordic/sensitive-data-archive/cmd/download/config.permissionModel
var permissionModel string

//go:linkname authAllowOpaque github.com/neicnordic/sensitive-data-archive/cmd/download/config.authAllowOpaque
var authAllowOpaque bool

//go:linkname oidcIssuer github.com/neicnordic/sensitive-data-archive/cmd/download/config.oidcIssuer
var oidcIssuer string

// Use unsafe to ensure go:linkname is permitted.
var _ = unsafe.Pointer(nil)

func setPermissionModel(t *testing.T, value string) {
	prev := permissionModel
	permissionModel = value
	if t != nil {
		t.Cleanup(func() { permissionModel = prev })
	}
}

func setAuthAllowOpaque(t *testing.T, value bool) {
	prev := authAllowOpaque
	authAllowOpaque = value
	if t != nil {
		t.Cleanup(func() { authAllowOpaque = prev })
	}
}

func setOIDCIssuer(t *testing.T, value string) {
	prev := oidcIssuer
	oidcIssuer = value
	if t != nil {
		t.Cleanup(func() { oidcIssuer = prev })
	}
}
