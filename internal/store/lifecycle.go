package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"leadership-journal/internal/domain"
)

// table names the journal tables that share the draft/finalized/voided
// lifecycle. Only these constants are ever interpolated into SQL.
type table string

const (
	tableMeeting    table = "meeting"
	tableEntry      table = "entry"
	tableCommitment table = "commitment"
	tableWorkItem   table = "work_item"
)

const lifecycleColumns = `created_at, created_tz, finalized_at, finalized_tz, voided_at, voided_tz, void_reason`

type scanner interface{ Scan(dest ...any) error }

// lifecycleScan reads the lifecycleColumns of any journal row.
type lifecycleScan struct {
	createdAt, createdTZ, voidReason             string
	finalizedAt, finalizedTZ, voidedAt, voidedTZ sql.NullString
}

func (l *lifecycleScan) dests() []any {
	return []any{&l.createdAt, &l.createdTZ, &l.finalizedAt, &l.finalizedTZ, &l.voidedAt, &l.voidedTZ, &l.voidReason}
}

func (l *lifecycleScan) build() (domain.Lifecycle, error) {
	var out domain.Lifecycle
	var err error
	if out.CreatedAt, err = parseStamp(l.createdAt, l.createdTZ); err != nil {
		return out, err
	}
	if out.FinalizedAt, err = scanNullStamp(l.finalizedAt, l.finalizedTZ); err != nil {
		return out, err
	}
	if out.VoidedAt, err = scanNullStamp(l.voidedAt, l.voidedTZ); err != nil {
		return out, err
	}
	out.VoidReason = l.voidReason
	return out, nil
}

// exists reports whether a row is there, to tell "missing" from "wrong state"
// when a state-guarded UPDATE or DELETE touches nothing.
func exists(ctx context.Context, q querier, t table, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ?`, t), id).Scan(&n)
	return n > 0, err
}

// requireDraft returns ErrNotFound or ErrNotDraft unless the row is a draft.
func requireDraft(ctx context.Context, q querier, t table, id int64) error {
	var isDraft bool
	err := q.QueryRowContext(ctx, fmt.Sprintf(`SELECT finalized_at IS NULL FROM %s WHERE id = ?`, t), id).Scan(&isDraft)
	if err != nil {
		return wrapErr(err)
	}
	if !isDraft {
		return ErrNotDraft
	}
	return nil
}

func finalizeRow(ctx context.Context, q querier, t table, id int64, at, tz string) error {
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET finalized_at = ?, finalized_tz = ? WHERE id = ? AND finalized_at IS NULL`, t), at, tz, id)
	if err != nil {
		return wrapErr(err)
	}
	return explainNoChange(ctx, q, res, t, id, ErrNotDraft)
}

func voidRow(ctx context.Context, q querier, t table, id int64, reason, at, tz string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET voided_at = ?, voided_tz = ?, void_reason = ?
		             WHERE id = ? AND finalized_at IS NOT NULL AND voided_at IS NULL`, t), at, tz, strings.TrimSpace(reason), id)
	if err != nil {
		return wrapErr(err)
	}
	return explainNoChange(ctx, q, res, t, id, ErrNotFinalized)
}

func deleteDraftRow(ctx context.Context, q querier, t table, id int64) error {
	res, err := q.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ? AND finalized_at IS NULL`, t), id)
	if err != nil {
		return wrapErr(err)
	}
	return explainNoChange(ctx, q, res, t, id, ErrNotDraft)
}

// explainNoChange maps "no row was touched" to ErrNotFound if the row does not
// exist, or to wrongState if it exists but was in the wrong lifecycle state.
func explainNoChange(ctx context.Context, q querier, res sql.Result, t table, id int64, wrongState error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	found, err := exists(ctx, q, t, id)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return wrongState
}

// finalizeStandalone finalizes an entry or commitment on its own. Rows that
// belong to a meeting are refused, because they lock together with it.
func (s *Store) finalizeStandalone(ctx context.Context, t table, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var meeting sql.NullInt64
		var isDraft bool
		err := tx.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT meeting_id, finalized_at IS NULL FROM %s WHERE id = ?`, t), id).Scan(&meeting, &isDraft)
		if err != nil {
			return wrapErr(err)
		}
		if !isDraft {
			return ErrNotDraft
		}
		if meeting.Valid {
			return ErrFinalizeWithMeeting
		}
		at, tz := formatStamp(s.now())
		return finalizeRow(ctx, tx, t, id, at, tz)
	})
}

// voidStandalone voids a finalized row.
func (s *Store) voidStandalone(ctx context.Context, t table, id int64, reason string) error {
	at, tz := formatStamp(s.now())
	return voidRow(ctx, s.db, t, id, reason, at, tz)
}

func where(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

func limitClause(limit int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d", limit)
}
