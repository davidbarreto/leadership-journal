package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"leadership-journal/internal/domain"
)

func TestPerson_TabsShowOnlyTheirOwnData(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	base := "/people/" + id(pid)

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"Led the incident review"}})
	c.post("/commitments", url.Values{"person_id": {id(pid)}, "kind": {"task"}, "direction": {"they_owe_me"}, "content": {"Write the runbook"}})
	mid := c.createMeeting(t, "one_on_one", pid)
	c.post("/meetings/"+id(mid), url.Values{"title": {"Biweekly"}, "starts_at": {"2024-02-20T10:00"}, "person_id": {id(pid)}})
	levels, _ := env.st.ListLevels(ctx())
	c.post(base+"/level", url.Values{"level_id": {id(levels[0].ID)}, "effective_from": {"2023-01-01"}})

	notes := c.get(base + "?tab=notes")
	mustStatus(t, notes, http.StatusOK)
	mustContain(t, notes, "Led the incident review", `aria-current="page">Notes</a>`)
	mustNotContain(t, notes, "Write the runbook", "Biweekly", "Documentation contributions")

	commitments := c.get(base + "?tab=commitments")
	mustContain(t, commitments, "Write the runbook")
	mustNotContain(t, commitments, "Led the incident review", "Biweekly")

	meetings := c.get(base + "?tab=meetings")
	mustContain(t, meetings, "Biweekly", "/meetings/"+id(mid))
	mustNotContain(t, meetings, "Led the incident review", "Write the runbook")

	work := c.get(base + "?tab=work")
	mustContain(t, work, "Documentation contributions", "No tasks yet")
	mustNotContain(t, work, "Led the incident review", "Write the runbook", "Biweekly")

	level := c.get(base + "?tab=level")
	mustContain(t, level, levels[0].Title, "1 Jan 2023")
	mustNotContain(t, level, "Led the incident review")

	// The header, with the current level, is on every tab.
	for _, page := range []string{"", "?tab=notes", "?tab=work"} {
		mustContain(t, c.get(base+page), "Ana", "Level "+levels[0].Code, "Report for your manager")
	}
}

func TestPerson_OverviewSummarizes(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	base := "/people/" + id(pid)

	overview := c.get(base)
	mustStatus(t, overview, http.StatusOK)
	mustContain(t, overview, `aria-current="page">Overview</a>`, "Nothing open.", "No meetings yet.", "Nothing noted yet.")

	for i, body := range []string{"first note", "second note", "third note", "fourth note"} {
		c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {body}, "occurred_at": {fmt.Sprintf("2024-02-%02d", i+1)}})
	}
	for _, what := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		c.post("/commitments", url.Values{"person_id": {id(pid)}, "kind": {"task"}, "direction": {"they_owe_me"}, "content": {"Commitment " + what}})
	}
	cid := firstCommitmentID(t, c, pid)
	c.post("/commitments/"+id(cid)+"/status", url.Values{"status": {"done"}})
	mid := c.createMeeting(t, "one_on_one", pid)

	if _, err := env.st.CreateRelease(ctx(), domain.Release{Name: "R24.1", StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)}); err != nil {
		t.Fatal(err)
	}

	overview = c.get(base)
	// The three latest notes, newest first; the oldest is only on the notes tab.
	mustContain(t, overview, "fourth note", "third note", "second note", "/meetings/"+id(mid), "R24.1", missingBadge, "1 more…")
	mustNotContain(t, overview, "first note")
	mustContain(t, c.get("/people/"+id(pid)+"?tab=notes"), "first note")

	// Unknown tabs fall back to the overview.
	unknown := c.get(base + "?tab=nope")
	mustStatus(t, unknown, http.StatusOK)
	mustContain(t, unknown, `aria-current="page">Overview</a>`)
}

func TestPerson_OverviewHasNoReleaseForNonReports(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	boss := c.createPerson(t, "Boss", "manager")
	if _, err := env.st.CreateRelease(ctx(), domain.Release{Name: "R24.1", StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)}); err != nil {
		t.Fatal(err)
	}
	mustNotContain(t, c.get("/people/"+id(boss)), "R24.1", "Report for your manager")
}

func TestPerson_ActionsReturnToTheirTab(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	notes := "/people/" + id(pid) + "?tab=notes"

	rec := c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"x"}, "back": {notes}})
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != notes {
		t.Errorf("redirect = %q, want %q", location(rec), notes)
	}
	// The forms on a tab post back to that tab.
	mustContain(t, c.get(notes), `name="back" value="/people/`+id(pid)+`?tab=notes"`)
}

