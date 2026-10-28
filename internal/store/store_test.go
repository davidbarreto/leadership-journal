package store

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// testClock returns a fixed instant in Lisbon so tests also exercise the
// UTC-plus-time-zone storage convention.
func testClock() time.Time {
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		panic(err)
	}
	return time.Date(2024, 3, 1, 10, 0, 0, 0, loc)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"), testClock)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustExec(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func mustFailWith(t *testing.T, s *Store, wantMsg, query string, args ...any) {
	t.Helper()
	_, err := s.db.Exec(query, args...)
	if err == nil {
		t.Fatalf("exec %q succeeded, want error containing %q", query, wantMsg)
	}
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("exec %q error = %v, want it to contain %q", query, err, wantMsg)
	}
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

const stamp = "2024-03-01T10:00:00Z"

func seedPerson(t *testing.T, s *Store) {
	t.Helper()
	mustExec(t, s, `INSERT INTO person (id, name, relationship, created_at, created_tz) VALUES (1, 'Ana', 'report', ?, 'UTC')`, stamp)
}

func TestOpen_AppliesMigrationsAndSeeds(t *testing.T) {
	s := newTestStore(t)

	if got := count(t, s, `SELECT COUNT(*) FROM schema_migrations`); got < 1 {
		t.Errorf("schema_migrations rows = %d, want at least 1", got)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM level WHERE track = 'engineer'`); got != 7 {
		t.Errorf("engineer levels = %d, want 7 (E1-E7)", got)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM level WHERE track = 'manager'`); got != 3 {
		t.Errorf("manager levels = %d, want 3 (EM1-EM3)", got)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM meeting_kind WHERE requires_person = 1 AND code = 'one_on_one'`); got != 1 {
		t.Errorf("one_on_one meeting kind missing or not requiring a person")
	}
	if got := count(t, s, `SELECT COUNT(*) FROM entry_kind WHERE requires_outcome = 1 AND code = 'progress_check'`); got != 1 {
		t.Errorf("progress_check entry kind missing or not requiring an outcome")
	}
	if got := count(t, s, `SELECT COUNT(*) FROM meeting_prompt WHERE meeting_kind = 'one_on_one' AND prompt_key IN ('learned', 'applied')`); got != 2 {
		t.Errorf("1:1 prompts learned/applied missing")
	}
}

func TestOpen_IsIdempotentAndRestrictsFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")

	s, err := Open(path, testClock)
	if err != nil {
		t.Fatal(err)
	}
	first := count(t, s, `SELECT COUNT(*) FROM schema_migrations`)
	s.Close()

	s, err = Open(path, testClock)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s.Close()
	if got := count(t, s, `SELECT COUNT(*) FROM schema_migrations`); got != first {
		t.Errorf("migrations rows after reopen = %d, want %d", got, first)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("db file mode = %o, want 600 (the data is sensitive)", perm)
	}
}

func TestMigrate_BacksUpBeforeApplyingPendingMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.db")

	v1 := fstest.MapFS{"migrations/0001_a.sql": {Data: []byte(`CREATE TABLE a (x INTEGER); INSERT INTO a VALUES (1);`)}}
	s, err := openWith(path, testClock, v1)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if backups, _ := filepath.Glob(filepath.Join(dir, "*.bak")); len(backups) != 0 {
		t.Fatalf("first-ever migration must not create a backup, got %v", backups)
	}

	v2 := fstest.MapFS{
		"migrations/0001_a.sql": v1["migrations/0001_a.sql"],
		"migrations/0002_b.sql": {Data: []byte(`CREATE TABLE b (y INTEGER);`)},
	}
	s, err = openWith(path, testClock, v2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	backups, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want exactly one", backups)
	}
	info, err := os.Stat(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("backup mode = %o, want 600", perm)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM b`); got != 0 {
		t.Errorf("migration 0002 was not applied")
	}
}

