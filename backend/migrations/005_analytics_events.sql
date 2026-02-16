CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_name TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_created_at
    ON events(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_events_name_created_at
    ON events(event_name, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_events_user_created_at
    ON events(user_id, created_at DESC);
