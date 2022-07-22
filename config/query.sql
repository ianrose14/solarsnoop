-- name: RecordAction :exec
INSERT INTO actions_log(powersink_id, timestamp, desired_action, desired_reason, executed_action, executed_reason, success, success_reason)
    VALUES($1, $2, $3, $4, $5, $6, $7, $8);

-- name: FetchRecentActions :many
SELECT timestamp, desired_action, desired_reason, executed_action, executed_reason, success, success_reason
FROM actions_log WHERE powersink_id=$1 ORDER BY timestamp DESC;

-- name: InsertEnphaseSystem :exec
INSERT INTO enphase_systems(user_id, system_id, name, public_name, timezone)
    VALUES($1, $2, $3, $4, $5);

-- name: InsertEnphaseTelemetry :exec
INSERT INTO enphase_telemetry(user_id, system_id, start_at, end_at, inserted_at, produced_watts, consumed_watts)
    VALUES($1, $2, $3, $4, $5, $6, $7);

-- name: QueryEnphaseSystems :many
SELECT system_id, name, public_name, timezone FROM enphase_systems WHERE user_id=$1 ORDER BY name ASC;

-- name: InsertEcobeeAccount :exec
INSERT INTO ecobee_accounts(user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time)
    VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertEcobeeThermostat :exec
INSERT INTO ecobee_thermostats(user_id, enphase_system_id, thermostat_id)
    VALUES ($1, $2, $3);

-- name: QueryEcobeeAccounts :many
SELECT user_id, enphase_system_id, access_token, refresh_token, created_time, last_refresh_time
    FROM ecobee_accounts ORDER BY user_id, enphase_system_id ASC;

-- name: UpdateEcobeeAccessToken :exec
UPDATE ecobee_accounts SET access_token=$1 WHERE user_id=$2 AND enphase_system_id=$3;

-- name: DeletePowersink :exec
DELETE FROM powersinks WHERE user_id=$1 AND system_id=$2 AND powersink_id=$3;

-- name: InsertPowersink :exec
INSERT INTO powersinks(user_id, system_id, created, channel, recipient)
    VALUES($1, $2, $3, $4, $5);

-- name: QueryPowersinksForSystem :many
SELECT powersink_id, channel, recipient FROM powersinks
    WHERE user_id=$1 AND system_id=$2;

-- name: QueryPowersinksAll :many
SELECT powersink_id, channel, recipient FROM powersinks;

-- name: QuerySessionsByUser :many
SELECT session_token FROM auth_sessions WHERE user_id=$1;

-- name: UpdateSession :exec
UPDATE auth_sessions SET access_token=$1, refresh_token=$2, last_refresh_time=$3 WHERE user_id=$4;

-- name: InsertSession :exec
INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)
    VALUES($1, $2, $3, $4, $5, $6);

-- UPDATE auth_sessions SET access_token=$1, refresh_token=$2, last_refresh_time=$3 WHERE user_id=$4;
--     IF @@ROWCOUNT=0
-- INSERT INTO auth_sessions(user_id, session_token, access_token, refresh_token, created_time, last_refresh_time)
--     VALUES($1, $2, $3, $4, $5, $6);

-- name: QuerySessions :many
SELECT user_id, session_token, access_token, refresh_token, created_time, last_refresh_time
    FROM auth_sessions WHERE session_token=$1
    GROUP BY user_id HAVING ROWID = MIN(ROWID) ORDER BY created_time DESC;

-- name: QuerySessionsAll :many
SELECT user_id, session_token, access_token, refresh_token, created_time, last_refresh_time
    FROM auth_sessions
    GROUP BY user_id HAVING ROWID = MIN(ROWID) ORDER BY created_time DESC;