func TestMigrate_FailedMigrationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	bad := fstest.MapFS{"migrations/0001_bad.sql": {Data: []byte(`CREATE TABLE ok (x INTEGER); INSERT INTO nope VALUES (1);`)}}

	if _, err := openWith(path, testClock, bad); err == nil {
		t.Fatal("expected the broken migration to fail")
	}

	good := fstest.MapFS{"migrations/0001_bad.sql": {Data: []byte(`CREATE TABLE ok (x INTEGER);`)}}
	s, err := openWith(path, testClock, good)
	if err != nil {
		t.Fatalf("fixed migration should apply cleanly after a rolled-back failure: %v", err)
	}
	s.Close()
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s := newTestStore(t)
	mustFailWith(t, s, "FOREIGN KEY",
		`INSERT INTO entry (person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		 VALUES (99, 'observation', 'x', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
}

func TestEntryLock(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO entry (id, person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		VALUES (1, 1, 'observation', 'draft text', ?, 'UTC', ?, 'UTC')`, stamp, stamp)

	mustExec(t, s, `UPDATE entry SET body = 'edited draft' WHERE id = 1`)

	mustExec(t, s, `UPDATE entry SET finalized_at = ?, finalized_tz = 'UTC' WHERE id = 1`, stamp)

	mustFailWith(t, s, "finalized", `UPDATE entry SET body = 'rewritten history' WHERE id = 1`)
	mustFailWith(t, s, "finalized", `UPDATE entry SET finalized_at = NULL, finalized_tz = NULL WHERE id = 1`)
	mustFailWith(t, s, "finalized", `DELETE FROM entry WHERE id = 1`)

	mustExec(t, s, `UPDATE entry SET voided_at = ?, voided_tz = 'UTC', void_reason = 'wrong person' WHERE id = 1`, stamp)
	mustFailWith(t, s, "finalized", `UPDATE entry SET void_reason = 'changed my mind' WHERE id = 1`)
}

