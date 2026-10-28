package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"leadership-journal/internal/auth"
	"leadership-journal/internal/store"
)

const (
	sessionCookie = "journal_session"
	preAuthCookie = "journal_preauth"
	flashCookie   = "journal_flash"

	maxLoginFailures = 5
	loginWindow      = 15 * time.Minute
	preAuthLifetime  = 15 * time.Minute
	maxFlashLength   = 300
)

// loginLimiter slows password guessing. There is a single owner, so one global
// counter is enough: five failures block logins for the next fifteen minutes,
// even with the right password.
type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	now      func() time.Time
	failures []time.Time
}

func newLoginLimiter(max int, window time.Duration, now func() time.Time) *loginLimiter {
	return &loginLimiter{max: max, window: window, now: now}
}

func (l *loginLimiter) prune() {
	cutoff := l.now().Add(-l.window)
	kept := l.failures[:0]
	for _, t := range l.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.failures = kept
}

func (l *loginLimiter) allowed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	return len(l.failures) < l.max
}

func (l *loginLimiter) recordFailure() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = append(l.failures, l.now())
}

func (l *loginLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = nil
}

// setCookie sets an HttpOnly, SameSite=Strict cookie. It is not marked Secure
// on purpose: the app runs over plain HTTP on loopback, and Secure cookies are
// not sent over http://localhost by every browser.
func setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// startSession logs the owner in: a fresh random token goes to the browser,
// only its hash is stored.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	csrf, err := auth.NewToken()
	if err != nil {
		return err
	}
	now := s.now()
	if _, err := s.store.DeleteExpiredSessions(r.Context(), now); err != nil {
		return err
	}
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), csrf, now.Add(s.ttl)); err != nil {
		return err
	}
	setCookie(w, sessionCookie, token, s.ttl)
	return nil
}

// currentSession finds the session of a request, if it has a valid one.
func (s *Server) currentSession(r *http.Request) (store.Session, bool, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.Session{}, false, nil
	}
	sess, err := s.store.GetSession(r.Context(), auth.HashToken(c.Value), s.now())
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, false, nil
	}
	return sess, err == nil, err
}

func sessionFromContext(ctx context.Context) (store.Session, bool) {
	sess, ok := ctx.Value(sessionKey).(store.Session)
	return sess, ok
}

// requireSession guards a handler: it needs a valid session and, for any
// state-changing request, the session's CSRF token in the form.
func (s *Server) requireSession(h handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		sess, ok, err := s.currentSession(r)
		if err != nil {
			return err
		}
		if !ok {
			clearCookie(w, sessionCookie)
			return s.redirectToLogin(w, r)
		}
		if isUnsafe(r.Method) {
			if err := r.ParseForm(); err != nil {
				return errBadForm
			}
			if !auth.Equal(r.PostFormValue("csrf_token"), sess.CSRFToken) {
				s.log.Warn("rejected request: bad CSRF token", "method", r.Method, "path", r.URL.Path, "request_id", requestID(r.Context()))
				return errForbidden
			}
		}
		return h(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	}
}

func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) error {
	has, err := s.store.HasUser(r.Context())
	if err != nil {
		return err
	}
	target := "/login"
	if !has {
		target = "/setup"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return nil
}

// preAuthToken is the CSRF token for the forms shown before login. The same
// value goes in a cookie and in a hidden field, and a post must present both.
// It is reused while the cookie lives so a re-rendered form stays valid.
func (s *Server) preAuthToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(preAuthCookie); err == nil && c.Value != "" {
		return c.Value, nil
	}
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	setCookie(w, preAuthCookie, token, preAuthLifetime)
	return token, nil
}

func checkPreAuth(r *http.Request) bool {
	c, err := r.Cookie(preAuthCookie)
	if err != nil || c.Value == "" {
		return false
	}
	return auth.Equal(r.PostFormValue("preauth_token"), c.Value)
}

type flash struct {
	Kind    string
	Message string
}

const (
	flashError = "error"
	flashOK    = "ok"
)

// setFlash leaves a one-time message for the next page the user sees.
func (s *Server) setFlash(w http.ResponseWriter, kind, message string) {
	if len(message) > maxFlashLength {
		message = message[:maxFlashLength]
	}
	raw, _ := json.Marshal(flash{Kind: kind, Message: message})
	setCookie(w, flashCookie, base64.RawURLEncoding.EncodeToString(raw), time.Minute)
}

// takeFlash reads and clears the pending message.
func (s *Server) takeFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	clearCookie(w, flashCookie)
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	var f flash
	if json.Unmarshal(raw, &f) != nil || f.Message == "" {
		return nil
	}
	if f.Kind != flashOK {
		f.Kind = flashError
	}
	return &f
}
