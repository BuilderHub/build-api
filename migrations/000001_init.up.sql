-- Build API initial schema (placeholder for future build session metadata)
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    dirty BOOLEAN NOT NULL DEFAULT FALSE
);
