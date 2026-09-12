package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/project"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) ProviderSettings(ctx context.Context) (ProviderSettings, error) {
	var settings ProviderSettings
	err := s.pool.QueryRow(ctx, `
		SELECT base_url, api_mode, model, timeout_seconds, max_output_tokens,
		       allow_private_endpoint, compatibility_tested_at, updated_at
        FROM ai_provider_settings WHERE singleton = true`,
	).Scan(&settings.BaseURL, &settings.Mode, &settings.Model, &settings.TimeoutSeconds,
		&settings.MaxOutputTokens, &settings.AllowPrivateEndpoint, &settings.CompatibilityTestedAt, &settings.UpdatedAt)
	if err != nil {
		return ProviderSettings{}, fmt.Errorf("load AI provider settings: %w", err)
	}
	return settings, nil
}

func (s *Store) MarkProviderCompatible(ctx context.Context, actorID string, expectedUpdatedAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin AI compatibility update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `
		UPDATE ai_provider_settings
		SET compatibility_tested_at = now(), updated_at = now()
		WHERE singleton = true AND updated_at = $1`, expectedUpdatedAt)
	if err != nil {
		return fmt.Errorf("mark AI provider compatible: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrInvalidSettings
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.provider_compatibility_tested", "settings", "ai_provider", map[string]any{"compatible": true}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit AI compatibility update: %w", err)
	}
	return nil
}

// BeginProviderCompatibilityTest clears the previous proof before any new
// network request is made. A failed or interrupted re-test must not leave a
// stale "ready" state behind, while the actual provider settings and secret
// remain untouched.
func (s *Store) BeginProviderCompatibilityTest(ctx context.Context, actorID string, expectedUpdatedAt time.Time) (time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin AI compatibility test: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE ai_provider_settings
		SET compatibility_tested_at = NULL, updated_at = now()
		WHERE singleton = true AND updated_at = $1
		RETURNING updated_at`, expectedUpdatedAt).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrInvalidSettings
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("invalidate AI compatibility: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.provider_compatibility_test_started", "settings", "ai_provider", nil); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit AI compatibility invalidation: %w", err)
	}
	return updatedAt, nil
}

func (s *Store) CreateConversation(ctx context.Context, actorID, projectID, title string) (Conversation, error) {
	conversationID, err := id.New()
	if err != nil {
		return Conversation{}, fmt.Errorf("create AI conversation ID: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, fmt.Errorf("begin AI conversation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT ai_enabled FROM projects WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, projectID).Scan(&enabled); errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, project.ErrNotFound
	} else if err != nil {
		return Conversation{}, fmt.Errorf("check AI project: %w", err)
	}
	if !enabled {
		return Conversation{}, ErrProjectDisabled
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_conversations WHERE project_id = $1 AND created_by = $2`, projectID, actorID).Scan(&count); err != nil {
		return Conversation{}, fmt.Errorf("count AI conversations: %w", err)
	}
	if count >= 50 {
		return Conversation{}, ErrConversationLimit
	}
	var conversation Conversation
	err = tx.QueryRow(ctx, `
        INSERT INTO ai_conversations(id, project_id, created_by, title)
        VALUES ($1, $2, $3, $4)
        RETURNING id, project_id, created_by, title, created_at, updated_at`,
		conversationID, projectID, actorID, title,
	).Scan(&conversation.ID, &conversation.ProjectID, &conversation.CreatedBy, &conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("insert AI conversation: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.conversation_created", "ai_conversation", conversationID, map[string]any{"project_id": projectID}); err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, fmt.Errorf("commit AI conversation: %w", err)
	}
	return conversation, nil
}

