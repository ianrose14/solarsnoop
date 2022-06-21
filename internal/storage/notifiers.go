package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ianrose14/solarsnoop/internal/notifications"
)

type NotifierConfig struct {
	Id        int64
	Kind      notifications.Kind
	Recipient string
}

func DeleteNotifier(ctx context.Context, db *sql.DB, userId string, systemId, notifierId int64) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "DELETE FROM notifiers WHERE user_id=? AND system_id=? AND notifier_id=?"
	_, err = conn.ExecContext(ctx, stmt, userId, systemId, notifierId)
	if err != nil {
		return fmt.Errorf("failed to delete from notifiers: %w", err)
	}

	return nil
}

func InsertNotifier(ctx context.Context, db *sql.DB, userId string, systemId int64, kind notifications.Kind, recipient string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT INTO notifiers(user_id, system_id, created, notifier_kind, recipient)" +
		" VALUES(?,?,?,?,?);"

	_, err = conn.ExecContext(ctx, stmt, userId, systemId, time.Now(), string(kind), recipient)
	if err != nil {
		return fmt.Errorf("failed to insert into notifiers: %w", err)
	}

	return nil
}

// QuerySystemNotifiers queries for all Notifiers registered for a specific system.
func QuerySystemNotifiers(ctx context.Context, db *sql.DB, userId string, systemId int64) ([]NotifierConfig, error) {
	// Check arguments to ensure we don't accidentally query for all notifiers.
	if userId == "" {
		return nil, fmt.Errorf("illegal arguments, userId must not be zero-valued")
	}
	if systemId == 0 {
		return nil, fmt.Errorf("illegal arguments, systemId must not be zero-valued")
	}

	return queryNotifiers(ctx, db, userId, systemId)
}

// QueryAllNotifiers queries for all Notifiers across all users/systems.
func QueryAllNotifiers(ctx context.Context, db *sql.DB) ([]NotifierConfig, error) {
	return queryNotifiers(ctx, db, "", 0)
}

// queryNotifiers queries for all Notifiers registered for a specific system (if userId and systemId are non-zero), or
// for all Notifiers across all users/systems (otherwise).
func queryNotifiers(ctx context.Context, db *sql.DB, userId string, systemId int64) ([]NotifierConfig, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "SELECT notifier_id, notifier_kind, recipient FROM notifiers"
	var args []any

	if userId != "" {
		stmt += " WHERE user_id=? AND system_id=?;"
		args = append(args, userId, systemId)
	} else {
		stmt += ";"
	}

	rows, err := conn.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer rows.Close()

	var notifs []NotifierConfig
	for rows.Next() {
		var dst NotifierConfig
		err := rows.Scan(&dst.Id, &dst.Kind, &dst.Recipient)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row contents: %w", err)
		}
		notifs = append(notifs, dst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate query results: %w", err)
	}

	return notifs, nil
}

func UpdateNotifier(ctx context.Context, db *sql.DB, notifierId int64, newRecipient string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "UPDATE notifiers SET recipient=? WHERE notifier_id=?;"
	_, err = conn.ExecContext(ctx, stmt, newRecipient, notifierId)
	if err != nil {
		return fmt.Errorf("failed to update notifier %d: %w", notifierId, err)
	}

	return nil
}
