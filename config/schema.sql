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
    timezone TEXT NOT NULL,
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
    channel TEXT NOT NULL,
    recipient TEXT,
    FOREIGN KEY(user_id, system_id) REFERENCES enphase_systems(user_id, system_id)
);

CREATE TABLE IF NOT EXISTS actions_log (
    powersink_id INTEGER NOT NULL,
    timestamp DATE NOT NULL,
    desired_action TEXT NOT NULL,
    desired_reason TEXT,
    executed_action TEXT NOT NULL,
    executed_reason TEXT,
    success BOOL NOT NULL,
    success_reason TEXT,
    FOREIGN KEY(powersink_id) REFERENCES powersinks(powersink_id)
);

CREATE INDEX IF NOT EXISTS idx_actions_log ON actions_log(powersink_id, timestamp);
