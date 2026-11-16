package store

import (
	"context"
	"database/sql"
	"time"
)

// HasUser reports whether the owner's password has been set up.
func (s *Store) HasUser(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_user`).Scan(&n)
	return n > 0, err
}

// CreateUser stores the owner's password hash. It fails with ErrDuplicate if
// one exists already, so the setup page cannot be used to replace a password.
func (s *Store) CreateUser(ctx context.Context, passwordHash string) error {
	at, _ := formatStamp(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_user (id, password_hash, created_at) VALUES (1, ?, ?)`, passwordHash, at)
	return wrapErr(err)
}

// UpdatePasswordHash replaces the owner's password hash.
func (s *Store) UpdatePasswordHash(ctx context.Context, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE app_user SET password_hash = ? WHERE id = 1`, passwordHash)
	if err != nil {
		return wrapErr(err)
	}
	return requireAffected(res)
}

// PasswordHash returns the stored hash, or ErrNotFound before setup.
func (s *Store) PasswordHash(ctx context.Context) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM app_user WHERE id = 1`).Scan(&h)
	return h, wrapErr(err)
}

// ResetOwner removes the owner's password and every session, so the setup page
// lets the owner choose a new password. It reports whether an owner existed.
// Journal data is untouched: the password only gates access to the app.
func (s *Store) ResetOwner(ctx context.Context) (bool, error) {
	var had bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM app_user`)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		had = n > 0
		_, err = tx.ExecContext(ctx, `DELETE FROM session`)
		return err
	})
	return had, err
}

// Session is a logged-in browser session.
type Session struct {
	CSRFToken string
	ExpiresAt time.Time
}

// CreateSession stores a session under the hash of its token.
func (s *Store) CreateSession(ctx context.Context, idHash, csrfToken string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO session (id_hash, csrf_token, expires_at) VALUES (?, ?, ?)`,
		idHash, csrfToken, expiresAt.Unix())
	return wrapErr(err)
}

// GetSession returns a session that exists and has not expired at now.
func (s *Store) GetSession(ctx context.Context, idHash string, now time.Time) (Session, error) {
	var sess Session
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT csrf_token, expires_at FROM session WHERE id_hash = ? AND expires_at > ?`,
		idHash, now.Unix()).Scan(&sess.CSRFToken, &expires)
	if err != nil {
		return Session{}, wrapErr(err)
	}
	sess.ExpiresAt = time.Unix(expires, 0)
	return sess, nil
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE id_hash = ?`, idHash)
	return err
}

// DeleteExpiredSessions removes sessions that expired at or before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE expires_at <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteOtherSessions logs out every session except the given one, used after
// a password change so a forgotten browser cannot stay logged in.
func (s *Store) DeleteOtherSessions(ctx context.Context, keepIDHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE id_hash <> ?`, keepIDHash)
	return err
}
