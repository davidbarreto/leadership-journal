package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"leadership-journal/internal/domain"
)

// CreateTeam adds a team; names are unique.
func (s *Store) CreateTeam(ctx context.Context, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, domain.ErrNameRequired
	}
	at, tz := formatStamp(s.now())
	res, err := s.db.ExecContext(ctx, `INSERT INTO team (name, created_at, created_tz) VALUES (?, ?, ?)`, name, at, tz)
	if err != nil {
		return 0, wrapErr(err)
	}
	return res.LastInsertId()
}

// ListTeams returns all teams sorted by name.
func (s *Store) ListTeams(ctx context.Context) ([]domain.Team, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Team, error) {
		var t domain.Team
		err := r.Scan(&t.ID, &t.Name)
		return t, err
	}, `SELECT id, name FROM team ORDER BY name COLLATE NOCASE`)
}

// ListLevels returns the active levels ordered by track and rank.
func (s *Store) ListLevels(ctx context.Context) ([]domain.Level, error) {
	return queryAll(ctx, s.db, scanLevel,
		`SELECT id, track, code, title, rank FROM level WHERE archived = 0 ORDER BY track, rank`)
}

func scanLevel(r *sql.Rows) (domain.Level, error) {
	var l domain.Level
	err := r.Scan(&l.ID, &l.Track, &l.Code, &l.Title, &l.Rank)
	return l, err
}

// CreateLevel adds a level to a track's scale.
func (s *Store) CreateLevel(ctx context.Context, l domain.Level) (int64, error) {
	if err := l.Validate(); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO level (track, code, title, rank) VALUES (?, ?, ?, ?)`, l.Track, l.Code, l.Title, l.Rank)
	if err != nil {
		return 0, wrapErr(err)
	}
	return res.LastInsertId()
}

// CreatePerson validates and stores a person.
func (s *Store) CreatePerson(ctx context.Context, p domain.Person) (int64, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	at, tz := formatStamp(s.now())
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO person (name, relationship, role_title, team_id, created_at, created_tz) VALUES (?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(p.Name), p.Relationship, p.RoleTitle, nullInt(p.TeamID), at, tz)
	if err != nil {
		return 0, wrapErr(err)
	}
	return res.LastInsertId()
}

// UpdatePerson changes a person's profile. Their history is untouched.
func (s *Store) UpdatePerson(ctx context.Context, p domain.Person) error {
	if err := p.Validate(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE person SET name = ?, relationship = ?, role_title = ?, team_id = ? WHERE id = ?`,
		strings.TrimSpace(p.Name), p.Relationship, p.RoleTitle, nullInt(p.TeamID), p.ID)
	if err != nil {
		return wrapErr(err)
	}
	return requireAffected(res)
}

// GetPerson returns one person, or ErrNotFound.
func (s *Store) GetPerson(ctx context.Context, id int64) (domain.Person, error) {
	return getPerson(ctx, s.db, id)
}

const personColumns = `id, name, relationship, role_title, team_id`

func scanPerson(r interface{ Scan(...any) error }) (domain.Person, error) {
	var p domain.Person
	var team sql.NullInt64
	if err := r.Scan(&p.ID, &p.Name, &p.Relationship, &p.RoleTitle, &team); err != nil {
		return domain.Person{}, err
	}
	p.TeamID = scanNullInt(team)
	return p, nil
}

func getPerson(ctx context.Context, q querier, id int64) (domain.Person, error) {
	p, err := scanPerson(q.QueryRowContext(ctx, `SELECT `+personColumns+` FROM person WHERE id = ?`, id))
	return p, wrapErr(err)
}

// ListPeople returns people sorted by name; an empty relationship means everyone.
func (s *Store) ListPeople(ctx context.Context, rel domain.Relationship) ([]domain.Person, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Person, error) { return scanPerson(r) },
		`SELECT `+personColumns+` FROM person WHERE (? = '' OR relationship = ?) ORDER BY name COLLATE NOCASE`,
		string(rel), string(rel))
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AssignLevel records that a person holds a level from a date, enforcing the
// history rules (each level once, ranks rising with the date within a track).
func (s *Store) AssignLevel(ctx context.Context, personID, levelID int64, from time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := getPerson(ctx, tx, personID); err != nil {
			return err
		}
		level, err := scanLevelRow(tx.QueryRowContext(ctx,
			`SELECT id, track, code, title, rank FROM level WHERE id = ?`, levelID))
		if err != nil {
			return wrapErr(err)
		}
		history, err := levelHistory(ctx, tx, personID)
		if err != nil {
			return err
		}
		if err := history.Add(domain.LevelAssignment{Level: level, EffectiveFrom: from}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO person_level (person_id, level_id, effective_from) VALUES (?, ?, ?)`,
			personID, levelID, formatDate(from))
		return wrapErr(err)
	})
}

func scanLevelRow(r *sql.Row) (domain.Level, error) {
	var l domain.Level
	err := r.Scan(&l.ID, &l.Track, &l.Code, &l.Title, &l.Rank)
	return l, err
}

// LevelHistory returns the levels a person has held.
func (s *Store) LevelHistory(ctx context.Context, personID int64) (domain.LevelHistory, error) {
	return levelHistory(ctx, s.db, personID)
}

func levelHistory(ctx context.Context, q querier, personID int64) (domain.LevelHistory, error) {
	return queryAll(ctx, q, func(r *sql.Rows) (domain.LevelAssignment, error) {
		var a domain.LevelAssignment
		var from string
		if err := r.Scan(&a.Level.ID, &a.Level.Track, &a.Level.Code, &a.Level.Title, &a.Level.Rank, &from); err != nil {
			return a, err
		}
		var err error
		a.EffectiveFrom, err = parseDate(from)
		return a, err
	}, `SELECT l.id, l.track, l.code, l.title, l.rank, pl.effective_from
	    FROM person_level pl JOIN level l ON l.id = pl.level_id
	    WHERE pl.person_id = ? ORDER BY pl.effective_from, l.rank`, personID)
}
