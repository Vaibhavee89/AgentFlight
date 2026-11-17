package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testToken  = "test-token-0123456789"
	otherToken = "second-token-9876543210"
)

// fakeClock is a clock the tests move by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newAuthServer builds a server with authentication on, driven by clock.
func newAuthServer(t *testing.T, clock *fakeClock, tokens ...string) *Server {
	t.Helper()
	srv, _ := newTestServer(t, 0)
	srv.cfg.AuthTokens = tokens
	srv.auth = newAuthenticator(tokens, clock.Now)
	return srv
}

// call sends one request straight to the handler, from the given peer address.
func call(h http.Handler, method, target, remote string, headers map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestAuth_ProtectedRoutesNeedCredentials(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{http.MethodPost, "/runs", `{"prompt":"x"}`},
		{http.MethodGet, "/runs", ""},
		{http.MethodGet, "/runs/abc", ""},
		{http.MethodGet, "/runs/abc/events", ""},
		{http.MethodGet, "/runs/abc/stream", ""},
		{http.MethodPost, "/runs/abc/cancel", ""},
		{http.MethodPost, "/runs/abc/resume", ""},
		{http.MethodPost, "/runs/abc/replay", ""},
		{http.MethodGet, "/bench/results", ""},
		{http.MethodGet, "/bench/compare", ""},
		{http.MethodGet, "/ui/../runs", ""}, // dot segments must not slip through the open /ui prefix
	}
	credentials := []struct {
		name    string
		headers map[string]string
		allowed bool
	}{
		{"none", nil, false},
		{"wrong token", bearer("not-the-token-at-all"), false},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}, false},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + testToken}, false},
		{"token without a scheme", map[string]string{"Authorization": testToken}, false},
		{"token as a prefix of the real one", bearer(testToken[:10]), false},
		{"the right token", bearer(testToken), true},
		{"scheme is case-insensitive", map[string]string{"Authorization": "bearer " + testToken}, true},
	}

	srv := newAuthServer(t, newFakeClock(), testToken)
	h := srv.Handler()
	n := 0
	for _, rt := range routes {
		for _, c := range credentials {
			n++
			// A fresh address for each case keeps the failure limiter out of this test.
			remote := fmt.Sprintf("10.0.0.%d:1", n)
			t.Run(rt.method+" "+rt.path+" / "+c.name, func(t *testing.T) {
				rec := call(h, rt.method, rt.path, remote, c.headers, rt.body)
				denied := rec.Code == http.StatusUnauthorized
				if denied == c.allowed {
					t.Fatalf("status = %d, want denied = %v", rec.Code, !c.allowed)
				}
				if denied && rec.Header().Get("WWW-Authenticate") == "" {
					t.Fatal("401 without a WWW-Authenticate header")
				}
			})
		}
	}
}

