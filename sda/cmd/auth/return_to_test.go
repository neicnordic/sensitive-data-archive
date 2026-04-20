package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateReturnTo(t *testing.T) {
	t.Run("valid https url", func(t *testing.T) {
		u, err := validateReturnTo("https://portal.com/auth/callback")
		assert.NoError(t, err)
		assert.Equal(t, "https", u.Scheme)
		assert.Equal(t, "portal.com", u.Hostname())
	})

	t.Run("reject fragment", func(t *testing.T) {
		_, err := validateReturnTo("https://portal.com/auth/callback#frag")
		assert.Error(t, err)
	})

	t.Run("reject userinfo", func(t *testing.T) {
		_, err := validateReturnTo("https://user:pass@portal.com/auth/callback")
		assert.Error(t, err)
	})

	t.Run("reject missing scheme", func(t *testing.T) {
		_, err := validateReturnTo("portal.com/auth/callback")
		assert.Error(t, err)
	})

	t.Run("reject relative url", func(t *testing.T) {
		_, err := validateReturnTo("/auth/callback")
		assert.Error(t, err)
	})
}

func TestIsAllowedReturnTo(t *testing.T) {
	allowlist := normalizeAllowlist([]string{
		" https://portal.com/auth/callback ",
		"",
		"  ",
	})

	assert.True(t, isAllowedReturnTo("https://portal.com/auth/callback", allowlist))
	assert.False(t, isAllowedReturnTo("https://portal.com/other", allowlist))
	assert.False(t, isAllowedReturnTo("http://portal.com/auth/callback", allowlist))
}

func TestIsLocalhost(t *testing.T) {
	assert.True(t, isLocalhost("localhost"))
	assert.True(t, isLocalhost("127.0.0.1"))
	assert.False(t, isLocalhost("portal.com"))
}
