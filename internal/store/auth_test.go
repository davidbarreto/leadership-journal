package store

import (
	"errors"
	"testing"
	"time"
)

func TestUser(t *testing.T) {
	s := newTestStore(t)

	if has, err := s.HasUser(ctx); err != nil || has {
		t.Fatalf("fresh database: HasUser = %v, %v; want false", has, err)
	}
	if _, err := s.PasswordHash(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("PasswordHash before setup error = %v, want ErrNotFound", err)
	}

	if err := s.CreateUser(ctx, "hash-1"); err != nil {
		t.Fatal(err)
	}
	// A second setup must not be able to overwrite the owner's password.
	if err := s.CreateUser(ctx, "attacker"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("second CreateUser error = %v, want ErrDuplicate", err)
	}
	if got, _ := s.PasswordHash(ctx); got != "hash-1" {
		t.Errorf("PasswordHash = %q, want hash-1", got)
	}

	if err := s.UpdatePasswordHash(ctx, "hash-2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PasswordHash(ctx); got != "hash-2" {
		t.Errorf("PasswordHash after update = %q, want hash-2", got)
	}
}

func TestUpdatePasswordHash_WithoutUser(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdatePasswordHash(ctx, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestSessions(t *testing.T) {
	s := newTestStore(t)
	now := day(2024, 3, 1)

	if err := s.CreateSession(ctx, "hash-a", "csrf-a", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "hash-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CSRFToken != "csrf-a" || !got.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("session = %+v", got)
	}

	if _, err := s.GetSession(ctx, "hash-a", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSession(ctx, "unknown", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session error = %v, want ErrNotFound", err)
	}

	s.CreateSession(ctx, "hash-b", "csrf-b", now.Add(10*time.Hour))
	removed, err := s.DeleteExpiredSessions(ctx, now.Add(2*time.Hour))
	if err != nil || removed != 1 {
		t.Errorf("DeleteExpiredSessions = %d, %v; want 1 (hash-a)", removed, err)
	}

	if err := s.DeleteSession(ctx, "hash-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "hash-b", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session still valid: %v", err)
	}
}
