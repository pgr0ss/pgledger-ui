package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/db"
	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
	"github.com/pgr0ss/pgledger_visualizer/internal/testdb"
)

func reconStore(t *testing.T) (testdb.ReconData, *ledger.Store) {
	t.Helper()
	t.Parallel()
	r := testdb.Recon(t)
	pool, err := ledger.Connect(t.Context(), r.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return r, ledger.NewStore(pool)
}

func accountNames(accounts []ledger.Account) []string {
	var names []string
	for _, a := range accounts {
		names = append(names, a.Name)
	}
	return names
}

func TestListAccountsSearch(t *testing.T) {
	_, s := reconStore(t)
	ctx := t.Context()

	accounts, more, err := s.ListAccounts(ctx, ledger.AccountFilter{Q: "receiv", Limit: ledger.PageSize})
	require.NoError(t, err)
	assert.False(t, more)
	assert.Equal(t, []string{"user1.receivables"}, accountNames(accounts))

	accounts, _, err = s.ListAccounts(ctx, ledger.AccountFilter{Q: "a_b", Limit: ledger.PageSize})
	require.NoError(t, err)
	assert.Equal(t, []string{"a_b"}, accountNames(accounts))
	assert.NotContains(t, accountNames(accounts), "axb")
}

func TestTraceKeysIgnoresNonObjectMetadata(t *testing.T) {
	_, s := reconStore(t)
	keys, err := s.TraceKeys(t.Context(), "pay", 20)
	require.NoError(t, err)
	assert.Equal(t, []ledger.KeyCount{{Key: "payment_id", Transfers: 6}}, keys)
}

func TestTraceValues(t *testing.T) {
	_, s := reconStore(t)
	values, err := s.TraceValues(t.Context(), "payment_id", "", 100)
	require.NoError(t, err)
	counts := map[string]int64{}
	for _, v := range values {
		counts[v.Value] = v.Transfers
	}
	assert.Equal(t, map[string]int64{"p_123": 4, "p_456": 2}, counts)
}

func gridKinds(entries []ledger.GridEntry) []string {
	var kinds []string
	seen := map[string]bool{}
	for _, e := range entries {
		if !seen[e.TransferID] {
			seen[e.TransferID] = true
			kinds = append(kinds, e.Metadata.Fields[0].Value)
		}
	}
	return kinds
}

func TestGridEntriesByMetadata(t *testing.T) {
	_, s := reconStore(t)
	ctx := t.Context()
	f := ledger.GridFilter{MetaKey: "payment_id", MetaValue: "p_123"}

	entries, truncated, err := s.GridEntries(ctx, f, ledger.OrderCreated, ledger.MaxGridTransfers)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Len(t, entries, 8)
	assert.Equal(t, []string{"payment_created", "payment_received", "refund_created", "refund_sent"}, gridKinds(entries))

	entries, _, err = s.GridEntries(ctx, f, ledger.OrderEvent, ledger.MaxGridTransfers)
	require.NoError(t, err)
	assert.Equal(t, "refund_sent", gridKinds(entries)[0])

	entries, truncated, err = s.GridEntries(ctx, f, ledger.OrderCreated, 3)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Equal(t, []string{"payment_received", "refund_created", "refund_sent"}, gridKinds(entries))
}

func TestGridEntriesByAccount(t *testing.T) {
	r, s := reconStore(t)
	entries, truncated, err := s.GridEntries(t.Context(), ledger.GridFilter{AccountID: r.Accounts["user1.receivables"]}, ledger.OrderCreated, ledger.MaxGridTransfers)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Len(t, gridKinds(entries), 4)
}

func TestListTransfersKeyset(t *testing.T) {
	_, s := reconStore(t)
	ctx := t.Context()

	page1, more, err := s.ListTransfers(ctx, ledger.TransferFilter{Limit: 2})
	require.NoError(t, err)
	assert.True(t, more)
	require.Len(t, page1, 2)

	page2, _, err := s.ListTransfers(ctx, ledger.TransferFilter{Before: page1[1].ID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.Less(t, page2[0].ID, page1[1].ID)
}

func TestBalanceAt(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	receivables := r.Accounts["user1.receivables"]

	received, err := s.GetTransfer(ctx, r.Transfer(t, "p_123", "payment_received").ID)
	require.NoError(t, err)
	b, err := s.BalanceAt(ctx, receivables, received.CreatedAt, ledger.OrderCreated)
	require.NoError(t, err)
	assert.Equal(t, "0.50", b.Amount)
	assert.Equal(t, int64(4), b.Entries)
	assert.NotEmpty(t, b.EntryID)

	first, err := s.GetTransfer(ctx, r.Transfers[0].ID)
	require.NoError(t, err)
	b, err = s.BalanceAt(ctx, receivables, first.CreatedAt.Add(-time.Microsecond), ledger.OrderCreated)
	require.NoError(t, err)
	assert.Equal(t, ledger.Balance{Amount: "0"}, b)

	at := time.Date(2025, 7, 22, 0, 0, 0, 0, time.UTC)
	b, err = s.BalanceAt(ctx, r.Accounts["user1.external"], at, ledger.OrderEvent)
	require.NoError(t, err)
	assert.Equal(t, ledger.Balance{Amount: "20.00", Entries: 1}, b)

	b, err = s.BalanceAt(ctx, r.Accounts["user1.available"], at, ledger.OrderEvent)
	require.NoError(t, err)
	assert.Equal(t, ledger.Balance{Amount: "0"}, b)
}

func balances(points []ledger.BalancePoint) []string {
	var out []string
	for _, p := range points {
		out = append(out, p.Balance)
	}
	return out
}

func TestBalanceHistory(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	receivables, err := s.GetAccount(ctx, r.Accounts["user1.receivables"])
	require.NoError(t, err)

	h, err := s.BalanceHistory(ctx, receivables.ID, ledger.ChartPoints)
	require.NoError(t, err)
	assert.Equal(t, []string{"50.00", "100.00", "50.00", "0.50"}, balances(h.Points))
	assert.Equal(t, "0.00", h.Start)
	assert.True(t, h.Complete)
	series := h.Series(receivables.CreatedAt)
	assert.Equal(t, []string{"0.00", "50.00", "100.00", "50.00", "0.50"}, balances(series))
	assert.Equal(t, receivables.CreatedAt, series[0].At)

	h, err = s.BalanceHistory(ctx, receivables.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"50.00", "0.50"}, balances(h.Points))
	assert.Equal(t, "100.00", h.Start)
	assert.False(t, h.Complete)
	series = h.Series(receivables.CreatedAt)
	assert.Equal(t, []string{"100.00", "50.00", "0.50"}, balances(series))
	assert.Equal(t, h.Points[0].At, series[0].At)
}

func TestAccountFlows(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	q := db.New(r.Admin)
	for _, tr := range []struct{ from, to, amount string }{
		{"user1.external", "a_b", "5.00"},
		{"axb", "a_b", "7.00"},
		{"a_b", "user1.available", "4.00"},
	} {
		_, err := q.CreateTransfer(ctx, db.CreateTransferParams{
			FromAccountID: r.Accounts[tr.from], ToAccountID: r.Accounts[tr.to], Amount: tr.amount,
			EventAt: time.Now(), Metadata: []byte(`{}`),
		})
		require.NoError(t, err)
	}
	ab := r.Accounts["a_b"]

	f, err := s.AccountFlows(ctx, ab, ledger.TopCounterparties)
	require.NoError(t, err)
	assert.Equal(t, ledger.Flows{
		In:  "12.00",
		Out: "7.00",
		TopIn: []ledger.Counterparty{
			{AccountID: r.Accounts["axb"], Name: "axb", Amount: "7.00"},
			{AccountID: r.Accounts["user1.external"], Name: "user1.external", Amount: "5.00"},
		},
		TopOut: []ledger.Counterparty{
			{AccountID: r.Accounts["user1.available"], Name: "user1.available", Amount: "4.00"},
			{AccountID: r.Accounts["axb"], Name: "axb", Amount: "3.00"},
		},
	}, f)

	f, err = s.AccountFlows(ctx, ab, 1)
	require.NoError(t, err)
	assert.Equal(t, "12.00", f.In)
	assert.Equal(t, "7.00", f.Out)
	assert.Equal(t, []string{"axb"}, counterpartyNames(f.TopIn))
	assert.Equal(t, []string{"user1.available"}, counterpartyNames(f.TopOut))

	a, err := q.CreateAccount(ctx, db.CreateAccountParams{Name: "idle", Currency: "USD"})
	require.NoError(t, err)
	f, err = s.AccountFlows(ctx, a, ledger.TopCounterparties)
	require.NoError(t, err)
	assert.Equal(t, ledger.Flows{In: "0", Out: "0"}, f)
}

func counterpartyNames(cs []ledger.Counterparty) []string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func TestGetNotFound(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()

	_, err := s.GetAccount(ctx, r.Accounts["axb"])
	require.NoError(t, err)

	_, err = s.GetAccount(ctx, "pgla_missing")
	assert.ErrorIs(t, err, ledger.ErrNotFound)
}

func TestConnectIsReadOnly(t *testing.T) {
	r, _ := reconStore(t)
	pool, err := ledger.Connect(context.Background(), r.URL)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(t.Context(), "select pgledger_create_account('x', 'USD')")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "25006", pgErr.Code)
}
