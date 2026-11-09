# pgledger visualizer

A read-only web UI for [pgledger](https://github.com/pgr0ss/pgledger) data. It reads only the public views
(`pgledger_accounts_view`, `pgledger_transfers_view`, `pgledger_entries_view`, and `pgledger_idempotency_keys_view` when
present) and connects with `default_transaction_read_only=on`.

```sh
pgledger-visualizer serve --database-url postgres://user:pass@host:5432/dbname
```

Features:

- Live search across accounts, transfers, entries, idempotency keys and metadata. Every id and name is a link. Lists
  and grids shorten ids, and prefixed ULIDs inside account names (`user.us_…XYQ6VWQ7.available`); hover for the full
  value.
- Transfers show the idempotency key they were created with, linking to every transfer created by that call. pgledger
  versions without `pgledger_idempotency_keys_view` work too; the UI then hides idempotency keys. It checks for the view
  at startup.
- Account pages show a balance chart (axes, gridlines, hover readout), total funds in and out with the top 5 accounts
  each way, the balance at any time (as recorded or by event time), and the account's entries. Drag across the chart to
  zoom the chart, funds, entries and grids to that `created_at` range.
- Trace: pick a transfer metadata key and value (e.g. `payment_id`) to follow money across accounts. A crosstab and an
  in/out grid put accounts in columns with time flowing downward.
- The crosstab and in/out grids bracket and number (`tx 1`, `tx 2`, …) transfers created in the same database
  transaction; hovering one highlights the rest. pgledger stamps `created_at` with `now()`, the transaction's start
  time, so shared `created_at` is the signal (exact across savepoints, which `xmin` is not, and visible through the
  views). Two transactions starting in the same microsecond would be grouped too.
- A header toggle switches displayed times between UTC and your browser's timezone.

## Try it with Docker

[`docker-compose.demo.yml`](docker-compose.demo.yml) needs only Docker. From a clone of this repository, it builds the
visualizer, starts Postgres, loads sample data and serves:

```sh
docker compose -f docker-compose.demo.yml up --build
```

Then open http://127.0.0.1:8080. Ctrl-C stops it, and `docker compose -f docker-compose.demo.yml down` removes the
containers.

- `--build` rebuilds the image from the checkout, so it picks up local changes.
- Postgres keeps its data in memory, so each start seeds fresh data spread over the 30 days before it.
- `DEMO_PORT=8081` serves on another port.

The [`Dockerfile`](Dockerfile) image runs `serve` on `0.0.0.0:8080` by default:
`docker run -p 127.0.0.1:8080:8080 -e DATABASE_URL=… <image>`.

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
go run ./cmd/pgledger-visualizer serve --database-url postgres://pgledger:pgledger@localhost:5432/pgledger
```

`/trace?key=payment_id&value=p_123` reproduces the crosstab in pgledger's `examples/reconciliation.sql.out`.

## Commands

- `pgledger-visualizer serve [--addr 127.0.0.1:8080] [--log-slow-queries-ms -1] [--database-url URL]`: binds to
  loopback by default and logs every request. Responses carry a same-origin Content-Security-Policy,
  `X-Frame-Options: DENY` and `X-Content-Type-Options: nosniff`. `--log-slow-queries-ms N` logs queries taking at least
  N ms with their arguments; `0` logs every query, `-1` (default) disables query logging.
- `pgledger-visualizer seed [--install-schema] [--users 20] [--seed 1] [--database-url URL]`: loads deterministic sample
  flows into an empty database, spread over the last 30 days. pgledger stamps rows with `now()`, so the seed overwrites
  `created_at` on the accounts, transfers and entries it creates, giving each simulated transaction (an FX's two legs
  share one) a distinct `created_at`. Most client-initiated calls (payment and refund creation, FX) carry a UUID
  idempotency key. `--install-schema` installs pgledger first if its views are missing.

### Environment variables

Every flag can instead be set by an environment variable named after it in upper snake case; a flag on the command
line wins. `--database-url` reads `DATABASE_URL`, and the rest add a `PGLEDGER_VISUALIZER_` prefix (`--addr` →
`PGLEDGER_VISUALIZER_ADDR`, `--log-slow-queries-ms` → `PGLEDGER_VISUALIZER_LOG_SLOW_QUERIES_MS`, `--install-schema` →
`PGLEDGER_VISUALIZER_INSTALL_SCHEMA`). Each command's `-h` lists them.

Setting the database URL through `DATABASE_URL` rather than `--database-url` keeps its password out of `ps` output.

With the [1Password CLI](https://developer.1password.com/docs/cli/), store the URL in an item and give `op run` a
[secret reference](https://developer.1password.com/docs/cli/secret-references/) (`op://<vault>/<item>/<field>`).
`op run` replaces references in the environment with their values and runs the command with them:

```sh
DATABASE_URL=op://Private/pgledger-prod/url op run -- pgledger-visualizer serve
```

The reference holds no secret, so it can also live in a committed env file:

```sh
echo 'DATABASE_URL=op://Private/pgledger-prod/url' > prod.env
op run --env-file=prod.env -- pgledger-visualizer serve
```

## Development

- `mise tasks`: list every task.
- `mise run dev`: like `mise run serve`, but restarts (via watchexec) when Go, SQL, template or static files change.
- `mise run check`: format SQL, templates, CSS and JS, regenerate sqlc code, tidy, test and lint.
- `mise run test`: needs Postgres (`mise run postgres-up`, or `mise run postgres-up-background` to detach). Each test
  runs in its own schema. Set `TEST_DATABASE_URL` to use another server.
- Bumping `go` in `mise.toml`: bump the `golang` image in `Dockerfile` too.
- `mise run sync-pgledger [dir]`: copy pgledger's schema files from `../pgledger` (or `dir`) into `internal/pgschema/sql`.
  Then run `mise run generate`, which re-checks every query against the new schema.

All SQL lives in `internal/db/queries/*.sql`. sqlc (pinned in `mise.toml`) compiles it into typed Go in `internal/db`,
and sqlfluff formats and lints it with pgledger's config.

Templates in `internal/web/templates` are formatted and linted by [djLint](https://djlint.com) (Go template profile,
`.djlintrc`). Keep each `{{if}}`, `{{range}}` and `{{end}}` on its own line and put conditionals inside attribute values
(`class="{{if …}}active{{end}}"`), not around attributes; djLint misformats the other forms.

`internal/web/static` (CSS and JS) is formatted and linted by [Biome](https://biomejs.dev) (`biome.json`).

Stack: Go with pgx, `html/template`, about 130 lines of vanilla JS and hand-written CSS, all embedded in one binary. No npm
and no build step.