func (s *Store) ListConversations(ctx context.Context, actorID, projectID string) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT c.id, c.project_id, c.created_by, c.title, c.created_at, c.updated_at
        FROM ai_conversations c
        JOIN projects p ON p.id = c.project_id AND p.deleted_at IS NULL
        WHERE c.project_id = $1 AND c.created_by = $2
        ORDER BY c.updated_at DESC LIMIT 50`, projectID, actorID)
	if err != nil {
		return nil, fmt.Errorf("list AI conversations: %w", err)
	}
	defer rows.Close()
	result := make([]Conversation, 0)
	for rows.Next() {
		var item Conversation
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.CreatedBy, &item.Title, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan AI conversation: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI conversations: %w", err)
	}
	return result, nil
}

func (s *Store) Conversation(ctx context.Context, actorID, projectID, conversationID string) (Conversation, error) {
	var conversation Conversation
	err := s.pool.QueryRow(ctx, `
        SELECT c.id, c.project_id, c.created_by, c.title, c.created_at, c.updated_at
        FROM ai_conversations c
        JOIN projects p ON p.id = c.project_id AND p.deleted_at IS NULL
        WHERE c.id = $1 AND c.project_id = $2 AND c.created_by = $3`, conversationID, projectID, actorID,
	).Scan(&conversation.ID, &conversation.ProjectID, &conversation.CreatedBy, &conversation.Title, &conversation.CreatedAt, &conversation.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrConversationNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("load AI conversation: %w", err)
	}
	return conversation, nil
}

func (s *Store) ListMessages(ctx context.Context, actorID, projectID, conversationID string, limit int) ([]Message, error) {
	if _, err := s.Conversation(ctx, actorID, projectID, conversationID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
        SELECT id, conversation_id, role, content, context_summary, created_at
        FROM (
            SELECT id, conversation_id, role, content, context_summary, created_at
            FROM ai_messages WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT $2
        ) recent ORDER BY created_at`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list AI messages: %w", err)
	}
	defer rows.Close()
	result := make([]Message, 0)
	for rows.Next() {
		var item Message
		var summary []byte
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &summary, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan AI message: %w", err)
		}
		if err := json.Unmarshal(summary, &item.ContextSummary); err != nil {
			return nil, fmt.Errorf("decode AI message context: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI messages: %w", err)
	}
	return result, nil
}

