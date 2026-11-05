package seed_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/seed"
	"github.com/pgr0ss/pgledger_visualizer/internal/testdb"
)

var opts = seed.Options{Users: 8, Seed: 1, Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func run(t *testing.T, pool *pgxpool.Pool) (seed.Summary, error) {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	return seed.Run(t.Context(), conn.Conn(), opts)
}

type transferRow struct {
	From, To, Amount, Metadata string
}

func transfers(t *testing.T, pool *pgxpool.Pool) []transferRow {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
		SELECT fa.name, ta.name, t.amount::TEXT, t.metadata::TEXT
		FROM pgledger_transfers_view t
		INNER JOIN pgledger_accounts_view fa ON t.from_account_id = fa.id
		INNER JOIN pgledger_accounts_view ta ON t.to_account_id = ta.id
		ORDER BY t.event_at, fa.name, ta.name, t.amount, t.metadata::TEXT`)
	require.NoError(t, err)
	defer rows.Close()
	var out []transferRow
	for rows.Next() {
		var r transferRow
		require.NoError(t, rows.Scan(&r.From, &r.To, &r.Amount, &r.Metadata))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestRun(t *testing.T) {
	t.Parallel()
	_, admin := testdb.New(t)

	sum, err := run(t, admin)
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

	_, err = run(t, admin)
	assert.ErrorContains(t, err, "sample data already present")
}

func TestRunIsDeterministic(t *testing.T) {
	t.Parallel()
	_, first := testdb.New(t)
	_, second := testdb.New(t)

	_, err := run(t, first)
	require.NoError(t, err)
	_, err = run(t, second)
	require.NoError(t, err)

	assert.Equal(t, transfers(t, first), transfers(t, second))
}
