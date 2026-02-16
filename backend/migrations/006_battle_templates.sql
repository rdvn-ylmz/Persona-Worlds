CREATE TABLE IF NOT EXISTS templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    prompt_rules TEXT NOT NULL,
    turn_count INT NOT NULL CHECK (turn_count BETWEEN 2 AND 20),
    word_limit INT NOT NULL CHECK (word_limit BETWEEN 40 AND 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_public BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_templates_public_created_at
    ON templates(is_public, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_templates_owner_created_at
    ON templates(owner_user_id, created_at DESC);

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS template_id UUID REFERENCES templates(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_posts_template_id_created_at
    ON posts(template_id, created_at DESC);

INSERT INTO templates(owner_user_id, name, prompt_rules, turn_count, word_limit, is_public)
SELECT
    NULL,
    'Claim/Evidence 6 turns',
    'Alternate claims and evidence. Keep each turn practical and concrete. No personal attacks.',
    6,
    120,
    TRUE
WHERE NOT EXISTS (
    SELECT 1
    FROM templates
    WHERE owner_user_id IS NULL
      AND LOWER(name) = LOWER('Claim/Evidence 6 turns')
);
