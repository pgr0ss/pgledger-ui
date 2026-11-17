package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pgr0ss/pgledger-visualizer/internal/ledger"
)

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", base{Title: "pgledger visualizer", Nav: "search", URL: r.URL})
}

type searchPage struct {
	base
	Q         string
	Accounts  []ledger.Account
	Transfers []ledger.Transfer
	Entries   []ledger.Entry
	Pairs     []ledger.MetaPair
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := searchPage{Title: "Search", Nav: "search", URL: r.URL, Q: r.FormValue("q")}
	if p.Q != "" {
		err := s.store.View(ctx, func(st *ledger.Store) error {
			var err error
			if p.Accounts, _, err = st.ListAccounts(ctx, ledger.AccountFilter{Q: p.Q, Limit: ledger.SearchLimit}); err != nil {
				return err
			}
			if p.Transfers, _, err = st.ListTransfers(ctx, ledger.TransferFilter{Q: p.Q, Limit: ledger.SearchLimit}); err != nil {
				return err
			}
			if p.Entries, _, err = st.ListEntries(ctx, ledger.EntryFilter{Q: p.Q, Limit: ledger.SearchLimit}); err != nil {
				return err
			}
			p.Pairs, err = st.MetadataPairs(ctx, p.Q, ledger.SearchLimit)
			return err
		})
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		// A submitted search that names one object exactly opens it. Live (partial)
		// searches never redirect, so typing an id still shows results as you go.
		if r.URL.Query().Get("partial") != "1" {
			if to := jumpTarget(p); to != "" {
				http.Redirect(w, r, to, http.StatusFound)
				return
			}
		}
	}
	s.render(w, r, http.StatusOK, "search", p)
}

// jumpTarget is the page for the object whose full id, or account name, is exactly
// the query, if the results hold one.
func jumpTarget(p searchPage) string {
	q := strings.TrimSpace(p.Q)
	for _, a := range p.Accounts {
		if a.ID == q {
			return "/accounts/" + url.PathEscape(a.ID)
		}
	}
	for _, t := range p.Transfers {
		if t.ID == q {
			return "/transfers/" + url.PathEscape(t.ID)
		}
	}
	for _, e := range p.Entries {
		if e.ID == q {
			return "/entries/" + url.PathEscape(e.ID)
		}
	}
	var named []ledger.Account
	for _, a := range p.Accounts {
		if a.Name == q {
			named = append(named, a)
		}
	}
	if len(named) == 1 {
		return "/accounts/" + url.PathEscape(named[0].ID)
	}
	return ""
}

type accountsPage struct {
	base
	Q, Key, Value      string
	Accounts           []ledger.Account
	Offset, Prev, Next int
	HasPrev, HasNext   bool
}

func (s *server) accounts(w http.ResponseWriter, r *http.Request) {
	p := accountsPage{
		Title: "Accounts", Nav: "accounts", URL: r.URL,
		Q: r.FormValue("q"), Key: r.FormValue("key"), Value: r.FormValue("value"),
	}
	p.Offset, _ = strconv.Atoi(r.FormValue("offset"))
	p.Offset = max(p.Offset, 0)
	var err error
	p.Accounts, p.HasNext, err = s.store.ListAccounts(r.Context(), ledger.AccountFilter{
		Q: p.Q, MetaKey: p.Key, MetaValue: p.Value, Offset: p.Offset, Limit: ledger.PageSize,
	})
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	p.HasPrev = p.Offset > 0
	p.Prev = max(p.Offset-ledger.PageSize, 0)
	p.Next = p.Offset + ledger.PageSize
	s.render(w, r, http.StatusOK, "accounts", p)
}

type gridView struct {
	Grid    ledger.Grid
	View    string
	Order   ledger.Order
	HideKey string
}

type balanceAt struct {
	At      time.Time
	Raw     string
	Invalid bool
	Balance ledger.Balance
}

type accountPage struct {
	base
	Account     ledger.Account
	View        string
	Order       ledger.Order
	Entries     []ledger.Entry
	Before      string
	Grid        gridView
	Chart       *Chart
	ChartCount  int
	ChartFirst  time.Time
	ChartCapped bool
	Flows       ledger.Flows
	Zoom        *zoom
	BalanceAt   *balanceAt
}

// zoom is the created_at range picked by dragging across the balance chart.
type zoom struct {
	ledger.TimeRange
	RawFrom, RawTo string
	Invalid        bool
}

