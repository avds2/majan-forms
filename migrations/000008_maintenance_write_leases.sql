CREATE TABLE maintenance_writes (
    request_id text PRIMARY KEY,
    lease_until timestamptz NOT NULL
);

CREATE INDEX maintenance_writes_lease_idx ON maintenance_writes(lease_until);
