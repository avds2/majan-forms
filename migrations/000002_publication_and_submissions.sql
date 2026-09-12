ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check
    CHECK (status IN ('draft', 'published', 'closed', 'trash'));

CREATE TABLE form_versions (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_number bigint NOT NULL CHECK (version_number > 0),
    schema jsonb NOT NULL,
    checksum text NOT NULL CHECK (length(checksum) = 64),
    publish_key_hash bytea NOT NULL,
    published_by uuid NOT NULL REFERENCES users(id),
    published_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT form_versions_schema_object CHECK (jsonb_typeof(schema) = 'object'),
    UNIQUE (project_id, version_number),
    UNIQUE (project_id, publish_key_hash)
);

ALTER TABLE projects ADD COLUMN active_version_id uuid REFERENCES form_versions(id);
CREATE INDEX form_versions_project_published_idx ON form_versions(project_id, published_at DESC);

CREATE FUNCTION reject_form_version_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'published form versions are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER form_versions_immutable
    BEFORE UPDATE OR DELETE ON form_versions
    FOR EACH ROW EXECUTE FUNCTION reject_form_version_mutation();

CREATE TABLE public_sessions (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_id uuid NOT NULL REFERENCES form_versions(id),
    answers jsonb NOT NULL DEFAULT '{}'::jsonb,
    history jsonb NOT NULL DEFAULT '[]'::jsonb,
    current_page_id uuid NOT NULL,
    started_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    submitted_at timestamptz,
    CONSTRAINT public_sessions_answers_object CHECK (jsonb_typeof(answers) = 'object'),
    CONSTRAINT public_sessions_history_array CHECK (jsonb_typeof(history) = 'array')
);

CREATE INDEX public_sessions_expiry_idx ON public_sessions(expires_at) WHERE submitted_at IS NULL;
CREATE INDEX public_sessions_project_version_idx ON public_sessions(project_id, version_id);

CREATE TABLE submissions (
    id uuid PRIMARY KEY,
    public_id uuid NOT NULL UNIQUE,
    public_session_id uuid NOT NULL UNIQUE REFERENCES public_sessions(id),
    project_id uuid NOT NULL REFERENCES projects(id),
    version_id uuid NOT NULL REFERENCES form_versions(id),
    ending_id uuid NOT NULL,
    idempotency_key_hash bytea NOT NULL,
    started_at timestamptz NOT NULL,
    submitted_at timestamptz NOT NULL DEFAULT now(),
    fingerprint_hash bytea,
    deleted_at timestamptz,
    UNIQUE (project_id, idempotency_key_hash)
);

CREATE INDEX submissions_project_time_idx ON submissions(project_id, submitted_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX submissions_version_time_idx ON submissions(version_id, submitted_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE submission_answers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    submission_id uuid NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    field_id uuid NOT NULL,
    field_type text NOT NULL,
    value_text text,
    value_number numeric,
    value_bool boolean,
    value_date date,
    value_time time,
    value_timestamp timestamptz,
    value_json jsonb,
    UNIQUE (submission_id, field_id),
    CONSTRAINT submission_answers_one_value CHECK (
        num_nonnulls(value_text, value_number, value_bool, value_date, value_time, value_timestamp, value_json) = 1
    )
);

CREATE INDEX submission_answers_field_text_idx ON submission_answers(field_id, value_text) WHERE value_text IS NOT NULL;
CREATE INDEX submission_answers_field_number_idx ON submission_answers(field_id, value_number) WHERE value_number IS NOT NULL;
CREATE INDEX submission_answers_field_date_idx ON submission_answers(field_id, value_date) WHERE value_date IS NOT NULL;

CREATE TABLE stored_files (
    id uuid PRIMARY KEY,
    public_session_id uuid REFERENCES public_sessions(id) ON DELETE CASCADE,
    submission_id uuid REFERENCES submissions(id) ON DELETE CASCADE,
    field_id uuid NOT NULL,
    storage_key text NOT NULL UNIQUE,
    original_name text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    detected_mime text NOT NULL,
    extension text NOT NULL,
    checksum text NOT NULL CHECK (length(checksum) = 64),
    status text NOT NULL CHECK (status IN ('staging', 'finalized', 'rejected')),
    created_at timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz,
    CONSTRAINT stored_files_single_owner CHECK (num_nonnulls(public_session_id, submission_id) = 1)
);

CREATE INDEX stored_files_session_idx ON stored_files(public_session_id) WHERE public_session_id IS NOT NULL;
CREATE INDEX stored_files_submission_idx ON stored_files(submission_id) WHERE submission_id IS NOT NULL;
CREATE INDEX stored_files_staging_idx ON stored_files(created_at) WHERE status = 'staging';

CREATE TABLE analytics_daily (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_id uuid NOT NULL REFERENCES form_versions(id) ON DELETE CASCADE,
    event_date date NOT NULL,
    page_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    views bigint NOT NULL DEFAULT 0 CHECK (views >= 0),
    starts bigint NOT NULL DEFAULT 0 CHECK (starts >= 0),
    steps bigint NOT NULL DEFAULT 0 CHECK (steps >= 0),
    completes bigint NOT NULL DEFAULT 0 CHECK (completes >= 0),
    PRIMARY KEY (project_id, version_id, event_date, page_id)
);
