package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", base{Title: "pgledger visualizer", Nav: "search", URL: r.URL})
}

type searchPage struct {
	base
	Q         string
	Accounts  []ledger.Account
	Transfers []ledger.Transfer
	Pairs     []ledger.MetaPair
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := searchPage{Title: "Search", Nav: "search", URL: r.URL, Q: r.FormValue("q")}
	if p.Q != "" {
		var err error
		if p.Accounts, _, err = s.store.ListAccounts(ctx, ledger.AccountFilter{Q: p.Q, Limit: ledger.SearchLimit}); err != nil {
			s.fail(w, r, err, "")
			return
		}
		if p.Transfers, _, err = s.store.ListTransfers(ctx, ledger.TransferFilter{Q: p.Q, Limit: ledger.SearchLimit}); err != nil {
			s.fail(w, r, err, "")
			return
		}
		if p.Pairs, err = s.store.MetadataPairs(ctx, p.Q, ledger.SearchLimit); err != nil {
			s.fail(w, r, err, "")
			return
		}
	}
	s.render(w, r, http.StatusOK, "search", p)
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
	BalanceAt   *balanceAt
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
	ctx := r.Context()
	id := r.PathValue("id")
	acct, err := s.store.GetAccount(ctx, id)
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	p := accountPage{
		Title: acct.Name, Nav: "accounts", URL: r.URL,
		Account: acct,
		View:    r.FormValue("view"),
		Order:   ledger.ParseOrder(r.FormValue("order")),
	}

	points, err := s.store.BalanceHistory(ctx, id, ledger.ChartPoints)
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	if p.Chart, err = BuildChart(points, 600, 160); err != nil {
		s.fail(w, r, err, id)
		return
	}
	if p.Chart != nil {
		p.ChartCount, p.ChartFirst, p.ChartCapped = len(points), points[0].At, len(points) == ledger.ChartPoints
	}

	if raw := r.FormValue("at"); raw != "" {
		b := &balanceAt{Raw: raw}
		var ok bool
		if b.At, ok = parseAt(raw); !ok {
			b.Invalid = true
		} else if b.Balance, err = s.store.BalanceAt(ctx, id, b.At, p.Order); err != nil {
			s.fail(w, r, err, id)
			return
		}
		p.BalanceAt = b
	}

	switch p.View {
	case "crosstab", "inout":
		entries, truncated, err := s.store.GridEntries(ctx, ledger.GridFilter{AccountID: id}, p.Order, ledger.MaxGridTransfers)
		if err != nil {
			s.fail(w, r, err, id)
			return
		}
		g, err := ledger.BuildGrid(entries, truncated)
		if err != nil {
			s.fail(w, r, err, id)
			return
		}
		p.Grid = gridView{Grid: g, View: p.View, Order: p.Order}
	default:
		p.View = "entries"
		var more bool
		p.Entries, more, err = s.store.ListEntries(ctx, ledger.EntryFilter{
			AccountID: id, Before: r.FormValue("before"), Limit: ledger.PageSize,
		})
		if err != nil {
			s.fail(w, r, err, id)
			return
		}
		if more {
			p.Before = p.Entries[len(p.Entries)-1].ID
		}
	}
	s.render(w, r, http.StatusOK, "account", p)
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
	t, err := s.store.GetTransfer(ctx, id)
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	entries, err := s.store.TransferEntries(ctx, id)
	if err != nil {
		s.fail(w, r, err, id)
		return
	}
	s.render(w, r, http.StatusOK, "transfer", transferPage{
		Title: "Transfer " + short(id), Nav: "transfers", URL: r.URL, Transfer: t, Entries: entries,
	})
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

	var err error
	if p.Keys, err = s.store.TraceKeys(ctx, p.Key, 20); err != nil {
		s.fail(w, r, err, "")
		return
	}
	for _, k := range p.Keys {
		if k.Key == p.Key {
			p.ExactKey = true
		}
	}
	if p.ExactKey {
		if p.Values, err = s.store.TraceValues(ctx, p.Key, p.VQ, 100); err != nil {
			s.fail(w, r, err, "")
			return
		}
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
