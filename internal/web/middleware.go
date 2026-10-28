package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"leadership-journal/internal/store"
)

type ctxKey int

const (
	sessionKey ctxKey = iota
	requestIDKey
)

// middleware wraps the router. Order matters: the outermost layers must still
// work when an inner one rejects the request, so even a 403 carries the
// security headers and is logged.
func (s *Server) middleware(next http.Handler) http.Handler {
	return s.recoverer(s.securityHeaders(s.requestLog(s.hostCheck(s.sameOrigin(next)))))
}

func isUnsafe(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// No scripts at all: the app is plain forms, so nothing injected into a
		// page can run. Styles come only from our own origin.
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// Pages hold sensitive data about people: keep them out of caches.
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// hostCheck defeats DNS rebinding: a hostile site can point its own domain at
// 127.0.0.1 and have the browser talk to this server as "same origin", but the
// Host header will carry the attacker's name.
func (s *Server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts[r.Host] {
			s.log.Warn("rejected request: unexpected Host header", "host", r.Host, "path", r.URL.Path, "request_id", requestID(r.Context()))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site state-changing requests, using the headers
// browsers attach themselves. This is defense in depth next to the CSRF token,
// and the only guard for the pre-login forms.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnsafe(r.Method) {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
				s.rejectCrossSite(w, r, "Sec-Fetch-Site", site)
				return
			}
			// Our Referrer-Policy is no-referrer, so browsers send "Origin: null"
			// even on same-origin form posts. Trust it only when Sec-Fetch-Site
			// vouches for the request, which was already checked above.
			sameOriginFetch := r.Header.Get("Sec-Fetch-Site") == "same-origin"
			if origin := r.Header.Get("Origin"); origin != "" && !(origin == "null" && sameOriginFetch) {
				u, err := url.Parse(origin)
				if err != nil || u.Scheme != "http" || !s.hosts[u.Host] {
					s.rejectCrossSite(w, r, "Origin", origin)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rejectCrossSite(w http.ResponseWriter, r *http.Request, header, value string) {
	s.log.Warn("rejected cross-site request", "header", header, "value", value, "method", r.Method, "path", r.URL.Path,
		"request_id", requestID(r.Context()))
	http.Error(w, "Forbidden", http.StatusForbidden)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

// requestLog logs one line per request with an id that is also sent to the
// browser, so a problem report can be matched to the log. It logs the path
// only: query strings and bodies can hold personal notes.
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(), "request_id", id)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic while handling request", "panic", rec, "method", r.Method, "path", r.URL.Path,
					"stack", string(debug.Stack()))
				http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// httpError is returned by handlers to produce a specific status page.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

var (
	errForbidden = &httpError{http.StatusForbidden, "Forbidden"}
	errBadForm   = &httpError{http.StatusBadRequest, "The form could not be read."}
)

// badInput is a mistake in what the user submitted that is not covered by a
// domain rule (e.g. an unparsable date). Its message is shown to the user.
type badInput struct{ msg string }

func (e *badInput) Error() string { return e.msg }

func userErrorf(msg string) error { return &badInput{msg: msg} }

// wrap runs a handler and turns its error into a response. Mistakes and rule
// violations become a message on the page the user came from; unknown errors
// are logged in full and shown without detail.
func (s *Server) wrap(h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnsafe(r.Method) {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}
		if err := h(w, r); err != nil {
			s.handleError(w, r, err)
		}
	})
}

func (s *Server) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpError
	switch {
	case errors.As(err, &he):
		s.renderError(w, r, he.code, he.msg)
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "Not found.")
	case isUserError(err):
		if isUnsafe(r.Method) {
			s.setFlash(w, flashError, userMessage(err))
			http.Redirect(w, r, s.backURL(r, "/"), http.StatusSeeOther)
			return
		}
		s.renderError(w, r, http.StatusBadRequest, userMessage(err))
	default:
		s.log.Error("request failed", slog.Any("error", err), "method", r.Method, "path", r.URL.Path,
			"request_id", requestID(r.Context()))
		s.renderError(w, r, http.StatusInternalServerError, "Something went wrong.")
	}
}

// backURL is where to return after an action: the form's own "back" field, or
// the page the request came from, limited to paths on this server.
func (s *Server) backURL(r *http.Request, fallback string) string {
	if b := r.PostFormValue("back"); isLocalPath(b) {
		return b
	}
	if ref := r.Referer(); ref != "" {
		if u, err := url.Parse(ref); err == nil && s.hosts[u.Host] && isLocalPath(u.RequestURI()) {
			return u.RequestURI()
		}
	}
	return fallback
}

// isLocalPath accepts only absolute paths on this server, never other sites
// or protocol-relative URLs, so a crafted "back" value cannot redirect away.
func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "/\\") && !strings.ContainsAny(p, "\r\n")
}
