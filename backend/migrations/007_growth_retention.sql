CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    type TEXT NOT NULL CHECK (type IN ('battle_remixed', 'template_used', 'persona_followed')),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_read_created_at
    ON notifications(user_id, read_at, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_created_at
    ON notifications(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS weekly_digests (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, week_start)
);

CREATE INDEX IF NOT EXISTS idx_weekly_digests_user_week_start
    ON weekly_digests(user_id, week_start DESC);

CREATE INDEX IF NOT EXISTS idx_events_battle_id_created_at
    ON events((metadata->>'battle_id'), created_at DESC);

CREATE INDEX IF NOT EXISTS idx_events_source_battle_id_created_at
    ON events((metadata->>'source_battle_id'), created_at DESC);
