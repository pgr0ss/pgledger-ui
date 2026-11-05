package ledger

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgr0ss/pgledger_visualizer/internal/db"
)

// Connect opens a read-only pool and checks that the pgledger views exist.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["application_name"] = "pgledger-visualizer"

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
