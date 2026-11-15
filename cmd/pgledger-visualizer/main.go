// Command pgledger-visualizer serves a read-only web UI over pgledger's views and
// loads sample data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pgr0ss/pgledger_visualizer/internal/ledger"
	"github.com/pgr0ss/pgledger_visualizer/internal/seed"
	"github.com/pgr0ss/pgledger_visualizer/internal/web"
)

const usage = `usage: pgledger-visualizer <command> [flags]

commands:
  serve   run the web UI (read-only)
  seed    load sample data

Every flag can also be set by an environment variable, shown in each command's -h.
run "pgledger-visualizer <command> -h" for flags`

func main() {
	if len(os.Args) < 2 {
		exitUsage(usage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(ctx, os.Args[2:])
	case "seed":
		err = runSeed(ctx, os.Args[2:])
	default:
		exitUsage(usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// envName is the environment variable that sets a flag the command line omits:
// DATABASE_URL for --database-url, else PGLEDGER_VISUALIZER_ plus the flag name
// in upper snake case.
func envName(flagName string) string {
	name := strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
	if name == "DATABASE_URL" {
		return name
	}
	return "PGLEDGER_VISUALIZER_" + name
}

// parse parses args into fs. Each flag takes its value from the command line,
// else its environment variable, else its default.
func parse(fs *flag.FlagSet, args []string, getenv func(string) string) error {
	var envErr error
	fs.VisitAll(func(f *flag.Flag) {
		name := envName(f.Name)
		f.Usage += " (env " + name + ")"
		if v := getenv(name); v != "" && envErr == nil {
			if err := f.Value.Set(v); err != nil {
				envErr = fmt.Errorf("invalid %s %q: %w", name, v, err)
			}
		}
	})
	if envErr != nil {
		return envErr
	}
	return fs.Parse(args)
}

func exitUsage(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}

// devDir is internal/web relative to the repository root.
const devDir = "internal/web"

type serveOptions struct {
	databaseURL, addr string
	slowMS            int
	dev               bool
}

func serveFlags(o *serveOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.StringVar(&o.databaseURL, "database-url", "", "Postgres URL")
	fs.StringVar(&o.addr, "addr", "127.0.0.1:8080", "listen address")
	fs.IntVar(&o.slowMS, "log-slow-queries-ms", -1, "log queries taking at least this many milliseconds; 0 logs all, -1 disables")
	fs.BoolVar(&o.dev, "dev", false, "read templates and static files from "+devDir+" and live-reload pages; run from the repository root")
	return fs
}

func serve(ctx context.Context, args []string) error {
	var o serveOptions
	if err := parse(serveFlags(&o), args, os.Getenv); err != nil {
		exitUsage(err.Error())
	}
	if o.databaseURL == "" {
		exitUsage("--database-url or DATABASE_URL is required")
	}
	if o.slowMS < -1 {
		exitUsage("--log-slow-queries-ms must be -1 or more")
	}

	var tracer pgx.QueryTracer
	if o.slowMS >= 0 {
		tracer = ledger.SlowQueryLogger{Threshold: time.Duration(o.slowMS) * time.Millisecond}
	}
	pool, err := ledger.Connect(ctx, o.databaseURL, tracer)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := ledger.NewStore(ctx, pool)
	if err != nil {
		return err
	}
	var opts web.Options
	if o.dev {
		if _, err := os.Stat(filepath.Join(devDir, "templates")); err != nil {
			exitUsage("--dev must run from the repository root: " + err.Error())
		}
		opts.DevDir = devDir
	}
	h, err := web.New(store, opts)
	if err != nil {
		return err
	}

	srv := &http.Server{Addr: o.addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	if o.dev {
		// Shutdown waits for open requests, and live-reload streams stay open
		// until their request context ends.
		srv.BaseContext = func(net.Listener) context.Context { return ctx }
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening on http://" + o.addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type seedOptions struct {
	databaseURL   string
	installSchema bool
	users         int
	seed          uint64
}

func seedFlags(o *seedOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	fs.StringVar(&o.databaseURL, "database-url", "", "Postgres URL")
	fs.BoolVar(&o.installSchema, "install-schema", false, "install pgledger if its views are missing")
	fs.IntVar(&o.users, "users", 20, "number of sample users")
	fs.Uint64Var(&o.seed, "seed", 1, "random seed")
	return fs
}

func runSeed(ctx context.Context, args []string) error {
	var o seedOptions
	if err := parse(seedFlags(&o), args, os.Getenv); err != nil {
		exitUsage(err.Error())
	}
	if o.databaseURL == "" {
		exitUsage("--database-url or DATABASE_URL is required")
	}

	conn, err := pgx.Connect(ctx, o.databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	sum, err := seed.Run(ctx, conn, seed.Options{
		Users: o.users, Seed: o.seed, Now: time.Now().UTC(), InstallSchema: o.installSchema,
	})
	if err != nil {
		return err
	}
	slog.Info("seeded", "accounts", sum.Accounts, "transfers", sum.Transfers)
	return nil
}
