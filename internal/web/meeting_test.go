package web

import (
	"net/http"
	"net/url"
	"testing"

	"leadership-journal/internal/store"
)

// minutes is a meeting page post with the minutes filled in; tests add the
// new note and commitment fields they need.
func minutes(pid int64, extra url.Values) url.Values {
	form := url.Values{
		"title": {"Biweekly"}, "starts_at": {"2024-03-05T14:00"}, "person_id": {id(pid)},
		"answer_learned": {"Go interfaces"}, "notes": {"Talked about the migration"},
		"note_kind": {"observation"}, "c_kind": {"task"}, "c_direction": {"they_owe_me"},
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

func TestMeetingSave_SavesNoteAndCommitmentWithTheMinutes(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	u := "/meetings/" + id(mid)

	rec := c.post(u, minutes(pid, url.Values{
		"note_body": {"Led the incident review"}, "note_tags": {"ownership"},
		"c_content": {"Write the runbook"}, "c_due_on": {"2024-04-01"},
	}))
	mustStatus(t, rec, http.StatusSeeOther)
	page := c.get(u)
	mustContain(t, page, "Draft saved.", "Go interfaces", "Led the incident review", "ownership", "Write the runbook", "1 Apr 2024")

	entries, _ := env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	commitments, _ := env.st.ListCommitments(ctx(), store.CommitmentFilter{MeetingID: &mid})
	if len(entries) != 1 || entries[0].PersonID != pid || len(commitments) != 1 || commitments[0].PersonID != pid {
		t.Fatalf("entries = %+v, commitments = %+v, want one of each about Ana", entries, commitments)
	}

	// With the new note and commitment left empty, saving adds neither.
	mustStatus(t, c.post(u, minutes(pid, nil)), http.StatusSeeOther)
	entries, _ = env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	commitments, _ = env.st.ListCommitments(ctx(), store.CommitmentFilter{MeetingID: &mid})
	if len(entries) != 1 || len(commitments) != 1 {
		t.Errorf("entries = %d, commitments = %d after an empty save, want 1 and 1", len(entries), len(commitments))
	}
	// The fields are blank again, ready for the next note.
	mustNotContain(t, c.get(u), `value="Write the runbook"`)
}

func TestMeetingSave_SaveAndFinalizeKeepsWhatWasTyped(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	u := "/meetings/" + id(mid)

	rec := c.post(u, minutes(pid, url.Values{"note_body": {"Last words"}, "then": {"finalize"}}))
	mustStatus(t, rec, http.StatusSeeOther)
	page := c.get(u)
	mustContain(t, page, "Meeting finalized", "Go interfaces", "Last words", `badge ok">Finalized</span>`)
	mustNotContain(t, page, "Save draft")

	entries, _ := env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	if len(entries) != 1 || !entries[0].IsFinalized() {
		t.Errorf("entries = %+v, want the new note locked with the meeting", entries)
	}
}

func TestMeetingSave_RefusedSaveShowsTheTextAgain(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	u := "/meetings/" + id(mid)

	// A learning goal without a deliverable is refused.
	rec := c.post(u, minutes(pid, url.Values{
		"answer_learned": {"Typed during the meeting"}, "note_body": {"An important note"},
		"c_kind": {"learning"}, "c_content": {"Learn Kubernetes"}, "c_due_on": {"2024-05-01"}, "then": {"finalize"},
	}))
	mustStatus(t, rec, http.StatusUnprocessableEntity)
	mustContain(t, rec, "Deliverable", "Nothing was saved yet",
		"Typed during the meeting", "An important note", `value="Learn Kubernetes"`, `value="2024-05-01"`,
		`<option value="learning" selected>`)

	m, _ := env.st.GetMeeting(ctx(), mid)
	entries, _ := env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	if len(m.Answers) != 0 || len(entries) != 0 || !m.IsDraft() {
		t.Errorf("answers = %+v, entries = %d, draft = %v: nothing should be stored", m.Answers, len(entries), m.IsDraft())
	}

	// A malformed date is refused the same way.
	rec = c.post(u, minutes(pid, url.Values{"note_body": {"kept"}, "c_content": {"x"}, "c_due_on": {"soon"}}))
	mustStatus(t, rec, http.StatusUnprocessableEntity)
	mustContain(t, rec, "Invalid date", "kept", "Go interfaces")
}

func TestMeetingSave_TeamMeetingNotesNameTheirPerson(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "team", 0)
	u := "/meetings/" + id(mid)

	mustContain(t, c.get(u), `name="note_person_id"`, `name="c_person_id"`)
	form := url.Values{"starts_at": {"2024-03-05T14:00"}, "note_kind": {"observation"}, "note_body": {"Ran the demo"}}
	rec := c.post(u, form)
	mustStatus(t, rec, http.StatusUnprocessableEntity)
	mustContain(t, rec, "Person is required", "Ran the demo")

	form.Set("note_person_id", id(ana))
	mustStatus(t, c.post(u, form), http.StatusSeeOther)
	entries, _ := env.st.ListEntries(ctx(), store.EntryFilter{MeetingID: &mid})
	if len(entries) != 1 || entries[0].PersonID != ana {
		t.Errorf("entries = %+v, want one note about Ana", entries)
	}
}

func TestMeetingSave_FinalizedMeetingIsNotRerendered(t *testing.T) {
	c := newEnv(t).loggedIn()
	pid := c.createPerson(t, "Ana", "report")
	mid := c.createMeeting(t, "one_on_one", pid)
	u := "/meetings/" + id(mid)
	c.post(u+"/finalize", nil)

	rec := c.post(u, minutes(pid, url.Values{"back": {u}}))
	mustStatus(t, rec, http.StatusSeeOther)
	mustContain(t, c.get(u), "finalized, so it can no longer be changed")
}
