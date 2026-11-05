// Package web serves the read-only pgledger visualizer.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var pageNames = []string{"home", "search", "accounts", "account", "transfers", "transfer", "entries", "entry", "trace", "error"}

type server struct {
	store *ledger.Store
	pages map[string]*template.Template
}

// base is embedded in every page's data.
type base struct {
	Title string
	Nav   string
	URL   *url.URL
}

func New(store *ledger.Store) (http.Handler, error) {
	s := &server{store: store, pages: map[string]*template.Template{}}
	for _, name := range pageNames {
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS,
			"templates/layout.html", "templates/partials.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		s.pages[name] = t
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /accounts", s.accounts)
	mux.HandleFunc("GET /accounts/{id}", s.account)
	mux.HandleFunc("GET /transfers", s.transfers)
	mux.HandleFunc("GET /transfers/{id}", s.transfer)
	mux.HandleFunc("GET /entries", s.entries)
	mux.HandleFunc("GET /entries/{id}", s.entry)
	mux.HandleFunc("GET /trace", s.trace)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return mux, nil
}

// render executes the page's results block for partial=1 requests and the
// full layout otherwise.
func (s *server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	name := "layout"
	if r.URL.Query().Get("partial") == "1" && s.pages[page].Lookup("results") != nil {
		name = "results"
	}
	s.execute(w, status, page, name, data)
}

func (s *server) execute(w http.ResponseWriter, status int, page, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("render", "page", page, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

type errorPage struct {
	base
	Message string
}

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error, id string) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, ledger.ErrNotFound):
		s.render(w, r, http.StatusNotFound, "error", errorPage{base{Title: "Not found", URL: r.URL}, "Not found: " + id})
	default:
		slog.Error("request", "url", r.URL.String(), "err", err)
		s.render(w, r, http.StatusInternalServerError, "error", errorPage{base{Title: "Error", URL: r.URL}, err.Error()})
	}
}
