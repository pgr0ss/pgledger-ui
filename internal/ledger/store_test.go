package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger-ui/internal/db"
	"github.com/pgr0ss/pgledger-ui/internal/ledger"
	"github.com/pgr0ss/pgledger-ui/internal/testdb"
)

func reconStore(t *testing.T) (testdb.ReconData, *ledger.Store) {
	t.Helper()
	t.Parallel()
	r := testdb.Recon(t)
	pool, err := ledger.Connect(t.Context(), r.URL, nil)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	s, err := ledger.NewStore(t.Context(), pool)
	require.NoError(t, err)
	return r, s
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

func TestGridGroupsTransfersFromOneTransaction(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	create := func(q *db.Queries, from, to string) {
		_, err := q.CreateTransfer(ctx, db.CreateTransferParams{
			FromAccountID: r.Accounts[from], ToAccountID: r.Accounts[to], Amount: "3.00", EventAt: time.Now(),
		})
		require.NoError(t, err)
	}
	require.NoError(t, pgx.BeginFunc(ctx, r.Admin, func(tx pgx.Tx) error {
		create(db.New(tx), "a_b", "axb")
		// A savepoint is a subtransaction with its own xid, but the same transaction start time.
		return pgx.BeginFunc(ctx, tx, func(sub pgx.Tx) error {
			create(db.New(sub), "axb", "a_b")
			return nil
		})
	}))

	entries, _, err := s.GridEntries(ctx, ledger.GridFilter{AccountID: r.Accounts["a_b"]}, ledger.OrderCreated, ledger.MaxGridTransfers)
	require.NoError(t, err)
	g, err := ledger.BuildGrid(entries, false)
	require.NoError(t, err)
	var txs []int
	for _, row := range g.Rows {
		txs = append(txs, row.Tx)
	}
	assert.Equal(t, []int{0, 0, 1, 1}, txs)
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

func TestTransferIdempotencyKey(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	const key = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	ids, err := db.New(r.Admin).CreateFxTransfers(ctx, db.CreateFxTransfersParams{
		From1: r.Accounts["a_b"], To1: r.Accounts["axb"], Amount1: "1.00",
		From2: r.Accounts["axb"], To2: r.Accounts["a_b"], Amount2: "2.00",
		EventAt: time.Now(), IdempotencyKey: pgtype.Text{String: key, Valid: true},
	})
	require.NoError(t, err)

	transfer, err := s.GetTransfer(ctx, ids[0])
	require.NoError(t, err)
	assert.Equal(t, key, transfer.IdempotencyKey)

	transfers, _, err := s.ListTransfers(ctx, ledger.TransferFilter{Q: key[:8], Limit: ledger.PageSize})
	require.NoError(t, err)
	var found []string
	for _, tr := range transfers {
		assert.Equal(t, key, tr.IdempotencyKey)
		found = append(found, tr.ID)
	}
	assert.ElementsMatch(t, ids, found)

	transfer, err = s.GetTransfer(ctx, r.Transfer(t, "p_123", "payment_created").ID)
	require.NoError(t, err)
	assert.Empty(t, transfer.IdempotencyKey)
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

	h, err := s.BalanceHistory(ctx, receivables.ID, ledger.TimeRange{}, ledger.ChartPoints)
	require.NoError(t, err)
	assert.Equal(t, []string{"50.00", "100.00", "50.00", "0.50"}, balances(h.Points))
	assert.Equal(t, "0.00", h.Start)
	assert.True(t, h.Complete)
	series := h.Series(receivables.CreatedAt, time.Time{})
	assert.Equal(t, []string{"0.00", "50.00", "100.00", "50.00", "0.50"}, balances(series))
	assert.Equal(t, receivables.CreatedAt, series[0].At)

	h, err = s.BalanceHistory(ctx, receivables.ID, ledger.TimeRange{}, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"50.00", "0.50"}, balances(h.Points))
	assert.Equal(t, "100.00", h.Start)
	assert.False(t, h.Complete)
	series = h.Series(receivables.CreatedAt, time.Time{})
	assert.Equal(t, []string{"100.00", "50.00", "0.50"}, balances(series))
	assert.Equal(t, h.Points[0].At, series[0].At)

	created := transfersCreated(t, s, r, 1, 2)
	h, err = s.BalanceHistory(ctx, receivables.ID, created, ledger.ChartPoints)
	require.NoError(t, err)
	assert.Equal(t, []string{"100.00", "50.00"}, balances(h.Points))
	assert.Equal(t, []string{r.Transfers[1].ID, r.Transfers[2].ID}, []string{h.Points[0].TransferID, h.Points[1].TransferID})
	assert.Equal(t, "50.00", h.Start)
	assert.True(t, h.Complete)
	end := created.To.Add(time.Minute)
	series = h.Series(created.From, end)
	assert.Equal(t, []string{"50.00", "100.00", "50.00", "50.00"}, balances(series))
	assert.Equal(t, created.From, series[0].At)
	assert.Equal(t, end, series[3].At)
}

// transfersCreated spans the created_at of Recon transfers i through j.
func transfersCreated(t *testing.T, s *ledger.Store, r testdb.ReconData, i, j int) ledger.TimeRange {
	t.Helper()
	from, err := s.GetTransfer(t.Context(), r.Transfers[i].ID)
	require.NoError(t, err)
	to, err := s.GetTransfer(t.Context(), r.Transfers[j].ID)
	require.NoError(t, err)
	return ledger.TimeRange{From: from.CreatedAt, To: to.CreatedAt}
}

func TestCreatedRangeFilters(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	receivables := r.Accounts["user1.receivables"]
	created := transfersCreated(t, s, r, 1, 2)

	entries, _, err := s.ListEntries(ctx, ledger.EntryFilter{AccountID: receivables, Limit: ledger.PageSize})
	require.NoError(t, err)
	assert.Len(t, entries, 4)
	entries, _, err = s.ListEntries(ctx, ledger.EntryFilter{AccountID: receivables, Created: created, Limit: ledger.PageSize})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, []string{r.Transfers[2].ID, r.Transfers[1].ID}, []string{entries[0].TransferID, entries[1].TransferID})

	grid, _, err := s.GridEntries(ctx, ledger.GridFilter{AccountID: receivables, Created: created}, ledger.OrderCreated, ledger.MaxGridTransfers)
	require.NoError(t, err)
	assert.Equal(t, []string{"payment_created", "payment_received"}, gridKinds(grid))

	f, err := s.AccountFlows(ctx, receivables, created, ledger.TopCounterparties)
	require.NoError(t, err)
	assert.Equal(t, "50.00", f.In)
	assert.Equal(t, "50.00", f.Out)
	assert.Equal(t, []string{"user1.available"}, counterpartyNames(f.TopOut))

	f, err = s.AccountFlows(ctx, receivables, ledger.TimeRange{From: created.To.Add(time.Microsecond)}, ledger.TopCounterparties)
	require.NoError(t, err)
	assert.Equal(t, ledger.Flows{
		In: "0", Out: "49.50",
		TopOut: []ledger.Counterparty{{AccountID: r.Accounts["user1.available"], Name: "user1.available", Amount: "49.50"}},
	}, f)
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

	f, err := s.AccountFlows(ctx, ab, ledger.TimeRange{}, ledger.TopCounterparties)
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

	f, err = s.AccountFlows(ctx, ab, ledger.TimeRange{}, 1)
	require.NoError(t, err)
	assert.Equal(t, "12.00", f.In)
	assert.Equal(t, "7.00", f.Out)
	assert.Equal(t, []string{"axb"}, counterpartyNames(f.TopIn))
	assert.Equal(t, []string{"user1.available"}, counterpartyNames(f.TopOut))

	a, err := q.CreateAccount(ctx, db.CreateAccountParams{Name: "idle", Currency: "USD"})
	require.NoError(t, err)
	f, err = s.AccountFlows(ctx, a, ledger.TimeRange{}, ledger.TopCounterparties)
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

func TestViewReadsOneSnapshot(t *testing.T) {
	r, s := reconStore(t)
	ctx := t.Context()
	id := r.Accounts["a_b"]
	entriesOf := func(v *ledger.Store) ([]ledger.Entry, error) {
		entries, _, err := v.ListEntries(ctx, ledger.EntryFilter{AccountID: id, Limit: ledger.PageSize})
		return entries, err
	}

	var acct ledger.Account
	var entries []ledger.Entry
	var transferID string
	require.NoError(t, s.View(ctx, func(v *ledger.Store) error {
		var err error
		if acct, err = v.GetAccount(ctx, id); err != nil {
			return err
		}
		transferID, err = db.New(r.Admin).CreateTransfer(ctx, db.CreateTransferParams{
			FromAccountID: id, ToAccountID: r.Accounts["axb"], Amount: "5.00", EventAt: time.Now(),
		})
		require.NoError(t, err)
		entries, err = entriesOf(v)
		return err
	}))
	assert.Equal(t, "-3.00", acct.Balance)
	require.Len(t, entries, 2)
	assert.Equal(t, acct.Balance, entries[0].CurrentBalance)

	require.NoError(t, s.View(ctx, func(v *ledger.Store) error {
		var err error
		entries, err = entriesOf(v)
		return err
	}))
	require.Len(t, entries, 3)
	assert.Equal(t, transferID, entries[0].TransferID)
	assert.Equal(t, "-8.00", entries[0].CurrentBalance)
}

func TestConnectIsReadOnly(t *testing.T) {
	r, _ := reconStore(t)
	pool, err := ledger.Connect(context.Background(), r.URL, nil)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(t.Context(), "select pgledger_create_account('x', 'USD')")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "25006", pgErr.Code)
}
