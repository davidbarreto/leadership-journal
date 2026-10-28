// Package web serves the journal as server-rendered HTML, bound to localhost.
// It depends on the concrete store (the only implementation) and on domain;
// all business rules live in domain and the store, never in handlers.
package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	texttemplate "text/template"
	"time"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

//go:embed templates/* static/*
var assets embed.FS

const (
	defaultSessionTTL = 12 * time.Hour
	maxFormBytes      = 1 << 20
)

// Options configures a Server.
type Options struct {
	Store *store.Store
	// Addr is the address the server listens on; it decides which Host
	// headers are accepted.
	Addr     string
	Location *time.Location
	// Now defaults to the wall clock in Location.
	Now        func() time.Time
	Logger     *slog.Logger
	SessionTTL time.Duration
	// ProgressDays defaults to domain.DefaultProgressDays.
	ProgressDays int
}

// Server is the HTTP application.
type Server struct {
	store        *store.Store
	log          *slog.Logger
	loc          *time.Location
	now          func() time.Time
	ttl          time.Duration
	progressDays int
	hosts        map[string]bool
	limiter      *loginLimiter
	pages        map[string]*template.Template
	reportMD     *texttemplate.Template
	handler      http.Handler
}

// New builds a Server.
func New(o Options) (*Server, error) {
	if o.Store == nil {
		return nil, errors.New("web: a store is required")
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		loc := o.Location
		o.Now = func() time.Time { return time.Now().In(loc) }
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.SessionTTL == 0 {
		o.SessionTTL = defaultSessionTTL
	}
	if o.ProgressDays == 0 {
		o.ProgressDays = domain.DefaultProgressDays
	}
	hosts, err := allowedHosts(o.Addr)
	if err != nil {
		return nil, err
	}
	pages, err := loadPages()
	if err != nil {
		return nil, err
	}
	reportMD, err := loadReportTemplate()
	if err != nil {
		return nil, err
	}
	s := &Server{
		store: o.Store, log: o.Logger, loc: o.Location, now: o.Now, ttl: o.SessionTTL, progressDays: o.ProgressDays,
		hosts: hosts, pages: pages, reportMD: reportMD,
		limiter: newLoginLimiter(maxLoginFailures, loginWindow, o.Now),
	}
	s.handler = s.middleware(s.routes())
	return s, nil
}

// Handler returns the complete HTTP handler, middleware included.
func (s *Server) Handler() http.Handler { return s.handler }

// allowedHosts lists the Host header values the server answers to: the
// loopback names on the listen port. Anything else, such as an attacker's
// domain rebound to 127.0.0.1, is refused.
func allowedHosts(addr string) (map[string]bool, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("web: invalid listen address %q: %w", addr, err)
	}
	return map[string]bool{
		"127.0.0.1:" + port: true,
		"localhost:" + port: true,
		"[::1]:" + port:     true,
	}, nil
}

// handlerFunc is an http.Handler that can return an error, which wrap turns
// into the right response in one place.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

type router struct {
	s   *Server
	mux *http.ServeMux
}

func (rt router) public(pattern string, h handlerFunc) { rt.mux.Handle(pattern, rt.s.wrap(h)) }
func (rt router) protected(pattern string, h handlerFunc) {
	rt.mux.Handle(pattern, rt.s.wrap(rt.s.requireSession(h)))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	rt := router{s: s, mux: mux}

	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	rt.public("GET /login", s.loginPage)
	rt.public("POST /login", s.loginSubmit)
	rt.public("GET /setup", s.setupPage)
	rt.public("POST /setup", s.setupSubmit)
	rt.protected("POST /logout", s.logout)

	rt.protected("GET /{$}", s.dashboard)
	s.registerPages(rt)
	return mux
}
