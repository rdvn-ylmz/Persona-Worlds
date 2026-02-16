CREATE TABLE IF NOT EXISTS persona_public_profiles (
    persona_id UUID PRIMARY KEY REFERENCES personas(id) ON DELETE CASCADE,
    slug TEXT NOT NULL UNIQUE,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    bio TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS persona_follows (
    follower_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followed_persona_id UUID NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (follower_user_id, followed_persona_id)
);

CREATE INDEX IF NOT EXISTS idx_persona_public_profiles_slug_public
    ON persona_public_profiles(slug, is_public);

CREATE INDEX IF NOT EXISTS idx_persona_follows_followed_persona_created_at
    ON persona_follows(followed_persona_id, created_at DESC);
