package main

import (
	"context"
	"crypto/rand"
	"net/http"
	"strings"
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

	id, sess := s.get(r)
	if sess == nil {
		id = rand.Text()
		sess = &session{flashes: map[string]any{}}
		s.sessions[id] = sess
		// Later calls for the same request must find this session.
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: id}) // #nosec G124 -- added to the incoming request only, never sent to a client
	}
	sess.expires = s.now().Add(s.ttl)
	sess.flashes[key] = value

	// The cookie is set on every call so that it lives as long as the
	// session it points to, but only once per response.
	for _, c := range w.Header().Values("Set-Cookie") {
		if strings.HasPrefix(c, sessionCookieName+"="+id+";") {
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(s.ttl.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// PopFlash returns the value stored under key in the session of the request
// and removes it, or nil if there is none.
func (s *sessionStore) PopFlash(r *http.Request, key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, sess := s.get(r)
	if sess == nil {
		return nil
	}
	value := sess.flashes[key]
	delete(sess.flashes, key)

	return value
}

// get returns the first unexpired session among the session cookies of the
// request, and its id. A request can carry several, for example one that
// outlived a restart of the service, or one that Iris set for the parent
// domain before this store replaced it. The caller must hold s.mu.
func (s *sessionStore) get(r *http.Request) (string, *session) {
	for _, cookie := range r.CookiesNamed(sessionCookieName) {
		sess, ok := s.sessions[cookie.Value]
		if !ok {
			continue
		}
		if !s.now().Before(sess.expires) {
			delete(s.sessions, cookie.Value)

			continue
		}

		return cookie.Value, sess
	}

	return "", nil
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
