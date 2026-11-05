// Package pgschema embeds pgledger's SQL so the visualizer can install it.
package pgschema

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/*.sql
var files embed.FS

var installOrder = []string{"ulid-to-uuid.sql", "uuid-to-ulid.sql", "pgledger.sql"}

// Install runs pgledger's schema files into the first schema on search_path.
func Install(ctx context.Context, conn *pgconn.PgConn) error {
	for _, name := range installOrder {
		sql, err := files.ReadFile("sql/" + name)
		if err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
		if _, err := conn.Exec(ctx, string(sql)).ReadAll(); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	return nil
}
