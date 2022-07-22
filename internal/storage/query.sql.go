// Code generated by sqlc. DO NOT EDIT.
// source: query.sql

package storage

import (
	"context"
	"database/sql"
	"time"
)

const deletePowersink = `-- name: DeletePowersink :exec
DELETE FROM powersinks WHERE user_id=$1 AND system_id=$2 AND powersink_id=$3
`

type DeletePowersinkParams struct {
	UserID      string
	SystemID    int64
	PowersinkID int32
}

func (q *Queries) DeletePowersink(ctx context.Context, arg DeletePowersinkParams) error {
	_, err := q.db.ExecContext(ctx, deletePowersink, arg.UserID, arg.SystemID, arg.PowersinkID)
	return err
}

const fetchRecentActions = `-- name: FetchRecentActions :many
SELECT timestamp, desired_action, desired_reason, executed_action, executed_reason, success, success_reason
FROM actions_log WHERE powersink_id=$1 ORDER BY timestamp DESC
`

type FetchRecentActionsRow struct {
	Timestamp      time.Time
	DesiredAction  string
	DesiredReason  sql.NullString
	ExecutedAction string
	ExecutedReason sql.NullString
	Success        bool
	SuccessReason  sql.NullString
}

func (q *Queries) FetchRecentActions(ctx context.Context, powersinkID int32) ([]FetchRecentActionsRow, error) {
	rows, err := q.db.QueryContext(ctx, fetchRecentActions, powersinkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []FetchRecentActionsRow
	for rows.Next() {
		var i FetchRecentActionsRow
		if err := rows.Scan(
			&i.Timestamp,
			&i.DesiredAction,
			&i.DesiredReason,
			&i.ExecutedAction,
			&i.ExecutedReason,
			&i.Success,
			&i.SuccessReason,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertEcobeeAccount = `-- name: InsertEcobeeAccount :exec
INSERT INTO ecobee_accounts(user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time)
    VALUES ($1, $2, $3, $4, $5, $6)
`

type InsertEcobeeAccountParams struct {
	UserID          string
	EnphaseSystemID int64
	AccessToken     string
	RefreshToken    string
	CreatedTime     time.Time
	LastRefreshTime time.Time
}

func (q *Queries) InsertEcobeeAccount(ctx context.Context, arg InsertEcobeeAccountParams) error {
	_, err := q.db.ExecContext(ctx, insertEcobeeAccount,
		arg.UserID,
		arg.EnphaseSystemID,
		arg.AccessToken,
		arg.RefreshToken,
		arg.CreatedTime,
		arg.LastRefreshTime,
	)
	return err
}

const insertEcobeeThermostat = `-- name: InsertEcobeeThermostat :exec
INSERT INTO ecobee_thermostats(user_id, enphase_system_id, thermostat_id)
    VALUES ($1, $2, $3)
`

type InsertEcobeeThermostatParams struct {
	UserID          string
	EnphaseSystemID int64
	ThermostatID    string
}

func (q *Queries) InsertEcobeeThermostat(ctx context.Context, arg InsertEcobeeThermostatParams) error {
	_, err := q.db.ExecContext(ctx, insertEcobeeThermostat, arg.UserID, arg.EnphaseSystemID, arg.ThermostatID)
	return err
}

const insertEnphaseSystem = `-- name: InsertEnphaseSystem :exec
INSERT INTO enphase_systems(user_id, system_id, name, public_name, timezone)
    VALUES($1, $2, $3, $4, $5)
`

type InsertEnphaseSystemParams struct {
	UserID     string
	SystemID   int64
	Name       string
	PublicName string
	Timezone   string
}

func (q *Queries) InsertEnphaseSystem(ctx context.Context, arg InsertEnphaseSystemParams) error {
	_, err := q.db.ExecContext(ctx, insertEnphaseSystem,
		arg.UserID,
		arg.SystemID,
		arg.Name,
		arg.PublicName,
		arg.Timezone,
	)
	return err
}

const insertEnphaseTelemetry = `-- name: InsertEnphaseTelemetry :exec
INSERT INTO enphase_telemetry(user_id, system_id, start_at, end_at, inserted_at, produced_watts, consumed_watts)
    VALUES($1, $2, $3, $4, $5, $6, $7)
`

type InsertEnphaseTelemetryParams struct {
	UserID        string
	SystemID      int64
	StartAt       time.Time
	EndAt         time.Time
	InsertedAt    time.Time
	ProducedWatts int64
	ConsumedWatts int64
}

func (q *Queries) InsertEnphaseTelemetry(ctx context.Context, arg InsertEnphaseTelemetryParams) error {
	_, err := q.db.ExecContext(ctx, insertEnphaseTelemetry,
		arg.UserID,
		arg.SystemID,
		arg.StartAt,
		arg.EndAt,
		arg.InsertedAt,
		arg.ProducedWatts,
		arg.ConsumedWatts,
	)
	return err
}

const insertPowersink = `-- name: InsertPowersink :exec
INSERT INTO powersinks(user_id, system_id, created, channel, recipient)
    VALUES($1, $2, $3, $4, $5)
`

type InsertPowersinkParams struct {
	UserID    string
	SystemID  int64
	Created   time.Time
	Channel   string
	Recipient sql.NullString
}

func (q *Queries) InsertPowersink(ctx context.Context, arg InsertPowersinkParams) error {
	_, err := q.db.ExecContext(ctx, insertPowersink,
		arg.UserID,
		arg.SystemID,
		arg.Created,
		arg.Channel,
		arg.Recipient,
	)
	return err
}

const insertSession = `-- name: InsertSession :exec
INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)
    VALUES($1, $2, $3, $4, $5, $6)
`

type InsertSessionParams struct {
	UserID          string
	SessionToken    string
	AccessToken     string
	RefreshToken    string
	CreatedTime     time.Time
	LastRefreshTime time.Time
}

func (q *Queries) InsertSession(ctx context.Context, arg InsertSessionParams) error {
	_, err := q.db.ExecContext(ctx, insertSession,
		arg.UserID,
		arg.SessionToken,
		arg.AccessToken,
		arg.RefreshToken,
		arg.CreatedTime,
		arg.LastRefreshTime,
	)
	return err
}

const queryEcobeeAccounts = `-- name: QueryEcobeeAccounts :many
SELECT user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time
    FROM ecobee_accounts ORDER BY user_id, enphase_system_id ASC
`

func (q *Queries) QueryEcobeeAccounts(ctx context.Context) ([]EcobeeAccount, error) {
	rows, err := q.db.QueryContext(ctx, queryEcobeeAccounts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []EcobeeAccount
	for rows.Next() {
		var i EcobeeAccount
		if err := rows.Scan(
			&i.UserID,
			&i.EnphaseSystemID,
			&i.AccessToken,
			&i.RefreshToken,
			&i.CreatedTime,
			&i.LastRefreshTime,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const queryEnphaseSystems = `-- name: QueryEnphaseSystems :many
SELECT system_id, name, public_name, timezone FROM enphase_systems WHERE user_id=$1 ORDER BY name ASC
`

type QueryEnphaseSystemsRow struct {
	SystemID   int64
	Name       string
	PublicName string
	Timezone   string
}

func (q *Queries) QueryEnphaseSystems(ctx context.Context, userID string) ([]QueryEnphaseSystemsRow, error) {
	rows, err := q.db.QueryContext(ctx, queryEnphaseSystems, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []QueryEnphaseSystemsRow
	for rows.Next() {
		var i QueryEnphaseSystemsRow
		if err := rows.Scan(
			&i.SystemID,
			&i.Name,
			&i.PublicName,
			&i.Timezone,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const queryPowersinksAll = `-- name: QueryPowersinksAll :many
SELECT powersink_id, channel, recipient FROM powersinks
`

type QueryPowersinksAllRow struct {
	PowersinkID int32
	Channel     string
	Recipient   sql.NullString
}

func (q *Queries) QueryPowersinksAll(ctx context.Context) ([]QueryPowersinksAllRow, error) {
	rows, err := q.db.QueryContext(ctx, queryPowersinksAll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []QueryPowersinksAllRow
	for rows.Next() {
		var i QueryPowersinksAllRow
		if err := rows.Scan(&i.PowersinkID, &i.Channel, &i.Recipient); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const queryPowersinksForSystem = `-- name: QueryPowersinksForSystem :many
SELECT powersink_id, channel, recipient FROM powersinks
    WHERE user_id=$1 AND system_id=$2
`

type QueryPowersinksForSystemParams struct {
	UserID   string
	SystemID int64
}

type QueryPowersinksForSystemRow struct {
	PowersinkID int32
	Channel     string
	Recipient   sql.NullString
}

func (q *Queries) QueryPowersinksForSystem(ctx context.Context, arg QueryPowersinksForSystemParams) ([]QueryPowersinksForSystemRow, error) {
	rows, err := q.db.QueryContext(ctx, queryPowersinksForSystem, arg.UserID, arg.SystemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []QueryPowersinksForSystemRow
	for rows.Next() {
		var i QueryPowersinksForSystemRow
		if err := rows.Scan(&i.PowersinkID, &i.Channel, &i.Recipient); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const querySessions = `-- name: QuerySessions :many

SELECT user_id, session_token, access_token, refresh_token, created_time, last_refresh_time
    FROM auth_sessions WHERE session_token=$1
    GROUP BY user_id HAVING ROWID = MIN(ROWID) ORDER BY created_time DESC
`

// UPDATE auth_sessions SET access_token=$1, refresh_token=$2, last_refresh_time=$3 WHERE user_id=$4;
//     IF @@ROWCOUNT=0
// INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)
//     VALUES($1, $2, $3, $4, $5, $6);
func (q *Queries) QuerySessions(ctx context.Context, sessionToken string) ([]AuthSession, error) {
	rows, err := q.db.QueryContext(ctx, querySessions, sessionToken)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AuthSession
	for rows.Next() {
		var i AuthSession
		if err := rows.Scan(
			&i.UserID,
			&i.SessionToken,
			&i.AccessToken,
			&i.RefreshToken,
			&i.CreatedTime,
			&i.LastRefreshTime,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const querySessionsAll = `-- name: QuerySessionsAll :many
SELECT user_id, session_token, access_token, refresh_token, created_time, last_refresh_time
    FROM auth_sessions
    GROUP BY user_id HAVING ROWID = MIN(ROWID) ORDER BY created_time DESC
`

func (q *Queries) QuerySessionsAll(ctx context.Context) ([]AuthSession, error) {
	rows, err := q.db.QueryContext(ctx, querySessionsAll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []AuthSession
	for rows.Next() {
		var i AuthSession
		if err := rows.Scan(
			&i.UserID,
			&i.SessionToken,
			&i.AccessToken,
			&i.RefreshToken,
			&i.CreatedTime,
			&i.LastRefreshTime,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const querySessionsByUser = `-- name: QuerySessionsByUser :many
SELECT session_token FROM auth_sessions WHERE user_id=$1
`

func (q *Queries) QuerySessionsByUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, querySessionsByUser, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var session_token string
		if err := rows.Scan(&session_token); err != nil {
			return nil, err
		}
		items = append(items, session_token)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const recordAction = `-- name: RecordAction :exec
INSERT INTO actions_log(powersink_id, timestamp, desired_action, desired_reason, executed_action, executed_reason, success, success_reason)
    VALUES($1, $2, $3, $4, $5, $6, $7, $8)
`

type RecordActionParams struct {
	PowersinkID    int32
	Timestamp      time.Time
	DesiredAction  string
	DesiredReason  sql.NullString
	ExecutedAction string
	ExecutedReason sql.NullString
	Success        bool
	SuccessReason  sql.NullString
}

func (q *Queries) RecordAction(ctx context.Context, arg RecordActionParams) error {
	_, err := q.db.ExecContext(ctx, recordAction,
		arg.PowersinkID,
		arg.Timestamp,
		arg.DesiredAction,
		arg.DesiredReason,
		arg.ExecutedAction,
		arg.ExecutedReason,
		arg.Success,
		arg.SuccessReason,
	)
	return err
}

const updateEcobeeAccessToken = `-- name: UpdateEcobeeAccessToken :exec
UPDATE ecobee_accounts SET access_token=$1 WHERE user_id=$2 AND enphase_system_id=$3
`

type UpdateEcobeeAccessTokenParams struct {
	AccessToken     string
	UserID          string
	EnphaseSystemID int64
}

func (q *Queries) UpdateEcobeeAccessToken(ctx context.Context, arg UpdateEcobeeAccessTokenParams) error {
	_, err := q.db.ExecContext(ctx, updateEcobeeAccessToken, arg.AccessToken, arg.UserID, arg.EnphaseSystemID)
	return err
}

const updateSession = `-- name: UpdateSession :exec
UPDATE auth_sessions SET access_token=$1, refresh_token=$2, last_refresh_time=$3 WHERE user_id=$4
`

type UpdateSessionParams struct {
	AccessToken     string
	RefreshToken    string
	LastRefreshTime time.Time
	UserID          string
}

func (q *Queries) UpdateSession(ctx context.Context, arg UpdateSessionParams) error {
	_, err := q.db.ExecContext(ctx, updateSession,
		arg.AccessToken,
		arg.RefreshToken,
		arg.LastRefreshTime,
		arg.UserID,
	)
	return err
}
