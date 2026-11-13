package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

// styleguidePage feeds /styleguide, which renders every partial in each of its
// states from fixed fixture data, without touching the database.
type styleguidePage struct {
	base
	Colors                  []string
	Text, Space, Radii      []token
	Amounts                 []string
	Account                 ledger.Account
	Meta, RawMeta           ledger.Metadata
	Entries                 []ledger.Entry
	TopIn, TopOut           []ledger.Counterparty
	Chart                   chartView
	Crosstab, InOut, Capped gridView
	Empty                   gridView
	Order                   ledger.Order
	TransferID, IdemKey     string
	At                      time.Time
}

// token is one step of a scale in tokens.css.
type token struct{ Name, Value string }

var (
	sgLiquidity = ledger.GridColumn{AccountID: "pgla_01M7B5Q70SF2DVP7EXHEG5W51H", Name: "liquidity.eur", Currency: "EUR"}
	sgUser016   = ledger.GridColumn{AccountID: "pgla_01M7B5Q8E2Y7W3NFQ0B4R6ZK1D", Name: "user016.eur.available", Currency: "EUR"}
	sgUser008   = ledger.GridColumn{AccountID: "pgla_01M7B5Q8E2Y7W3NFQ0B4R6ZK0X", Name: "user008.eur.available", Currency: "EUR"}
	sgStart     = time.Date(2026, 10, 14, 18, 21, 30, 378000000, time.UTC)
)

func (s *server) styleguide(w http.ResponseWriter, r *http.Request) {
	p, err := styleguideData()
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	p.base = base{Title: "Styleguide", URL: r.URL}
	p.Order = ledger.ParseOrder(r.FormValue("order"))
	s.render(w, r, http.StatusOK, "styleguide", p)
}

func styleguideData() (styleguidePage, error) {
	meta := ledger.ParseMetadata([]byte(`{"payment_id":"p_00030","fx_id":"fx_00006","kind":"fx",` +
		`"from_currency":"EUR","to_currency":"USD","rate":{"num":1.08,"den":1}}`))
	p := styleguidePage{
		Colors: []string{"bg", "surface", "border", "fg", "muted", "accent", "pos", "neg", "pos-tint", "neg-tint", "hover", "hover-strong"},
		Text: []token{
			{"xl", "22px"},
			{"lg", "18px"},
			{"md", "16px"},
			{"base", "14px"},
			{"mono", "13px"},
			{"sm", "12px"},
			{"xs", "11px"},
		},
		Space: []token{
			{"1", "4px"},
			{"2", "8px"},
			{"3", "12px"},
			{"4", "16px"},
			{"5", "20px"},
			{"6", "24px"},
			{"7", "40px"},
		},
		Radii:   []token{{"sm", "4px"}, {"", "6px"}, {"pill", "999px"}},
		Amounts: []string{"95.82", "-873.67", "0.00"},
		Account: ledger.Account{
			ID: sgLiquidity.AccountID, Name: sgLiquidity.Name, Currency: "EUR", Balance: "-873.67", Version: 11,
			AllowNegativeBalance: true, AllowPositiveBalance: true,
			Metadata:  ledger.ParseMetadata([]byte(`{"type":"system"}`)),
			CreatedAt: sgStart, UpdatedAt: sgStart.Add(27 * 24 * time.Hour),
		},
		Meta:       meta,
		RawMeta:    ledger.ParseMetadata([]byte(`["legacy",1]`)),
		TransferID: "pglt_01M7B5Q70SF2DVP7EXD0T4DA4X",
		IdemKey:    "checkout-p_00030-6f1c2a9e-0b7d-4e55-9a51-3c2d8e7f4b10",
		At:         sgStart.Add(7 * 24 * time.Hour),
		TopIn: []ledger.Counterparty{
			{AccountID: "pgla_01M7B5Q8E2Y7W3NFQ0B4R6ZK2T", Name: "user020.eur.available", Amount: "95.82"},
			{AccountID: sgUser016.AccountID, Name: sgUser016.Name, Amount: "77.31"},
			{AccountID: sgUser008.AccountID, Name: sgUser008.Name, Amount: "12.96"},
		},
	}

	// Each transfer moves money from one account to another. The first two share
	// created_at, so the grid brackets them as one database transaction.
	transfers := []struct {
		id       string
		at       time.Duration
		from, to ledger.GridColumn
		amount   string
	}{
		{"pglt_01M7B5Q70SF2DVP7EXD0T4DA4X", 6 * 24 * time.Hour, sgLiquidity, sgUser016, "157.28"},
		{"pglt_01M7B5Q70SF2DVP7EXPNB0W7K4", 6 * 24 * time.Hour, sgLiquidity, sgUser016, "199.84"},
		{"pglt_01M7B5Q70SF2DVP7EXW0XSNTJ5", 8 * 24 * time.Hour, sgUser016, sgLiquidity, "77.31"},
		{"pglt_01M7B5Q70SF2DVP7EXMTYAKFF7", 10 * 24 * time.Hour, sgLiquidity, sgUser008, "226.11"},
		{"pglt_01M7B5Q70SF2DVP7EXN3M2TW20", 11 * 24 * time.Hour, sgUser008, sgLiquidity, "12.96"},
	}
	var entries []ledger.GridEntry
	for i, t := range transfers {
		at := sgStart.Add(t.at)
		md := ledger.ParseMetadata(fmt.Appendf(nil, `{"kind":"fx","payment_id":"p_%05d"}`, 30+i))
		for _, leg := range []struct {
			col    ledger.GridColumn
			amount string
		}{{t.from, "-" + t.amount}, {t.to, t.amount}} {
			entries = append(entries, ledger.GridEntry{
				EntryID: "pgle_" + t.id[5:], TransferID: t.id, AccountID: leg.col.AccountID, AccountName: leg.col.Name,
				Currency: leg.col.Currency, Amount: leg.amount, BalanceAfter: "0.00",
				CreatedAt: at, EventAt: at, Metadata: md,
			})
		}
		p.Entries = append(p.Entries, ledger.Entry{
			ID: "pgle_" + t.id[5:], AccountID: t.from.AccountID, AccountName: t.from.Name, Currency: "EUR",
			TransferID: t.id, Amount: "-" + t.amount, PreviousBalance: "0.00", CurrentBalance: "-" + t.amount,
			CreatedAt: at, EventAt: at, Metadata: md, CounterpartyID: t.to.AccountID, CounterpartyName: t.to.Name,
		})
	}
	g, err := ledger.BuildGrid(entries, false)
	if err != nil {
		return p, err
	}
	p.Crosstab = gridView{Grid: g, View: "crosstab", Order: ledger.OrderCreated}
	p.InOut = gridView{Grid: g, View: "inout", Order: ledger.OrderCreated}
	capped := g
	capped.Truncated = true
	p.Capped = gridView{Grid: capped, View: "crosstab", Order: ledger.OrderCreated}

	balances := []string{"0.00", "-157.28", "-357.12", "-279.81", "-501.17", "-727.28", "-631.46", "-618.50", "-748.14", "-798.91", "-875.86", "-873.67"}
	points := make([]ledger.BalancePoint, len(balances))
	for i, b := range balances {
		points[i] = ledger.BalancePoint{At: sgStart.Add(time.Duration(i) * 58 * time.Hour), Balance: b}
		if i > 0 {
			points[i].TransferID = transfers[i%len(transfers)].id
		}
	}
	chart, err := BuildChart(points, 600, 160)
	if err != nil {
		return p, err
	}
	p.Chart = chartView{Chart: chart, Currency: "EUR", Count: len(points) - 1, First: sgStart}
	return p, nil
}
