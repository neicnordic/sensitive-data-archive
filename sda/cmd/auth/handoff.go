package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var ErrHandoffStoreFull = errors.New("handoff store is full")

type HandoffItem struct {
	Token     string
	Exp       string
	Sub       string
	TokenType string
	CreatedAt time.Time
}

type HandoffStore interface {
	Put(item HandoffItem) (string, error)
	GetAndDelete(code string) (HandoffItem, bool)
}

type MemoryHandoffStore struct {
	mu         sync.Mutex
	data       map[string]HandoffItem
	ttl        time.Duration
	maxEntries int
}

func NewMemoryHandoffStore(ttl time.Duration, maxEntries int) *MemoryHandoffStore {
	return &MemoryHandoffStore{
		data:       make(map[string]HandoffItem),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (s *MemoryHandoffStore) Put(item HandoffItem) (string, error) {
	code, err := randomCode(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.maxEntries > 0 && len(s.data) >= s.maxEntries {
		return "", ErrHandoffStoreFull
	}

	s.data[code] = item
	return code, nil
}

func (s *MemoryHandoffStore) GetAndDelete(code string) (HandoffItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.data[code]
	if !ok {
		return HandoffItem{}, false
	}
	if time.Since(item.CreatedAt) > s.ttl {
		delete(s.data, code)
		return HandoffItem{}, false
	}
	delete(s.data, code)
	return item, true
}

func randomCode(nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CleanupExpired removes expired handoff items from the store.
// It returns the number of removed items.
func (s *MemoryHandoffStore) CleanupExpired() int {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for code, item := range s.data {
		if now.Sub(item.CreatedAt) > s.ttl {
			delete(s.data, code)
			removed++
		}
	}
	return removed
}

// StartCleanup starts a background goroutine that periodically removes expired items.
// It stops when ctx is done.
// Safe to call once; if you call multiple times, you'll start multiple cleanup loops.
func (s *MemoryHandoffStore) StartCleanup(ctx context.Context, interval time.Duration, onCleanup func(removed int)) {
	if interval <= 0 {
		interval = time.Minute
	}

	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				removed := s.CleanupExpired()
				if removed > 0 && onCleanup != nil {
					onCleanup(removed)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
