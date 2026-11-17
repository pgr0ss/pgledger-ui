package seed_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger-visualizer/internal/seed"
	"github.com/pgr0ss/pgledger-visualizer/internal/testdb"
)

var opts = seed.Options{Users: 8, Seed: 1, Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func run(t *testing.T, pool *pgxpool.Pool, o seed.Options) (seed.Summary, error) {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	return seed.Run(t.Context(), conn.Conn(), o)
}

type transferRow struct {
	From, To, Amount, Metadata, IdempotencyKey string
}

func transfers(t *testing.T, pool *pgxpool.Pool) []transferRow {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
		SELECT fa.name, ta.name, t.amount::TEXT, t.metadata::TEXT, coalesce(k.idempotency_key, '')
		FROM pgledger_transfers_view t
		INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
		INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
		LEFT JOIN pgledger_idempotency_keys_view k ON t.id = k.transfer_id
		ORDER BY t.event_at, fa.name, ta.name, t.amount, t.metadata::TEXT`)
	require.NoError(t, err)
	defer rows.Close()
	var out []transferRow
	for rows.Next() {
		var r transferRow
		require.NoError(t, rows.Scan(&r.From, &r.To, &r.Amount, &r.Metadata, &r.IdempotencyKey))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestRun(t *testing.T) {
	t.Parallel()
	_, admin := testdb.New(t)

	sum, err := run(t, admin, opts)
	require.NoError(t, err)
	assert.Equal(t, 3+8*4+2, sum.Accounts)
	assert.Len(t, transfers(t, admin), sum.Transfers)

	rows, err := admin.Query(t.Context(), `
		SELECT a.currency, sum(e.amount)::TEXT
		FROM pgledger_entries_view e
		INNER JOIN pgledger_accounts_view a ON e.account_id = a.id
		GROUP BY 1 ORDER BY 1`)
	require.NoError(t, err)
	sums := map[string]string{}
	for rows.Next() {
		var currency, total string
		require.NoError(t, rows.Scan(&currency, &total))
		sums[currency] = total
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, map[string]string{"EUR": "0.00", "USD": "0.00"}, sums)

	var payments int
	require.NoError(t, admin.QueryRow(t.Context(),
		`SELECT count(DISTINCT metadata ->> 'payment_id') FROM pgledger_transfers_view`).Scan(&payments))
	assert.GreaterOrEqual(t, payments, 8)

	rows, err = admin.Query(t.Context(), `
		SELECT DISTINCT
			t.metadata ->> 'from_currency' || '->' || (t.metadata ->> 'to_currency'),
			regexp_replace(fa.name, '^user\d+\.', '') || ' -> ' || regexp_replace(ta.name, '^user\d+\.', ''),
			fa.currency
		FROM pgledger_transfers_view t
		INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
		INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
		WHERE t.metadata ->> 'kind' = 'fx'
		ORDER BY 1, 2`)
	require.NoError(t, err)
	var legs [][3]string
	for rows.Next() {
		var leg [3]string
		require.NoError(t, rows.Scan(&leg[0], &leg[1], &leg[2]))
		legs = append(legs, leg)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][3]string{
		{"EUR->USD", "eur.available -> liquidity.eur", "EUR"},
		{"EUR->USD", "liquidity.usd -> available", "USD"},
		{"USD->EUR", "available -> liquidity.usd", "USD"},
		{"USD->EUR", "liquidity.eur -> eur.available", "EUR"},
	}, legs)

	type keyed struct{ Transfers, Keyed, Keys int }
	rows, err = admin.Query(t.Context(), `
		SELECT
			t.metadata ->> 'kind',
			count(*),
			count(k.idempotency_key),
			count(DISTINCT k.idempotency_key)
		FROM pgledger_transfers_view t
		LEFT JOIN pgledger_idempotency_keys_view k ON t.id = k.transfer_id
		GROUP BY 1`)
	require.NoError(t, err)
	kinds := map[string]keyed{}
	for rows.Next() {
		var kind string
		var k keyed
		require.NoError(t, rows.Scan(&kind, &k.Transfers, &k.Keyed, &k.Keys))
		kinds[kind] = k
	}
	require.NoError(t, rows.Err())
	for _, kind := range []string{"payment_created", "refund_created"} {
		k := kinds[kind]
		assert.Positive(t, k.Keyed, kind)
		assert.Equal(t, k.Keyed, k.Keys, kind)
	}
	assert.Positive(t, kinds["fx"].Keys)
	assert.Equal(t, 2*kinds["fx"].Keys, kinds["fx"].Keyed, "each keyed fx's two legs share its key")
	for _, kind := range []string{"payment_received", "refund_sent", "fee"} {
		assert.Positive(t, kinds[kind].Transfers, kind)
		assert.Zero(t, kinds[kind].Keyed, kind)
	}

	var malformed int
	require.NoError(t, admin.QueryRow(t.Context(), `
		SELECT count(*) FROM pgledger_idempotency_keys_view
		WHERE idempotency_key !~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'`,
	).Scan(&malformed))
	assert.Zero(t, malformed, "keys are version 4 UUIDs")

	_, err = run(t, admin, opts)
	assert.ErrorContains(t, err, "sample data already present")
}

