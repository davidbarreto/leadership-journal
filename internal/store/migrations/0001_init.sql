-- Schema for the leadership journal. See docs/data-model.md for the reasoning.
--
-- Conventions:
--   * every instant is x_at (UTC, RFC 3339) plus x_tz (IANA zone name);
--   * dates without time are YYYY-MM-DD text;
--   * journal rows are drafts until finalized_at is set, then locked by the
--     triggers below (they can only be voided, never changed or deleted).

-- ---------------------------------------------------------------------------
-- Reference data (mutable)
-- ---------------------------------------------------------------------------

CREATE TABLE team (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    created_tz TEXT NOT NULL
);

CREATE TABLE level (
    id           INTEGER PRIMARY KEY,
    track        TEXT NOT NULL,
    code         TEXT NOT NULL UNIQUE,
    title        TEXT NOT NULL,
    rank         INTEGER NOT NULL,
    expectations TEXT NOT NULL DEFAULT '',
    archived     INTEGER NOT NULL DEFAULT 0,
    UNIQUE (track, rank)
);

CREATE TABLE meeting_kind (
    code            TEXT PRIMARY KEY,
    label           TEXT NOT NULL,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    requires_person INTEGER NOT NULL DEFAULT 0,
    archived        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE entry_kind (
    code             TEXT PRIMARY KEY,
    label            TEXT NOT NULL,
    sort_order       INTEGER NOT NULL DEFAULT 0,
    requires_outcome INTEGER NOT NULL DEFAULT 0,
    archived         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE tag (
    id       INTEGER PRIMARY KEY,
    name     TEXT NOT NULL UNIQUE,
    archived INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE meeting_prompt (
    id           INTEGER PRIMARY KEY,
    meeting_kind TEXT NOT NULL REFERENCES meeting_kind (code),
    prompt_key   TEXT NOT NULL,
    prompt_text  TEXT NOT NULL,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    archived     INTEGER NOT NULL DEFAULT 0,
    UNIQUE (meeting_kind, prompt_key)
);

CREATE TABLE release (
    id        INTEGER PRIMARY KEY,
    name      TEXT NOT NULL,
    starts_on TEXT NOT NULL,
    ends_on   TEXT NOT NULL
);

-- ---------------------------------------------------------------------------
-- People
-- ---------------------------------------------------------------------------

CREATE TABLE person (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    relationship TEXT NOT NULL CHECK (relationship IN ('report', 'manager', 'self')),
    role_title   TEXT NOT NULL DEFAULT '',
    team_id      INTEGER REFERENCES team (id),
    created_at   TEXT NOT NULL,
    created_tz   TEXT NOT NULL,
    CHECK (relationship = 'report' OR team_id IS NULL)
);

CREATE UNIQUE INDEX person_single_self ON person (relationship) WHERE relationship = 'self';

-- A person holds each level once; the application keeps ranks increasing with
-- the date inside a track.
CREATE TABLE person_level (
    person_id      INTEGER NOT NULL REFERENCES person (id),
    level_id       INTEGER NOT NULL REFERENCES level (id),
    effective_from TEXT NOT NULL,
    PRIMARY KEY (person_id, level_id)
);

-- ---------------------------------------------------------------------------
-- Journal (append-only ledger)
-- ---------------------------------------------------------------------------

CREATE TABLE meeting (
    id           INTEGER PRIMARY KEY,
    kind         TEXT NOT NULL REFERENCES meeting_kind (code),
    title        TEXT NOT NULL DEFAULT '',
    person_id    INTEGER REFERENCES person (id),
    team_id      INTEGER REFERENCES team (id),
    starts_at    TEXT NOT NULL,
    starts_tz    TEXT NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    metadata     TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    created_at   TEXT NOT NULL,
    created_tz   TEXT NOT NULL,
    finalized_at TEXT,
    finalized_tz TEXT,
    voided_at    TEXT,
    voided_tz    TEXT,
    void_reason  TEXT NOT NULL DEFAULT '',
    CHECK ((finalized_at IS NULL) = (finalized_tz IS NULL)),
    CHECK ((voided_at IS NULL) = (voided_tz IS NULL)),
    CHECK (voided_at IS NULL OR finalized_at IS NOT NULL)
);

CREATE INDEX meeting_person ON meeting (person_id, starts_at);

-- The prompt wording is copied into the answer so rewording a prompt later
-- does not change what past meetings show.
CREATE TABLE meeting_answer (
    id          INTEGER PRIMARY KEY,
    meeting_id  INTEGER NOT NULL REFERENCES meeting (id),
    prompt_key  TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    answer      TEXT NOT NULL,
    UNIQUE (meeting_id, prompt_key)
);

CREATE TABLE entry (
    id                INTEGER PRIMARY KEY,
    person_id         INTEGER NOT NULL REFERENCES person (id),
    meeting_id        INTEGER REFERENCES meeting (id),
    kind              TEXT NOT NULL REFERENCES entry_kind (code),
    body              TEXT NOT NULL,
    outcome           TEXT CHECK (outcome IS NULL OR outcome IN ('progress', 'no_progress', 'pending')),
    occurred_at       TEXT NOT NULL,
    occurred_tz       TEXT NOT NULL,
    corrects_entry_id INTEGER REFERENCES entry (id),
    created_at        TEXT NOT NULL,
    created_tz        TEXT NOT NULL,
    finalized_at      TEXT,
    finalized_tz      TEXT,
    voided_at         TEXT,
    voided_tz         TEXT,
    void_reason       TEXT NOT NULL DEFAULT '',
    CHECK ((finalized_at IS NULL) = (finalized_tz IS NULL)),
    CHECK ((voided_at IS NULL) = (voided_tz IS NULL)),
    CHECK (voided_at IS NULL OR finalized_at IS NOT NULL)
);

CREATE INDEX entry_person ON entry (person_id, occurred_at);

CREATE TABLE entry_tag (
    entry_id INTEGER NOT NULL REFERENCES entry (id),
    tag_id   INTEGER NOT NULL REFERENCES tag (id),
    PRIMARY KEY (entry_id, tag_id)
);

CREATE TABLE commitment (
    id           INTEGER PRIMARY KEY,
    person_id    INTEGER NOT NULL REFERENCES person (id),
    meeting_id   INTEGER REFERENCES meeting (id),
    kind         TEXT NOT NULL CHECK (kind IN ('task', 'learning')),
    direction    TEXT NOT NULL CHECK (direction IN ('they_owe_me', 'i_owe_them')),
    content      TEXT NOT NULL,
    deliverable  TEXT NOT NULL DEFAULT '',
    due_on       TEXT,
    created_at   TEXT NOT NULL,
    created_tz   TEXT NOT NULL,
    finalized_at TEXT,
    finalized_tz TEXT,
    voided_at    TEXT,
    voided_tz    TEXT,
    void_reason  TEXT NOT NULL DEFAULT '',
    CHECK ((finalized_at IS NULL) = (finalized_tz IS NULL)),
    CHECK ((voided_at IS NULL) = (voided_tz IS NULL)),
    CHECK (voided_at IS NULL OR finalized_at IS NOT NULL)
);

-- Status log: the current status of a commitment is its latest event.
CREATE TABLE commitment_event (
    id            INTEGER PRIMARY KEY,
    commitment_id INTEGER NOT NULL REFERENCES commitment (id),
    status        TEXT NOT NULL CHECK (status IN ('open', 'done', 'missed', 'dropped')),
    note          TEXT NOT NULL DEFAULT '',
    at            TEXT NOT NULL,
    tz            TEXT NOT NULL
);

CREATE INDEX commitment_event_latest ON commitment_event (commitment_id, id);

CREATE TABLE work_item (
    id                 INTEGER PRIMARY KEY,
    person_id          INTEGER NOT NULL REFERENCES person (id),
    release_id         INTEGER NOT NULL REFERENCES release (id),
    title              TEXT NOT NULL,
    external_ref       TEXT,
    picked_by          TEXT NOT NULL CHECK (picked_by IN ('self', 'lead')),
    stretch            TEXT NOT NULL CHECK (stretch IN ('below', 'at', 'above')),
    stretch_rationale  TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('planned', 'in_progress', 'done', 'dropped', 'moved')),
    investigation_done INTEGER NOT NULL DEFAULT 0,
    investigation_url  TEXT NOT NULL DEFAULT '',
    outcome_notes      TEXT NOT NULL DEFAULT '',
    assigned_at        TEXT NOT NULL,
    assigned_tz        TEXT NOT NULL,
    completed_at       TEXT,
    completed_tz       TEXT,
    moved_from_id      INTEGER REFERENCES work_item (id),
    created_at         TEXT NOT NULL,
    created_tz         TEXT NOT NULL,
    finalized_at       TEXT,
    finalized_tz       TEXT,
    voided_at          TEXT,
    voided_tz          TEXT,
    void_reason        TEXT NOT NULL DEFAULT '',
    CHECK ((finalized_at IS NULL) = (finalized_tz IS NULL)),
    CHECK ((voided_at IS NULL) = (voided_tz IS NULL)),
    CHECK (voided_at IS NULL OR finalized_at IS NOT NULL),
    CHECK ((completed_at IS NULL) = (completed_tz IS NULL))
);

-- A ticket is never reused by someone else, except through a reassignment,
-- which marks the original row as moved.
CREATE UNIQUE INDEX work_item_ticket ON work_item (external_ref)
    WHERE external_ref IS NOT NULL AND voided_at IS NULL AND status <> 'moved';

CREATE INDEX work_item_person ON work_item (person_id, release_id);

-- ---------------------------------------------------------------------------
-- Application access (single user)
-- ---------------------------------------------------------------------------

CREATE TABLE app_user (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

-- Only a hash of the session token is stored, so a copy of the database file
-- cannot be used to take over a session.
CREATE TABLE session (
    id_hash    TEXT PRIMARY KEY,
    csrf_token TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------
-- Locking: finalized rows can only be voided
-- ---------------------------------------------------------------------------

CREATE TRIGGER entry_lock_update BEFORE UPDATE ON entry
WHEN OLD.finalized_at IS NOT NULL AND NOT (
    OLD.voided_at IS NULL AND NEW.voided_at IS NOT NULL
    AND NEW.person_id IS OLD.person_id AND NEW.meeting_id IS OLD.meeting_id
    AND NEW.kind IS OLD.kind AND NEW.body IS OLD.body AND NEW.outcome IS OLD.outcome
    AND NEW.occurred_at IS OLD.occurred_at AND NEW.occurred_tz IS OLD.occurred_tz
    AND NEW.corrects_entry_id IS OLD.corrects_entry_id
    AND NEW.created_at IS OLD.created_at AND NEW.created_tz IS OLD.created_tz
    AND NEW.finalized_at IS OLD.finalized_at AND NEW.finalized_tz IS OLD.finalized_tz
)
BEGIN SELECT RAISE(ABORT, 'entry is finalized: it can only be voided'); END;

CREATE TRIGGER entry_lock_delete BEFORE DELETE ON entry
WHEN OLD.finalized_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'entry is finalized: it cannot be deleted'); END;

CREATE TRIGGER entry_meeting_open BEFORE INSERT ON entry
WHEN NEW.meeting_id IS NOT NULL
    AND (SELECT finalized_at FROM meeting WHERE id = NEW.meeting_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: it cannot take new entries'); END;

CREATE TRIGGER entry_tag_lock_insert BEFORE INSERT ON entry_tag
WHEN (SELECT finalized_at FROM entry WHERE id = NEW.entry_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'entry is finalized: its tags are locked'); END;

CREATE TRIGGER entry_tag_lock_delete BEFORE DELETE ON entry_tag
WHEN (SELECT finalized_at FROM entry WHERE id = OLD.entry_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'entry is finalized: its tags are locked'); END;

CREATE TRIGGER meeting_lock_update BEFORE UPDATE ON meeting
WHEN OLD.finalized_at IS NOT NULL AND NOT (
    OLD.voided_at IS NULL AND NEW.voided_at IS NOT NULL
    AND NEW.kind IS OLD.kind AND NEW.title IS OLD.title AND NEW.person_id IS OLD.person_id
    AND NEW.team_id IS OLD.team_id AND NEW.starts_at IS OLD.starts_at AND NEW.starts_tz IS OLD.starts_tz
    AND NEW.notes IS OLD.notes AND NEW.metadata IS OLD.metadata
    AND NEW.created_at IS OLD.created_at AND NEW.created_tz IS OLD.created_tz
    AND NEW.finalized_at IS OLD.finalized_at AND NEW.finalized_tz IS OLD.finalized_tz
)
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: it can only be voided'); END;

CREATE TRIGGER meeting_lock_delete BEFORE DELETE ON meeting
WHEN OLD.finalized_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: it cannot be deleted'); END;

CREATE TRIGGER meeting_answer_lock_insert BEFORE INSERT ON meeting_answer
WHEN (SELECT finalized_at FROM meeting WHERE id = NEW.meeting_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: its answers are locked'); END;

CREATE TRIGGER meeting_answer_lock_update BEFORE UPDATE ON meeting_answer
WHEN (SELECT finalized_at FROM meeting WHERE id = OLD.meeting_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: its answers are locked'); END;

CREATE TRIGGER meeting_answer_lock_delete BEFORE DELETE ON meeting_answer
WHEN (SELECT finalized_at FROM meeting WHERE id = OLD.meeting_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: its answers are locked'); END;

CREATE TRIGGER commitment_lock_update BEFORE UPDATE ON commitment
WHEN OLD.finalized_at IS NOT NULL AND NOT (
    OLD.voided_at IS NULL AND NEW.voided_at IS NOT NULL
    AND NEW.person_id IS OLD.person_id AND NEW.meeting_id IS OLD.meeting_id
    AND NEW.kind IS OLD.kind AND NEW.direction IS OLD.direction AND NEW.content IS OLD.content
    AND NEW.deliverable IS OLD.deliverable AND NEW.due_on IS OLD.due_on
    AND NEW.created_at IS OLD.created_at AND NEW.created_tz IS OLD.created_tz
    AND NEW.finalized_at IS OLD.finalized_at AND NEW.finalized_tz IS OLD.finalized_tz
)
BEGIN SELECT RAISE(ABORT, 'commitment is finalized: it can only be voided'); END;

CREATE TRIGGER commitment_lock_delete BEFORE DELETE ON commitment
WHEN OLD.finalized_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'commitment is finalized: it cannot be deleted'); END;

CREATE TRIGGER commitment_meeting_open BEFORE INSERT ON commitment
WHEN NEW.meeting_id IS NOT NULL
    AND (SELECT finalized_at FROM meeting WHERE id = NEW.meeting_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'meeting is finalized: it cannot take new commitments'); END;

-- Status events are append-only; they can only disappear together with a
-- commitment that is still a draft.
CREATE TRIGGER commitment_event_no_update BEFORE UPDATE ON commitment_event
BEGIN SELECT RAISE(ABORT, 'commitment events are append-only'); END;

CREATE TRIGGER commitment_event_lock_delete BEFORE DELETE ON commitment_event
WHEN (SELECT finalized_at FROM commitment WHERE id = OLD.commitment_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'commitment events are append-only'); END;

-- A work item stays editable (status, investigation, outcome, completion)
-- because those change as the work progresses; only the judgment is locked.
CREATE TRIGGER work_item_lock_update BEFORE UPDATE ON work_item
WHEN OLD.finalized_at IS NOT NULL AND NOT (
    NEW.person_id IS OLD.person_id AND NEW.release_id IS OLD.release_id
    AND NEW.picked_by IS OLD.picked_by AND NEW.stretch IS OLD.stretch
    AND NEW.stretch_rationale IS OLD.stretch_rationale
    AND NEW.assigned_at IS OLD.assigned_at AND NEW.assigned_tz IS OLD.assigned_tz
    AND NEW.created_at IS OLD.created_at AND NEW.created_tz IS OLD.created_tz
    AND NEW.finalized_at IS OLD.finalized_at AND NEW.finalized_tz IS OLD.finalized_tz
)
BEGIN SELECT RAISE(ABORT, 'work item judgment is locked'); END;

CREATE TRIGGER work_item_lock_delete BEFORE DELETE ON work_item
WHEN OLD.finalized_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'work item is locked: it cannot be deleted'); END;

-- ---------------------------------------------------------------------------
-- Full-text search (kept in sync by triggers, in the same transaction)
-- ---------------------------------------------------------------------------

CREATE VIRTUAL TABLE search_index USING fts5 (
    kind UNINDEXED,
    ref_id UNINDEXED,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER entry_index_insert AFTER INSERT ON entry
BEGIN INSERT INTO search_index (kind, ref_id, body) VALUES ('entry', NEW.id, NEW.body); END;

CREATE TRIGGER entry_index_update AFTER UPDATE OF body ON entry
BEGIN
    DELETE FROM search_index WHERE kind = 'entry' AND ref_id = OLD.id;
    INSERT INTO search_index (kind, ref_id, body) VALUES ('entry', NEW.id, NEW.body);
END;

CREATE TRIGGER entry_index_delete AFTER DELETE ON entry
BEGIN DELETE FROM search_index WHERE kind = 'entry' AND ref_id = OLD.id; END;

CREATE TRIGGER meeting_index_insert AFTER INSERT ON meeting
BEGIN INSERT INTO search_index (kind, ref_id, body) VALUES ('meeting', NEW.id, NEW.title || ' ' || NEW.notes); END;

CREATE TRIGGER meeting_index_update AFTER UPDATE OF title, notes ON meeting
BEGIN
    DELETE FROM search_index WHERE kind = 'meeting' AND ref_id = OLD.id;
    INSERT INTO search_index (kind, ref_id, body) VALUES ('meeting', NEW.id, NEW.title || ' ' || NEW.notes);
END;

CREATE TRIGGER meeting_index_delete AFTER DELETE ON meeting
BEGIN DELETE FROM search_index WHERE kind = 'meeting' AND ref_id = OLD.id; END;

CREATE TRIGGER answer_index_insert AFTER INSERT ON meeting_answer
BEGIN INSERT INTO search_index (kind, ref_id, body) VALUES ('answer', NEW.id, NEW.answer); END;

CREATE TRIGGER answer_index_update AFTER UPDATE OF answer ON meeting_answer
BEGIN
    DELETE FROM search_index WHERE kind = 'answer' AND ref_id = OLD.id;
    INSERT INTO search_index (kind, ref_id, body) VALUES ('answer', NEW.id, NEW.answer);
END;

CREATE TRIGGER answer_index_delete AFTER DELETE ON meeting_answer
BEGIN DELETE FROM search_index WHERE kind = 'answer' AND ref_id = OLD.id; END;

-- ---------------------------------------------------------------------------
-- Defaults (editable and extendable from the settings page)
-- ---------------------------------------------------------------------------

INSERT INTO level (track, code, title, rank) VALUES
    ('engineer', 'E1', 'Intern', 10),
    ('engineer', 'E2', 'Junior', 20),
    ('engineer', 'E3', 'Middle', 30),
    ('engineer', 'E4', 'Middle', 40),
    ('engineer', 'E5', 'Middle', 50),
    ('engineer', 'E6', 'Senior', 60),
    ('engineer', 'E7', 'Lead', 70),
    ('manager', 'EM1', 'Junior', 10),
    ('manager', 'EM2', 'Middle', 20),
    ('manager', 'EM3', 'Senior', 30);

INSERT INTO meeting_kind (code, label, sort_order, requires_person) VALUES
    ('one_on_one', '1:1', 10, 1),
    ('team', 'Team meeting', 20, 0),
    ('standup', 'Standup', 30, 0),
    ('other', 'Other', 40, 0);

INSERT INTO entry_kind (code, label, sort_order, requires_outcome) VALUES
    ('objective', 'Objective', 10, 0),
    ('strength', 'Strength', 20, 0),
    ('improvement_point', 'Improvement point', 30, 0),
    ('feedback_received', 'Feedback received', 40, 0),
    ('feedback_given', 'Feedback given', 50, 0),
    ('observation', 'Observation', 60, 0),
    ('progress_check', 'Progress check', 70, 1);

INSERT INTO meeting_prompt (meeting_kind, prompt_key, prompt_text, sort_order) VALUES
    ('one_on_one', 'learned', 'What did you learn in the last 2 weeks?', 10),
    ('one_on_one', 'applied', 'How did you apply it?', 20),
    ('one_on_one', 'blockers', 'Any blockers?', 30),
    ('one_on_one', 'feedback', 'Feedback for me?', 40),
    ('team', 'agenda', 'Agenda', 10),
    ('team', 'discussion', 'Discussion', 20),
    ('team', 'decisions', 'Decisions', 30),
    ('standup', 'yesterday', 'Yesterday', 10),
    ('standup', 'today', 'Today', 20),
    ('standup', 'blockers', 'Blockers', 30);

INSERT INTO tag (name) VALUES ('ownership'), ('communication'), ('documentation'), ('learning');
