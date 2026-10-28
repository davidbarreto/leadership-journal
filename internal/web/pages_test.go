package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func idFrom(t *testing.T, loc, prefix string) int64 {
	t.Helper()
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("redirect %q does not start with %q", loc, prefix)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(loc, prefix), 10, 64)
	if err != nil {
		t.Fatalf("redirect %q has no numeric id: %v", loc, err)
	}
	return id
}

func (c *client) createPerson(t *testing.T, name, relationship string) int64 {
	t.Helper()
	rec := c.post("/people", url.Values{"name": {name}, "role_title": {"Backend engineer"}, "relationship": {relationship}})
	mustStatus(t, rec, http.StatusSeeOther)
	return idFrom(t, location(rec), "/people/")
}

func (c *client) createMeeting(t *testing.T, kind string, personID int64) int64 {
	t.Helper()
	form := url.Values{"kind": {kind}, "starts_at": {"2024-03-01T09:30"}}
	if personID != 0 {
		form.Set("person_id", strconv.FormatInt(personID, 10))
	}
	rec := c.post("/meetings", form)
	mustStatus(t, rec, http.StatusSeeOther)
	return idFrom(t, location(rec), "/meetings/")
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func TestPeople_EmptyList(t *testing.T) {
	c := newEnv(t).loggedIn()
	mustContain(t, c.get("/people"), "No people yet")
}

func TestPeople_CreateShowAndEdit(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	teamID, err := env.st.CreateTeam(ctx(), "Payments")
	if err != nil {
		t.Fatal(err)
	}

	rec := c.post("/people", url.Values{
		"name": {"Ana Souza"}, "role_title": {"Backend engineer"}, "relationship": {"report"}, "team_id": {id(teamID)},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	pid := idFrom(t, location(rec), "/people/")

	page := c.get(location(rec))
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Ana Souza", "Backend engineer", "Payments", "Person added.")

	mustContain(t, c.get("/people"), "Ana Souza", "Payments")

	edit := c.get("/people/" + id(pid) + "/edit")
	mustStatus(t, edit, http.StatusOK)
	mustContain(t, edit, `value="Ana Souza"`)

	rec = c.post("/people/"+id(pid), url.Values{"name": {"Ana S. Souza"}, "role_title": {"Staff engineer"}, "relationship": {"report"}})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get("/people/" + id(pid))
	mustContain(t, page, "Ana S. Souza", "Staff engineer")
	mustNotContain(t, page, "Payments") // the team was cleared by the edit
}

func TestPeople_ManagerAndSelfAppearInTheirOwnGroups(t *testing.T) {
	c := newEnv(t).loggedIn()
	c.createPerson(t, "Me Myself", "self")
	c.createPerson(t, "Big Boss", "manager")
	c.createPerson(t, "Ana", "report")

	page := c.get("/people")
	mustContain(t, page, "Reports", "Your manager", "You", "Me Myself", "Big Boss", "Ana")
}

func TestPeople_ValidationMessagesAreShownToTheUser(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()

	rec := c.post("/people", url.Values{"name": {"  "}, "relationship": {"report"}, "back": {"/people"}})
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/people" {
		t.Fatalf("redirect = %q, want back to /people", location(rec))
	}
	mustContain(t, c.get("/people"), "Name is required.")
	mustNotContain(t, c.get("/people"), "Name is required.") // a flash shows only once

	team, _ := env.st.CreateTeam(ctx(), "Payments")
	rec = c.post("/people", url.Values{"name": {"Boss"}, "relationship": {"manager"}, "team_id": {id(team)}, "back": {"/people"}})
	mustStatus(t, rec, http.StatusSeeOther)
	mustContain(t, c.get("/people"), "Only reports can belong to a team")
}

func TestPeople_UnknownOrMalformedIDsAre404(t *testing.T) {
	c := newEnv(t).loggedIn()
	for _, path := range []string{"/people/999", "/people/abc", "/people/0", "/people/-1", "/people/999/edit"} {
		mustStatus(t, c.get(path), http.StatusNotFound)
	}
}

func TestPeople_LevelHistoryFollowsTheRules(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	levels, _ := env.st.ListLevels(ctx())
	byCode := map[string]string{}
	for _, l := range levels {
		byCode[l.Code] = id(l.ID)
	}
	back := "/people/" + id(pid)

	for _, step := range []struct{ code, from string }{{"E3", "2022-01-01"}, {"E5", "2024-01-01"}} {
		rec := c.post(back+"/level", url.Values{"level_id": {byCode[step.code]}, "effective_from": {step.from}})
		mustStatus(t, rec, http.StatusSeeOther)
	}
	page := c.get(back)
	mustContain(t, page, "E5", "E3", "1 Jan 2022", "1 Jan 2024")

	c.post(back+"/level", url.Values{"level_id": {byCode["E4"]}, "effective_from": {"2025-01-01"}, "back": {back}})
	mustContain(t, c.get(back), "higher rank")

	c.post(back+"/level", url.Values{"level_id": {byCode["E4"]}, "effective_from": {"not a date"}, "back": {back}})
	mustContain(t, c.get(back), "date")
}

func TestMeeting_FullLifecycle(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	url1 := "/meetings/" + id(mid)

	page := c.get(url1)
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Draft", "What did you learn in the last 2 weeks?", "How did you apply it?",
		`<textarea name="notes"`, "Finalize meeting", "Ana")

	rec := c.post(url1, url.Values{
		"title": {"Biweekly 1:1"}, "starts_at": {"2024-03-05T14:00"}, "notes": {"Talked about the **migration**"},
		"answer_learned": {"Go interfaces"}, "answer_applied": {""}, "person_id": {id(pid)},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(url1)
	mustContain(t, page, "Draft saved.", "Go interfaces", "Biweekly 1:1")

	// Things noted in the meeting belong to it.
	rec = c.post("/entries", url.Values{
		"person_id": {id(pid)}, "meeting_id": {id(mid)}, "kind": {"observation"}, "body": {"Led the incident review"},
		"tags": {"Ownership, communication"}, "back": {url1},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	rec = c.post("/commitments", url.Values{
		"person_id": {id(pid)}, "meeting_id": {id(mid)}, "kind": {"task"}, "direction": {"they_owe_me"},
		"content": {"Write the runbook"}, "due_on": {"2024-04-01"}, "back": {url1},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(url1)
	mustContain(t, page, "Led the incident review", "ownership", "communication", "Write the runbook", "1 Apr 2024")

	// A meeting entry cannot be locked by itself.
	entries, _ := env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	c.post("/entries/"+id(entries[0].ID)+"/finalize", url.Values{"back": {url1}})
	mustContain(t, c.get(url1), "Finalize the meeting instead")

	rec = c.post(url1+"/finalize", nil)
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(url1)
	mustContain(t, page, "Finalized", "Go interfaces", "Led the incident review", "<strong>migration</strong>")
	mustNotContain(t, page, `<textarea name="notes"`, "Finalize meeting", "Draft")

	// Locked: edits and deletion are refused with a clear message.
	c.post(url1, url.Values{"title": {"x"}, "starts_at": {"2024-03-05T14:00"}, "notes": {"rewritten"}, "back": {url1}})
	mustContain(t, c.get(url1), "finalized")
	mustNotContain(t, c.get(url1), "rewritten")
	c.post(url1+"/delete", url.Values{"back": {url1}})
	mustContain(t, c.get(url1), "finalized")

	// Voiding needs a reason and keeps the record.
	c.post(url1+"/void", url.Values{"reason": {"  "}, "back": {url1}})
	mustContain(t, c.get(url1), "reason")
	rec = c.post(url1+"/void", url.Values{"reason": {"Wrong person"}})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(url1)
	mustContain(t, page, "Voided", "Wrong person", "Go interfaces")
}

func TestMeeting_DraftsCanBeDeleted(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)

	rec := c.post("/meetings/"+id(mid)+"/delete", nil)
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != "/meetings" {
		t.Errorf("redirect after delete = %q, want /meetings", location(rec))
	}
	mustStatus(t, c.get("/meetings/"+id(mid)), http.StatusNotFound)
}

func TestMeeting_ListShowsMeetingsAndRequiresAPersonFor1on1(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	c.createMeeting(t, "one_on_one", pid)
	c.createMeeting(t, "team", 0)

	page := c.get("/meetings")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Ana", "Team meeting", "1:1")

	rec := c.post("/meetings", url.Values{"kind": {"one_on_one"}, "starts_at": {"2024-03-01T09:30"}, "back": {"/meetings"}})
	mustStatus(t, rec, http.StatusSeeOther)
	mustContain(t, c.get("/meetings"), "Person is required")
}

func TestMeeting_NotesAreSanitizedWhenShown(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	u := "/meetings/" + id(mid)

	c.post(u, url.Values{
		"title": {`<b>bold title</b>`}, "starts_at": {"2024-03-05T14:00"}, "person_id": {id(pid)},
		"notes":          {"<script>alert('x')</script> [click](javascript:alert(1)) <img src=x onerror=alert(2)>"},
		"answer_learned": {"<script>alert('answer')</script>"},
	})
	c.post(u+"/finalize", nil)

	page := c.get(u)
	mustStatus(t, page, http.StatusOK)
	mustNotContain(t, page, "<script>alert", "javascript:", "onerror", "<b>bold title</b>")
	mustContain(t, page, "&lt;b&gt;bold title&lt;/b&gt;")
}

func TestEntries_StandaloneLifecycle(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	back := "/people/" + id(pid)

	rec := c.post("/entries", url.Values{
		"person_id": {id(pid)}, "kind": {"strength"}, "body": {"Great at **mentoring**"}, "tags": {"communication"},
		"occurred_at": {"2024-02-10"}, "back": {back},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page := c.get(back)
	mustContain(t, page, "mentoring", "Strength", "Draft", "10 Feb 2024", "communication")

	// Edit the draft.
	eid := firstEntryID(t, c, pid)
	edit := c.get("/entries/" + id(eid) + "/edit")
	mustStatus(t, edit, http.StatusOK)
	mustContain(t, edit, "Great at **mentoring**")
	rec = c.post("/entries/"+id(eid), url.Values{
		"kind": {"observation"}, "body": {"Mentored two juniors"}, "tags": {"ownership"}, "occurred_at": {"2024-02-11"}, "back": {back},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(back)
	mustContain(t, page, "Mentored two juniors", "ownership")
	mustNotContain(t, page, "Great at")

	// Finalize: it now shows as locked and has no edit page.
	rec = c.post("/entries/"+id(eid)+"/finalize", url.Values{"back": {back}})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(back)
	mustContain(t, page, "Finalized")
	mustNotContain(t, page, "/entries/"+id(eid)+"/edit")
	c.get("/entries/" + id(eid) + "/edit")
	c.post("/entries/"+id(eid), url.Values{"kind": {"observation"}, "body": {"rewritten"}, "occurred_at": {"2024-02-11"}, "back": {back}})
	mustContain(t, c.get(back), "finalized")
	mustNotContain(t, c.get(back), "rewritten")

	// Void it, keeping the record.
	rec = c.post("/entries/"+id(eid)+"/void", url.Values{"reason": {"Mixed up with another person"}, "back": {back}})
	mustStatus(t, rec, http.StatusSeeOther)
	mustContain(t, c.get(back), "Voided", "Mixed up with another person", "Mentored two juniors")

	// A second entry that stays a draft can be deleted for good.
	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"scratch note"}, "back": {back}})
	page = c.get(back)
	mustContain(t, page, "scratch note")
	var scratch int64
	for _, e := range entriesOf(t, c, pid) {
		if e.Body == "scratch note" {
			scratch = e.ID
		}
	}
	c.post("/entries/"+id(scratch)+"/delete", url.Values{"back": {back}})
	mustNotContain(t, c.get(back), "scratch note")
}

func TestEntries_OutcomeRulesFollowTheKind(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	back := "/people/" + id(pid)

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"progress_check"}, "body": {"Applied Go in the billing job"}, "back": {back}})
	mustContain(t, c.get(back), "needs an outcome")

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"progress_check"}, "body": {"Applied Go in the billing job"}, "outcome": {"progress"}, "back": {back}})
	page := c.get(back)
	mustContain(t, page, "Applied Go in the billing job", "Progress")

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"x"}, "outcome": {"progress"}, "back": {back}})
	mustContain(t, c.get(back), "does not take an outcome")

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {" "}, "back": {back}})
	mustContain(t, c.get(back), "Body is required")
}

