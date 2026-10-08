package main

import (
	"context"
	"crypto/rand"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "_session_id"

// session holds the flash values of one browser session.
type session struct {
	expires time.Time
	flashes map[string]any
}

// sessionStore keeps flash values in memory between requests. A flash is a
// value set by one request and read once by a later one, for example the s3cmd
// config that is offered for download after a login. A session, and its cookie,
// is only created when a flash is set.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	ttl      time.Duration
	now      func() time.Time
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{sessions: map[string]*session{}, ttl: ttl, now: time.Now}
}

// SetFlash stores a value under key in the session of the request, and starts
// a session if the request has none.
func (s *sessionStore) SetFlash(w http.ResponseWriter, r *http.Request, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := s.get(r)
	if sess == nil {
		id := rand.Text()
		sess = &session{flashes: map[string]any{}}
		s.sessions[id] = sess

		cookie := &http.Cookie{
			Name:     sessionCookieName,
			Value:    id,
			Path:     "/",
			MaxAge:   int(s.ttl.Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, cookie)
		// Later calls for the same request must find this session.
		r.AddCookie(cookie)
	}
	sess.expires = s.now().Add(s.ttl)
	sess.flashes[key] = value
}

// PopFlash returns the value stored under key in the session of the request
// and removes it, or nil if there is none.
func (s *sessionStore) PopFlash(r *http.Request, key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := s.get(r)
	if sess == nil {
		return nil
	}
	value := sess.flashes[key]
	delete(sess.flashes, key)

	return value
}

// get returns the unexpired session of the request, or nil. The caller must
// hold s.mu.
func (s *sessionStore) get(r *http.Request) *session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	sess, ok := s.sessions[cookie.Value]
	if !ok {
		return nil
	}
	if !s.now().Before(sess.expires) {
		delete(s.sessions, cookie.Value)

		return nil
	}

	return sess
}

// removeExpired drops the sessions whose time to live has passed.
func (s *sessionStore) removeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for id, sess := range s.sessions {
		if !now.Before(sess.expires) {
			delete(s.sessions, id)
		}
	}
}

// expireLoop removes expired sessions every ttl until ctx is done.
func (s *sessionStore) expireLoop(ctx context.Context) {
	ticker := time.NewTicker(s.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.removeExpired()
		}
	}
}
