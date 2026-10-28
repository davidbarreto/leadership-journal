# Data model

Status: **implemented** (migrations `0001` and `0002`). See "Implementation notes" at the end for where the code differs from this design. SQLite, forward-only migrations.

## Conventions

- **Primary keys**: `INTEGER PRIMARY KEY` named `id`, unless stated.
- **Instants** (a moment in time): every `x_at` column is RFC 3339 **UTC** text and has a sibling `x_tz` holding the IANA zone name (e.g. `Europe/Lisbon`) the owner was in. Only `x_at` is used for ordering and comparison. The `_tz` columns are not repeated in the tables below. A single Go type builds and reads the pair.
- **Dates without time** (`*_on`, `effective_from`): `TEXT` `YYYY-MM-DD`.
- **Free text** (`body`, `notes`, `answer`, ...) is Markdown, rendered server-side and sanitized. Never log it.
- **Fixed enums** (their values drive code or metrics): `TEXT` with `CHECK (col IN (...))`.
- **Extensible kinds** (pure classification): their own small tables, defaults inserted by a migration, more added from a settings page. `code` is immutable, `label` is editable, removal means `archived_at`. Behavior is carried by flags in the row (e.g. `requires_person`), never by checking a code in Go.

### Drafts and the ledger

Every journal row (meeting, entry, answer, commitment, work item) has `finalized_at`:

- `NULL` = **draft**: editable, hard-deletable, excluded from metrics and exports, still searchable.
- set = **locked**: SQLite triggers reject `UPDATE` and `DELETE`, except for setting `voided_at`/`void_reason` (work items are the one exception: only their judgment columns lock, see `work_item`). This is enforced in the database and covered by tests.
- Finalizing a **meeting** locks it and everything created in it (answers, entries, commitments) in one transaction. Standalone entries are finalized individually.
- A locked row that is wrong is **voided** (hidden by default, never removed). To correct it, add a new row; an entry may reference the one it corrects (`corrects_entry_id`).
- Reference data (teams, levels, kinds, tags, releases, prompts) is ordinary mutable data.

## Overview

```
team 1─* person 1─* person_level *─1 level
              │
              ├─* meeting *─1 meeting_kind ;  meeting_prompt (per kind)
              │      └─* meeting_answer
              ├─* entry *─1 entry_kind ; entry *─* tag
              ├─* commitment ─* commitment_event
              └─* work_item *─1 release
```

`person.relationship` is `report`, `manager` or `self`. The owner's own journal and the notes about the owner's manager use the same tables as the team's.

## Reference tables

### team
`id`, `name` UNIQUE, `created_at`.

### level
Seniority scale for people. Ranks are only comparable **within a track**.

| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| track | TEXT NOT NULL | e.g. `engineer`, `manager`, `qa` |
| code | TEXT NOT NULL UNIQUE | e.g. `E4`, `EM2` |
| title | TEXT NOT NULL | seniority name, e.g. `Senior` |
| rank | INTEGER NOT NULL | higher = more senior; UNIQUE (track, rank). Integers seeded with gaps of 10 so a level can be inserted between two others without renumbering |
| expectations | TEXT | what is expected of someone at this level (reference for reviews) |
| archived_at | TEXT NULL | |

Seed:

| track | code | title |
|---|---|---|
| engineer | E1 | Intern |
| engineer | E2 | Junior |
| engineer | E3 | Middle |
| engineer | E4 | Middle |
| engineer | E5 | Middle |
| engineer | E6 | Senior |
| engineer | E7 | Lead |
| manager | EM1 | Junior |
| manager | EM2 | Middle |
| manager | EM3 | Senior |

Ranks: E1=10, E2=20, ... E7=70 and EM1=10, EM2=20, EM3=30 (each track has its own scale). The QA scale is added from the UI.

### meeting_kind
| column | notes |
|---|---|
| code PK, label, sort_order, archived_at | |
| requires_person | boolean; 1:1s need a person, team meetings don't |

Seed: `one_on_one` (requires_person), `team`, `standup`, `other`.

### entry_kind
| column | notes |
|---|---|
| code PK, label, sort_order, archived_at | |
| requires_outcome | boolean; entries of this kind must carry an `outcome` |

Seed: `objective`, `strength`, `improvement_point`, `feedback_received` (from the person to the owner), `feedback_given` (from the owner to the person), `observation`, `progress_check` (requires_outcome).

### tag
`id`, `name` UNIQUE (lowercase), `archived_at`. Linked to entries through `entry_tag (entry_id, tag_id)`, PK on both. Suggested: `ownership`, `communication`, `documentation`, `learning`.

### meeting_prompt
Minutes template per meeting kind.

