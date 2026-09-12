ALTER TABLE submissions
    ADD COLUMN search_text text NOT NULL DEFAULT '';

UPDATE submissions s
SET search_text = lower(concat_ws(' ',
    s.public_id::text,
    (
        SELECT string_agg(
            coalesce(
                a.value_text,
                a.value_number::text,
                a.value_bool::text,
                a.value_date::text,
                a.value_time::text,
                a.value_timestamp::text,
                a.value_json::text
            ),
            ' ' ORDER BY a.id
        )
        FROM submission_answers a
        WHERE a.submission_id = s.id
    )
));

CREATE INDEX submissions_search_idx
    ON submissions USING gin(to_tsvector('simple', search_text));

CREATE INDEX submissions_deleted_purge_idx
    ON submissions(deleted_at)
    WHERE deleted_at IS NOT NULL;

CREATE INDEX submission_answers_submission_idx
    ON submission_answers(submission_id, id);
