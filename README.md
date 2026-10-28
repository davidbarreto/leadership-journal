# Leadership Journal

Local-only web app to keep notes, organize 1:1s and other meetings, track performance reviews and the growth of a team. Single user; data never leaves the machine.

> Status: planning. No code yet.

## Planned functionality
- People: team members, level, status.
- Meetings: 1:1s and others, with notes and action items.
- Bi-weekly improvement journal per person (what was learned, how it was applied, `progress` / `no_progress`).
- Learning goals with a deadline and an observable deliverable (demo, presentation).
- Release tasks with the lead's per-person stretch judgment (below / at / above) and who picked them; metrics derived from them.
- Documentation contributions and investigation records.
- Incidents and post-mortems, feedback, review cycles, PIPs.
- Full-text search and history (append-only ledger).
- Report export for sharing with a manager.

## Architecture
- Go backend (business rules, auth, search, history).
- SQLite database (pure-Go driver, FTS5 for search).
- Server-rendered HTML + CSS (`html/template`, optionally HTMX).
- Runs on `127.0.0.1` only.

## Project structure
```
cmd/journal/        entry point (will only wire things together)
internal/domain/    pure types and business rules (no DB, HTTP or logging)
internal/store/     (planned) SQLite repositories and migrations
internal/web/       (planned) HTTP handlers and templates
internal/config/    (planned) flags/env loading
docs/               design docs (see docs/data-model.md)
```
Dependency direction: `web` and `store` depend on `domain`; `domain` depends on nothing in the project. `web` declares the small interfaces it needs and `main` plugs the store in.

`internal/` is enforced by the Go toolchain: only code in this module can import it.

## Development
Requires Go 1.23+ (no other tooling, no Docker).

- `make test`: run the full suite (must pass before every commit)
- `make cover`: tests with coverage
- `make vet`: static checks

## Installation and usage
_To be defined once the server exists._