| column | notes |
|---|---|
| id PK | |
| meeting_kind | → meeting_kind.code |
| key | stable key, UNIQUE (meeting_kind, key) |
| text | the wording shown |
| sort_order, archived_at | |

Seed:

- `one_on_one`: `learned` (What did you learn in the last 2 weeks?), `applied` (How did you apply it?), `blockers`, `feedback`
- `team`: `agenda`, `discussion`, `decisions`
- `standup`: `yesterday`, `today`, `blockers`

### release
`id`, `name`, `starts_on`, `ends_on`.

## Journal tables

### person
| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| name | TEXT NOT NULL | |
| relationship | TEXT NOT NULL | `report` \| `manager` \| `self` |
| role_title | TEXT | |
| team_id | INTEGER NULL → team | one team at a time; NULL for manager/self |
| started_on, left_on | TEXT NULL | |
| created_at | TEXT NOT NULL | |

Exactly one `self` row (partial unique index).

### person_level
Level history, so a past task is judged against the level the person had *then*.

| column | type | notes |
|---|---|---|
| person_id | INTEGER → person | |
| level_id | INTEGER → level | |
| effective_from | TEXT NOT NULL | date |
| PK | (person_id, level_id) | a person holds each level once; going back to a previous level (e.g. leaving and rejoining) is out of scope |

Rule (application-level, tested): within a track, a later `effective_from` must have a higher `rank`. A move between tracks (e.g. E6 to EM1) is just another row.

### meeting
| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| kind | TEXT NOT NULL → meeting_kind | |
| title | TEXT | |
| person_id | INTEGER NULL → person | required when the kind has `requires_person` |
| team_id | INTEGER NULL → team | for team meetings |
| starts_at | TEXT NOT NULL | UTC (+ `starts_tz`) |
| notes | TEXT NOT NULL DEFAULT '' | free-form minutes |
| metadata | TEXT NULL | JSON, `CHECK (json_valid(metadata))`; for extras you will not query (e.g. attendees) |
| created_at, finalized_at, voided_at, void_reason | | |

### meeting_answer
| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| meeting_id | INTEGER → meeting | |
| prompt_key | TEXT NOT NULL | |
| prompt_text | TEXT NOT NULL | **snapshot** of the wording at the time, so rewording later does not change history |
| answer | TEXT NOT NULL | |
| UNIQUE | (meeting_id, prompt_key) | |

### entry
The core journal item. Always about one person; optionally tied to a meeting.

| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| person_id | INTEGER NOT NULL → person | |
| meeting_id | INTEGER NULL → meeting | NULL = noted between meetings |
| kind | TEXT NOT NULL → entry_kind | |
| body | TEXT NOT NULL | |
| outcome | TEXT NULL | `progress` \| `no_progress` \| `pending`; required iff the kind has `requires_outcome` |
| occurred_at | TEXT NOT NULL | when it happened (defaults to the meeting time) |
| corrects_entry_id | INTEGER NULL → entry | |
| created_at, finalized_at, voided_at, void_reason | | |

The bi-weekly improvement journal is the 1:1's `learned`/`applied` answers plus a `progress_check` entry with its outcome. `NO PROGRESS` is `outcome = 'no_progress'`. A person is overdue when their last finalized `progress_check` is older than 2 weeks.

### commitment
Covers promises, action items and learning goals.

| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| person_id | INTEGER NOT NULL → person | |
| meeting_id | INTEGER NULL → meeting | where it was made |
| kind | TEXT NOT NULL | `task` \| `learning` |
| direction | TEXT NOT NULL | `they_owe_me` \| `i_owe_them` |
| content | TEXT NOT NULL | |
| deliverable | TEXT NULL | what will be shown; required for `learning` (a demo or presentation, not "I read it") |
| due_on | TEXT NULL | required for `learning` |
| created_at, finalized_at, voided_at, void_reason | | |

### commitment_event
Append-only status log; the current status is the latest event (view).

| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| commitment_id | INTEGER → commitment | |
| status | TEXT NOT NULL | `open` \| `done` \| `missed` \| `dropped` |
| note | TEXT | evidence for `done` (e.g. what was presented) |
| at | TEXT NOT NULL | UTC (+ `tz`) |

### work_item
A task a person worked on in a release. Entered manually; links are optional since the client's tools are reachable only through VDI.

