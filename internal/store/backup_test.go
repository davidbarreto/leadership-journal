package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leadership-journal/internal/domain"
)

func TestBackup_WritesAConsistentPrivateSnapshot(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "journal.db"), testClock)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mustCreatePerson(t, s, "Ana", domain.RelationshipReport)

	path, err := s.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasSuffix(path, ".bak") || !strings.Contains(path, "20240301-100000") {
		t.Errorf("backup path = %q, want a timestamped .bak next to the database", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %o, want 600", info.Mode().Perm())
	}

	// The snapshot is a complete, usable database.
	restored, err := Open(path, testClock)
	if err != nil {
		t.Fatalf("the backup does not open: %v", err)
	}
	defer restored.Close()
	people, err := restored.ListPeople(ctx, "")
	if err != nil || len(people) != 1 || people[0].Name != "Ana" {
		t.Errorf("restored people = %+v, %v", people, err)
	}
}

func TestBackup_NeverOverwritesAnEarlierBackup(t *testing.T) {
	s := newTestStore(t)
	first, err := s.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	second, err := s.Backup(ctx) // same second: the clock is fixed
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two backups share the path %q", first)
	}
	// Each snapshot keeps the data as of when it was taken.
	for path, want := range map[string]int{first: 0, second: 1} {
		snap, err := Open(path, testClock)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		people, err := snap.ListPeople(ctx, "")
		snap.Close()
		if err != nil || len(people) != want {
			t.Errorf("%s holds %d people (%v), want %d", filepath.Base(path), len(people), err, want)
		}
	}
}
