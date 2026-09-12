ALTER TABLE projects
    ADD COLUMN status_before_trash text
    CHECK (status_before_trash IS NULL OR status_before_trash IN ('draft', 'published', 'closed'));

UPDATE projects
SET status_before_trash = CASE WHEN active_version_id IS NULL THEN 'draft' ELSE 'published' END
WHERE status = 'trash' AND status_before_trash IS NULL;
