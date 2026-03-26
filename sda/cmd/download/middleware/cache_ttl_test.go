//go:build visas

package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeCacheTTL_BoundedByTokenExp(t *testing.T) {
	ensureTestConfig(t)

	tokenExp := time.Now().Add(30 * time.Minute)

	ttl := computeCacheTTL(&tokenExp, nil)

	assert.InDelta(t, float64(30*time.Minute), float64(ttl), float64(time.Second))
}

func TestComputeCacheTTL_BoundedByVisaExpiry(t *testing.T) {
	ensureTestConfig(t)

	tokenExp := time.Now().Add(2 * time.Hour)
	visaExp := time.Now().Add(20 * time.Minute)

	ttl := computeCacheTTL(&tokenExp, &visaExp)

	assert.InDelta(t, float64(20*time.Minute), float64(ttl), float64(time.Second))
}

func TestComputeCacheTTL_UsesConfigTTLWhenNoToken(t *testing.T) {
	ensureTestConfig(t)

	ttl := computeCacheTTL(nil, nil)

	assert.Equal(t, 2*time.Hour, ttl)
}

func TestComputeCacheTTL_ExpiredToken(t *testing.T) {
	ensureTestConfig(t)

	tokenExp := time.Now().Add(-1 * time.Minute)

	ttl := computeCacheTTL(&tokenExp, nil)

	assert.Equal(t, time.Duration(0), ttl, "expired token should return TTL=0")
}

func TestComputeCacheTTL_ExpiredVisa(t *testing.T) {
	ensureTestConfig(t)

	tokenExp := time.Now().Add(1 * time.Hour)
	visaExp := time.Now().Add(-5 * time.Minute)

	ttl := computeCacheTTL(&tokenExp, &visaExp)

	assert.Equal(t, time.Duration(0), ttl, "expired visa should return TTL=0 even if token is valid")
}
