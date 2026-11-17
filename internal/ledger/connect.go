package ledger

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger-visualizer/internal/db"
)

// Connect opens a read-only pool and checks that the pgledger views exist.
// tracer may be nil.
func Connect(ctx context.Context, url string, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["application_name"] = "pgledger-visualizer"
	cfg.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	installed, err := db.New(pool).SchemaInstalled(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if !installed {
		pool.Close()
		return nil, errors.New(`pgledger views not found on search_path; install pgledger or run "pgledger-visualizer seed --install-schema"`)
	}
	return pool, nil
}

// SlowQueryLogger logs queries that take at least Threshold.
type SlowQueryLogger struct {
	Threshold time.Duration
}

type queryStartKey struct{}

type queryStart struct {
	sql  string
	args []any
	at   time.Time
}

func (l SlowQueryLogger) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{data.SQL, data.Args, time.Now()})
}

func (l SlowQueryLogger) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryStartKey{}).(queryStart)
	if !ok {
		return
	}
	elapsed := time.Since(start.at)
	if elapsed < l.Threshold {
		return
	}
	attrs := []any{"query", queryName(start.sql), "duration", elapsed, "args", start.args}
	if data.Err != nil {
		attrs = append(attrs, "err", data.Err)
	}
	slog.Info("query", attrs...)
}

// queryName returns the sqlc query name from its "-- name: X :kind" header,
// or the whole SQL when there is none.
func queryName(sql string) string {
	header, _, _ := strings.Cut(sql, "\n")
	if name, ok := strings.CutPrefix(header, "-- name: "); ok {
		name, _, _ = strings.Cut(name, " ")
		return name
	}
	return sql
}
