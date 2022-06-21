package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"
)

var (
	chaos = rand.New(rand.NewSource(time.Now().Unix()))
)

type AuthSession struct {
	UserId          string
	SessionToken    string
	AccessToken     string
	RefreshToken    string
	CreatedTime     time.Time
	LastRefreshTime time.Time
}

// UpsertSession creates a new or updates and existing auth session, returning the session's unique token.
func UpsertSession(ctx context.Context, db *sql.DB, userId, accessToken, refreshToken string) (string, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: false})
	if err != nil {
		return "", fmt.Errorf("failed to start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt := "SELECT session_token FROM auth_sessions WHERE user_id=?"
	rows, err := tx.QueryContext(ctx, stmt, userId)
	if err != nil {
		return "", fmt.Errorf("failed to query auth_sessions: %w", err)
	}
	defer rows.Close()

	var sessionToken string
	if rows.Next() {
		err := rows.Scan(&sessionToken)
		if err != nil {
			return "", fmt.Errorf("failed to scan row contents: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("failed to iterate query results: %w", err)
	}

	if sessionToken != "" {
		// found an existing row - we should update it
		stmt := "UPDATE auth_sessions SET access_token=?, refresh_token=?, last_refresh_time=? WHERE user_id=?"
		_, err := tx.ExecContext(ctx, stmt, accessToken, refreshToken, time.Now(), userId)
		if err != nil {
			return "", fmt.Errorf("failed to update auth_sessions: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("failed to commit db tx: %w", err)
		}

		committed = true
		return sessionToken, nil
	}

	// else, no existing row - we should create one

	sessionToken = fmt.Sprintf("%x", chaos.Int63())
	stmt = "INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)" +
		" VALUES(?,?,?,?,?,?);"

	_, err = tx.ExecContext(ctx, stmt, userId, sessionToken, accessToken, refreshToken, time.Now(), 0)
	if err != nil {
		return "", fmt.Errorf("failed to insert into auth_sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit db tx: %w", err)
	}

	committed = true
	return sessionToken, nil
}

func QuerySessions(ctx context.Context, db *sql.DB, sessionToken string) ([]*AuthSession, error) {
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

	var sessions []*AuthSession
	for rows.Next() {
		var dst AuthSession
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
