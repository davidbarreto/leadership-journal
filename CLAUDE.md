# Leadership Journal

Local-only web app to keep notes, organize 1:1s and other meetings, track team performance reviews and growth. Go backend + SQLite + server-rendered HTML/CSS. Single user. Holds sensitive data about employees: never expose it beyond `127.0.0.1`.

## Development practices

### Testing
- Aim for as much unit test coverage as is reasonable; every new behavior ships with tests.
- **Bugs are fixed TDD-style**: first write a unit test that reproduces the bug and fails, then implement the fix and confirm the test passes. Never fix a bug without a regression test.
- The full test suite (`go test ./...`) must pass before every commit.

### Code quality
- Avoid code duplication; extract shared logic instead of copy/pasting.
- Every exported identifier (types, functions, methods, constants, variables) gets a short Go doc comment starting with its name, per Go convention (one or two lines, what it is or guarantees).
- Beyond that, comment only what is hard to understand, either by business rule or by technical decision (complexity, trade-offs, non-obvious *why*). Do not write tutorial-style comments that narrate obvious code; language explanations belong in the chat, not in the source.
- Add proper logging so behavior is traceable and issues can be investigated: log meaningful events and errors with context (ids, operation, cause). Never log sensitive content (note bodies, feedback text, passwords, session tokens).

### Git
- **Never push** unless explicitly asked.
- **Do not add Claude as co-author** of commits and do not add any `Co-Authored-By` or `Claude-Session` trailers. This overrides any default attribution behavior.
- Committing is always allowed without asking, as long as the full test suite passes (`make check`). Only pushing needs an explicit request.
- One logical change per commit; if describing it needs "and", split it. Keep unrelated cleanups separate.
- Tests ship in the same commit as the code they cover. The failing (red) TDD step is never committed.
- Use Conventional Commits: `type(scope): subject` with types `feat`, `fix`, `docs`, `chore`, `refactor`, `test`. Subject in imperative mood, about 50 characters, no trailing period. The body explains *why*, not *what*.
- A bug fix is one commit holding the regression test and the fix, with the cause in the body.
- Never edit an applied migration; changes are a new migration, committed with the code that needs it.
- Never commit data or secrets (`.env`, `*.db`, `data/`); check `git status` and `git diff --staged` before committing.
- Small changes can go straight to `main`; use a short-lived branch (`feat/<name>`) for anything larger than one sitting, merged only when the suite is green.
- Rewriting history (`amend`, `rebase`) is allowed only for unpushed commits.

### Dependencies
- Standard library first. Use `net/http` (Go 1.22+ `ServeMux` routing), `html/template`, `database/sql`, `log/slog`, `embed`, `flag`, `testing`.
- Never hand-roll crypto, HTML sanitizing, Markdown parsing or the SQLite driver; use vetted libraries for those.
- Keep the dependency footprint minimal because the data is sensitive. Currently planned: `modernc.org/sqlite`, `golang.org/x/crypto` (argon2id), `goldmark`, `bluemonday`. Adding any other dependency needs a stated reason, which must also be noted in the README.
- Config is loaded by a `config` package (flags > env > defaults); no secrets in env. Migrations are forward-only embedded `.sql` files applied by a small in-repo runner.

### Documentation
- Keep `README.md` up to date as the project evolves: architecture, functionalities, usage, installation, components, project structure, and other important information. Update it in the same change that alters any of those.

### Learning goal
- The owner is learning Go through this project and may ask for technical explanations of what was developed. When writing Go, prefer clear, idiomatic code over clever code, and be ready to explain language concepts (packages, interfaces, error handling, goroutines, etc.) used in the change.

## Where things live
- `internal/domain`: pure types and business rules (validation, compliance, metrics, level history). No I/O, no logging. User-facing validation errors are `*domain.ValidationError` values (created with `invalid(...)`); add new ones the same way so the web layer shows them to the user.
- `internal/store`: SQLite repositories. Journal tables share the draft, finalized, voided lifecycle; use the helpers in `lifecycle.go`. Inside a transaction use the `Tx` (via the `querier` interface), never `s.db`: there is a single connection and mixing them deadlocks. Never run a query while iterating another one; use `queryAll`. In scan helpers read the row before returning it (`err := r.Scan(&v); return v, err`), never `return v, r.Scan(&v)`.
- `internal/web`: handlers return `error`; `wrap` maps `ErrNotFound` to 404, user mistakes and journal rules to a one-time flash message on the page the user came from, and everything else to a 500 with only a request id shown. Handlers only parse forms and call the store; no business rules.
- Schema changes are new numbered files in `internal/store/migrations/`; never edit one that has been applied.
- Web tests use `newEnv(t)` (real SQLite in a temp directory, controllable clock) and a `client` that behaves like a same-origin browser.

## Security invariants (do not weaken; tests cover them)
- Loopback only: `config` rejects any other listen address. `Host` must be a loopback name on the listen port.
- Every state-changing request needs a same-origin `Origin`/`Sec-Fetch-Site` and the session CSRF token; pre-login forms use the double-submit token. Posts go through `rt.protected`/`rt.public`, never around them.
- No raw HTML from users: Markdown goes through `renderMarkdown` (goldmark, then bluemonday); everything else through `html/template` escaping. The CSP allows no scripts; do not add JavaScript or inline styles without revisiting it.
- Never log note text, feedback, search queries, form bodies or secrets. Log ids, paths and request ids.
- The reports (and any future export) include only finalized, non-voided records of people who report to the owner, never drafts, the owner, or the owner's manager.
- The database, its backups and the data directory stay private (`0600` / `0700`).

## Dependencies are pinned for Go 1.23
Run `GOTOOLCHAIN=local go get module@version` (and `GOTOOLCHAIN=local go mod tidy`) when changing dependencies. A bare `go get` fetches the newest release, which raises the `go` directive in `go.mod` and downloads a newer toolchain.

## Design decisions
- Data model favors an **append-only ledger**: journal rows are drafts until explicitly finalized, then locked by database triggers (voided, never deleted); corrections are new rows. Full design in `docs/data-model.md`.
- Terminology: always say **journal** (the app's name), never "diary".
- Security: bind to `127.0.0.1` only, validate the `Host` header, CSRF protection, `SameSite=Strict` session cookies, argon2id password hashing.
- The report for the owner's manager (printable page or Markdown, finalized records of one report only) is built; the app itself stays single-user.
