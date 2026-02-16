DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'battle_status_enum') THEN
        CREATE TYPE battle_status_enum AS ENUM ('PENDING', 'PROCESSING', 'DONE', 'FAILED');
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS battles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    topic TEXT NOT NULL,
    persona_a_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    persona_b_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    status battle_status_enum NOT NULL DEFAULT 'PENDING',
    verdict JSONB NOT NULL DEFAULT '{}'::jsonb,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT battles_distinct_personas CHECK (persona_a_id <> persona_b_id)
);

CREATE TABLE IF NOT EXISTS battle_turns (
    battle_id UUID NOT NULL REFERENCES battles(id) ON DELETE CASCADE,
    turn_index INT NOT NULL CHECK (turn_index BETWEEN 1 AND 6),
    persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (battle_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_battles_room_created_at
    ON battles(room_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_battles_status_created_at
    ON battles(status, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_battle_turns_battle_turn_index
    ON battle_turns(battle_id, turn_index ASC);

CREATE TABLE IF NOT EXISTS battle_creation_events (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    battle_id UUID NOT NULL REFERENCES battles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_battle_creation_events_user_created_at
    ON battle_creation_events(user_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_battle_creation_events_battle_id
    ON battle_creation_events(battle_id);
