package web

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

// finalizedCheck records a finalized progress check through the store, with an
// occurrence date the test controls.
func (e *testEnv) finalizedCheck(t *testing.T, person int64, when time.Time, outcome domain.Outcome, body string) {
	t.Helper()
	id, err := e.st.CreateEntry(ctx(), domain.Entry{
		PersonID: person, Kind: "progress_check", Body: body, Outcome: outcome, OccurredAt: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.FinalizeEntry(ctx(), id); err != nil {
		t.Fatal(err)
	}
}

func TestDashboard_EmptyState(t *testing.T) {
	c := newEnv(t).loggedIn()
	page := c.get("/")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Add your first person")
}

func TestDashboard_ShowsWhatNeedsAttention(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	bia := c.createPerson(t, "Bia", "report")
	cris := c.createPerson(t, "Cris", "report")
	c.createPerson(t, "Boss", "manager")

	now := env.now // 2024-03-01
	env.finalizedCheck(t, ana, now.AddDate(0, 0, -5), domain.OutcomeProgress, "applied it")
	env.finalizedCheck(t, cris, now.AddDate(0, 0, -50), domain.OutcomeNoProgress, "nothing")
	env.finalizedCheck(t, cris, now.AddDate(0, 0, -36), domain.OutcomeNoProgress, "nothing again")

	// Open commitments, one of them past due.
	due := now.AddDate(0, 0, -10)
	for _, c2 := range []domain.Commitment{
		{PersonID: bia, Kind: domain.CommitmentTask, Direction: domain.TheyOweMe, Content: "Write the runbook", DueOn: &due},
		{PersonID: ana, Kind: domain.CommitmentTask, Direction: domain.IOweThem, Content: "Send the budget"},
	} {
		if _, err := env.st.CreateCommitment(ctx(), c2); err != nil {
			t.Fatal(err)
		}
	}

	// A stretch task for Ana in the running release.
	rid, _ := env.st.CreateRelease(ctx(), domain.Release{Name: "R24.1", StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)})
	wid, err := env.st.CreateWorkItem(ctx(), domain.WorkItem{
		PersonID: ana, ReleaseID: rid, Title: "Migrate billing", PickedBy: domain.PickedBySelf,
		Stretch: domain.StretchAbove, StretchRationale: "new area",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.st.FinalizeWorkItem(ctx(), wid)

	page := c.get("/")
	mustStatus(t, page, http.StatusOK)

	// Improvement journal: Ana on time, Bia and Cris overdue, Cris on a streak.
	mustContain(t, page, "Improvement journal", "NO PROGRESS ×2")
	mustContain(t, page, "Write the runbook", "Send the budget", `badge bad">Overdue</span>`)
	// Release compliance for the release running today.
	mustContain(t, page, "R24.1", compliantBadge, missingBadge)
	// The commitment drafts are waiting to be finalized.
	mustContain(t, page, "Commitments: 2")
}

func TestDashboard_NeverMentionsOtherPeopleAsReports(t *testing.T) {
	c := newEnv(t).loggedIn()
	c.createPerson(t, "Me Myself", "self")
	c.createPerson(t, "Big Boss", "manager")
	page := c.get("/")
	mustNotContain(t, page, "Me Myself", "Big Boss")
}

func TestSearch(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")

	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"Great ownership during the outage"}})
	c.post("/entries", url.Values{"person_id": {id(pid)}, "kind": {"observation"}, "body": {"<script>alert(1)</script> another outage note"}})
	mid := c.createMeeting(t, "one_on_one", pid)
	c.post("/meetings/"+id(mid), url.Values{
		"title": {"Sync"}, "starts_at": {"2024-03-05T14:00"}, "person_id": {id(pid)},
		"notes": {"Discussed the outage follow-ups"}, "answer_learned": {"Incident response"},
	})

	page := c.get("/search?q=outage")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "<mark>outage</mark>", "/people/"+id(pid), "/meetings/"+id(mid), "Draft")
	mustNotContain(t, page, "<script>alert")
	mustContain(t, page, "&lt;script&gt;alert(1)&lt;/script&gt;")

	mustContain(t, c.get("/search?q=respon"), "<mark>response</mark>") // prefix match through an answer

	none := c.get("/search?q=zebra")
	mustStatus(t, none, http.StatusOK)
	mustContain(t, none, "Nothing found")

	for _, q := range []string{"", `"`, `AND OR NOT`, `(((`, `a:b*`} {
		mustStatus(t, c.get("/search?q="+url.QueryEscape(q)), http.StatusOK)
	}
}

func TestSearch_HidesVoidedRecords(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	eid, _ := env.st.CreateEntry(ctx(), domain.Entry{PersonID: pid, Kind: "observation", Body: "Mixed up kubernetes note", OccurredAt: env.now})
	env.st.FinalizeEntry(ctx(), eid)
	mustContain(t, c.get("/search?q=kubernetes"), "Mixed up")
	env.st.VoidEntry(ctx(), eid, "wrong person")
	mustNotContain(t, c.get("/search?q=kubernetes"), "Mixed up")
}

