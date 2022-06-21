package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ianrose14/solarsnoop/internal/powertrend"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
)

func InsertMessageAttempt(ctx context.Context, db *sql.DB, notifierId int64, phase powertrend.Phase, sendErr error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	cols := []string{"notifier_id", "timestamp", "state_change", "success"}
	args := []any{notifierId, time.Now(), string(phase), err == nil}

	if sendErr != nil {
		cols = append(cols, "error_message")
		args = append(args, sendErr.Error())
	}

	stmt := "INSERT INTO message_log(" + strings.Join(cols, ",") + ")" +
		" VALUES(" + strings.Repeat("?,", len(cols)-1) + "?);"

	_, err = conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("failed to insert into message_log: %w", err)
	}

	return nil
}

func InsertSystems(ctx context.Context, db *sql.DB, userId string, systems []*enphase.System) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT OR IGNORE INTO enphase_systems(user_id, system_id, name, public_name)" +
		" VALUES"

	var args []interface{}
	for i, system := range systems {
		if i > 0 {
			stmt += ","
		}
		stmt += " (?,?,?,?)"
		args = append(args, userId, system.SystemId, system.Name, system.PublicName)
	}
	stmt += ";"

	_, err = conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("failed to insert to enphase_systems: %w", err)
	}

	return nil
}

func InsertUsageData(ctx context.Context, db *sql.DB, userId string, systemId int64, startAt, endAt time.Time, wattsProduced, wattsConsumed int64) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT INTO enphase_telemetry(user_id, system_id, start_at, end_at, inserted_at, produced_watts, consumed_watts)" +
		" VALUES(?,?,?,?,?,?,?)"

	_, err = conn.ExecContext(ctx, stmt, userId, systemId, startAt, endAt, time.Now(), wattsProduced, wattsConsumed)
	if err != nil {
		return fmt.Errorf("failed to insert into enphase_telemetry: %w", err)
	}

	return nil
}

func QuerySystems(ctx context.Context, db *sql.DB, userId string) ([]*enphase.System, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "SELECT system_id, name, public_name FROM enphase_systems WHERE user_id=? ORDER BY name ASC"

	rows, err := conn.QueryContext(ctx, stmt, userId)
	if err != nil {
		return nil, fmt.Errorf("failed to query enphase_systems: %w", err)
	}
	defer rows.Close()

	var systems []*enphase.System
	for rows.Next() {
		var dst enphase.System
		err := rows.Scan(&dst.SystemId, &dst.Name, &dst.PublicName)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row contents: %w", err)
		}

		systems = append(systems, &dst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate query results: %w", err)
	}

	return systems, nil
}

func UpsertDatabaseTables(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	sqlSchema := map[string]string{
		"auth_sessions": "CREATE TABLE IF NOT EXISTS auth_sessions (" +
			"user_id TEXT NOT NULL PRIMARY KEY," +
			"session_token TEXT NOT NULL," +
			"access_token TEXT NOT NULL," +
			"refresh_token TEXT NOT NULL," +
			"created_time DATE NOT NULL," +
			"last_refresh_time DATE NOT NULL" +
			");",
		"enphase_systems": "CREATE TABLE IF NOT EXISTS enphase_systems (" +
			"user_id TEXT NOT NULL," +
			"system_id INTEGER NOT NULL," +
			"name TEXT," +
			"public_name TEXT," +
			"PRIMARY KEY (user_id, system_id)" +
			");",
		"enphase_telemetry": "CREATE TABLE IF NOT EXISTS enphase_telemetry (" +
			"user_id TEXT NOT NULL," +
			"system_id INTEGER NOT NULL," +
			"start_at DATE NOT NULL," +
			"end_at DATE NOT NULL," +
			"inserted_at DATE NOT NULL," +
			"produced_watts INTEGER NOT NULL," +
			"consumed_watts INTEGER NOT NULL," +
			"FOREIGN KEY(user_id, system_id) REFERENCES enphase_systems(user_id, system_id)" +
			");",
		"notifiers": "CREATE TABLE IF NOT EXISTS notifiers (" +
			"notifier_id INTEGER NOT NULL PRIMARY KEY," +
			"user_id TEXT NOT NULL," +
			"system_id INTEGER NOT NULL," +
			"created DATE NOT NULL," +
			"notifier_kind string NOT NULL," +
			"recipient string," +
			"FOREIGN KEY(user_id, system_id) REFERENCES enphase_systems(user_id, system_id)" +
			");",
		"message_log": "CREATE TABLE IF NOT EXISTS message_log (" +
			"notifier_id INTEGER NOT NULL," +
			"timestamp DATE NOT NULL," +
			"state_change string NOT NULL," + // string instance of Phase
			"success bool NOT NULL," +
			"error_message string," +
			"FOREIGN KEY(notifier_id) REFERENCES notifiers(notifier_id)" +
			");",
		"idx_message_log": "CREATE INDEX IF NOT EXISTS idx_message_log" +
			" ON message_log(notifier_id, timestamp);",
	}

	for name, stmt := range sqlSchema {
		_, err = conn.ExecContext(ctx, stmt)
		if err != nil {
			kind := "object"
			if strings.HasPrefix(stmt, "CREATE TABLE") {
				kind = "table"
			} else if strings.Contains(stmt, "INDEX") {
				kind = "index"
			}
			return fmt.Errorf("failed to create %s %s: %w", name, kind, err)
		}
	}

	return nil
}
