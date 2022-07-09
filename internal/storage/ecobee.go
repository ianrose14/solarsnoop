package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type EcobeeAccount struct {
	UserId          string
	EnphaseSystemId int64
	AccessToken     string
	RefreshToken    string
	Created         time.Time
	LastRefreshTime time.Time
}

func InsertEcobeeAccount(ctx context.Context, db *sql.DB, userId string, systemId int64, accessToken, refreshToken string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT OR IGNORE INTO ecobee_accounts(user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time)" +
		" VALUES (?,?,?,?,?,?);"
	args := []interface{}{
		userId, systemId, accessToken, refreshToken, time.Now(), time.Now(),
	}

	_, err = conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("failed to insert to ecobee_accounts: %w", err)
	}

	return nil
}

func InsertEcobeeThermostat(ctx context.Context, db *sql.DB, userId string, systemId int64, thermostatId string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "INSERT OR IGNORE INTO ecobee_thermostats(user_id, enphase_system_id, thermostat_id)" +
		" VALUES (?,?,?,?);"
	args := []interface{}{userId, systemId, thermostatId}

	_, err = conn.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("failed to insert to ecobee_thermostats: %w", err)
	}

	return nil
}

func QueryEcobeeAccounts(ctx context.Context, db *sql.DB) ([]*EcobeeAccount, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "SELECT user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time " +
		" FROM ecobee_accounts ORDER BY user_id, enphase_system_id ASC"

	rows, err := conn.QueryContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("failed to query ecobee_accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*EcobeeAccount
	for rows.Next() {
		var dst EcobeeAccount
		err := rows.Scan(&dst.UserId, &dst.EnphaseSystemId, &dst.AccessToken, &dst.RefreshToken, &dst.Created, &dst.LastRefreshTime)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row contents: %w", err)
		}

		accounts = append(accounts, &dst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate query results: %w", err)
	}

	return accounts, nil
}

func UpdateEcobeeAccount(ctx context.Context, db *sql.DB, userId string, systemId int64, accessToken string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	stmt := "UPDATE ecobee_accounts SET access_token=? WHERE user_id=? AND enphase_system_id=?;"
	_, err = conn.ExecContext(ctx, stmt, accessToken, userId, systemId)
	if err != nil {
		return fmt.Errorf("failed to update ecobee account: %w", err)
	}

	return nil
}