func parseZoom(r *http.Request) *zoom {
	z := &zoom{RawFrom: r.FormValue("from"), RawTo: r.FormValue("to")}
	if z.RawFrom == "" && z.RawTo == "" {
		return nil
	}
	var ok bool
	if z.RawFrom != "" {
		if z.From, ok = parseAt(z.RawFrom); !ok {
			z.Invalid = true
		}
	}
	if z.RawTo != "" {
		if z.To, ok = parseAt(z.RawTo); !ok {
			z.Invalid = true
		}
	}
	if !z.From.IsZero() && !z.To.IsZero() && z.To.Before(z.From) {
		z.Invalid = true
	}
	if z.Invalid {
		z.TimeRange = ledger.TimeRange{}
	}
	return z
}

var atLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", time.RFC3339}

func parseAt(s string) (time.Time, bool) {
	for _, layout := range atLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (s *server) account(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p accountPage
	err := s.store.View(r.Context(), func(st *ledger.Store) error {
		var err error
		p, err = loadAccount(r, st, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	s.render(w, r, http.StatusOK, "account", p)
}

func loadAccount(r *http.Request, st *ledger.Store, id string) (accountPage, error) {
	ctx := r.Context()
	acct, err := st.GetAccount(ctx, id)
	if err != nil {
		return accountPage{}, err
	}
	p := accountPage{
		Title: "Account " + acct.Name, Nav: "accounts", URL: r.URL,
		Account: acct,
		View:    r.FormValue("view"),
		Order:   ledger.ParseOrder(r.FormValue("order")),
		Zoom:    parseZoom(r),
	}
	var created ledger.TimeRange
	if p.Zoom != nil {
		created = p.Zoom.TimeRange
	}

	hist, err := st.BalanceHistory(ctx, id, created, ledger.ChartPoints)
	if err != nil {
		return accountPage{}, err
	}
	start := acct.CreatedAt
	if !created.From.IsZero() {
		start = created.From
	}
	series := hist.Series(start, created.To)
	if p.Chart, err = BuildChart(series, 600, 160); err != nil {
		return accountPage{}, err
	}
	if p.Chart != nil {
		p.ChartCount, p.ChartFirst, p.ChartCapped = len(hist.Points), series[0].At, !hist.Complete
	}

	if p.Flows, err = st.AccountFlows(ctx, id, created, ledger.TopCounterparties); err != nil {
		return accountPage{}, err
	}

	if raw := r.FormValue("at"); raw != "" {
		b := &balanceAt{Raw: raw}
		var ok bool
		if b.At, ok = parseAt(raw); !ok {
			b.Invalid = true
		} else if b.Balance, err = st.BalanceAt(ctx, id, b.At, p.Order); err != nil {
			return accountPage{}, err
		}
		p.BalanceAt = b
	}

	switch p.View {
	case "crosstab", "inout":
		entries, truncated, err := st.GridEntries(ctx, ledger.GridFilter{AccountID: id, Created: created}, p.Order, ledger.MaxGridTransfers)
		if err != nil {
			return accountPage{}, err
		}
		g, err := ledger.BuildGrid(entries, truncated)
		if err != nil {
			return accountPage{}, err
		}
		p.Grid = gridView{Grid: g, View: p.View, Order: p.Order}
	default:
		p.View = "entries"
		var more bool
		p.Entries, more, err = st.ListEntries(ctx, ledger.EntryFilter{
			AccountID: id, Before: r.FormValue("before"), Created: created, Limit: ledger.PageSize,
		})
		if err != nil {
			return accountPage{}, err
		}
		if more {
			p.Before = p.Entries[len(p.Entries)-1].ID
		}
	}
	return p, nil
}

type transfersPage struct {
	base
	Q         string
	Transfers []ledger.Transfer
	Before    string
}

func (s *server) transfers(w http.ResponseWriter, r *http.Request) {
	p := transfersPage{Title: "Transfers", Nav: "transfers", URL: r.URL, Q: r.FormValue("q")}
	transfers, more, err := s.store.ListTransfers(r.Context(), ledger.TransferFilter{
		Q: p.Q, Before: r.FormValue("before"), Limit: ledger.PageSize,
	})
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	p.Transfers = transfers
	if more {
		p.Before = transfers[len(transfers)-1].ID
	}
	s.render(w, r, http.StatusOK, "transfers", p)
}

type transferPage struct {
	base
	Transfer ledger.Transfer
	Entries  []ledger.Entry
}

func (s *server) transfer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	p := transferPage{Title: "Transfer " + short(id), Nav: "transfers", URL: r.URL}
	err := s.store.View(ctx, func(st *ledger.Store) error {
		var err error
		if p.Transfer, err = st.GetTransfer(ctx, id); err != nil {
			return err
		}
		p.Entries, err = st.TransferEntries(ctx, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	s.render(w, r, http.StatusOK, "transfer", p)
}

type entriesPage struct {
	base
	Q       string
	Entries []ledger.Entry
	Before  string
}

func (s *server) entries(w http.ResponseWriter, r *http.Request) {
	p := entriesPage{Title: "Entries", Nav: "entries", URL: r.URL, Q: r.FormValue("q")}
	entries, more, err := s.store.ListEntries(r.Context(), ledger.EntryFilter{
		Q: p.Q, Before: r.FormValue("before"), Limit: ledger.PageSize,
	})
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	p.Entries = entries
	if more {
		p.Before = entries[len(entries)-1].ID
	}
	s.render(w, r, http.StatusOK, "entries", p)
}

type entryPage struct {
	base
	Entry ledger.Entry
}

func (s *server) entry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, err := s.store.GetEntry(r.Context(), id)
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	s.render(w, r, http.StatusOK, "entry", entryPage{Title: "Entry " + short(id), Nav: "entries", URL: r.URL, Entry: e})
}

// traceTransfer is one grid row flattened for the trace page's transfers tab.
type traceTransfer struct {
	ID                                     string
	CreatedAt, EventAt                     time.Time
	FromID, FromName, ToID, ToName, Amount string
	Currency                               string
	Metadata                               ledger.Metadata
}

type tracePage struct {
	base
	Key, VQ, Value string
	Keys           []ledger.KeyCount
	ExactKey       bool
	Values         []ledger.TraceValue
	View           string
	Order          ledger.Order
	Grid           gridView
	Transfers      []traceTransfer
}

func (s *server) trace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := tracePage{
		Title: "Trace", Nav: "trace", URL: r.URL,
		Key: r.FormValue("key"), VQ: r.FormValue("vq"), Value: r.FormValue("value"),
		View: r.FormValue("view"), Order: ledger.ParseOrder(r.FormValue("order")),
	}

	if p.Key != "" && p.Value != "" {
		p.Title = p.Key + " = " + p.Value
		entries, truncated, err := s.store.GridEntries(ctx, ledger.GridFilter{MetaKey: p.Key, MetaValue: p.Value}, p.Order, ledger.MaxGridTransfers)
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		g, err := ledger.BuildGrid(entries, truncated)
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		if p.View != "inout" && p.View != "transfers" {
			p.View = "crosstab"
		}
		p.Grid = gridView{Grid: g, View: p.View, Order: p.Order, HideKey: p.Key}
		if p.View == "transfers" {
			p.Transfers = flattenGrid(g)
		}
		s.execute(w, http.StatusOK, "trace", "layout", p)
		return
	}

	err := s.store.View(ctx, func(st *ledger.Store) error {
		var err error
		if p.Keys, err = st.TraceKeys(ctx, p.Key, 20); err != nil {
			return err
		}
		for _, k := range p.Keys {
			if k.Key == p.Key {
				p.ExactKey = true
			}
		}
		if p.ExactKey {
			p.Values, err = st.TraceValues(ctx, p.Key, p.VQ, 100)
		}
		return err
	})
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	s.render(w, r, http.StatusOK, "trace", p)
}

func flattenGrid(g ledger.Grid) []traceTransfer {
	out := make([]traceTransfer, len(g.Rows))
	for i, row := range g.Rows {
		t := traceTransfer{ID: row.TransferID, CreatedAt: row.CreatedAt, EventAt: row.EventAt, Metadata: row.Metadata}
		for c, cell := range row.Cells {
			col := g.Columns[c]
			switch cell.Sign {
			case -1:
				t.FromID, t.FromName = col.AccountID, col.Name
			case 1:
				t.ToID, t.ToName, t.Amount, t.Currency = col.AccountID, col.Name, cell.Amount, col.Currency
			}
		}
		out[i] = t
	}
	return out
}

// chartView is what the chart partial needs: the chart and its caption.
type chartView struct {
	Chart    *Chart
	Currency string
	Count    int
	First    time.Time
	Capped   bool
}

func (p accountPage) ChartView() chartView {
	return chartView{Chart: p.Chart, Currency: p.Account.Currency, Count: p.ChartCount, First: p.ChartFirst, Capped: p.ChartCapped}
}
