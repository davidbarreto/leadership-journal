package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"leadership-journal/internal/auth"
	"leadership-journal/internal/store"
)

func TestHostHeaderMustBeALoopbackNameOnTheListenPort(t *testing.T) {
	env := newEnv(t)
	tests := []struct {
		host string
		ok   bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{"evil.example.com", false},
		{"evil.example.com:8080", false}, // DNS rebinding: attacker name, our port
		{"127.0.0.1:9999", false},
		{"127.0.0.1", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			rec := env.client().get("/login", "Host", tc.host)
			if tc.ok && rec.Code == http.StatusForbidden {
				t.Errorf("Host %q was rejected", tc.host)
			}
			if !tc.ok && rec.Code != http.StatusForbidden {
				t.Errorf("Host %q got status %d, want 403 (DNS rebinding protection)", tc.host, rec.Code)
			}
		})
	}
}

func TestResponsesCarrySecurityHeaders(t *testing.T) {
	env := newEnv(t)
	for _, path := range []string{"/login", "/setup", "/static/style.css", "/nope"} {
		h := env.client().get(path).Header()
		want := map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
			"Content-Security-Policy": "default-src 'none'",
		}
		for name, prefix := range want {
			if got := h.Get(name); !strings.HasPrefix(got, prefix) {
				t.Errorf("%s: %s = %q, want prefix %q", path, name, got, prefix)
			}
		}
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src") {
			t.Errorf("%s: unexpected CSP %q", path, csp)
		}
	}
}

func TestStaticFilesNeedNoLogin(t *testing.T) {
	env := newEnv(t)
	rec := env.client().get("/static/style.css")
	mustStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// protectedPaths lists the pages that must redirect to the login page.
var protectedPaths = []string{"/", "/people"}

func TestEverythingElseRequiresLogin(t *testing.T) {
	env := newEnv(t)

	rec := env.client().get("/people")
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/setup" {
		t.Errorf("before setup, redirect = %q, want /setup", location(rec))
	}

	env.loggedIn() // completes setup
	for _, path := range protectedPaths {
		rec := env.client().get(path)
		mustStatus(t, rec, http.StatusSeeOther)
		if location(rec) != "/login" {
			t.Errorf("%s redirect = %q, want /login", path, location(rec))
		}
	}
	rec = env.client().do(http.MethodPost, "/logout", url.Values{"csrf_token": {"anything"}})
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/login" {
		t.Errorf("an unauthenticated POST redirects to %q, want /login", location(rec))
	}
}

func TestSetup_RunsOnceAndNeedsAProperPassword(t *testing.T) {
	env := newEnv(t)
	c := env.client()

	rec := c.get("/setup")
	mustStatus(t, rec, http.StatusOK)
	token := preAuthField.FindStringSubmatch(rec.Body.String())[1]

	short := c.do(http.MethodPost, "/setup", url.Values{"preauth_token": {token}, "password": {"short"}, "confirm": {"short"}})
	mustStatus(t, short, http.StatusUnprocessableEntity)
	mustContain(t, short, "at least 12 characters")

	mismatch := c.do(http.MethodPost, "/setup", url.Values{"preauth_token": {token}, "password": {testPassword}, "confirm": {testPassword + "x"}})
	mustStatus(t, mismatch, http.StatusUnprocessableEntity)
	mustContain(t, mismatch, "do not match")

	ok := c.do(http.MethodPost, "/setup", url.Values{"preauth_token": {token}, "password": {testPassword}, "confirm": {testPassword}})
	mustStatus(t, ok, http.StatusSeeOther)
	if location(ok) != "/" {
		t.Errorf("redirect after setup = %q", location(ok))
	}
	mustStatus(t, c.get("/people"), http.StatusOK)

	// Once a password exists, setup is closed for everyone.
	other := env.client()
	rec = other.get("/setup")
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/login" {
		t.Errorf("setup after completion redirects to %q, want /login", location(rec))
	}
	forged := other.do(http.MethodPost, "/setup", url.Values{"preauth_token": {token}, "password": {"attacker password 1"}, "confirm": {"attacker password 1"}},
		"Cookie", "journal_preauth="+token)
	if forged.Code == http.StatusOK || forged.Code == http.StatusUnprocessableEntity {
		t.Errorf("a second setup attempt got status %d", forged.Code)
	}
	login := other.loginForm("attacker password 1")
	mustStatus(t, other.do(http.MethodPost, "/login", login), http.StatusUnauthorized)
}

func TestSetup_RejectsCrossSiteAndTokenlessPosts(t *testing.T) {
	env := newEnv(t)
	c := env.client()
	token := preAuthField.FindStringSubmatch(c.get("/setup").Body.String())[1]
	good := url.Values{"preauth_token": {token}, "password": {testPassword}, "confirm": {testPassword}}

	// A web page the owner visits can post a form to localhost. Without these
	// checks it could set the password before the owner does.
	mustStatus(t, c.do(http.MethodPost, "/setup", good, "Origin", "https://evil.example.com"), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/setup", good, "Sec-Fetch-Site", "cross-site"), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/setup", good, "Origin", "null"), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/setup", url.Values{"password": {testPassword}, "confirm": {testPassword}}), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/setup", url.Values{"preauth_token": {"guessed"}, "password": {testPassword}, "confirm": {testPassword}}), http.StatusForbidden)

	if has, _ := env.st.HasUser(ctx()); has {
		t.Fatal("a rejected request created the user anyway")
	}
}

