package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryHandoffStore_PutAndGetAndDelete(t *testing.T) {
	s := NewMemoryHandoffStore(2*time.Minute, 100)

	item := HandoffItem{
		Token:     "token",
		Exp:       "2099-01-01 00:00:00",
		Sub:       "user",
		TokenType: "raw",
		CreatedAt: time.Now().UTC(),
	}

	code, err := s.Put(item)
	assert.NoError(t, err)
	assert.NotEmpty(t, code)

	got, ok := s.GetAndDelete(code)
	assert.True(t, ok)
	assert.Equal(t, item.Token, got.Token)
	assert.Equal(t, item.Exp, got.Exp)
	assert.Equal(t, item.Sub, got.Sub)
	assert.Equal(t, item.TokenType, got.TokenType)

	// single-use: second call must fail
	_, ok = s.GetAndDelete(code)
	assert.False(t, ok)
}

func TestMemoryHandoffStore_Expired(t *testing.T) {
	s := NewMemoryHandoffStore(1*time.Millisecond, 100)

	item := HandoffItem{
		Token:     "token",
		Exp:       "2099-01-01 00:00:00",
		Sub:       "user",
		TokenType: "raw",
		CreatedAt: time.Now().UTC(),
	}

	code, err := s.Put(item)
	assert.NoError(t, err)

	// Wait until TTL expires
	time.Sleep(5 * time.Millisecond)

	_, ok := s.GetAndDelete(code)
	assert.False(t, ok)
}

func TestMemoryHandoffStore_GetMissing(t *testing.T) {
	s := NewMemoryHandoffStore(2*time.Minute, 100)

	_, ok := s.GetAndDelete("does-not-exist")
	assert.False(t, ok)
}

func TestRandomCode_UniqueEnough(t *testing.T) {
	// Not a strict uniqueness proof; just a sanity check.
	s := NewMemoryHandoffStore(2*time.Minute, 100)

	item := HandoffItem{
		Token:     "token",
		Exp:       "2099-01-01 00:00:00",
		Sub:       "user",
		TokenType: "raw",
		CreatedAt: time.Now().UTC(),
	}

	code1, err := s.Put(item)
	assert.NoError(t, err)
	code2, err := s.Put(item)
	assert.NoError(t, err)

	assert.NotEqual(t, code1, code2)
}

func TestMemoryHandoffStore_Full(t *testing.T) {
	s := NewMemoryHandoffStore(2*time.Minute, 1)

	_, err := s.Put(HandoffItem{CreatedAt: time.Now().UTC()})
	assert.NoError(t, err)

	_, err = s.Put(HandoffItem{CreatedAt: time.Now().UTC()})
	assert.ErrorIs(t, err, ErrHandoffStoreFull)
}

func TestMemoryHandoffStore_CleanupExpired(t *testing.T) {
	s := NewMemoryHandoffStore(10*time.Millisecond, 100)

	// insert expired
	s.mu.Lock()
	s.data["x"] = HandoffItem{CreatedAt: time.Now().Add(-time.Hour)}
	s.mu.Unlock()

	removed := s.CleanupExpired()
	assert.Equal(t, 1, removed)
	assert.Equal(t, 0, len(s.data))
}
