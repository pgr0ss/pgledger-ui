# pgledger-visualizer

pgledger-visualizer is a read-only web UI for [pgledger](https://github.com/pgr0ss/pgledger) (a double-entry ledger implementation in PostgreSQL).

Simply point it at a database which uses pgledger views:

```sh
pgledger-visualizer serve --database-url postgres://user:pass@host:5432/dbname
```

## Why

Ledgers are incredibly useful, but they can be a bit hard to wrap your head around visually. What are the money flows? What is the set of accounts per user? Why is the balance in this account not what I expect?

This app aims to help address that problem as a companion to [pgledger](https://github.com/pgr0ss/pgledger). pgledger standardizes the ledger modeling, and pgledger-visualizer helps visualize and debug the money flows.

## Screenshots

(More in [screenshots](screenshots))

<a href="screenshots/light/02-search.png">
  <img src="screenshots/light/02-search.png" alt="screenshot search" width="300" />
</a>
<a href="screenshots/light/03-account-entries.png">
  <img src="screenshots/light/03-account-entries.png" alt="screenshot account" width="300" />
</a>
<a href="screenshots/light/05-trace-crosstab.png">
  <img src="screenshots/light/05-trace-crosstab.png" alt="screenshot trace" width="300" />
</a>

## Features

- Live search across accounts, transfers, entries, idempotency keys and metadata.
- Everything is linked. Easily click and navigate from accounts to transfers to entries, and more. Also, one-click copy on all IDs and names.
- Transfers show their idempotency key, linking to every transfer created with it, and their metadata, linking to its trace.
- Account pages show a balance chart, total funds in and out with the top 5 accounts each way, the balance at any time (as recorded or by event time), and the account's entries.
- Trace view shows all transfers for a metadata key/value pair (e.g. `payment_id`). A crosstab and an in/out grid put accounts in columns with time flowing downward.
- Local settings allow switching between light/dark mode and UTC/local timezone.

## Run without cloning

With Go installed:

```sh
go run github.com/pgr0ss/pgledger-visualizer@latest serve --database-url postgres://user:pass@host:5432/dbname
```

Or install the `pgledger-visualizer` binary into `$(go env GOPATH)/bin`:

```sh
go install github.com/pgr0ss/pgledger-visualizer@latest
```

## Demo it with Docker

[`docker-compose.demo.yml`](docker-compose.demo.yml) needs only Docker. From a clone of this repository, it builds the
visualizer, starts Postgres, loads sample data and serves:

```sh
docker compose -f docker-compose.demo.yml up --build
```

Then open http://127.0.0.1:8080.

## Commands

- `pgledger-visualizer [serve] [--addr 127.0.0.1:8080] [--log-slow-queries-ms -1] [--dev] [--database-url URL]`: the
  default command.
  - `--log-slow-queries-ms N` logs queries taking at least N ms with their arguments; `0` logs every query, and `-1` (the default) disables query logging.
  - `--dev` reads templates and static files from `internal/web` on every request and live-reloads open pages when one changes. Only works when run from the root of a checkout.
- `pgledger-visualizer seed [--install-schema] [--users 20] [--seed 1] [--database-url URL]`: loads deterministic sample
  flows into an empty database, spread over the last 30 days.
  - `--install-schema` installs pgledger first if its views are missing.

### Environment variables

Every flag can instead be set by an environment variable named after it in upper snake case. If both the flag and environment variable are set, the flag wins. Each command's `-h` lists them.

- `--database-url` → `DATABASE_URL` (note that this is the only one without the `PGLEDGER_VISUALIZER_` prefix)
- `--addr` → `PGLEDGER_VISUALIZER_ADDR`
- `--log-slow-queries-ms` → `PGLEDGER_VISUALIZER_LOG_SLOW_QUERIES_MS`
- `--install-schema` → `PGLEDGER_VISUALIZER_INSTALL_SCHEMA`

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

## Security

`pgledger-visualizer serve` is read-only. It connects to the database with `default_transaction_read_only=on`, and queries the public views (e.g. `pgledger_transfers_view`). However, `default_transaction_read_only` is a session default, not a permission. If you are using pgledger-visualizer in production, it would be safer to use it with a read-only connection or user.

There is no authentication. If you want to host it for others, run it behind an authenticating proxy.

Pass the database URL through the `DATABASE_URL` environment variable rather than `--database-url` to keep its password out of `ps` output.

The server sets some security-focused response headers, such as a same-origin `Content-Security-Policy` (no inline or third-party scripts), `X-Frame-Options: DENY` and `X-Content-Type-Options: nosniff`.

## Development

The project relies on [Mise](https://mise.jdx.dev/) and Docker for dependencies.

- `mise install`: install all dependencies.
- `mise run postgres-up`: start Postgres in the foreground (or `mise run postgres-up-background` to run it in the background). The other tasks need it.
- `mise run seed`: install pgledger into the `pgledger-visualizer` database and load sample data.
- `mise run dev`: run the app with auto-reloading so you can use it in a local browser (http://127.0.0.1:8080).
- `mise run check`: format SQL, templates, CSS and JS, regenerate sqlc code, tidy, test and lint.

[/styleguide](http://127.0.0.1:8080/styleguide) shows the design tokens and every partial in each of its states, from fixture data. Open it next to `mise run dev` when working on the design.
