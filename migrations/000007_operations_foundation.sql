CREATE TABLE maintenance_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    write_locked boolean NOT NULL DEFAULT false,
    reason text NOT NULL DEFAULT '',
    owner_token uuid,
    locked_at timestamptz,
    lease_until timestamptz,
    CONSTRAINT maintenance_lock_shape CHECK (
        (NOT write_locked AND owner_token IS NULL AND locked_at IS NULL AND lease_until IS NULL) OR
        (write_locked AND owner_token IS NOT NULL AND locked_at IS NOT NULL AND lease_until IS NOT NULL)
    )
);

INSERT INTO maintenance_state(singleton) VALUES (true);