func TestAuth_OpenRoutesNeedNoCredentials(t *testing.T) {
	h := newAuthServer(t, newFakeClock(), testToken).Handler()
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/session", http.StatusOK},
		{http.MethodGet, "/ui/", http.StatusOK},
		{http.MethodGet, "/ui", http.StatusMovedPermanently},
		{http.MethodPost, "/logout", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			if rec := call(h, tt.method, tt.path, "198.51.100.2:1", nil, ""); rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestAuth_DisabledLeavesTheAPIOpen(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	h := srv.Handler()

	if rec := call(h, http.MethodGet, "/runs", "198.51.100.3:1", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /runs without auth enabled = %d, want 200", rec.Code)
	}
	rec := call(h, http.MethodGet, "/session", "198.51.100.3:1", nil, "")
	if !strings.Contains(rec.Body.String(), `"auth_required":false`) {
		t.Fatalf("/session = %s, want auth_required false", rec.Body)
	}
	if rec := call(h, http.MethodPost, "/login", "198.51.100.3:1", nil, `{"token":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("POST /login with auth off = %d, want 404", rec.Code)
	}
}

func TestAuth_MoreThanOneTokenWorks(t *testing.T) {
	h := newAuthServer(t, newFakeClock(), testToken, otherToken).Handler()
	for _, tok := range []string{testToken, otherToken} {
		if rec := call(h, http.MethodGet, "/runs", "198.51.100.4:1", bearer(tok), ""); rec.Code != http.StatusOK {
			t.Fatalf("token %q: status = %d, want 200", tok, rec.Code)
		}
	}
	if rec := call(h, http.MethodGet, "/runs", "198.51.100.4:1", bearer("a-third-token-unknown"), ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: status = %d, want 401", rec.Code)
	}
}

// loginClient returns an HTTP client with a cookie jar, against a real server.
func loginClient(t *testing.T, srv *Server) (*http.Client, string) {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}, ts.URL
}

func do(t *testing.T, c *http.Client, method, url, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func TestAuth_LoginSessionLifecycle(t *testing.T) {
	clock := newFakeClock()
	srv := newAuthServer(t, clock, testToken)
	client, base := loginClient(t, srv)

	// Signed out: data is refused and the session endpoint says why.
	if resp, _ := do(t, client, http.MethodGet, base+"/runs", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /runs before login = %d, want 401", resp.StatusCode)
	}
	if _, body := do(t, client, http.MethodGet, base+"/session", "", nil); !strings.Contains(body, `"authenticated":false`) || !strings.Contains(body, `"auth_required":true`) {
		t.Fatalf("/session before login = %s", body)
	}

	// A wrong token sets no cookie.
	resp, _ := do(t, client, http.MethodPost, base+"/login", `{"token":"definitely-wrong-token"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
		t.Fatalf("bad login = %d with %d cookies, want 401 and none", resp.StatusCode, len(resp.Cookies()))
	}

	// The right token gives a cookie that cannot be read by scripts or sent cross-site.
	resp, _ = do(t, client, http.MethodPost, base+"/login", `{"token":"`+testToken+`"}`, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login = %d, want 204", resp.StatusCode)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login set no session cookie")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.MaxAge != int(sessionTTL.Seconds()) {
		t.Fatalf("cookie = %+v, want HttpOnly, SameSite=Strict, Path=/, MaxAge=%d", session, int(sessionTTL.Seconds()))
	}
	if strings.Contains(session.Value, testToken) || len(session.Value) != 64 {
		t.Fatalf("cookie value %q must be a random id, not the token", session.Value)
	}

	if resp, _ := do(t, client, http.MethodGet, base+"/runs", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /runs with session = %d, want 200", resp.StatusCode)
	}
	if _, body := do(t, client, http.MethodGet, base+"/session", "", nil); !strings.Contains(body, `"authenticated":true`) {
		t.Fatalf("/session after login = %s", body)
	}

	// Logging out ends the session on the server, so the old cookie is dead.
	if resp, _ := do(t, client, http.MethodPost, base+"/logout", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", resp.StatusCode)
	}
	stolen := map[string]string{"Cookie": sessionCookie + "=" + session.Value}
	fresh := &http.Client{}
	if resp, _ := do(t, fresh, http.MethodGet, base+"/runs", "", stolen); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /runs with a logged-out cookie = %d, want 401", resp.StatusCode)
	}
}

func TestAuth_SessionExpires(t *testing.T) {
	clock := newFakeClock()
	h := newAuthServer(t, clock, testToken).Handler()

	rec := call(h, http.MethodPost, "/login", "198.51.100.5:1", nil, `{"token":"`+testToken+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login = %d", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]
	headers := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	clock.Advance(sessionTTL - time.Minute)
	if rec := call(h, http.MethodGet, "/runs", "198.51.100.5:1", headers, ""); rec.Code != http.StatusOK {
		t.Fatalf("just before expiry = %d, want 200", rec.Code)
	}
	clock.Advance(2 * time.Minute)
	if rec := call(h, http.MethodGet, "/runs", "198.51.100.5:1", headers, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after expiry = %d, want 401", rec.Code)
	}
}

// The session cookie is sent by the browser on its own, so a write that comes
// from another site must be refused. A bearer token is never sent on its own.
func TestAuth_CrossSiteWriteWithSessionIsRefused(t *testing.T) {
	h := newAuthServer(t, newFakeClock(), testToken).Handler()
	rec := call(h, http.MethodPost, "/login", "198.51.100.6:1", nil, `{"token":"`+testToken+`"}`)
	cookie := rec.Result().Cookies()[0]
	session := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"session, same origin", merge(session, map[string]string{"Origin": "http://example.com"}), http.StatusAccepted},
		{"session, no Origin header", session, http.StatusAccepted},
		{"session, other origin", merge(session, map[string]string{"Origin": "http://evil.example"}), http.StatusForbidden},
		{"bearer, other origin", merge(bearer(testToken), map[string]string{"Origin": "http://evil.example"}), http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := call(h, http.MethodPost, "http://example.com/runs", "198.51.100.6:1", tt.headers, `{"prompt":"hi"}`)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// The dashboard follows a live run over EventSource, which cannot send an
// Authorization header, so the stream has to accept the session cookie.
func TestAuth_StreamWorksWithSessionCookie(t *testing.T) {
	srv := newAuthServer(t, newFakeClock(), testToken)
	client, base := loginClient(t, srv)

	if resp, _ := do(t, client, http.MethodPost, base+"/login", `{"token":"`+testToken+`"}`, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	resp, body := do(t, client, http.MethodPost, base+"/runs", `{"prompt":"hi"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /runs = %d: %s", resp.StatusCode, body)
	}
	id := between(body, `"id":"`, `"`)

	resp, stream := do(t, client, http.MethodGet, base+"/runs/"+id+"/stream", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream = %d %q, want 200 text/event-stream", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(stream, "event: run_finished") {
		t.Fatalf("stream did not reach run_finished:\n%s", stream)
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	if j := strings.Index(s, end); j >= 0 {
		return s[:j]
	}
	return s
}

func TestAuth_RepeatedFailuresAreThrottledPerAddress(t *testing.T) {
	clock := newFakeClock()
	h := newAuthServer(t, clock, testToken).Handler()
	const attacker, bystander = "203.0.113.9:1", "203.0.113.10:1"

	for i := 0; i < failureLimit; i++ {
		if rec := call(h, http.MethodGet, "/runs", attacker, bearer("guess-number-"+string(rune('a'+i))), ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d = %d, want 401", i, rec.Code)
		}
	}

	// Past the limit even the right token is refused, with a hint to wait.
	rec := call(h, http.MethodGet, "/runs", attacker, bearer(testToken), "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after %d failures = %d (Retry-After %q), want 429 with Retry-After", failureLimit, rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := call(h, http.MethodPost, "/login", attacker, nil, `{"token":"`+testToken+`"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login while throttled = %d, want 429", rec.Code)
	}

	// Another address is not affected.
	if rec := call(h, http.MethodGet, "/runs", bystander, bearer(testToken), ""); rec.Code != http.StatusOK {
		t.Fatalf("other address = %d, want 200", rec.Code)
	}

	// The window ends and the address is let back in.
	clock.Advance(failureWindow + time.Second)
	if rec := call(h, http.MethodGet, "/runs", attacker, bearer(testToken), ""); rec.Code != http.StatusOK {
		t.Fatalf("after the window = %d, want 200", rec.Code)
	}
}

func TestAuth_FailedLoginsCountTowardsTheLimit(t *testing.T) {
	h := newAuthServer(t, newFakeClock(), testToken).Handler()
	const peer = "203.0.113.20:1"
	for i := 0; i < failureLimit; i++ {
		call(h, http.MethodPost, "/login", peer, nil, `{"token":"wrong-token-number-`+string(rune('a'+i))+`"}`)
	}
	if rec := call(h, http.MethodPost, "/login", peer, nil, `{"token":"`+testToken+`"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login after %d bad logins = %d, want 429", failureLimit, rec.Code)
	}
}

// Requests with no credentials, and valid ones, are not guesses and must not
// use up the allowance.
func TestAuth_OnlyWrongTokensCountAsFailures(t *testing.T) {
	h := newAuthServer(t, newFakeClock(), testToken).Handler()
	const peer = "203.0.113.30:1"
	for i := 0; i < failureLimit*3; i++ {
		call(h, http.MethodGet, "/runs", peer, nil, "")
		call(h, http.MethodGet, "/runs", peer, bearer(testToken), "")
		call(h, http.MethodGet, "/runs", peer, map[string]string{"Cookie": sessionCookie + "=stale"}, "")
	}
	if rec := call(h, http.MethodGet, "/runs", peer, bearer(testToken), ""); rec.Code != http.StatusOK {
		t.Fatalf("after many credential-less and valid requests = %d, want 200", rec.Code)
	}
}

func TestSessionStore(t *testing.T) {
	clock := newFakeClock()

	t.Run("ids are random and unique", func(t *testing.T) {
		s := newSessionStore(time.Hour, 100, clock.Now)
		seen := map[string]bool{}
		for i := 0; i < 50; i++ {
			id, err := s.create()
			if err != nil || len(id) != 64 || seen[id] {
				t.Fatalf("create() = %q, %v (duplicate or malformed)", id, err)
			}
			seen[id] = true
		}
	})
	t.Run("unknown id is not valid", func(t *testing.T) {
		if newSessionStore(time.Hour, 10, clock.Now).valid("nope") {
			t.Fatal("unknown session reported valid")
		}
	})
	t.Run("a full table drops the soonest to expire", func(t *testing.T) {
		s := newSessionStore(time.Hour, 3, clock.Now)
		var ids []string
		for i := 0; i < 4; i++ {
			clock.Advance(time.Second)
			id, _ := s.create()
			ids = append(ids, id)
		}
		if len(s.m) != 3 {
			t.Fatalf("table holds %d sessions, want 3", len(s.m))
		}
		if s.valid(ids[0]) || !s.valid(ids[3]) {
			t.Fatal("expected the oldest session evicted and the newest kept")
		}
	})
	t.Run("revoke ends a session", func(t *testing.T) {
		s := newSessionStore(time.Hour, 10, clock.Now)
		id, _ := s.create()
		s.revoke(id)
		if s.valid(id) {
			t.Fatal("revoked session still valid")
		}
	})
}
