# Leadership Journal

A local-only web app to keep notes about the people you manage: 1:1s and other meetings, feedback, commitments, learning goals, the tasks they take on, and their growth over time. Built for one person (the lead), runs on `127.0.0.1`, and keeps everything in a single SQLite file. Nothing leaves your machine.

## Quick start

Requirements: Go 1.23 or newer. No Docker, no other tooling.

```sh
make run
```

Open the address it prints (`http://127.0.0.1:8080` by default). The first time, you choose the password that protects the journal. It cannot be recovered, so keep it somewhere safe.

| Setting | Flag | Environment variable | Default |
|---|---|---|---|
| Listen address (loopback only) | `-addr` | `JOURNAL_ADDR` | `127.0.0.1:8080` |
| Data directory | `-data-dir` | `JOURNAL_DATA_DIR` | `~/.leadership-journal` |
| Log level | `-log-level` | `JOURNAL_LOG_LEVEL` | `info` |
| Time zone | `-timezone` | `JOURNAL_TIMEZONE` (then `TZ`) | detected from the system |
| Days allowed between progress checks | `-progress-days` | `JOURNAL_PROGRESS_DAYS` | `14` |

Flags win over environment variables, which win over defaults. Any address that is not `127.0.0.1`, `localhost` or `::1` is refused at startup. Use a different `-data-dir` for a scratch instance with fake data.

## What it does

- **People**: your reports, your manager and yourself (your own growth uses the same tools), with team and level history.
- **Meetings** (1:1, team, standup, other): minutes with a question template per kind, notes in Markdown, action items, and notes about people made in the meeting.
- **Notes** about a person (objectives, strengths, improvement points, feedback received and given, observations, progress checks) with tags, also between meetings.
- **Commitments**: promises and action items in both directions, with an append-only status history. Learning goals must name a deliverable (a demo or presentation, not "I read it") and a due date.
- **Improvement journal**: a progress check per person every 14 days by default (configurable with `-progress-days`). The dashboard flags overdue checks and `NO PROGRESS` streaks.
- **Work**: releases and tasks per person. You record, at assignment, how much of a stretch the task is for that person (below, at or above), who picked it, and why. Each release shows who has a finalized stretch task. Tasks can be reassigned, keeping the original.
- **Metrics**: share of stretch tasks picked by the person vs assigned by you, share of tasks with an investigation first, and documentation contributions (finalized notes tagged `documentation`).
- **Search** across notes, meeting notes and answers.
- **Report for your manager**: finalized records only, of one report, filtered by period, section and kind of note, as a printable page or a Markdown download.
- **Settings**: add teams, levels (e.g. a QA scale), meeting kinds, entry kinds and minutes questions without touching code. Change the password. Back up the database.

### The draft / finalized / voided ledger

Everything you write starts as a **draft** you can edit or delete freely, and is saved only when you press the button. **Finalize** locks it, enforced by database triggers, not just by the app. After that a record can only be **voided**, with a reason; it is hidden but kept, so the history shows what you recorded and when. To change your mind about a person, add a new note rather than rewriting the old one. Drafts are excluded from metrics, reports and compliance. Finalizing a meeting locks everything noted in it.

### How it maps to the management process

| Process rule | Where |
|---|---|
| One task above their level per release | Work: stretch judgment per person and task, release standing |
| Investigate first, documented in Confluence | Work: "investigated" flag and optional page reference |
| Learning tasks with an observable result and a deadline | Commitments of kind *learning* (deliverable and due date required) |
| Improvement journal every two weeks, `NO PROGRESS` when nothing was applied | Progress-check notes, 1:1 minutes template, dashboard flags |
| Metrics: % hard tasks self-taken vs assigned, documentation | Person page and report |
| No-blame post-mortems; PIP; promotion evidence | Not built yet (see below) |

## Security

The journal holds sensitive information about people, so the design assumes mistakes and hostile web pages, not only strangers on the network.

- Listens on loopback only; refuses anything else at startup.
- Rejects requests whose `Host` header is not a loopback name on the listen port (defeats DNS rebinding). Rejects cross-site state-changing requests (`Origin`, `Sec-Fetch-Site`) and requires a per-session CSRF token on every one. First-run setup is protected the same way, so another web page cannot claim the journal.
- Password hashed with argon2id; five failed logins block further attempts for fifteen minutes. Sessions last twelve hours, are stored only as hashes and sent as `HttpOnly`, `SameSite=Strict` cookies.
- A content security policy that allows no scripts at all, `nosniff`, `no-referrer`, frame denial and `no-store`. Markdown is rendered without raw HTML and sanitized again.
- The data directory is created `0700`, the database and backups `0600`. Logs contain request ids and paths, never note text, queries or form data.

What the app cannot do for you: the database is **not encrypted**. On a company laptop, rely on full-disk encryption (FileVault), keep the data directory out of cloud-synced folders, and keep backups on encrypted storage. Check your employer's policy on keeping performance notes about colleagues, and write every note as if the person could read it.

## Architecture

```
cmd/journal/        entry point: wires config, store and web, shuts down gracefully
internal/domain/    pure types and rules; no database, HTTP or logging
internal/store/     SQLite repositories, embedded forward-only migrations, lock triggers
internal/auth/      argon2id password hashing and session tokens
internal/config/    flags > environment > defaults; loopback-only address check
internal/web/       handlers, templates (html/template) and CSS; no JavaScript
docs/               design documents (data-model.md)
```

`domain` depends on nothing in the project. `store` and `web` depend on `domain`; `web` uses the concrete `*store.Store` because there is only one implementation, and its tests run against a real temporary SQLite database. `internal/` is enforced by the Go toolchain: only this module can import it.

Business rules live in `domain` (validation, release compliance, metrics, progress checks, level history) and in the database (locking). Handlers only translate forms to calls and results to pages.

The data model and its reasoning are in [docs/data-model.md](docs/data-model.md). Migrations are forward-only SQL files embedded in the binary and applied in a transaction each. Before any pending migration runs on an existing database, a snapshot is written next to it (`journal.db.pre-<version>.bak`).

## Development

```sh
make check   # gofmt, go vet and the full test suite: must pass before every commit
make test    # tests only
make cover   # tests with coverage
make fmt     # gofmt -w
make build   # bin/journal
```

Practices (details in `CLAUDE.md`): tests first, a failing regression test before every bug fix, no duplicated logic, explanatory comments only where the reason is not obvious, structured logs for tracing, standard library first.

### Dependencies

| Module | Why |
|---|---|
| `modernc.org/sqlite` | SQLite driver without CGO, includes FTS5 for search |
| `golang.org/x/crypto` | argon2id password hashing (never hand-roll crypto) |
| `github.com/yuin/goldmark` | Markdown rendering |
| `github.com/microcosm-cc/bluemonday` | HTML sanitizing of rendered Markdown |

Versions are pinned to ones that build with Go 1.23. When adding or upgrading a dependency run `GOTOOLCHAIN=local go get module@version`; a bare `go get` pulls the newest release, which silently raises the required Go version.

## Not built yet

- Incidents and post-mortems with follow-up tasks assigned to the owner of the issue.
- Review cycles and PIPs (goals, checkpoints, outcome).
- Corrections of locked notes from the UI (the store supports a note that corrects another).
- Archiving or renaming kinds, levels, prompts and tags from the settings page; `started on` / `left on` dates for people; attendees of team meetings.
- Importing from Jira or Confluence: they are only reachable from the client's VDI, so tasks and investigation links are entered by hand.
- A command-line way to reset a forgotten password.
