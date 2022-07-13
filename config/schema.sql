CREATE TABLE IF NOT EXISTS auth_sessions (
    user_id TEXT NOT NULL PRIMARY KEY,
    session_token TEXT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    created_time DATE NOT NULL,
    last_refresh_time DATE NOT NULL
);

CREATE TABLE IF NOT EXISTS ecobee_accounts (
    user_id TEXT NOT NULL,
    enphase_system_id BIGINT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    created_time DATE NOT NULL,
    last_refresh_time DATE NOT NULL
);

CREATE TABLE IF NOT EXISTS ecobee_thermostats (
    user_id TEXT NOT NULL,
    enphase_system_id BIGINT NOT NULL,
    thermostat_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS enphase_systems (
    user_id TEXT NOT NULL,
    system_id BIGINT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    public_name TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, system_id)
);

CREATE TABLE IF NOT EXISTS enphase_telemetry (
    user_id TEXT NOT NULL,
    system_id BIGINT NOT NULL,
    start_at DATE NOT NULL,
    end_at DATE NOT NULL,
    inserted_at DATE NOT NULL,
    produced_watts BIGINT NOT NULL,
    consumed_watts BIGINT NOT NULL,
    FOREIGN KEY(user_id, system_id) REFERENCES enphase_systems(user_id, system_id)
);

CREATE TABLE IF NOT EXISTS powersinks (
    powersink_id INTEGER NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL,
    system_id BIGINT NOT NULL,
    created DATE NOT NULL,
    powersink_kind string NOT NULL,
    recipient string,
    FOREIGN KEY(user_id, system_id) REFERENCES enphase_systems(user_id, system_id)
);

CREATE TABLE IF NOT EXISTS message_log (
    powersink_id INTEGER NOT NULL,
    timestamp DATE NOT NULL,
    state_change string NOT NULL,
    success bool NOT NULL,
    error_message string,
    FOREIGN KEY(powersink_id) REFERENCES powersinks(powersink_id)
);

CREATE INDEX IF NOT EXISTS idx_message_log ON message_log(powersink_id, timestamp);