func TestCommitments_StatusFlow(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	back := "/people/" + id(pid)

	// A learning goal must name what will be shown.
	c.post("/commitments", url.Values{
		"person_id": {id(pid)}, "kind": {"learning"}, "direction": {"they_owe_me"}, "content": {"Learn Kubernetes"}, "back": {back},
	})
	mustContain(t, c.get(back), "deliverable")

	rec := c.post("/commitments", url.Values{
		"person_id": {id(pid)}, "kind": {"learning"}, "direction": {"they_owe_me"}, "content": {"Learn Kubernetes"},
		"deliverable": {"Demo to the guild"}, "due_on": {"2024-05-01"}, "back": {back},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page := c.get(back)
	mustContain(t, page, "Learn Kubernetes", "Demo to the guild", "1 May 2024", `badge warn">Open</span>`)

	cid := firstCommitmentID(t, c, pid)
	rec = c.post("/commitments/"+id(cid)+"/status", url.Values{"status": {"done"}, "note": {"Presented on 3 May"}, "back": {back}})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(back)
	mustContain(t, page, `badge ok">Done</span>`, "Presented on 3 May")
	mustNotContain(t, page, `badge warn">Open</span>`)

	c.post("/commitments/"+id(cid)+"/status", url.Values{"status": {"paused"}, "back": {back}})
	mustContain(t, c.get(back), "status")

	// Finalize, then void with a reason.
	c.post("/commitments/"+id(cid)+"/finalize", url.Values{"back": {back}})
	mustContain(t, c.get(back), "Finalized")
	c.post("/commitments/"+id(cid)+"/void", url.Values{"reason": {"Duplicate"}, "back": {back}})
	mustContain(t, c.get(back), "Voided", "Duplicate")
}

func TestBackRedirectsNeverLeaveTheServer(t *testing.T) {
	c := newEnv(t).loggedIn()
	for _, tc := range []struct{ back, want string }{
		{"/people", "/people"},
		{"/meetings?x=1", "/meetings?x=1"},
		{"//evil.example.com/steal", "/"},
		{"https://evil.example.com", "/"},
		{"/\\evil.example.com", "/"},
		{"", "/"},
	} {
		rec := c.post("/people", url.Values{"name": {""}, "relationship": {"report"}, "back": {tc.back}})
		mustStatus(t, rec, http.StatusSeeOther)
		if got := location(rec); got != tc.want {
			t.Errorf("back=%q redirected to %q, want %q", tc.back, got, tc.want)
		}
	}

	// The Referer is honored only when it is one of our own pages.
	rec := c.post("/people", url.Values{"name": {""}, "relationship": {"report"}}, "Referer", "http://"+testAddr+"/meetings")
	if location(rec) != "/meetings" {
		t.Errorf("own Referer redirected to %q, want /meetings", location(rec))
	}
	rec = c.post("/people", url.Values{"name": {""}, "relationship": {"report"}}, "Referer", "https://evil.example.com/x")
	if location(rec) != "/" {
		t.Errorf("foreign Referer redirected to %q, want /", location(rec))
	}
}

func TestInternalErrorsDoNotLeakDetails(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	env.st.Close() // simulate a broken database

	rec := c.get("/people/" + id(pid))
	mustStatus(t, rec, http.StatusInternalServerError)
	mustContain(t, rec, "Something went wrong", "Reference")
	mustNotContain(t, rec, "sql", "database is closed")
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("the response should carry a request id for finding the log line")
	}
}

// helpers that read back through the store

func entryFilterFor(personID int64) store.EntryFilter {
	return store.EntryFilter{PersonID: &personID, IncludeVoided: true}
}

func commitmentFilterFor(personID int64) store.CommitmentFilter {
	return store.CommitmentFilter{PersonID: &personID}
}

func entriesOf(t *testing.T, c *client, personID int64) []domain.Entry {
	t.Helper()
	list, err := c.env.st.ListEntries(ctx(), entryFilterFor(personID))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func firstEntryID(t *testing.T, c *client, personID int64) int64 {
	t.Helper()
	list := entriesOf(t, c, personID)
	if len(list) == 0 {
		t.Fatal("no entries")
	}
	return list[0].ID
}

func firstCommitmentID(t *testing.T, c *client, personID int64) int64 {
	t.Helper()
	list, err := c.env.st.ListCommitments(ctx(), commitmentFilterFor(personID))
	if err != nil || len(list) == 0 {
		t.Fatalf("commitments = %v, %v", list, err)
	}
	return list[0].ID
}
