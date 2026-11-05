package web

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

const timeLayout = "2006-01-02 15:04:05.000"

var funcs = template.FuncMap{
	"short":           short,
	"ts":              ts,
	"tsMinute":        tsMinute,
	"signClass":       signClass,
	"cellClass":       cellClass,
	"traceURL":        traceURL,
	"accountsMetaURL": accountsMetaURL,
	"withParam":       withParam,
	"withoutParams":   withoutParams,
	"pills":           func(md ledger.Metadata, hide string) pillsView { return pillsView{md, hide} },
	"maxGrid":         func() int { return ledger.MaxGridTransfers },
	"accountRef":      func(id, name string) accountRef { return accountRef{id, name} },
	"prettyJSON":      prettyJSON,
}

type accountRef struct {
	ID, Name string
}

type pillsView struct {
	Metadata ledger.Metadata
	Hide     string
}

// short keeps an id's prefix and its last 8 characters.
func short(id string) string {
	i := strings.IndexByte(id, '_')
	if i < 0 || len(id)-i-1 <= 8 {
		return id
	}
	return id[:i+1] + "…" + id[len(id)-8:]
}

func ts(t time.Time) template.HTML {
	u := t.UTC()
	return template.HTML(`<time datetime="` + u.Format(time.RFC3339Nano) + `">` + u.Format(timeLayout) + `</time>`)
}

// tsMinute is ts truncated to minutes, for axis labels.
func tsMinute(t time.Time) template.HTML {
	u := t.UTC()
	return template.HTML(`<time datetime="` + u.Format(time.RFC3339Nano) + `" data-fmt="minute">` + u.Format("2006-01-02 15:04") + `</time>`)
}

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
