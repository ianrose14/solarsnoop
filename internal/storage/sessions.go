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

// UpsertSession creates a new or updates and existing auth session, returning the session's unique token.
func UpsertSession(ctx context.Context, db *sql.DB, userId, accessToken, refreshToken string) (string, error) {
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

	st := New(tx)

	rows, err := st.QuerySessionsByUser(ctx, userId)
	if err != nil {
		return "", fmt.Errorf("failed to query auth_sessions: %w", err)
	}
	if len(rows) > 0 {
		// found an existing row - we should update it
		params := UpdateSessionParams{
			AccessToken:     accessToken,
			RefreshToken:    refreshToken,
			LastRefreshTime: time.Now(),
			UserID:          userId,
		}
		sessionToken := rows[0]
		if err := st.UpdateSession(ctx, params); err != nil {
			return "", fmt.Errorf("failed to update auth_sessions: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("failed to commit db tx: %w", err)
		}

		committed = true
		return sessionToken, nil
	}

	// else, no existing row - we should create one

	sessionToken := fmt.Sprintf("%x", chaos.Int63())
	params := InsertSessionParams{
		UserID:          userId,
		SessionToken:    sessionToken,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		CreatedTime:     time.Now(),
		LastRefreshTime: time.Now(),
	}
	if err := st.InsertSession(ctx, params); err != nil {
		return "", fmt.Errorf("failed to insert into auth_sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit db tx: %w", err)
	}

	committed = true
	return sessionToken, nil
}
