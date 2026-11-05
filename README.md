# pgledger visualizer

A read-only web UI for [pgledger](https://github.com/pgr0ss/pgledger) data. It reads only the public views
(`pgledger_accounts_view`, `pgledger_transfers_view`, `pgledger_entries_view`) and connects with
`default_transaction_read_only=on`.

```sh
pgledger-visualizer serve --db postgres://user:pass@host:5432/dbname
```

Features:

- Live search across accounts, transfers, entries and metadata. Every id and name is a link.
- Account pages show a balance chart (axes, gridlines, hover readout), the balance at any time (as recorded or by event
  time), and the account's entries.
- Trace: pick a transfer metadata key and value (e.g. `payment_id`) to follow money across accounts. A crosstab and an
  in/out grid put accounts in columns with time flowing downward.
- A header toggle switches displayed times between UTC and your browser's timezone.

## Quickstart

```sh
mise install
mise run demo   # start Postgres on :5432, reset the pgledger_visualizer database, load sample data, serve
```

Then open http://127.0.0.1:8080.

To use a different port, put `POSTGRES_PORT=5455` in a gitignored `.env`. docker compose reads it directly, and mise loads
it into every task, including `mise run test`. Setting `DATABASE_URL` overrides the URL used by `mise run seed`,
`mise run serve` and `mise run dev`.

## Pointing at pgledger's example database

In `../pgledger`, run `just run-examples`. Then:

```sh
go run ./cmd/pgledger-visualizer serve --db postgres://pgledger:pgledger@localhost:5432/pgledger
```

`/trace?key=payment_id&value=p_123` reproduces the crosstab in pgledger's `examples/reconciliation.sql.out`.

## Commands

- `pgledger-visualizer serve [--addr 127.0.0.1:8080] [--db URL]`: binds to loopback by default.
- `pgledger-visualizer seed [--install-schema] [--users 20] [--seed 1] [--db URL]`: loads deterministic sample payment
  flows into an empty database. `--install-schema` installs pgledger first if its views are missing.

`--db` defaults to `$DATABASE_URL`.

## Development

- `mise tasks`: list every task.
- `mise run dev`: like `mise run serve`, but restarts (via watchexec) when Go, SQL, template or static files change.
- `mise run check`: format SQL and templates, regenerate sqlc code, tidy, test and lint.
- `mise run test`: needs Postgres (`mise run up`). Each test runs in its own schema. Set `TEST_DATABASE_URL` to use another
  server.
- `mise run sync-pgledger [dir]`: copy pgledger's schema files from `../pgledger` (or `dir`) into `internal/pgschema/sql`.
  Then run `mise run generate`, which re-checks every query against the new schema.

All SQL lives in `internal/db/queries/*.sql`. sqlc (a `go tool` pinned in `go.mod`) compiles it into typed Go in
`internal/db`, and sqlfluff formats and lints it with pgledger's config.

Templates in `internal/web/templates` are formatted and linted by [djLint](https://djlint.com) (Go template profile,
`.djlintrc`). Keep each `{{if}}`, `{{range}}` and `{{end}}` on its own line and put conditionals inside attribute values
(`class="{{if …}}active{{end}}"`), not around attributes; djLint misformats the other forms.

Stack: Go with pgx, `html/template`, about 130 lines of vanilla JS and hand-written CSS, all embedded in one binary. No npm
and no build step.