func TestDraftEntryCanBeDeleted(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO entry (id, person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		VALUES (1, 1, 'observation', 'x', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
	mustExec(t, s, `DELETE FROM entry WHERE id = 1`)
}

func TestVoidingRequiresFinalizing(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustFailWith(t, s, "CHECK",
		`INSERT INTO entry (person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz, voided_at, voided_tz)
		 VALUES (1, 'observation', 'x', ?, 'UTC', ?, 'UTC', ?, 'UTC')`, stamp, stamp, stamp)
}

func TestMeetingLockCoversAnswersAndNewEntries(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO meeting (id, kind, person_id, starts_at, starts_tz, created_at, created_tz)
		VALUES (1, 'one_on_one', 1, ?, 'UTC', ?, 'UTC')`, stamp, stamp)
	mustExec(t, s, `INSERT INTO meeting_answer (id, meeting_id, prompt_key, prompt_text, answer) VALUES (1, 1, 'learned', 'What did you learn?', 'Go')`)
	mustExec(t, s, `UPDATE meeting_answer SET answer = 'Go and SQLite' WHERE id = 1`)

	mustExec(t, s, `UPDATE meeting SET finalized_at = ?, finalized_tz = 'UTC' WHERE id = 1`, stamp)

	mustFailWith(t, s, "finalized", `UPDATE meeting SET notes = 'rewritten' WHERE id = 1`)
	mustFailWith(t, s, "finalized", `UPDATE meeting_answer SET answer = 'rewritten' WHERE id = 1`)
	mustFailWith(t, s, "finalized", `DELETE FROM meeting_answer WHERE id = 1`)
	mustFailWith(t, s, "finalized", `INSERT INTO meeting_answer (meeting_id, prompt_key, prompt_text, answer) VALUES (1, 'applied', 'How?', 'x')`)
	mustFailWith(t, s, "finalized", `INSERT INTO entry (person_id, meeting_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		VALUES (1, 1, 'observation', 'late addition', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
}

func TestEntryTagsLockWithTheirEntry(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO entry (id, person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		VALUES (1, 1, 'observation', 'x', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
	// Tags 1 and 2 (ownership, communication) come from the seed data.
	mustExec(t, s, `INSERT INTO entry_tag (entry_id, tag_id) VALUES (1, 1)`)
	mustExec(t, s, `UPDATE entry SET finalized_at = ?, finalized_tz = 'UTC' WHERE id = 1`, stamp)

	mustFailWith(t, s, "finalized", `INSERT INTO entry_tag (entry_id, tag_id) VALUES (1, 2)`)
	mustFailWith(t, s, "finalized", `DELETE FROM entry_tag WHERE entry_id = 1`)
}

func TestCommitmentEventsAreAppendOnly(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO commitment (id, person_id, kind, direction, content, created_at, created_tz)
		VALUES (1, 1, 'task', 'they_owe_me', 'Write runbook', ?, 'UTC')`, stamp)
	mustExec(t, s, `INSERT INTO commitment_event (id, commitment_id, status, at, tz) VALUES (1, 1, 'open', ?, 'UTC')`, stamp)

	mustFailWith(t, s, "append-only", `UPDATE commitment_event SET status = 'done' WHERE id = 1`)

	mustExec(t, s, `UPDATE commitment SET finalized_at = ?, finalized_tz = 'UTC' WHERE id = 1`, stamp)
	mustExec(t, s, `INSERT INTO commitment_event (commitment_id, status, at, tz) VALUES (1, 'done', ?, 'UTC')`, stamp)
	mustFailWith(t, s, "append-only", `DELETE FROM commitment_event WHERE id = 1`)
	mustFailWith(t, s, "finalized", `UPDATE commitment SET content = 'rewritten' WHERE id = 1`)
}

func TestDraftCommitmentEventsCanBeDeletedWithTheDraft(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO commitment (id, person_id, kind, direction, content, created_at, created_tz)
		VALUES (1, 1, 'task', 'they_owe_me', 'x', ?, 'UTC')`, stamp)
	mustExec(t, s, `INSERT INTO commitment_event (commitment_id, status, at, tz) VALUES (1, 'open', ?, 'UTC')`, stamp)
	mustExec(t, s, `DELETE FROM commitment_event WHERE commitment_id = 1`)
	mustExec(t, s, `DELETE FROM commitment WHERE id = 1`)
}

func seedWorkItem(t *testing.T, s *Store) {
	t.Helper()
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO release (id, name, starts_on, ends_on) VALUES (1, 'R1', '2024-01-01', '2024-03-31')`)
	mustExec(t, s, `INSERT INTO work_item (id, person_id, release_id, title, external_ref, picked_by, stretch, stretch_rationale,
			status, assigned_at, assigned_tz, created_at, created_tz)
		VALUES (1, 1, 1, 'Migrate billing', 'PROJ-1', 'lead', 'above', 'new area', 'planned', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
}

func TestWorkItemLocksOnlyTheJudgment(t *testing.T) {
	s := newTestStore(t)
	seedWorkItem(t, s)
	mustExec(t, s, `UPDATE work_item SET stretch = 'at' WHERE id = 1`)
	mustExec(t, s, `UPDATE work_item SET finalized_at = ?, finalized_tz = 'UTC' WHERE id = 1`, stamp)

	mustExec(t, s, `UPDATE work_item SET status = 'in_progress', investigation_done = 1, outcome_notes = 'going well' WHERE id = 1`)
	mustExec(t, s, `UPDATE work_item SET status = 'done', completed_at = ?, completed_tz = 'UTC' WHERE id = 1`, stamp)

	mustFailWith(t, s, "locked", `UPDATE work_item SET stretch = 'below' WHERE id = 1`)
	mustFailWith(t, s, "locked", `UPDATE work_item SET stretch_rationale = 'something else' WHERE id = 1`)
	mustFailWith(t, s, "locked", `UPDATE work_item SET picked_by = 'self' WHERE id = 1`)
	mustFailWith(t, s, "locked", `UPDATE work_item SET person_id = 1, release_id = 2 WHERE id = 1`)
	mustFailWith(t, s, "locked", `DELETE FROM work_item WHERE id = 1`)
}

func TestWorkItemTicketIsUniqueUnlessMovedOrVoided(t *testing.T) {
	s := newTestStore(t)
	seedWorkItem(t, s)
	mustExec(t, s, `INSERT INTO person (id, name, relationship, created_at, created_tz) VALUES (2, 'Bia', 'report', ?, 'UTC')`, stamp)

	const insertSecond = `INSERT INTO work_item (person_id, release_id, title, external_ref, picked_by, stretch, stretch_rationale,
			status, assigned_at, assigned_tz, created_at, created_tz)
		VALUES (2, 1, 'Migrate billing', 'PROJ-1', 'lead', 'at', 'r', 'planned', ?, 'UTC', ?, 'UTC')`

	mustFailWith(t, s, "UNIQUE", insertSecond, stamp, stamp)

	mustExec(t, s, `UPDATE work_item SET status = 'moved' WHERE id = 1`)
	mustExec(t, s, insertSecond, stamp, stamp)

	mustExec(t, s, `INSERT INTO work_item (person_id, release_id, title, picked_by, stretch, stretch_rationale,
			status, assigned_at, assigned_tz, created_at, created_tz)
		VALUES (1, 1, 'no ticket a', 'self', 'at', 'r', 'planned', ?, 'UTC', ?, 'UTC'),
		       (1, 1, 'no ticket b', 'self', 'at', 'r', 'planned', ?, 'UTC', ?, 'UTC')`, stamp, stamp, stamp, stamp)
}

func TestSearchIndexFollowsEntries(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	search := `SELECT COUNT(*) FROM search_index WHERE kind = 'entry' AND search_index MATCH ?`

	mustExec(t, s, `INSERT INTO entry (id, person_id, kind, body, occurred_at, occurred_tz, created_at, created_tz)
		VALUES (1, 1, 'observation', 'Great ownership during the outage', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
	if got := count(t, s, search, "outage"); got != 1 {
		t.Fatalf("after insert, matches = %d, want 1", got)
	}

	mustExec(t, s, `UPDATE entry SET body = 'Quiet week' WHERE id = 1`)
	if got := count(t, s, search, "outage"); got != 0 {
		t.Errorf("after update, old text still matches (%d)", got)
	}
	if got := count(t, s, search, "quiet"); got != 1 {
		t.Errorf("after update, new text matches = %d, want 1", got)
	}

	mustExec(t, s, `DELETE FROM entry WHERE id = 1`)
	if got := count(t, s, search, "quiet"); got != 0 {
		t.Errorf("after delete, entry is still indexed (%d)", got)
	}
}

func TestSearchIndexFollowsMeetingsAndAnswers(t *testing.T) {
	s := newTestStore(t)
	seedPerson(t, s)
	mustExec(t, s, `INSERT INTO meeting (id, kind, person_id, title, notes, starts_at, starts_tz, created_at, created_tz)
		VALUES (1, 'one_on_one', 1, 'Quarterly sync', 'Discussed kubernetes migration', ?, 'UTC', ?, 'UTC')`, stamp, stamp)
	mustExec(t, s, `INSERT INTO meeting_answer (id, meeting_id, prompt_key, prompt_text, answer) VALUES (1, 1, 'learned', 'Learned?', 'Prometheus alerting')`)

	if got := count(t, s, `SELECT COUNT(*) FROM search_index WHERE kind = 'meeting' AND search_index MATCH 'kubernetes'`); got != 1 {
		t.Errorf("meeting notes matches = %d, want 1", got)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM search_index WHERE kind = 'meeting' AND search_index MATCH 'quarterly'`); got != 1 {
		t.Errorf("meeting title matches = %d, want 1", got)
	}
	if got := count(t, s, `SELECT COUNT(*) FROM search_index WHERE kind = 'answer' AND search_index MATCH 'prometheus'`); got != 1 {
		t.Errorf("answer matches = %d, want 1", got)
	}

	mustExec(t, s, `UPDATE meeting_answer SET answer = 'Grafana dashboards' WHERE id = 1`)
	if got := count(t, s, `SELECT COUNT(*) FROM search_index WHERE kind = 'answer' AND search_index MATCH 'prometheus'`); got != 0 {
		t.Errorf("stale answer still indexed")
	}
}

func TestSinglePersonWithRelationshipSelf(t *testing.T) {
	s := newTestStore(t)
	mustExec(t, s, `INSERT INTO person (name, relationship, created_at, created_tz) VALUES ('Me', 'self', ?, 'UTC')`, stamp)
	mustFailWith(t, s, "UNIQUE", `INSERT INTO person (name, relationship, created_at, created_tz) VALUES ('Me too', 'self', ?, 'UTC')`, stamp)
}

func TestOnlyReportsBelongToATeam(t *testing.T) {
	s := newTestStore(t)
	mustExec(t, s, `INSERT INTO team (id, name, created_at, created_tz) VALUES (1, 'Payments', ?, 'UTC')`, stamp)
	mustFailWith(t, s, "CHECK", `INSERT INTO person (name, relationship, team_id, created_at, created_tz) VALUES ('Boss', 'manager', 1, ?, 'UTC')`, stamp)
}
