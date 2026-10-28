package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Errors callers are expected to branch on.
var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicate      = errors.New("already exists")
	ErrNotDraft       = errors.New("only drafts can be changed or deleted")
	ErrNotFinalized   = errors.New("only finalized records can be voided")
	ErrReasonRequired = errors.New("a reason is required to void a record")
	ErrCannotMove     = errors.New("only planned or in-progress tasks can be reassigned")

	// ErrFinalizeWithMeeting is returned when finalizing an entry or commitment
	// that belongs to a meeting: those lock together with the meeting.
	ErrFinalizeWithMeeting = errors.New("finalize the meeting instead: its entries and commitments lock with it")
)

// querier is what *sql.DB and *sql.Tx have in common, so helpers can run
// either standalone or inside a transaction. With a single connection, code
// inside a transaction must use the Tx: calling the DB would wait forever.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// wrapErr turns driver errors into the package's sentinel errors.
func wrapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		return errors.Join(ErrDuplicate, err)
	case strings.Contains(err.Error(), "is finalized"), strings.Contains(err.Error(), "is locked"):
		// Raised by the lock triggers; the friendlier checks in this package
		// normally catch these first.
		return errors.Join(ErrNotDraft, err)
	}
	return err
}

// queryAll runs a query and maps every row with scan. It fully drains and
// closes the rows before returning, which the single-connection setup needs.
func queryAll[T any](ctx context.Context, q querier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func scanNullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// nullText stores empty strings as NULL, for columns where "none" must not
// collide with other "none" values (e.g. unique ticket references).
func nullText(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
