package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ianrose14/solarsnoop/internal/powersinks"
)

func (row QueryPowersinksAllRow) Kind() powersinks.Kind {
	return powersinks.Kind(row.PowersinkKind)
}

func (row QueryPowersinkForSystemRow) Kind() powersinks.Kind {
	return powersinks.Kind(row.PowersinkKind)
}

func Str(s string) sql.NullString {
	return sql.NullString{String: s}
}

func UpsertDatabaseTables(ctx context.Context, db *sql.DB, schema string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	return nil
}