| column | type | notes |
|---|---|---|
| id | INTEGER PK | |
| person_id | INTEGER NOT NULL → person | |
| release_id | INTEGER NOT NULL → release | |
| title | TEXT NOT NULL | |
| external_ref | TEXT NULL | Jira key/URL |
| stretch | TEXT NOT NULL | `below` \| `at` \| `above`: how much of a stretch this task is **for this person**, judged by the lead at assignment |
| stretch_rationale | TEXT NOT NULL | why, e.g. new area for them, or they did the same before |
| picked_by | TEXT NOT NULL | `self` \| `lead` |
| status | TEXT NOT NULL | `planned` \| `in_progress` \| `done` \| `dropped` \| `moved` (reassigned to someone else) |
| investigation_done | INTEGER NOT NULL DEFAULT 0 | boolean |
| investigation_url | TEXT NULL | Confluence page |
| outcome_notes | TEXT | how it went, how independent |
| assigned_at | TEXT NOT NULL | |
| completed_at | TEXT NULL | |
| moved_from_id | INTEGER NULL → work_item | set on the new row created when a task is reassigned |
| created_at, finalized_at, voided_at, void_reason | | |

A work item is the link between one task and one person: a ticket is never reused by someone else, so `external_ref` is unique among non-voided rows (partial unique index, when not NULL). Difficulty depends on the person (prior experience, business knowledge), so there is no task level; the stretch judgment is made once, at assignment, and is never changed after the row is locked. A wrongly entered item is voided and entered again.

Unlike other journal rows, a locked work item stays partly editable, because its status, investigation fields, outcome notes and completion date change as the work progresses. Only the judgment columns are locked by trigger: `person_id`, `release_id`, `picked_by`, `stretch`, `stretch_rationale` and `assigned_at`. The trade-off is that status changes are not kept as a history; only `assigned_at` and `completed_at` are.

Reassigning a task before it is done keeps the original row, sets its status to `moved`, and creates a new row for the new person with `moved_from_id` pointing at the original and its own stretch judgment. A `moved` item does not count towards the original person's release compliance.

## Derived (views, not stored)

- `person_level_at(person_id, date)` and `current_level`.
- `release_compliance`: per person and release, whether at least one task has `stretch = 'above'` and is not voided, `dropped` or `moved`.
- `commitment_current`: latest `commitment_event` per commitment.
- Metrics (finalized rows only): % of `above` tasks picked by `self` vs `lead`; % of tasks with `investigation_done`; count of entries tagged `documentation` per period; consecutive `no_progress` checks per person.

## Search

SQLite FTS5 over `entry.body`, `meeting.notes` and `meeting_answer.answer`, updated by the application in the same transaction as the write. Voided rows are excluded; drafts are shown with a "draft" marker.

## Deferred (not in the first schemas)

Incident and post-mortem (owner, follow-up commitments), review cycle, PIP (goals, checkpoints, outcome), report export (explicit per-person selection; never includes `self` or `manager` unless asked).

## Decisions

- Drafts are saved only on an explicit "Save draft" (no auto-save).
- Finalizing a meeting is a deliberate button.
- Terminology: "journal", never "diary".

## Open questions

1. QA level codes, when you want them seeded.

## Implementation notes

Where the implementation differs from, or goes beyond, the design above:

- **Reference data is archived with a boolean `archived` column**, not `archived_at` plus a zone. The instant-and-zone rule applies to journal data. The infrastructure tables `app_user` and `session` use plain UTC timestamps (`session.expires_at` is a Unix time) because they are not part of the record.
- **Drafts and locking** follow the design: `finalized_at` NULL means draft; triggers reject changes and deletes on locked rows, except voiding. Two refinements:
  - Entries and commitments that belong to a meeting cannot be finalized on their own; they lock with the meeting. That way a draft meeting can always be deleted whole. Voiding a meeting voids what was noted in it.
  - Nothing can be added to a finalized meeting (a trigger rejects new entries, commitments and answers for it).
- **Work items** lock only `person_id`, `release_id`, `picked_by`, `stretch`, `stretch_rationale`, `assigned_at` and the creation and finalization stamps. A partial unique index keeps a ticket reference unique among rows that are neither voided nor `moved`.
- **Views**: only `commitment_current` is a SQL view. Release compliance, work metrics, progress overdue and no-progress streaks are computed by the `domain` package from the rows (`ReleaseCompliant`, `ComputeWorkMetrics`, `ProgressOverdue`, `ConsecutiveNoProgress`), where they are unit-tested. A progress check is any finalized entry whose kind has `requires_outcome = 1`; no kind code is hard-coded.
- **Documentation contributions** are finalized entries tagged `documentation`, as suggested.
- **Search** is an FTS5 table kept in sync by triggers on `entry`, `meeting` and `meeting_answer`, so it is updated in the same transaction as any write, including draft edits and deletes. Voided records are filtered out when querying.
- **Schema present but not yet used by the UI**: `meeting.team_id`, `meeting.metadata`, `entry.corrects_entry_id` (the store accepts it), and `person_level`-based reporting beyond the history shown on the person page.
- **Not in the schema yet**: `person.started_on` / `left_on`, incidents, post-mortems, review cycles and PIPs (the "Deferred" list above).