func TestLogin_WrongPasswordsAreGenericAndRateLimited(t *testing.T) {
	env := newEnv(t)
	env.loggedIn()
	c := env.client()

	for i := 0; i < 5; i++ {
		rec := c.do(http.MethodPost, "/login", c.loginForm("wrong password "+string(rune('a'+i))))
		mustStatus(t, rec, http.StatusUnauthorized)
		mustContain(t, rec, "Invalid password")
	}

	// The limit holds even for the right password: guessing must stay slow.
	rec := c.do(http.MethodPost, "/login", c.loginForm(testPassword))
	mustStatus(t, rec, http.StatusTooManyRequests)

	env.advance(16 * time.Minute)
	rec = c.do(http.MethodPost, "/login", c.loginForm(testPassword))
	mustStatus(t, rec, http.StatusSeeOther)
}

func TestLogin_SuccessResetsTheFailureCount(t *testing.T) {
	env := newEnv(t)
	env.loggedIn()
	c := env.client()
	for i := 0; i < 4; i++ {
		c.do(http.MethodPost, "/login", c.loginForm("nope nope nope"))
	}
	mustStatus(t, c.do(http.MethodPost, "/login", c.loginForm(testPassword)), http.StatusSeeOther)

	// A new browser: the first one is logged in now and would be redirected.
	other := env.client()
	for i := 0; i < 4; i++ {
		mustStatus(t, other.do(http.MethodPost, "/login", other.loginForm("nope nope nope")), http.StatusUnauthorized)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	env := newEnv(t)
	c := env.client()
	page := c.get("/setup").Body.String()
	token := preAuthField.FindStringSubmatch(page)[1]
	rec := c.do(http.MethodPost, "/setup", url.Values{"preauth_token": {token}, "password": {testPassword}, "confirm": {testPassword}})

	var session *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "journal_session" {
			session = ck
		}
	}
	if session == nil {
		t.Fatal("no session cookie set")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.MaxAge <= 0 {
		t.Errorf("session cookie = %+v, want HttpOnly, SameSite=Strict, Path=/ and a lifetime", session)
	}
	if len(session.Value) < 40 {
		t.Errorf("session token too short: %q", session.Value)
	}

	// The database must hold only the hash of the token, never the token.
	if _, err := env.st.GetSession(ctx(), session.Value, env.now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the raw session token is stored in the database (err = %v); only its hash may be", err)
	}
	if _, err := env.st.GetSession(ctx(), auth.HashToken(session.Value), env.now); err != nil {
		t.Errorf("the session is not stored under the hash of its token: %v", err)
	}
}

func TestSessionsExpire(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	mustStatus(t, c.get("/people"), http.StatusOK)

	env.advance(13 * time.Hour)
	rec := c.get("/people")
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/login" {
		t.Errorf("expired session redirect = %q", location(rec))
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	saved := *c.cookies["journal_session"]

	rec := c.post("/logout", nil)
	mustStatus(t, rec, http.StatusSeeOther)

	replay := env.client()
	replay.cookies["journal_session"] = &saved
	rec = replay.get("/people")
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/login" {
		t.Errorf("a logged-out session was accepted again: %d %q", rec.Code, location(rec))
	}
}

func TestStateChangingRequestsNeedTheCSRFToken(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	good := c.csrf()

	mustStatus(t, c.do(http.MethodPost, "/logout", url.Values{}), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/logout", url.Values{"csrf_token": {"wrong"}}), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/logout", url.Values{"csrf_token": {good + "x"}}), http.StatusForbidden)
	// Still logged in after the failed attempts.
	mustStatus(t, c.get("/people"), http.StatusOK)
	mustStatus(t, c.do(http.MethodPost, "/logout", url.Values{"csrf_token": {good}}), http.StatusSeeOther)
}

func TestCrossSiteRequestsAreRejectedEvenWithAValidToken(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	form := url.Values{"csrf_token": {c.csrf()}}

	for _, h := range [][]string{
		{"Origin", "https://evil.example.com"},
		{"Origin", "http://127.0.0.1:9999"},
		{"Origin", "null"},
		{"Sec-Fetch-Site", "cross-site"},
		{"Sec-Fetch-Site", "same-site"},
	} {
		rec := c.do(http.MethodPost, "/logout", form, h...)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST with %s: %s got %d, want 403", h[0], h[1], rec.Code)
		}
	}
	mustStatus(t, c.get("/people"), http.StatusOK)
}

func TestPostsWithoutOriginHeadersStillNeedTheToken(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	// Non-browser clients send no Origin; the CSRF token is then the only guard.
	rec := c.do(http.MethodPost, "/logout", url.Values{"csrf_token": {"wrong"}}, "Origin", "", "Sec-Fetch-Site", "")
	mustStatus(t, rec, http.StatusForbidden)
}

func TestUnknownPathsAndMethods(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	mustStatus(t, c.get("/definitely-not-a-page"), http.StatusNotFound)
	mustStatus(t, c.do(http.MethodDelete, "/people", nil), http.StatusMethodNotAllowed)
}

func TestMarkdownIsSanitized(t *testing.T) {
	tests := []struct {
		name, in string
		has      []string
		hasNot   []string
	}{
		{"formatting works", "**bold** and a list:\n\n- one\n- two", []string{"<strong>bold</strong>", "<li>one</li>"}, nil},
		{"script tags are dropped", "hi <script>alert(1)</script>", []string{"hi"}, []string{"<script", "alert(1)</script"}},
		{"javascript links are dropped", "[x](javascript:alert(1))", nil, []string{"javascript:"}},
		{"event handlers are dropped", `<img src=x onerror="alert(1)">`, nil, []string{"onerror"}},
		{"raw html does not pass through", "<iframe src=//evil></iframe>", nil, []string{"<iframe"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(renderMarkdown(tc.in))
			for _, s := range tc.has {
				if !strings.Contains(got, s) {
					t.Errorf("output %q lacks %q", got, s)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(got, s) {
					t.Errorf("output %q contains %q", got, s)
				}
			}
		})
	}
}
