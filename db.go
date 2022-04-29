package main

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/ianrose14/solarsnoop/pkg/enphase"
)

/*
   CREATE TABLE IF NOT EXISTS auth_sessions (
   session_id integer primary key autoincrement,
   session_token text,
   access_token text,
   refresh_token text,
   last_refesh_time text,
   ) STRICT;


*/

var (
	chaos = rand.New(rand.NewSource(time.Now().Unix()))
)

type authSession struct {
	UserId          string
	SessionToken    string
	AccessToken     string
	RefreshToken    string
	CreatedTime     time.Time
	LastRefreshTime time.Time
}

func insertSession(ctx context.Context, db *sql.DB, userId, accessToken, refreshToken string) (*authSession, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	session := authSession{
		UserId:       userId,
		SessionToken: fmt.Sprintf("%x", chaos.Int63()),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		CreatedTime:  time.Now(),
	}

	stmt := "INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)" +
		" values(?,?,?,?,?,?);"

	_, err = conn.ExecContext(ctx, stmt, session.UserId, session.SessionToken, session.AccessToken, session.RefreshToken, session.CreatedTime, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to insert into auth_sessions: %w", err)
	}

	return &session, nil
}

func insertSystems(ctx context.Context, db *sql.DB, userId string, systems []*enphase.System) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT OR IGNORE INTO enphase_systems(user_id, system_id, name, public_name)" +
		" values"

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

func querySessions(ctx context.Context, db *sql.DB, sessionToken string) ([]*authSession, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	var args []interface{}
	stmt := "SELECT user_id, session_token, access_token, refresh_token, created_time, last_refresh_time" +
		" FROM auth_sessions"

	if sessionToken != "" {
		stmt += " WHERE session_token=?"
		args = append(args, sessionToken)
	}
	stmt += " GROUP BY user_id HAVING ROWID = MIN(ROWID) ORDER BY created_time DESC;"

	rows, err := conn.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query auth_sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*authSession
	for rows.Next() {
		var dst authSession
		err := rows.Scan(&dst.UserId, &dst.SessionToken, &dst.AccessToken, &dst.RefreshToken, &dst.CreatedTime, &dst.LastRefreshTime)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row contents: %w", err)
		}

		sessions = append(sessions, &dst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate query results: %w", err)
	}

	return sessions, nil
}

func querySystems(ctx context.Context, db *sql.DB, userId string) ([]*enphase.System, error) {
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

func upsertDatabaseTables(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "CREATE TABLE IF NOT EXISTS auth_sessions (" +
		"user_id TEXT NOT NULL," +
		"session_token TEXT NOT NULL," +
		"access_token TEXT NOT NULL," +
		"refresh_token TEXT NOT NULL," +
		"created_time DATE NOT NULL," +
		"last_refresh_time DATE NOT NULL" +
		");"

	_, err = conn.ExecContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("failed to create auth_sessions table: %w", err)
	}

	stmt = "CREATE TABLE IF NOT EXISTS enphase_systems (" +
		"user_id TEXT NOT NULL," +
		"system_id INTEGER NOT NULL," +
		"name TEXT," +
		"public_name TEXT," +
		"PRIMARY KEY (user_id, system_id)" +
		");"

	_, err = conn.ExecContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("failed to create enphase_systems table: %w", err)
	}

	// TODO: create telemetry table

	return nil
}
