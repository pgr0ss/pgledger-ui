package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgr0ss/pgledger_visualizer/internal/db"
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
	pool, err := ledger.Connect(t.Context(), r.URL, nil)
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

func TestStaticAssetsCachedByContentHash(t *testing.T) {
	f := newFixture(t)
	_, body := f.get(t, "/")
	m := regexp.MustCompile(`href="(/static/app\.css\?v=\w+)"`).FindStringSubmatch(body)
	require.NotNil(t, m, body)

	cacheControl := func(path string) string {
		res, err := http.Get(f.srv.URL + path)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode, path)
		return res.Header.Get("Cache-Control")
	}
	assert.Equal(t, "public, max-age=31536000, immutable", cacheControl(m[1]))
	assert.Empty(t, cacheControl("/static/app.css?v=stale"))
}

func TestStaticAssetsRevalidateByETag(t *testing.T) {
	f := newFixture(t)
	res, err := http.Get(f.srv.URL + "/static/app.js")
	require.NoError(t, err)
	_ = res.Body.Close()
	etag := res.Header.Get("ETag")
	require.NotEmpty(t, etag)

	revalidate := func(etag string) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+"/static/app.js", nil)
		require.NoError(t, err)
		req.Header.Set("If-None-Match", etag)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		return res.StatusCode
	}
	assert.Equal(t, http.StatusNotModified, revalidate(etag))
	assert.Equal(t, http.StatusOK, revalidate(`"stale"`))
}

func TestStaticServesOnlyFiles(t *testing.T) {
	f := newFixture(t)
	status, _ := f.get(t, "/static/app.css")
	require.Equal(t, http.StatusOK, status)

	status, body := f.get(t, "/static/")
	assert.Equal(t, http.StatusNotFound, status, body)
}

func TestTransferPage(t *testing.T) {
	f := newFixture(t)
	status, body := f.get(t, "/transfers/"+f.Transfer(t, "p_123", "payment_created").ID)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "/accounts/"+f.Accounts["user1.external"])
	assert.Contains(t, body, "/trace?key=payment_id&amp;value=p_123")
}

func TestTransferPageIdempotencyKey(t *testing.T) {
	f := newFixture(t)
	const key = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	id, err := db.New(f.Admin).CreateTransfer(t.Context(), db.CreateTransferParams{
		FromAccountID: f.Accounts["a_b"], ToAccountID: f.Accounts["axb"], Amount: "1.00",
		EventAt: time.Now(), IdempotencyKey: pgtype.Text{String: key, Valid: true},
	})
	require.NoError(t, err)

	_, body := f.get(t, "/transfers/"+id)
	assert.Contains(t, body, `<a href="/transfers?q=`+key+`"`)

	_, body = f.get(t, "/transfers?partial=1&q="+key)
	assert.Contains(t, body, `href="/transfers/`+id+`"`)
	assert.Contains(t, body, "<code>7c9e6679…</code>")
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

func TestSearchByEntryID(t *testing.T) {
	f := newFixture(t)
	entries, err := f.store.TransferEntries(t.Context(), f.Transfer(t, "p_123", "refund_sent").ID)
	require.NoError(t, err)
	id := entries[0].ID

	_, body := f.get(t, "/search?partial=1&q="+id[:len(id)-4])
	assert.Contains(t, body, `href="/entries/`+id+`"`)
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

	status, body := f.get(t, "/accounts/"+f.Accounts["user1.receivables"]+"?order=created_at&at="+url.QueryEscape(at))
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
	assert.Contains(t, body, `Funds in <span class="amount">100.00</span> USD`)
	assert.Contains(t, body, `Funds out <span class="amount">99.50</span> USD`)
}

func TestAccountZoom(t *testing.T) {
	f := newFixture(t)
	from, err := f.store.GetTransfer(t.Context(), f.Transfers[1].ID)
	require.NoError(t, err)
	to, err := f.store.GetTransfer(t.Context(), f.Transfers[2].ID)
	require.NoError(t, err)
	path := "/accounts/" + f.Accounts["user1.receivables"]
	q := url.Values{
		"from": {from.CreatedAt.UTC().Format(time.RFC3339Nano)},
		"to":   {to.CreatedAt.UTC().Format(time.RFC3339Nano)},
	}

	status, body := f.get(t, path+"?"+q.Encode())
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, `Funds in <span class="amount">50.00</span> USD`)
	assert.Contains(t, body, `Funds out <span class="amount">50.00</span> USD`)
	assert.Contains(t, body, `<a href="`+path+`">Reset zoom</a>`)
	assert.Contains(t, body, `<input type="hidden" name="from" value="`+q.Get("from")+`">`)

	_, body = f.get(t, path+"?from=garbage")
	assert.Contains(t, body, `invalid range "garbage"`)
	assert.Contains(t, body, `Funds in <span class="amount">100.00</span> USD`)
}

func TestAccountNotFound(t *testing.T) {
	f := newFixture(t)
	status, _ := f.get(t, "/accounts/"+f.Accounts["user1.receivables"])
	assert.Equal(t, http.StatusOK, status)

	status, body := f.get(t, "/accounts/pgla_missing")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, body, "Not found: pgla_missing")
}
