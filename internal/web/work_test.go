package web

import (
	"net/http"
	"net/url"
	"testing"
)

func (c *client) createWorkItem(t *testing.T, release, person int64, title, ticket, stretch string) int64 {
	t.Helper()
	rec := c.post("/work", url.Values{
		"release_id": {id(release)}, "person_id": {id(person)}, "title": {title}, "external_ref": {ticket},
		"picked_by": {"lead"}, "stretch": {stretch}, "rationale": {"First time in billing"}, "assigned_at": {"2024-01-15"},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	return idFrom(t, location(rec), "/work/")
}

const (
	compliantBadge = `badge ok">Has a stretch task</span>`
	missingBadge   = `badge bad">No stretch task</span>`
)

func TestWork_EmptyState(t *testing.T) {
	c := newEnv(t).loggedIn()
	page := c.get("/work")
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "No releases yet")
}

func TestWork_ReleaseValidation(t *testing.T) {
	c := newEnv(t).loggedIn()
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-04-01"}, "ends_on": {"2024-03-01"}, "back": {"/work"}})
	mustContain(t, c.get("/work"), "End date is before start date")
	c.post("/releases", url.Values{"name": {""}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-01"}, "back": {"/work"}})
	mustContain(t, c.get("/work"), "Name is required")
}

func TestWork_ItemLifecycleAndCompliance(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	bia := c.createPerson(t, "Bia", "report")

	rec := c.post("/releases", url.Values{"name": {"R24.1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	mustStatus(t, rec, http.StatusSeeOther)
	releases, _ := env.st.ListReleases(ctx())
	rid := releases[0].ID

	item := c.createWorkItem(t, rid, ana, "Migrate billing job", "PROJ-1", "above")
	detail := "/work/" + id(item)

	page := c.get(detail)
	mustStatus(t, page, http.StatusOK)
	mustContain(t, page, "Migrate billing job", "PROJ-1", "Draft", "First time in billing", "Ana")

	// Drafts are not part of the record yet, so nobody complies.
	board := c.get("/work?release=" + id(rid))
	mustContain(t, board, "R24.1", "Ana", "Bia", missingBadge)
	mustNotContain(t, board, compliantBadge)

	rec = c.post(detail+"/finalize", nil)
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(detail)
	mustContain(t, page, "Finalized", "stretch judgment is locked")
	board = c.get("/work?release=" + id(rid))
	mustContain(t, board, compliantBadge, missingBadge) // Ana yes, Bia no
	_ = bia

	// The judgment can no longer be edited.
	c.post(detail, url.Values{
		"person_id": {id(ana)}, "release_id": {id(rid)}, "title": {"Migrate billing job"}, "picked_by": {"self"},
		"stretch": {"below"}, "rationale": {"changed my mind"}, "assigned_at": {"2024-01-15"}, "back": {detail},
	})
	mustContain(t, c.get(detail), "finalized")
	mustNotContain(t, c.get(detail), "changed my mind")

	// ...but progress can: the task moves on after it is locked.
	rec = c.post(detail+"/tracking", url.Values{
		"status": {"in_progress"}, "investigation_done": {"1"}, "investigation_url": {"https://wiki.example/investigation"},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	rec = c.post(detail+"/tracking", url.Values{
		"status": {"done"}, "investigation_done": {"1"}, "outcome_notes": {"Worked independently"}, "completed_at": {"2024-02-20"},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	page = c.get(detail)
	mustContain(t, page, "Worked independently", "20 Feb 2024", `badge ok">Done</span>`)

	// Ana's profile now shows the metrics derived from it.
	profile := c.get("/people/" + id(ana))
	mustContain(t, profile, "Migrate billing job", "100%", compliantBadge)
}

func TestWork_TicketsCannotBeEnteredTwice(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	bia := c.createPerson(t, "Bia", "report")
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	rid := int64(1)

	c.createWorkItem(t, rid, ana, "First", "PROJ-1", "above")
	c.post("/work", url.Values{
		"release_id": {id(rid)}, "person_id": {id(bia)}, "title": {"Duplicate"}, "external_ref": {"PROJ-1"},
		"picked_by": {"lead"}, "stretch": {"at"}, "rationale": {"x"}, "assigned_at": {"2024-01-15"}, "back": {"/work"},
	})
	mustContain(t, c.get("/work"), "already exists")
}

func TestWork_ValidationMessages(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})

	c.post("/work", url.Values{
		"release_id": {"1"}, "person_id": {id(ana)}, "title": {"Task"}, "picked_by": {"lead"}, "stretch": {"at"},
		"rationale": {""}, "assigned_at": {"2024-01-15"}, "back": {"/work"},
	})
	mustContain(t, c.get("/work"), "Stretch rationale is required")

	c.post("/work", url.Values{
		"release_id": {"1"}, "person_id": {id(ana)}, "title": {"Task"}, "picked_by": {"lead"}, "stretch": {"huge"},
		"rationale": {"x"}, "assigned_at": {"2024-01-15"}, "back": {"/work"},
	})
	mustContain(t, c.get("/work"), "Stretch must be below, at or above")
}

func TestWork_MovingATaskKeepsTheOriginal(t *testing.T) {
	env := newEnv(t)
	c := env.loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	bia := c.createPerson(t, "Bia", "report")
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	orig := c.createWorkItem(t, 1, ana, "Migrate billing job", "PROJ-1", "above")
	c.post("/work/"+id(orig)+"/finalize", nil)
	detail := "/work/" + id(orig)

	// Moving to the same person, or without a new judgment, is refused.
	c.post(detail+"/move", url.Values{
		"person_id": {id(ana)}, "picked_by": {"lead"}, "stretch": {"at"}, "rationale": {"x"}, "back": {detail},
	})
	mustContain(t, c.get(detail), "different person")
	c.post(detail+"/move", url.Values{"person_id": {id(bia)}, "picked_by": {"lead"}, "stretch": {"at"}, "rationale": {""}, "back": {detail}})
	mustContain(t, c.get(detail), "Stretch rationale is required")

	rec := c.post(detail+"/move", url.Values{
		"person_id": {id(bia)}, "picked_by": {"lead"}, "stretch": {"at"}, "rationale": {"Bia built the billing job"}, "assigned_at": {"2024-02-01"},
	})
	mustStatus(t, rec, http.StatusSeeOther)
	newID := idFrom(t, location(rec), "/work/")

	moved := c.get(detail)
	mustContain(t, moved, `badge bad">Moved</span>`, "Moved to", "/work/"+id(newID))
	fresh := c.get("/work/" + id(newID))
	mustContain(t, fresh, "Bia", "Draft", "Moved from", "/work/"+id(orig), "Bia built the billing job")

	// The original person no longer gets credit for it.
	mustContain(t, c.get("/work?release=1"), missingBadge)
	mustNotContain(t, c.get("/work?release=1"), compliantBadge)
}

func TestWork_FinishedTasksCannotBeMoved(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	bia := c.createPerson(t, "Bia", "report")
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})
	orig := c.createWorkItem(t, 1, ana, "Done already", "PROJ-9", "above")
	detail := "/work/" + id(orig)
	c.post(detail+"/tracking", url.Values{"status": {"done"}})
	mustNotContain(t, c.get(detail), `action="`+detail+`/move"`)

	c.post(detail+"/move", url.Values{"person_id": {id(bia)}, "picked_by": {"lead"}, "stretch": {"at"}, "rationale": {"x"}, "back": {detail}})
	mustContain(t, c.get(detail), "reassigned")
}

func TestWork_VoidAndDelete(t *testing.T) {
	c := newEnv(t).loggedIn()
	ana := c.createPerson(t, "Ana", "report")
	c.post("/releases", url.Values{"name": {"R1"}, "starts_on": {"2024-01-01"}, "ends_on": {"2024-03-31"}})

	draft := c.createWorkItem(t, 1, ana, "Scratch", "PROJ-1", "at")
	rec := c.post("/work/"+id(draft)+"/delete", nil)
	mustStatus(t, rec, http.StatusSeeOther)
	mustStatus(t, c.get("/work/"+id(draft)), http.StatusNotFound)

	final := c.createWorkItem(t, 1, ana, "Entered twice", "PROJ-1", "at")
	d := "/work/" + id(final)
	c.post(d+"/finalize", nil)
	c.post(d+"/delete", url.Values{"back": {d}})
	mustContain(t, c.get(d), "finalized")
	c.post(d+"/void", url.Values{"reason": {""}, "back": {d}})
	mustContain(t, c.get(d), "reason")
	c.post(d+"/void", url.Values{"reason": {"Entered twice by mistake"}})
	mustContain(t, c.get(d), "Voided", "Entered twice by mistake")
}

func TestWork_UnknownItemIs404(t *testing.T) {
	c := newEnv(t).loggedIn()
	mustStatus(t, c.get("/work/999"), http.StatusNotFound)
	mustStatus(t, c.get("/work/xyz"), http.StatusNotFound)
	mustStatus(t, c.get("/work?release=999"), http.StatusNotFound)
}
