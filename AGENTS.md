# Agent notes

See [README.md](README.md) for what the app does, its commands and the basic `mise` tasks.

Stack: Go with pgx, `html/template`, a few hundred lines of vanilla JS and hand-written CSS, all embedded in one binary.
No npm and no build step.

## Tasks

- Run `mise run check` after making code changes.
- `mise run test`: each test runs in its own schema. Set `TEST_DATABASE_URL` to use another server.
- `mise run psql`: open psql in the compose Postgres container on the `pgledger-visualizer` database.
- `mise run sync-pgledger [dir]`: copy pgledger's schema files from `../pgledger` (or `dir`) into `internal/pgschema/sql`.
  Then run `mise run generate`, which re-checks every query against the new schema.
- `mise run screenshots`: capture the running visualizer (`$BASE`, default `http://127.0.0.1:8080`) in light and dark
  mode into `screenshots/{light,dark}`, via headless Google Chrome. Pages are listed in `scripts/screenshots.sh`.
- Bumping `go` in `mise.toml`: bump the `golang` image in `Dockerfile` too.

## Seed data

pgledger stamps rows with `now()`, so `seed` overwrites `created_at` on the accounts, transfers and entries it creates,
giving each simulated transaction (an FX's two legs share one) a distinct `created_at`. Most client-initiated calls
(payment and refund creation, FX) carry a UUID idempotency key.

## SQL

All SQL lives in `internal/db/queries/*.sql`. sqlc (pinned in `mise.toml`) compiles it into typed Go in `internal/db`,
and sqlfluff formats and lints it with pgledger's config.

## Templates

Templates in `internal/web/templates` are formatted and linted by [djLint](https://djlint.com) (Go template profile,
`.djlintrc`). Keep each `{{if}}`, `{{range}}` and `{{end}}` on its own line and put conditionals inside attribute values
(`class="{{if …}}active{{end}}"`), not around attributes; djLint misformats the other forms.

## CSS and JS

`internal/web/static` (CSS and JS) is formatted and linted by [Biome](https://biomejs.dev) (`biome.json`). The CSS
uses cascade layers, declared in `tokens.css` and linked in that order from `layout.html`:

- `tokens.css`: colors (each written once with `light-dark()`; tints derived with `color-mix()`), type, space and
  radius scales. Use these variables rather than raw values.
- `base.css`: element defaults.
- `components.css`: one block per partial or shared element, styled by class name.
- `pages.css`: layout used by a single page.
- `utilities.css`: `.muted`, `.pos`, `.neg`, `.zero`, `.error`. The last layer, so these always win.

Later layers win over earlier ones whatever the selector specificity. The header's theme toggle sets `data-theme` on
`<html>`, which switches `color-scheme`.
