// Package web serves the read-only pgledger visualizer.
package web

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pgr0ss/pgledger-visualizer/internal/ledger"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var pageNames = []string{"home", "search", "accounts", "account", "transfers", "transfer", "entries", "entry", "trace", "styleguide", "error"}

// stylesheets are linked in this order. tokens.css comes first because it
// declares the cascade layer order.
var stylesheets = []string{"tokens.css", "base.css", "components.css", "pages.css", "utilities.css"}

// Options configure the server.
type Options struct {
	// DevDir, when set, is the internal/web directory. Templates and static
	// files are then read from it on every request instead of the binary, and
	// pages load a script that reloads them when a file there changes.
	DevDir string
}

type server struct {
	store     *ledger.Store
	dev       bool
	templates fs.FS
	static    fs.FS
	pages     map[string]*template.Template
	assets    map[string]string // static file name -> content hash; nil in dev
	boot      string            // identifies this process to dev-reload.js
}

// base is embedded in every page's data.
type base struct {
	Title string
	Nav   string
	URL   *url.URL
}

func New(store *ledger.Store, opts Options) (http.Handler, error) {
	s := &server{store: store, boot: strconv.FormatInt(time.Now().UnixNano(), 36)}
	if opts.DevDir != "" {
		s.dev = true
		s.templates = os.DirFS(opts.DevDir)
		s.static = os.DirFS(filepath.Join(opts.DevDir, "static"))
	} else {
		static, err := fs.Sub(staticFS, "static")
		if err != nil {
			return nil, err
		}
		s.templates, s.static = templateFS, static
		if s.assets, err = hashAssets(static); err != nil {
			return nil, err
		}
	}
	// Parse once even in dev, so a broken template fails at startup.
	pages, err := s.parsePages()
	if err != nil {
		return nil, err
	}
	if !s.dev {
		s.pages = pages
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
	mux.HandleFunc("GET /styleguide", s.styleguide)
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.serveAssets(http.FileServerFS(s.static))))
	if s.dev {
		mux.HandleFunc("GET /_dev/events", s.devEvents)
	}
	return logRequests(securityHeaders(mux)), nil
}

func (s *server) parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range pageNames {
		t, err := template.New(name).Funcs(funcs).Funcs(template.FuncMap{
			"asset":                      s.asset,
			"stylesheets":                func() []string { return stylesheets },
			"dev":                        func() bool { return s.dev },
			"idempotencyKeysTableExists": s.idempotencyKeysTableExists,
		}).ParseFS(s.templates,
			"templates/layout.html", "templates/partials.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// idempotencyKeysTableExists is false without a store, as on /styleguide in tests.
func (s *server) idempotencyKeysTableExists() bool {
	return s.store != nil && s.store.IdempotencyKeysTableExists()
}

// page returns a parsed page, re-reading the templates in dev.
func (s *server) page(name string) (*template.Template, error) {
	if !s.dev {
		return s.pages[name], nil
	}
	pages, err := s.parsePages()
	if err != nil {
		return nil, err
	}
	return pages[name], nil
}

func hashAssets(static fs.FS) (map[string]string, error) {
	assets := map[string]string{}
	err := fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		hash, err := hashFile(static, path)
		assets[path] = hash
		return err
	})
	return assets, err
}

func hashFile(fsys fs.FS, path string) (string, error) {
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]), nil
}

// assetHash returns a static file's content hash, reading the file in dev.
func (s *server) assetHash(name string) (string, bool) {
	if !s.dev {
		v, ok := s.assets[name]
		return v, ok
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	if st, err := fs.Stat(s.static, name); err != nil || st.IsDir() {
		return "", false
	}
	v, err := hashFile(s.static, name)
	return v, err == nil
}

// asset returns a URL for a static file that changes whenever its content does.
func (s *server) asset(name string) (string, error) {
	v, ok := s.assetHash(name)
	if !ok {
		return "", fmt.Errorf("unknown asset %q", name)
	}
	return "/static/" + name + "?v=" + v, nil
}

// serveAssets serves only static files, never directory listings. Each file
// gets its content hash as an ETag, and browsers may cache it forever when the
// request carries that hash. In dev, browsers always revalidate.
func (s *server) serveAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := s.assetHash(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"`+hash+`"`)
		switch {
		case s.dev:
			w.Header().Set("Cache-Control", "no-cache")
		case r.URL.Query().Get("v") == hash:
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		slog.Info("request", "method", r.Method, "url", r.URL.RequestURI(), "status", cmp.Or(rec.status, http.StatusOK), "duration", time.Since(start))
	})
}

// contentSecurityPolicy allows inline style attributes because the balance chart
// positions its axis labels with them.
const contentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// render executes the page's results block for partial=1 requests and the
// full layout otherwise.
func (s *server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	name := "layout"
	if r.URL.Query().Get("partial") == "1" {
		if t, err := s.page(page); err == nil && t.Lookup("results") != nil {
			name = "results"
		}
	}
	s.execute(w, status, page, name, data)
}

func (s *server) execute(w http.ResponseWriter, status int, page, name string, data any) {
	t, err := s.page(page)
	if err != nil {
		slog.Error("parse", "page", page, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
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