func TestRunBackdates(t *testing.T) {
	t.Parallel()
	_, admin := testdb.New(t)
	// Enough users that some steps for one payment fall on the same hour offset.
	_, err := run(t, admin, seed.Options{Users: 20, Seed: opts.Seed, Now: opts.Now})
	require.NoError(t, err)
	ctx := t.Context()

	var first, last time.Time
	require.NoError(t, admin.QueryRow(ctx,
		`SELECT min(created_at), max(created_at) FROM pgledger_transfers_view`).Scan(&first, &last))
	assert.False(t, first.Before(opts.Now.Add(-30*24*time.Hour)), first)
	assert.False(t, last.After(opts.Now), last)
	assert.Greater(t, last.Sub(first), 14*24*time.Hour)

	var accountsCreated []time.Time
	rows, err := admin.Query(ctx, `SELECT DISTINCT created_at FROM pgledger_accounts_view`)
	require.NoError(t, err)
	for rows.Next() {
		var at time.Time
		require.NoError(t, rows.Scan(&at))
		accountsCreated = append(accountsCreated, at)
	}
	require.NoError(t, rows.Err())
	require.Len(t, accountsCreated, 1)
	assert.True(t, accountsCreated[0].Before(first))

	var entries, mismatched, backwards, staleUpdatedAt int
	require.NoError(t, admin.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE e.created_at <> t.created_at),
			count(*) FILTER (WHERE e.created_at < e.previous_created_at)
		FROM (
			SELECT *, lag(created_at) OVER (PARTITION BY account_id ORDER BY account_version) AS previous_created_at
			FROM pgledger_entries_view
		) e
		INNER JOIN pgledger_transfers_view t ON e.transfer_id = t.id`).Scan(&entries, &mismatched, &backwards))
	assert.Positive(t, entries)
	assert.Zero(t, mismatched, "entries created_at differs from their transfer")
	assert.Zero(t, backwards, "entry created_at goes backwards as account_version increases")

	require.NoError(t, admin.QueryRow(ctx, `
		SELECT count(*) FROM pgledger_accounts_view a
		WHERE a.updated_at <> coalesce(
			(SELECT max(e.created_at) FROM pgledger_entries_view e WHERE e.account_id = a.id), a.created_at)`,
	).Scan(&staleUpdatedAt))
	assert.Zero(t, staleUpdatedAt)

	var fxs, shared, fxPairs int
	require.NoError(t, admin.QueryRow(ctx, `
		SELECT
			(SELECT count(DISTINCT metadata ->> 'fx_id') FROM pgledger_transfers_view),
			count(*) FILTER (WHERE n > 1),
			count(*) FILTER (WHERE n = 2 AND fx_ids = 1 AND fx_legs = 2)
		FROM (
			SELECT count(*) AS n, count(DISTINCT metadata ->> 'fx_id') AS fx_ids, count(metadata ->> 'fx_id') AS fx_legs
			FROM pgledger_transfers_view
			GROUP BY created_at
		) g`).Scan(&fxs, &shared, &fxPairs))
	assert.Positive(t, fxs)
	assert.Equal(t, fxs, fxPairs, "each fx's two legs share a created_at")
	assert.Equal(t, fxs, shared, "only fx legs share a created_at")
}

func TestRunIsDeterministic(t *testing.T) {
	t.Parallel()
	_, first := testdb.New(t)
	_, second := testdb.New(t)

	_, err := run(t, first, opts)
	require.NoError(t, err)
	_, err = run(t, second, opts)
	require.NoError(t, err)

	assert.Equal(t, transfers(t, first), transfers(t, second))
}
