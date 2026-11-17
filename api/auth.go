package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "agentflight_session"

	// sessionTTL is how long a dashboard login lasts.
	sessionTTL = 12 * time.Hour
	// maxSessions bounds the in-memory session table.
	maxSessions = 1000

	// failureLimit bad credentials from one address within failureWindow earn a
	// 429 until the window ends.
	failureLimit  = 10
	failureWindow = time.Minute
)

// authenticator decides whether a request may use the API. A request is let in
// by a bearer token in its Authorization header, which scripts use, or by a
// session cookie from POST /login, which the dashboard uses because a browser
// EventSource cannot send headers. Tokens are held as SHA-256 digests and
// compared in constant time.
type authenticator struct {
	digests  [][sha256.Size]byte
	sessions *sessionStore
	failures *failureLimiter
}

func newAuthenticator(tokens []string, now func() time.Time) *authenticator {
	digests := make([][sha256.Size]byte, 0, len(tokens))
	for _, t := range tokens {
		digests = append(digests, sha256.Sum256([]byte(t)))
	}
	return &authenticator{
		digests:  digests,
		sessions: newSessionStore(sessionTTL, maxSessions, now),
		failures: newFailureLimiter(failureLimit, failureWindow, now),
	}
}

// validToken reports whether candidate is one of the configured tokens. It
// checks every token even after a match, so timing does not reveal which one.
func (a *authenticator) validToken(candidate string) bool {
	sum := sha256.Sum256([]byte(candidate))
	ok := 0
	for _, d := range a.digests {
		ok |= subtle.ConstantTimeCompare(sum[:], d[:])
	}
	return ok == 1
}

// credentialKind says how a request proved itself, and whether it tried to.
type credentialKind int

const (
	credNone        credentialKind = iota // nothing presented
	credBadBearer                         // a bearer token that is wrong
	credBearer                            // a valid bearer token
	credSession                           // a valid session cookie
	credStaleCookie                       // a cookie for a session that is gone
)

func (a *authenticator) check(r *http.Request) credentialKind {
	if token, ok := bearerToken(r); ok {
		if a.validToken(token) {
			return credBearer
		}
		return credBadBearer
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if a.sessions.valid(c.Value) {
			return credSession
		}
		return credStaleCookie
	}
	return credNone
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// isOpenPath lists what needs no credentials: the health probe, the static UI
// shell (it holds no data, and has to load to show a login form), and the
// session endpoints. The path is cleaned first so "/ui/../runs" cannot pass.
func isOpenPath(p string) bool {
	p = path.Clean(p)
	switch p {
	case "/healthz", "/session", "/login", "/logout", "/ui":
		return true
	}
	return strings.HasPrefix(p, "/ui/")
}

// sameOrigin reports whether the request's Origin header, if it has one, is the
// server's own host. A browser sends Origin on cross-site writes, so this backs
// up the SameSite=Strict cookie against cross-site request forgery.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// withAuth enforces authentication on every route except the open paths. It is
// the identity when authentication is off.
func (s *Server) withAuth(next http.Handler) http.Handler {
	a := s.auth
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		if wait, blocked := a.failures.blocked(ip); blocked {
			tooManyFailures(w, wait)
			return
		}

		switch kind := a.check(r); kind {
		case credBearer:
			next.ServeHTTP(w, r)
		case credSession:
			if !isSafeMethod(r.Method) && !sameOrigin(r) {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			next.ServeHTTP(w, r)
		default:
			if kind == credBadBearer {
				a.failures.record(ip)
				s.logger.Warn("authentication failed", "remote", ip, "method", r.Method, "path", r.URL.Path)
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="agentflight"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
		}
	})
}

func tooManyFailures(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
}

type sessionView struct {
	AuthRequired  bool `json:"auth_required"`
	Authenticated bool `json:"authenticated"`
}

// handleSession tells the dashboard whether it must show a login form. It is
// open, and reveals nothing beyond whether the caller is signed in.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, sessionView{AuthRequired: false, Authenticated: true})
		return
	}
	kind := s.auth.check(r)
	writeJSON(w, http.StatusOK, sessionView{
		AuthRequired:  true,
		Authenticated: kind == credBearer || kind == credSession,
	})
}

