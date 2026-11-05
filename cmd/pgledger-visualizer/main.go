// Command pgledger-visualizer serves a read-only web UI over pgledger's views and
// loads sample data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

run "pgledger-visualizer <command> -h" for flags`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
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
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parse(fs *flag.FlagSet, args []string) string {
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "Postgres URL (default $DATABASE_URL)")
	_ = fs.Parse(args)
	if *dbURL == "" {
		fmt.Fprintln(os.Stderr, "--db or DATABASE_URL is required")
		os.Exit(2)
	}
	return *dbURL
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	slowMS := fs.Int("log-slow-queries-ms", -1, "log queries taking at least this many milliseconds; 0 logs all, -1 disables")
	dbURL := parse(fs, args)
	if *slowMS < -1 {
		fmt.Fprintln(os.Stderr, "--log-slow-queries-ms must be -1 or more")
		os.Exit(2)
	}

	var tracer pgx.QueryTracer
	if *slowMS >= 0 {
		tracer = ledger.SlowQueryLogger{Threshold: time.Duration(*slowMS) * time.Millisecond}
	}
	pool, err := ledger.Connect(ctx, dbURL, tracer)
	if err != nil {
		return err
	}
	defer pool.Close()
	h, err := web.New(ledger.NewStore(pool))
	if err != nil {
		return err
	}

	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening on http://" + *addr)

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

func runSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	install := fs.Bool("install-schema", false, "install pgledger if its views are missing")
	users := fs.Int("users", 20, "number of sample users")
	seedValue := fs.Uint64("seed", 1, "random seed")
	dbURL := parse(fs, args)

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	sum, err := seed.Run(ctx, conn, seed.Options{
		Users: *users, Seed: *seedValue, Now: time.Now().UTC(), InstallSchema: *install,
	})
	if err != nil {
		return err
	}
	slog.Info("seeded", "accounts", sum.Accounts, "transfers", sum.Transfers)
	return nil
}
