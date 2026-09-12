// Package audit writes metadata-only security and administration events.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func Insert(ctx context.Context, executor Executor, actorID *string, action, targetType, targetID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	var nullableTarget any
	if targetID != "" {
		nullableTarget = targetID
	}
	if _, err := executor.Exec(ctx, `
        INSERT INTO audit_log(actor_user_id, action, target_type, target_id, metadata)
        VALUES ($1, $2, $3, $4, $5::jsonb)`, actorID, action, targetType, nullableTarget, string(encoded)); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
