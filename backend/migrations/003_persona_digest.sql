CREATE TABLE IF NOT EXISTS persona_activity_events (
    id BIGSERIAL PRIMARY KEY,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('post_created', 'reply_generated', 'thread_participated')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS persona_digests (
    id BIGSERIAL PRIMARY KEY,
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    stats JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_persona_activity_events_persona_type_created_at
    ON persona_activity_events(persona_id, type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_persona_activity_events_persona_created_at
    ON persona_activity_events(persona_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_persona_digests_persona_date
    ON persona_digests(persona_id, date);

CREATE INDEX IF NOT EXISTS idx_persona_digests_persona_date
    ON persona_digests(persona_id, date DESC);
