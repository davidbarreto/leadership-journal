package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"leadership-journal/internal/store"
)

const (
	testAddr     = "127.0.0.1:8080"
	testPassword = "correct horse battery staple"
)

func ctx() context.Context { return context.Background() }

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// testEnv is a server over a real SQLite store with a controllable clock.
type testEnv struct {
	t   *testing.T
	srv *Server
	st  *store.Store
	now time.Time
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	env := &testEnv{t: t, now: time.Date(2024, 3, 1, 10, 0, 0, 0, loc)}
	clock := func() time.Time { return env.now }

	env.st, err = store.Open(filepath.Join(t.TempDir(), "journal.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.st.Close() })

	env.srv, err = New(Options{
		Store:    env.st,
		Addr:     testAddr,
		Location: loc,
		Now:      clock,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *testEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

// client behaves like a browser on the same origin: it keeps cookies and adds
// the Origin and Sec-Fetch-Site headers browsers add to form posts.
type client struct {
	env     *testEnv
	cookies map[string]*http.Cookie
	token   string
}

func (e *testEnv) client() *client {
	return &client{env: e, cookies: map[string]*http.Cookie{}}
}

// do sends a request. headers is a list of key, value pairs that override the
// defaults, which is how tests simulate hostile requests.
func (c *client) do(method, path string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	c.env.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Host = testAddr
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", "http://"+testAddr)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			req.Host = headers[i+1]
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.env.srv.Handler().ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck
		}
	}
	return rec
}

func (c *client) get(path string, headers ...string) *httptest.ResponseRecorder {
	c.env.t.Helper()
	return c.do(http.MethodGet, path, nil, headers...)
}

var (
	csrfField    = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	preAuthField = regexp.MustCompile(`name="preauth_token" value="([^"]+)"`)
)

// post sends a form with the session's CSRF token filled in (fetching it from
// a page first if needed), unless the form already carries one.
func (c *client) post(path string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	c.env.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", c.csrf())
	}
	return c.do(http.MethodPost, path, form, headers...)
}

func (c *client) csrf() string {
	c.env.t.Helper()
	if c.token == "" {
		page := c.get("/people").Body.String()
		m := csrfField.FindStringSubmatch(page)
		if m == nil {
			c.env.t.Fatalf("no csrf token on /people: is the client logged in?\n%s", page)
		}
		c.token = m[1]
	}
	return c.token
}

// setup completes the first-run password setup and leaves c logged in.
func (c *client) setup() {
	c.env.t.Helper()
	page := c.get("/setup").Body.String()
	m := preAuthField.FindStringSubmatch(page)
	if m == nil {
		c.env.t.Fatalf("setup page has no pre-auth token:\n%s", page)
	}
	rec := c.do(http.MethodPost, "/setup", url.Values{
		"preauth_token": {m[1]}, "password": {testPassword}, "confirm": {testPassword},
	})
	if rec.Code != http.StatusSeeOther {
		c.env.t.Fatalf("setup status = %d: %s", rec.Code, rec.Body.String())
	}
}

// loggedIn returns a client that has finished setup.
func (e *testEnv) loggedIn() *client {
	c := e.client()
	c.setup()
	return c
}

func (c *client) loginForm(password string) url.Values {
	c.env.t.Helper()
	page := c.get("/login").Body.String()
	m := preAuthField.FindStringSubmatch(page)
	if m == nil {
		c.env.t.Fatalf("login page has no pre-auth token:\n%s", page)
	}
	return url.Values{"preauth_token": {m[1]}, "password": {password}}
}

// location returns the redirect target of a response.
func location(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; location %q; body:\n%s", rec.Code, want, location(rec), rec.Body.String())
	}
}

func mustContain(t *testing.T, rec *httptest.ResponseRecorder, parts ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Errorf("body does not contain %q:\n%s", p, body)
		}
	}
}

func mustNotContain(t *testing.T, rec *httptest.ResponseRecorder, parts ...string) {
	t.Helper()
	body := rec.Body.String()
	for _, p := range parts {
		if strings.Contains(body, p) {
			t.Errorf("body unexpectedly contains %q", p)
		}
	}
}
