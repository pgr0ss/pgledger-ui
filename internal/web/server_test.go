package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
	"github.com/pgr0ss/pgledger_visualizer/internal/testdb"
	"github.com/pgr0ss/pgledger_visualizer/internal/web"
)

type fixture struct {
	testdb.ReconData
	store *ledger.Store
	srv   *httptest.Server
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	t.Parallel()
	r := testdb.Recon(t)
	pool, err := ledger.Connect(t.Context(), r.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := ledger.NewStore(pool)
	h, err := web.New(store)
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return fixture{ReconData: r, store: store, srv: srv}
}

func (f fixture) get(t *testing.T, path string) (int, string) {
	t.Helper()
	res, err := http.Get(f.srv.URL + path)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res.StatusCode, string(body)
}

func TestTransferPage(t *testing.T) {
	f := newFixture(t)
	status, body := f.get(t, "/transfers/"+f.Transfer(t, "p_123", "payment_created").ID)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "/accounts/"+f.Accounts["user1.external"])
	assert.Contains(t, body, "/trace?key=payment_id&amp;value=p_123")
}

func TestTraceGrid(t *testing.T) {
	f := newFixture(t)
	status, body := f.get(t, "/trace?key=payment_id&value=p_123")
	assert.Equal(t, http.StatusOK, status)

	last := -1
	for _, name := range []string{"user1.external", "user1.receivables", "user1.available", "user1.pending_outbound"} {
		i := strings.Index(body, ">"+name+"</a>")
		require.Greater(t, i, last, name)
		last = i
	}
	assert.Contains(t, body, "-30.00")
	assert.Contains(t, body, `data-col="3"`)
	assert.NotContains(t, body, "+ in")

	_, body = f.get(t, "/trace?key=payment_id&value=p_123&view=inout")
	assert.Contains(t, body, "+ in")
}

func TestSearchPartial(t *testing.T) {
	f := newFixture(t)
	_, body := f.get(t, "/search?q=p_12")
	assert.Contains(t, body, "<html")

	_, body = f.get(t, "/search?q=p_12&partial=1")
	assert.Contains(t, body, "value=p_123")
	assert.NotContains(t, body, "<html")
}

func TestTraceKeySuggestions(t *testing.T) {
	f := newFixture(t)
	_, body := f.get(t, "/trace?key=pay&partial=1")
	assert.Contains(t, body, "/trace?key=payment_id")
}

func TestAccountBalanceAt(t *testing.T) {
	f := newFixture(t)
	received, err := f.store.GetTransfer(t.Context(), f.Transfer(t, "p_123", "payment_received").ID)
	require.NoError(t, err)
	at := received.CreatedAt.UTC().Format(time.RFC3339Nano)

	status, body := f.get(t, "/accounts/"+f.Accounts["user1.receivables"]+"?order=created&at="+url.QueryEscape(at))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "(4 entries)")
	assert.Contains(t, body, "0.50</span> USD</strong>")
}

func TestAccountPage(t *testing.T) {
	f := newFixture(t)
	status, body := f.get(t, "/accounts/"+f.Accounts["user1.receivables"])
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, `<svg class="balance-chart"`)
	assert.Contains(t, body, `<time datetime="`)
}

func TestAccountNotFound(t *testing.T) {
	f := newFixture(t)
	status, _ := f.get(t, "/accounts/"+f.Accounts["user1.receivables"])
	assert.Equal(t, http.StatusOK, status)

	status, body := f.get(t, "/accounts/pgla_missing")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, body, "Not found: pgla_missing")
}
