ALTER TABLE personas
    ADD COLUMN IF NOT EXISTS writing_samples JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS do_not_say JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS catchphrases JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS preferred_language TEXT NOT NULL DEFAULT 'en',
    ADD COLUMN IF NOT EXISTS formality INT NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'personas_preferred_language_check'
    ) THEN
        ALTER TABLE personas
            ADD CONSTRAINT personas_preferred_language_check
            CHECK (preferred_language IN ('tr', 'en'));
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'personas_formality_check'
    ) THEN
        ALTER TABLE personas
            ADD CONSTRAINT personas_formality_check
            CHECK (formality BETWEEN 0 AND 3);
    END IF;
END
$$;

ALTER TABLE quota_events
    DROP CONSTRAINT IF EXISTS quota_events_quota_type_check;

ALTER TABLE quota_events
    ADD CONSTRAINT quota_events_quota_type_check
    CHECK (quota_type IN ('draft', 'reply', 'preview'));