type loginRequest struct {
	Token string `json:"token"`
}

// handleLogin trades a valid token for a session cookie. The cookie is
// HttpOnly, so page scripts cannot read it, SameSite=Strict, and Secure when
// the connection is TLS.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "authentication is not enabled")
		return
	}
	ip := clientIP(r)
	if wait, blocked := s.auth.failures.blocked(ip); blocked {
		tooManyFailures(w, wait)
		return
	}

	var body loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !s.auth.validToken(body.Token) {
		s.auth.failures.record(ip)
		s.logger.Warn("login failed", "remote", ip)
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	id, err := s.auth.sessions.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create session: "+err.Error())
		return
	}
	http.SetCookie(w, s.sessionCookie(r, id, int(sessionTTL.Seconds())))
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout ends the session in the request's cookie, if any, and clears it.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil {
		if c, err := r.Cookie(sessionCookie); err == nil {
			s.auth.sessions.revoke(c.Value)
		}
		http.SetCookie(w, s.sessionCookie(r, "", -1))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	}
}

// clientIP is the host part of the peer address. X-Forwarded-For is not used:
// a client can set it to anything. Behind a reverse proxy every request
// therefore shares the proxy's address, and the failure limit is shared too.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// sessionStore is an in-memory table of login sessions. Sessions do not survive
// a restart, which just means signing in again.
type sessionStore struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	now func() time.Time
	m   map[string]time.Time
}

func newSessionStore(ttl time.Duration, max int, now func() time.Time) *sessionStore {
	return &sessionStore{ttl: ttl, max: max, now: now, m: make(map[string]time.Time)}
}

// create makes a session with a random 256-bit id. When the table is full it
// drops expired sessions, then the one closest to expiry.
func (s *sessionStore) create() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	id := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.m) >= s.max {
		for k, exp := range s.m {
			if !exp.After(now) {
				delete(s.m, k)
			}
		}
	}
	for len(s.m) >= s.max {
		var oldest string
		var oldestExp time.Time
		for k, exp := range s.m {
			if oldest == "" || exp.Before(oldestExp) {
				oldest, oldestExp = k, exp
			}
		}
		delete(s.m, oldest)
	}
	s.m[id] = now.Add(s.ttl)
	return id, nil
}

func (s *sessionStore) valid(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[id]
	if !ok {
		return false
	}
	if !exp.After(s.now()) {
		delete(s.m, id)
		return false
	}
	return true
}

func (s *sessionStore) revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// failureLimiter counts failed credentials per client address in a fixed
// window, so guessing a token is slow.
type failureLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	m      map[string]*failureBucket
}

type failureBucket struct {
	count int
	reset time.Time
}

func newFailureLimiter(limit int, window time.Duration, now func() time.Time) *failureLimiter {
	return &failureLimiter{limit: limit, window: window, now: now, m: make(map[string]*failureBucket)}
}

// record counts one failure for ip.
func (l *failureLimiter) record(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.m) > 1024 {
		for k, w := range l.m {
			if !w.reset.After(now) {
				delete(l.m, k)
			}
		}
	}
	w, ok := l.m[ip]
	if !ok || !w.reset.After(now) {
		w = &failureBucket{reset: now.Add(l.window)}
		l.m[ip] = w
	}
	w.count++
}

// blocked reports whether ip has used up its failures, and how long until the
// window ends.
func (l *failureLimiter) blocked(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.m[ip]
	if !ok {
		return 0, false
	}
	now := l.now()
	if !w.reset.After(now) {
		delete(l.m, ip)
		return 0, false
	}
	if w.count >= l.limit {
		return w.reset.Sub(now), true
	}
	return 0, false
}
