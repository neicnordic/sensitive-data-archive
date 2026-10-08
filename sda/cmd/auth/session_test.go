package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionCookie returns the session cookie set on the response, if any.
func sessionCookie(res *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range res.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}

	return nil
}

func TestFlashIsReadOnce(t *testing.T) {
	store := newSessionStore(time.Minute)

	res := httptest.NewRecorder()
	store.SetFlash(res, httptest.NewRequest(http.MethodGet, "/", nil), "key", "value")
	cookie := sessionCookie(res)
	require.NotNil(t, cookie, "setting a flash must start a session")
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	assert.Equal(t, "value", store.PopFlash(req, "key"))
	assert.Nil(t, store.PopFlash(req, "key"), "a flash can only be read once")
}

func TestFlashesInOneSessionAreIndependent(t *testing.T) {
	store := newSessionStore(time.Minute)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	store.SetFlash(res, req, "inbox", "a")
	store.SetFlash(res, req, "download", "b")
	assert.Len(t, res.Result().Cookies(), 1, "one request must not start two sessions")

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie(res))
	assert.Equal(t, "a", store.PopFlash(req, "inbox"))
	assert.Equal(t, "b", store.PopFlash(req, "download"), "reading one flash must not drop the others")
}

func TestFlashReusesExistingSession(t *testing.T) {
	store := newSessionStore(time.Minute)

	res := httptest.NewRecorder()
	store.SetFlash(res, httptest.NewRequest(http.MethodGet, "/", nil), "first", "a")
	cookie := sessionCookie(res)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	store.SetFlash(res, req, "second", "b")
	assert.Nil(t, sessionCookie(res), "a request with a valid session must not get a new one")

	assert.Equal(t, "a", store.PopFlash(req, "first"))
	assert.Equal(t, "b", store.PopFlash(req, "second"))
}

func TestFlashWithoutSession(t *testing.T) {
	store := newSessionStore(time.Minute)

	assert.Nil(t, store.PopFlash(httptest.NewRequest(http.MethodGet, "/", nil), "key"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "unknown"}) // #nosec G124 -- request built by the unit test, no browser involved
	assert.Nil(t, store.PopFlash(req, "key"))
}

func TestFlashExpires(t *testing.T) {
	store := newSessionStore(time.Minute)
	now := time.Now()
	store.now = func() time.Time { return now }

	res := httptest.NewRecorder()
	store.SetFlash(res, httptest.NewRequest(http.MethodGet, "/", nil), "key", "value")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie(res))

	now = now.Add(2 * time.Minute)
	assert.Nil(t, store.PopFlash(req, "key"), "an expired session must not be read")

	store.removeExpired()
	assert.Empty(t, store.sessions, "expired sessions must be removed from memory")
}
