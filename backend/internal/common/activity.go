package common

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
)

type DBExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func InsertPersonaActivityEvent(ctx context.Context, executor DBExecutor, personaID, eventType string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}

	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	_, err = executor.Exec(ctx, `
		INSERT INTO persona_activity_events(persona_id, type, metadata)
		VALUES ($1, $2, $3::jsonb)
	`, personaID, eventType, raw)
	return err
}