func (s *Store) InsertMessage(ctx context.Context, actorID, projectID, conversationID, role, content string, summary map[string]any) (Message, error) {
	messageID, err := id.New()
	if err != nil {
		return Message{}, fmt.Errorf("create AI message ID: %w", err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return Message{}, fmt.Errorf("encode AI message context: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("begin AI message: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var authorized bool
	if err := tx.QueryRow(ctx, `
        SELECT p.ai_enabled
        FROM ai_conversations c JOIN projects p ON p.id = c.project_id
        WHERE c.id = $1 AND c.project_id = $2 AND c.created_by = $3 AND p.deleted_at IS NULL
        FOR UPDATE`, conversationID, projectID, actorID,
	).Scan(&authorized); errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrConversationNotFound
	} else if err != nil {
		return Message{}, fmt.Errorf("authorize AI message: %w", err)
	}
	if !authorized {
		return Message{}, ErrProjectDisabled
	}
	var message Message
	err = tx.QueryRow(ctx, `
        INSERT INTO ai_messages(id, conversation_id, role, content, context_summary)
        VALUES ($1, $2, $3, $4, $5::jsonb)
        RETURNING id, conversation_id, role, content, context_summary, created_at`,
		messageID, conversationID, role, content, string(encoded),
	).Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &encoded, &message.CreatedAt)
	if err != nil {
		return Message{}, fmt.Errorf("insert AI message: %w", err)
	}
	message.ContextSummary = summary
	if _, err := tx.Exec(ctx, `UPDATE ai_conversations SET updated_at = now() WHERE id = $1`, conversationID); err != nil {
		return Message{}, fmt.Errorf("touch AI conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("commit AI message: %w", err)
	}
	return message, nil
}

// InsertExchange persists a completed user/assistant turn atomically. Provider
// failures therefore do not leave unanswered user messages in the history.
func (s *Store) InsertExchange(ctx context.Context, actorID, projectID, conversationID, userContent, assistantContent string, summary map[string]any) (Message, error) {
	userMessageID, err := id.New()
	if err != nil {
		return Message{}, fmt.Errorf("create AI user message ID: %w", err)
	}
	assistantMessageID, err := id.New()
	if err != nil {
		return Message{}, fmt.Errorf("create AI assistant message ID: %w", err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return Message{}, fmt.Errorf("encode AI message context: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("begin AI exchange: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var authorized bool
	if err := tx.QueryRow(ctx, `
        SELECT p.ai_enabled
        FROM ai_conversations c JOIN projects p ON p.id = c.project_id
        WHERE c.id = $1 AND c.project_id = $2 AND c.created_by = $3 AND p.deleted_at IS NULL
        FOR UPDATE`, conversationID, projectID, actorID,
	).Scan(&authorized); errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrConversationNotFound
	} else if err != nil {
		return Message{}, fmt.Errorf("authorize AI exchange: %w", err)
	}
	if !authorized {
		return Message{}, ErrProjectDisabled
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO ai_messages(id, conversation_id, role, content, context_summary, created_at)
        VALUES ($1, $2, 'user', $3, '{}'::jsonb, clock_timestamp())`,
		userMessageID, conversationID, userContent,
	); err != nil {
		return Message{}, fmt.Errorf("insert AI user message: %w", err)
	}
	var message Message
	err = tx.QueryRow(ctx, `
        INSERT INTO ai_messages(id, conversation_id, role, content, context_summary, created_at)
        VALUES ($1, $2, 'assistant', $3, $4::jsonb, clock_timestamp())
        RETURNING id, conversation_id, role, content, context_summary, created_at`,
		assistantMessageID, conversationID, assistantContent, string(encoded),
	).Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &encoded, &message.CreatedAt)
	if err != nil {
		return Message{}, fmt.Errorf("insert AI assistant message: %w", err)
	}
	message.ContextSummary = summary
	if _, err := tx.Exec(ctx, `UPDATE ai_conversations SET updated_at = now() WHERE id = $1`, conversationID); err != nil {
		return Message{}, fmt.Errorf("touch AI conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("commit AI exchange: %w", err)
	}
	return message, nil
}

func (s *Store) DeleteConversation(ctx context.Context, actorID, projectID, conversationID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin AI conversation deletion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `DELETE FROM ai_conversations WHERE id = $1 AND project_id = $2 AND created_by = $3`, conversationID, projectID, actorID)
	if err != nil {
		return fmt.Errorf("delete AI conversation: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrConversationNotFound
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.conversation_deleted", "project", projectID, map[string]any{"conversation_id": conversationID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit AI conversation deletion: %w", err)
	}
	return nil
}

type schemaFieldSummary struct {
	VersionNumber int64  `json:"version_number"`
	PageTitle     string `json:"page_title"`
	FieldID       string `json:"field_id"`
	FieldType     string `json:"field_type"`
	Label         string `json:"label"`
}

func (s *Store) SchemaSummary(ctx context.Context, projectID string) ([]schemaFieldSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT version_number, schema FROM form_versions WHERE project_id = $1 ORDER BY version_number DESC LIMIT 20`, projectID)
	if err != nil {
		return nil, fmt.Errorf("load AI form schemas: %w", err)
	}
	defer rows.Close()
	result := make([]schemaFieldSummary, 0)
	for rows.Next() {
		var number int64
		var encoded []byte
		if err := rows.Scan(&number, &encoded); err != nil {
			return nil, fmt.Errorf("scan AI form schema: %w", err)
		}
		var schema form.Schema
		if err := json.Unmarshal(encoded, &schema); err != nil {
			return nil, fmt.Errorf("decode AI form schema: %w", err)
		}
		for _, page := range schema.Pages {
			for _, field := range page.Fields {
				result = append(result, schemaFieldSummary{
					VersionNumber: number, PageTitle: page.Title, FieldID: field.ID,
					FieldType: string(field.Type), Label: field.Label,
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI form schemas: %w", err)
	}
	return result, nil
}

func (s *Store) SampleText(ctx context.Context, projectID, fieldID string, limit, maxCharacters int) ([]string, error) {
	var allowed bool
	if err := s.pool.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM form_versions v,
                 jsonb_array_elements(v.schema->'pages') page,
                 jsonb_array_elements(page->'fields') field
            WHERE v.project_id = $1 AND field->>'id' = $2
              AND field->>'type' IN ('short_text', 'long_text')
        )`, projectID, fieldID).Scan(&allowed); err != nil {
		return nil, fmt.Errorf("validate AI text field: %w", err)
	}
	if !allowed {
		return nil, ErrInvalidMessage
	}
	rows, err := s.pool.Query(ctx, `
        SELECT left(a.value_text, $4)
        FROM submission_answers a
        JOIN submissions s ON s.id = a.submission_id
        WHERE s.project_id = $1 AND s.deleted_at IS NULL AND a.field_id::text = $2
          AND a.field_type IN ('short_text', 'long_text') AND a.value_text IS NOT NULL
        ORDER BY s.submitted_at DESC LIMIT $3`, projectID, fieldID, limit, maxCharacters)
	if err != nil {
		return nil, fmt.Errorf("sample AI text answers: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0, limit)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan AI text sample: %w", err)
		}
		result = append(result, strings.TrimSpace(value))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI text samples: %w", err)
	}
	return result, nil
}

func dateBounds(from, to string) (*time.Time, *time.Time, error) {
	var fromTime, toTime *time.Time
	// Some OpenAI-compatible providers serialize a nullable date as the string
	// "null" even when the JSON schema requests a literal null. Treat that
	// common compatibility quirk as an omitted optional bound.
	if strings.EqualFold(strings.TrimSpace(from), "null") {
		from = ""
	}
	if strings.EqualFold(strings.TrimSpace(to), "null") {
		to = ""
	}
	if from != "" {
		parsed, err := time.Parse(time.DateOnly, from)
		if err != nil {
			return nil, nil, ErrInvalidMessage
		}
		fromTime = &parsed
	}
	if to != "" {
		parsed, err := time.Parse(time.DateOnly, to)
		if err != nil {
			return nil, nil, ErrInvalidMessage
		}
		toTime = &parsed
	}
	if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
		return nil, nil, ErrInvalidMessage
	}
	return fromTime, toTime, nil
}

func (s *Store) SaveProviderSettings(ctx context.Context, actorID string, input SaveProviderInput) (ProviderSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProviderSettings{}, fmt.Errorf("begin AI provider update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var saved ProviderSettings
	err = tx.QueryRow(ctx, `
        UPDATE ai_provider_settings
		SET base_url = $1, api_mode = $2, model = $3, timeout_seconds = $4,
		    max_output_tokens = $5, allow_private_endpoint = $6,
		    compatibility_tested_at = NULL, updated_by = $7, updated_at = now()
        WHERE singleton = true
        RETURNING base_url, api_mode, model, timeout_seconds, max_output_tokens,
                  allow_private_endpoint, updated_at`,
		input.BaseURL, input.Mode, input.Model, input.TimeoutSeconds, input.MaxOutputTokens,
		input.AllowPrivateEndpoint, actorID,
	).Scan(&saved.BaseURL, &saved.Mode, &saved.Model, &saved.TimeoutSeconds,
		&saved.MaxOutputTokens, &saved.AllowPrivateEndpoint, &saved.UpdatedAt)
	if err != nil {
		return ProviderSettings{}, fmt.Errorf("save AI provider settings: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.provider_updated", "settings", "ai_provider", map[string]any{
		"api_mode": input.Mode, "model": input.Model, "allow_private_endpoint": input.AllowPrivateEndpoint,
	}); err != nil {
		return ProviderSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProviderSettings{}, fmt.Errorf("commit AI provider update: %w", err)
	}
	return saved, nil
}

func (s *Store) ProjectSettings(ctx context.Context, projectID string) (ProjectSettings, error) {
	var settings ProjectSettings
	err := s.pool.QueryRow(ctx, `
        SELECT id, ai_enabled, ai_text_sampling_enabled
        FROM projects WHERE id = $1 AND deleted_at IS NULL`, projectID,
	).Scan(&settings.ProjectID, &settings.Enabled, &settings.TextSamplingEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectSettings{}, project.ErrNotFound
	}
	if err != nil {
		return ProjectSettings{}, fmt.Errorf("load project AI settings: %w", err)
	}
	return settings, nil
}

func (s *Store) SaveProjectSettings(ctx context.Context, actorID, projectID string, input SaveProjectSettingsInput) (ProjectSettings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProjectSettings{}, fmt.Errorf("begin project AI update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var saved ProjectSettings
	err = tx.QueryRow(ctx, `
        UPDATE projects
        SET ai_enabled = $2, ai_text_sampling_enabled = $3, updated_by = $4, updated_at = now()
        WHERE id = $1 AND deleted_at IS NULL
        RETURNING id, ai_enabled, ai_text_sampling_enabled`,
		projectID, input.Enabled, input.TextSamplingEnabled, actorID,
	).Scan(&saved.ProjectID, &saved.Enabled, &saved.TextSamplingEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectSettings{}, project.ErrNotFound
	}
	if err != nil {
		return ProjectSettings{}, fmt.Errorf("save project AI settings: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "ai.project_settings_updated", "project", projectID, map[string]any{
		"enabled": input.Enabled, "text_sampling_enabled": input.TextSamplingEnabled,
	}); err != nil {
		return ProjectSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectSettings{}, fmt.Errorf("commit project AI update: %w", err)
	}
	return saved, nil
}
