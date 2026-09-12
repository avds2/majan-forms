CREATE TABLE ai_provider_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    base_url text NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_mode text NOT NULL DEFAULT 'responses' CHECK (api_mode IN ('responses', 'chat_completions')),
    model text NOT NULL DEFAULT '',
    timeout_seconds integer NOT NULL DEFAULT 60 CHECK (timeout_seconds BETWEEN 5 AND 180),
    max_output_tokens integer NOT NULL DEFAULT 2048 CHECK (max_output_tokens BETWEEN 128 AND 32768),
    allow_private_endpoint boolean NOT NULL DEFAULT false,
    compatibility_tested_at timestamptz,
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO ai_provider_settings(singleton) VALUES (true);

ALTER TABLE projects
    ADD COLUMN ai_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN ai_text_sampling_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE ai_conversations (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by uuid NOT NULL REFERENCES users(id),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ai_conversations_project_updated_idx
    ON ai_conversations(project_id, updated_at DESC);

CREATE TABLE ai_messages (
    id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 100000),
    context_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_messages_context_object CHECK (jsonb_typeof(context_summary) = 'object')
);

CREATE INDEX ai_messages_conversation_created_idx
    ON ai_messages(conversation_id, created_at);