func TestSettings_ExtendTheJournalWithoutCode(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	page := c.get("/settings")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Teams", "Levels", "Meeting kinds", "Entry kinds", "E4", "EM2", "1:1", "Progress check")

	// A team appears when adding a person.
	c.post("/settings/teams", url.Values{"name": {"Payments"}})
	mustContain(t, c.get("/people/new"), "Payments")
	c.post("/settings/teams", url.Values{"name": {"Payments"}, "back": {"/settings"}})
	mustContain(t, c.get("/settings"), "already exists")

	// A new meeting kind can require a person, which the journal then enforces.
	c.post("/settings/meeting-kinds", url.Values{"label": {"Skip-level"}, "requires_person": {"1"}})
	mustContain(t, c.get("/meetings"), "Skip-level")
	c.post("/meetings", url.Values{"kind": {"skip_level"}, "starts_at": {"2024-03-01T09:00"}, "back": {"/meetings"}})
	mustContain(t, c.get("/meetings"), "Person is required")

	// A new entry kind shows up in the note forms.
	pid := c.createPerson(t, "Ana", "report")
	c.post("/settings/entry-kinds", url.Values{"label": {"Kudos"}})
	mustContain(t, c.get("/people/"+id(pid)), `<option value="kudos">Kudos</option>`)

	// A new prompt shows up in the minutes of that meeting kind.
	c.post("/settings/prompts", url.Values{"meeting_kind": {"one_on_one"}, "text": {"How is your workload?"}})
	mid := c.createMeeting(t, "one_on_one", pid)
	mustContain(t, c.get("/meetings/"+id(mid)), "How is your workload?", `name="answer_how_is_your_workload"`)

	// A new level (e.g. the QA scale) is available for people.
	c.post("/settings/levels", url.Values{"track": {"qa"}, "code": {"QA1"}, "title": {"Junior QA"}, "rank": {"10"}})
	mustContain(t, c.get("/people/"+id(pid)), "QA1")
	c.post("/settings/levels", url.Values{"track": {"qa"}, "code": {"QA2"}, "title": {""}, "rank": {"20"}, "back": {"/settings"}})
	mustContain(t, c.get("/settings"), "Title is required")
	c.post("/settings/levels", url.Values{"track": {"qa"}, "code": {"QA3"}, "title": {"x"}, "rank": {"abc"}, "back": {"/settings"}})
	mustContain(t, c.get("/settings"), "Invalid")
}

func TestReport_ContainsOnlyFinalizedRecordsOfAReport(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	self := c.createPerson(t, "Me Myself", "self")

	mk := func(body, kind string, when time.Time, finalize bool) int64 {
		eid, err := env.st.CreateEntry(ctx(), domain.Entry{PersonID: ana, Kind: kind, Body: body, OccurredAt: when})
		if err != nil {
			t.Fatal(err)
		}
		if finalize {
			env.st.FinalizeEntry(ctx(), eid)
		}
		return eid
	}
	mk("Led the incident review", "observation", day(2024, 2, 10), true)
	mk("Mentored two juniors", "strength", day(2024, 1, 20), true)
	mk("PRIVATE DRAFT THOUGHTS", "observation", day(2024, 2, 11), false)
	wrong := mk("Noted on the wrong person", "observation", day(2024, 2, 12), true)
	env.st.VoidEntry(ctx(), wrong, "wrong person")
	env.finalizedCheck(t, ana, day(2024, 2, 14), domain.OutcomeProgress, "Applied Go in the billing job")

	cid, _ := env.st.CreateCommitment(ctx(), domain.Commitment{PersonID: ana, Kind: domain.CommitmentTask, Direction: domain.TheyOweMe, Content: "Write the runbook"})
	env.st.FinalizeCommitment(ctx(), cid)
	env.st.CreateCommitment(ctx(), domain.Commitment{PersonID: ana, Kind: domain.CommitmentTask, Direction: domain.TheyOweMe, Content: "DRAFT COMMITMENT"})

	page := c.get("/people/" + id(ana) + "/report")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Ana", "Led the incident review", "Mentored two juniors", "Applied Go in the billing job",
		"Write the runbook", "only finalized records")
	mustNotContain(t, page, "PRIVATE DRAFT THOUGHTS", "Noted on the wrong person", "DRAFT COMMITMENT")

	// Only people who report to you can be reported on.
	mustStatus(t, c.get("/people/"+id(self)+"/report"), http.StatusForbidden)
	mustStatus(t, c.get("/people/999/report"), http.StatusNotFound)
}

func TestReport_FiltersByPeriodSectionAndKind(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	mk := func(body, kind string, when time.Time) {
		eid, _ := env.st.CreateEntry(ctx(), domain.Entry{PersonID: ana, Kind: kind, Body: body, OccurredAt: when})
		env.st.FinalizeEntry(ctx(), eid)
	}
	mk("January observation", "observation", day(2024, 1, 15))
	mk("February observation", "observation", day(2024, 2, 15))
	mk("February strength", "strength", day(2024, 2, 16))
	base := "/people/" + id(ana) + "/report?submitted=1&sections=entries"

	period := c.get(base + "&from=2024-02-01&to=2024-02-29&kind=observation&kind=strength")
	mustContain(t, period, "February observation", "February strength")
	mustNotContain(t, period, "January observation")

	kinds := c.get(base + "&kind=strength")
	mustContain(t, kinds, "February strength")
	mustNotContain(t, kinds, "February observation", "January observation")

	sections := c.get("/people/" + id(ana) + "/report?submitted=1&sections=commitments")
	mustNotContain(t, sections, "February observation", "February strength")

	mustContain(t, c.get(base+"&from=oops"), "Invalid date")
}

func TestReport_MarkdownDownload(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana Souza", "report")
	eid, _ := env.st.CreateEntry(ctx(), domain.Entry{PersonID: ana, Kind: "strength", Body: "Great at **mentoring**", OccurredAt: day(2024, 2, 10)})
	env.st.FinalizeEntry(ctx(), eid)
	env.st.CreateEntry(ctx(), domain.Entry{PersonID: ana, Kind: "strength", Body: "DRAFT ONLY", OccurredAt: day(2024, 2, 11)})

	rec := c.get("/people/" + id(ana) + "/report?format=md")
	mustStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="report-ana-souza.md"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	mustContain(t, rec, "# Report: Ana Souza", "Great at **mentoring**", "Strength")
	mustNotContain(t, rec, "DRAFT ONLY")
}
