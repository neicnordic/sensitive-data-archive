package main

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

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
	mu   sync.Mutex
	data map[string]HandoffItem
	ttl  time.Duration
}

func NewMemoryHandoffStore(ttl time.Duration) *MemoryHandoffStore {
	return &MemoryHandoffStore{
		data: make(map[string]HandoffItem),
		ttl:  ttl,
	}
}

func (s *MemoryHandoffStore) Put(item HandoffItem) (string, error) {
	code, err := randomCode(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
