package web

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pgr0ss/pgledger-visualizer/internal/ledger"
)

const timeLayout = "2006-01-02 15:04:05.000"

var funcs = template.FuncMap{
	"short":           short,
	"shortKey":        shortKey,
	"isoTime":         isoTime,
	"tsText":          tsText,
	"tsMinuteText":    tsMinuteText,
	"signClass":       signClass,
	"cellClass":       cellClass,
	"traceURL":        traceURL,
	"accountsMetaURL": accountsMetaURL,
	"withParam":       withParam,
	"withoutParams":   withoutParams,
	"pills":           func(md ledger.Metadata, hide string) pillsView { return pillsView{md, hide} },
	"maxGrid":         func() int { return ledger.MaxGridTransfers },
	"accountRef":      func(id, name string) accountRef { return accountRef{id, name} },
	"flow": func(label, total, currency, sign string, rows []ledger.Counterparty) flowView {
		return flowView{label, total, currency, sign, rows}
	},
	"prettyJSON": prettyJSON,
}

// flowView is what the counterparties partial needs: one direction of an account's flows.
type flowView struct {
	Label, Total, Currency, Sign string
	Rows                         []ledger.Counterparty
}

type accountRef struct {
	ID, Name string
}

var prefixedULID = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9]*_[0-7][0-9A-HJKMNP-TV-Z]{25}\b`)

// ShortName shortens each prefixed ULID in the name as short does.
func (a accountRef) ShortName() string { return prefixedULID.ReplaceAllStringFunc(a.Name, short) }

// Title is the hover text: the id, preceded by the full name when ShortName shortens it.
func (a accountRef) Title() string {
	if a.ShortName() == a.Name {
		return a.ID
	}
	return a.Name + " " + a.ID
}

type pillsView struct {
	Metadata ledger.Metadata
	Hide     string
}

// short keeps an id's prefix and its last 6 characters, with the ellipsis in
// place of the underscore: pglt_01M7…F4QJEP becomes pglt…F4QJEP.
func short(id string) string {
	const tail = 6
	i := strings.IndexByte(id, '_')
	if i < 0 || len(id)-i-1 <= tail {
		return id
	}
	return id[:i] + "…" + id[len(id)-tail:]
}

// shortKey keeps an idempotency key's first 8 characters, a UUID's first group.
func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "…"
}

func isoTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsText(t time.Time) string { return t.UTC().Format(timeLayout) }

// tsMinuteText is tsText truncated to minutes, for axis labels.
func tsMinuteText(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") }

func signClass(amount string) string {
	switch {
	case strings.HasPrefix(amount, "-"):
		return "neg"
	case strings.Trim(amount, "0.") == "":
		return "zero"
	default:
		return "pos"
	}
}

// cellClass styles a grid cell by its entry's sign; empty cells get none.
func cellClass(sign int) string {
	switch sign {
	case 1:
		return "cell pos"
	case -1:
		return "cell neg"
	default:
		return ""
	}
}

func traceURL(key, value string) string {
	return "/trace?" + url.Values{"key": {key}, "value": {value}}.Encode()
}

func accountsMetaURL(key, value string) string {
	return "/accounts?" + url.Values{"key": {key}, "value": {value}}.Encode()
}

// withParam returns u with k set to v. Changing anything else resets keyset
// pagination.
func withParam(u *url.URL, k, v string) string {
	q := u.Query()
	if k != "before" {
		q.Del("before")
	}
	q.Set(k, v)
	q.Del("partial")
	return u.Path + "?" + q.Encode()
}

// withoutParams returns u without the keys, resetting keyset pagination.
func withoutParams(u *url.URL, keys ...string) string {
	q := u.Query()
	for _, k := range append(keys, "before", "partial") {
		q.Del(k)
	}
	if len(q) == 0 {
		return u.Path
	}
	return u.Path + "?" + q.Encode()
}

func prettyJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return raw
	}
	return buf.String()
}
