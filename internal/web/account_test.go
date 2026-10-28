package web

import (
	"net/http"
	"net/url"
	"testing"

	"leadership-journal/internal/domain"
)

const newPassword = "a completely different passphrase"

func TestSettings_ChangePassword(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	other := env.client() // a second browser, logged in with the same password
	mustStatus(t, other.do(http.MethodPost, "/login", other.loginForm(testPassword)), http.StatusSeeOther)

	change := func(current, next, confirm string) {
		c.post("/settings/password", url.Values{"current": {current}, "password": {next}, "confirm": {confirm}, "back": {"/settings"}})
	}

	change("not the password", newPassword, newPassword)
	mustContain(t, c.get("/settings"), "current password is not right")

	change(testPassword, newPassword, newPassword+"x")
	mustContain(t, c.get("/settings"), "do not match")

	change(testPassword, "short", "short")
	mustContain(t, c.get("/settings"), "at least 12 characters")

	// None of those changed anything.
	fresh := env.client()
	mustStatus(t, fresh.do(http.MethodPost, "/login", fresh.loginForm(testPassword)), http.StatusSeeOther)

	change(testPassword, newPassword, newPassword)
	mustContain(t, c.get("/settings"), "Password changed")

	// The browser that changed it stays logged in; the others are logged out.
	mustStatus(t, c.get("/people"), http.StatusOK)
	rec := other.get("/people")
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/login" {
		t.Errorf("another session redirect = %q, want /login", location(rec))
	}

	// Only the new password works now.
	again := env.client()
	mustStatus(t, again.do(http.MethodPost, "/login", again.loginForm(testPassword)), http.StatusUnauthorized)
	again2 := env.client()
	mustStatus(t, again2.do(http.MethodPost, "/login", again2.loginForm(newPassword)), http.StatusSeeOther)
}

func TestSettings_ChangePasswordIsRateLimitedLikeLogin(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	for i := 0; i < 5; i++ {
		c.post("/settings/password", url.Values{"current": {"wrong wrong wrong"}, "password": {newPassword}, "confirm": {newPassword}, "back": {"/settings"}})
	}
	// Guessing the current password through this form must stay slow too.
	c.post("/settings/password", url.Values{"current": {testPassword}, "password": {newPassword}, "confirm": {newPassword}, "back": {"/settings"}})
	mustContain(t, c.get("/settings"), "Too many failed attempts")

	other := env.client()
	mustStatus(t, other.do(http.MethodPost, "/login", other.loginForm(testPassword)), http.StatusTooManyRequests)
}

func TestSettings_BackupWritesASnapshot(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	c.createPerson(t, "Ana", "report")

	rec := c.post("/settings/backup", nil)
	mustStatus(t, rec, http.StatusSeeOther)
	page := c.get("/settings")
	mustContain(t, page, "Backup written to", ".bak")
}

func TestDocumentationContributionsAreCounted(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	mk := func(body string, when int, tags []string, finalize bool) {
		id, err := env.st.CreateEntry(ctx(), domain.Entry{
			PersonID: ana, Kind: "observation", Body: body, OccurredAt: day(2024, 2, when), Tags: tags,
		})
		if err != nil {
			t.Fatal(err)
		}
		if finalize {
			env.st.FinalizeEntry(ctx(), id)
		}
	}
	mk("Wrote the incident runbook", 10, []string{"Documentation"}, true)
	mk("Updated the onboarding guide", 20, []string{"documentation", "learning"}, true)
	mk("A draft doc note", 21, []string{"documentation"}, false) // drafts are not part of the record
	mk("Unrelated note", 22, []string{"ownership"}, true)

	mustContain(t, c.get("/people/"+id(ana)), "Documentation contributions: 2")

	base := "/people/" + id(ana) + "/report"
	mustContain(t, c.get(base), "Documentation contributions: 2")
	mustContain(t, c.get(base+"?submitted=1&sections=work&from=2024-02-15&to=2024-02-28"), "Documentation contributions: 1")
	mustContain(t, c.get(base+"?format=md"), "Documentation contributions: 2")
}