func TestPerson_AddFormsUnfoldWhenEmptyOrAsked(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	notes := "/people/" + id(pid) + "?tab=notes"
	const open = `<details class="noprint" open><summary>Add a note</summary>`
	const folded = `<details class="noprint"><summary>Add a note</summary>`

	mustContain(t, c.get(notes), open)
	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"x"}})
	mustContain(t, c.get(notes), folded)
	mustContain(t, c.get(notes+"&add=1"), open)
}

func TestPerson_StartA1on1InOneClick(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")

	page := c.get("/people/" + id(pid) + "?tab=notes")
	mustContain(t, page, `name="kind" value="one_on_one"`, "Start 1:1")

	// What the button posts: the person and the kind, no time (meaning now).
	rec := c.post("/meetings", url.Values{"person_id": {id(pid)}, "kind": {"one_on_one"}})
	mustStatus(t, rec, http.StatusSeeOther)
	mid := idFrom(t, location(rec), "/meetings/")
	m, err := env.st.GetMeeting(ctx(), mid)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "one_on_one" || m.PersonID == nil || *m.PersonID != pid || !m.StartsAt.Equal(env.now) || !m.IsDraft() {
		t.Errorf("meeting = %+v, want a draft 1:1 with Ana starting now", m)
	}
}

func TestPerson_QuickActionsFollowTheRelationship(t *testing.T) {
	c := newEnv(t).loggedIn()
	self := c.createPerson(t, "Me", "self")
	boss := c.createPerson(t, "Boss", "manager")
	ana := c.createPerson(t, "Ana", "report")

	mustNotContain(t, c.get("/people/"+id(self)), "Start 1:1", "Add task")
	bossPage := c.get("/people/" + id(boss))
	mustContain(t, bossPage, "Start 1:1", "Add note")
	mustNotContain(t, bossPage, "Add task")
	mustContain(t, c.get("/people/"+id(ana)), "Start 1:1", "Add task")

	// The people list offers the same button to everyone but the owner.
	list := c.get("/people").Body.String()
	for _, who := range []int64{boss, ana} {
		if !strings.Contains(list, `name="person_id" value="`+id(who)+`"`) {
			t.Errorf("people list has no start button for person %d", who)
		}
	}
	if strings.Contains(list, `name="person_id" value="`+id(self)+`"`) {
		t.Error("people list offers a meeting about the owner")
	}
}

func TestPerson_AddATaskFromTheWorkTab(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	boss := c.createPerson(t, "Boss", "manager")
	tab := "/people/" + id(ana) + "?tab=work"

	// Without a release there is nowhere to put a task yet.
	mustContain(t, c.get(tab), "Add a release on the")
	mustNotContain(t, c.get(tab), `action="/work"`)

	c.post("/releases", url.Values{"name": {"R23.4"}, "starts_on": {"2023-10-01"}, "ends_on": {"2023-12-31"}})
	c.post("/releases", url.Values{"name": {"R24.1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	page := c.get(tab)
	// The person is fixed and the running release is preselected.
	mustContain(t, page, `action="/work"`, `<input type="hidden" name="person_id" value="`+id(ana)+`">`,
		`<option value="2" selected>R24.1</option>`, `<details class="noprint" open><summary>Add a task</summary>`)
	mustNotContain(t, page, `<select name="person_id">`)

	rec := c.post("/work", url.Values{
		"release_id": {"2"}, "person_id": {id(ana)}, "title": {"Migrate billing job"}, "picked_by": {"self"},
		"stretch": {"above"}, "rationale": {"New area"}, "back": {tab},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	if location(rec) != tab {
		t.Errorf("redirect = %q, want back to %q", location(rec), tab)
	}
	mustContain(t, c.get(tab), "Migrate billing job", "Task saved as a draft")

	// Managers do not get tasks.
	mustNotContain(t, c.get("/people/"+id(boss)+"?tab=work"), `action="/work"`, "Add a release on the")
}

func TestPerson_WorkTabShowsTheTicket(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/releases", url.Values{"name": {"R24.1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	c.createWorkItem(t, 1, ana, "Migrate billing job", "BILL-482", "above")
	c.createWorkItem(t, 1, ana, "Pair on alerts", "", "at")

	page := c.get("/people/" + id(ana) + "?tab=work")
	mustContain(t, page, "<th>Ticket</th>", "<td>BILL-482</td>", "Pair on alerts")
}
