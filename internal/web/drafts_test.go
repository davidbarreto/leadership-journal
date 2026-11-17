package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"leadership-journal/internal/domain"
)

func TestDrafts_ListsWhatWaitsAndFinalizesTheSelection(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", ana)
	c.post("/meetings/"+id(mid), minutes(ana, url.Values{"note_body": {"Said in the 1:1"}}))
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"Ready to lock"}})
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"Still thinking"}})
	c.post("/commitments", url.Values{"person_id": {id(ana)}, "kind": {"task"}, "direction": {"they_owe_me"}, "content": {"Write the runbook"}})
	rid, _ := env.st.CreateRelease(ctx(), domain.Release{Name: "R24.1", StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)})
	task := c.createWorkItem(t, rid, ana, "Migrate billing job", "", "above")

	page := c.get("/drafts")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Ready to lock", "Still thinking", "Write the runbook", "Migrate billing job", "/meetings/"+id(mid),
		`Drafts <span class="badge">5</span>`)
	// The note made in the meeting is counted with it, not listed apart.
	mustNotContain(t, page, "Said in the 1:1")

	var ready, cid int64
	for _, e := range entriesOf(t, c, ana) {
		if e.Body == "Ready to lock" {
			ready = e.ID
		}
	}
	cid = firstCommitmentID(t, c, ana)
	rec := c.post("/drafts/finalize", url.Values{
		"meeting": {id(mid)}, "entry": {id(ready)}, "commitment": {id(cid)}, "task": {id(task)},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/drafts" {
		t.Errorf("redirect = %q, want /drafts", location(rec))
	}
	page = c.get("/drafts")
	mustContain(t, page, "Finalized 4 records", "Still thinking", `Drafts <span class="badge">1</span>`)
	mustNotContain(t, page, "Ready to lock", "Write the runbook", "Migrate billing job")

	m, _ := env.st.GetMeeting(ctx(), mid)
	w, _ := env.st.GetWorkItem(ctx(), task)
	if !m.IsFinalized() || !w.IsFinalized() {
		t.Errorf("meeting finalized = %v, task finalized = %v, want both", m.IsFinalized(), w.IsFinalized())
	}
}

func TestDrafts_RefusesBadSelections(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"x"}})
	eid := firstEntryID(t, c, ana)

	for _, form := range []url.Values{
		{},
		{"entry": {"abc"}},
		{"entry": {"-1"}},
	} {
		form.Set("back", "/drafts")
		rec := c.post("/drafts/finalize", form)
		mustStatus(t, rec, http.StatusSeeOther)
		page := c.get("/drafts")
		if !strings.Contains(page.Body.String(), "Select at least one") && !strings.Contains(page.Body.String(), "Invalid selection") {
			t.Errorf("form %v: no error shown", form)
		}
	}

	// Finalizing something already finalized changes nothing and says why.
	c.post("/entries/"+id(eid)+"/finalize", nil)
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"fresh"}})
	var fresh int64
	for _, e := range entriesOf(t, c, ana) {
		if e.Body == "fresh" {
			fresh = e.ID
		}
	}
	c.post("/drafts/finalize", url.Values{"entry": {id(fresh), id(eid)}, "back": {"/drafts"}})
	mustContain(t, c.get("/drafts"), "can no longer be changed", "fresh")
}

func TestDrafts_EmptyState(t *testing.T) {
	c := newEnv(t).loggedIn()
	page := c.get("/drafts")
	mustContain(t, page, "Nothing is waiting")
	mustNotContain(t, page, "Finalize selected", `Drafts <span class="badge">`)
}

func TestDashboard_LinksToTheDrafts(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"x"}})
	mustContain(t, c.get("/"), "Notes: 1", `href="/drafts"`, "Review and finalize")
}

func TestDrafts_FinalizingNeedsTheCSRFToken(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/entries", url.Values{"person_id": {id(ana)}, "kind": {"observation"}, "body": {"x"}})
	eid := firstEntryID(t, c, ana)

	mustStatus(t, c.do(http.MethodPost, "/drafts/finalize", url.Values{"entry": {id(eid)}}), http.StatusForbidden)
	mustStatus(t, c.do(http.MethodPost, "/drafts/finalize", url.Values{"entry": {id(eid)}, "csrf_token": {"wrong"}}), http.StatusForbidden)
	if e := entriesOf(t, c, ana)[0]; !e.IsDraft() {
		t.Error("a post without the token finalized the entry")
	}
}
