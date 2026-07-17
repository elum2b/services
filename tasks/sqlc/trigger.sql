DROP TRIGGER IF EXISTS task_progress_create_stats ON task_progress;
DROP TRIGGER IF EXISTS task_progress_update_stats ON task_progress;
DROP FUNCTION IF EXISTS task_progress_create_stats_fn();
DROP FUNCTION IF EXISTS task_progress_update_stats_fn();

CREATE FUNCTION task_progress_create_stats_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_stats_event (
        workspace_id,
        task_id,
        progress_id,
        app_id,
        platform_id,
        platform_user_id,
        event_type,
        claim_mode,
        amount,
        occurred_at
    )
    SELECT
        NEW.workspace_id,
        NEW.task_id,
        NEW.id,
        NEW.app_id,
        NEW.platform_id,
        NEW.platform_user_id,
        'progress_created',
        NULL,
        0,
        NEW.created_at
    UNION ALL
    SELECT
        NEW.workspace_id,
        NEW.task_id,
        NEW.id,
        NEW.app_id,
        NEW.platform_id,
        NEW.platform_user_id,
        'progress_added',
        NULL,
        NEW.progress,
        NEW.updated_at
    WHERE NEW.progress > 0
    UNION ALL
    SELECT
        NEW.workspace_id,
        NEW.task_id,
        NEW.id,
        NEW.app_id,
        NEW.platform_id,
        NEW.platform_user_id,
        NEW.status,
        CASE WHEN NEW.status = 'claimed' THEN definition.claim_mode ELSE NULL END,
        0,
        COALESCE(NEW.claimed_at, NEW.ready_at, NEW.updated_at)
    FROM task_definition definition
    WHERE definition.workspace_id = NEW.workspace_id
      AND definition.id = NEW.task_id
      AND NEW.status IN ('ready', 'claimed');

    RETURN NEW;
END;
$$;

CREATE FUNCTION task_progress_update_stats_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_stats_event (
        workspace_id,
        task_id,
        progress_id,
        app_id,
        platform_id,
        platform_user_id,
        event_type,
        claim_mode,
        amount,
        occurred_at
    )
    SELECT
        NEW.workspace_id,
        NEW.task_id,
        NEW.id,
        NEW.app_id,
        NEW.platform_id,
        NEW.platform_user_id,
        'progress_added',
        NULL,
        NEW.progress - OLD.progress,
        NEW.updated_at
    WHERE NEW.progress > OLD.progress
    UNION ALL
    SELECT
        NEW.workspace_id,
        NEW.task_id,
        NEW.id,
        NEW.app_id,
        NEW.platform_id,
        NEW.platform_user_id,
        NEW.status,
        CASE WHEN NEW.status = 'claimed' THEN definition.claim_mode ELSE NULL END,
        0,
        COALESCE(NEW.claimed_at, NEW.ready_at, NEW.updated_at)
    FROM task_definition definition
    WHERE definition.workspace_id = NEW.workspace_id
      AND definition.id = NEW.task_id
      AND NEW.status IN ('ready', 'claimed')
      AND NEW.status <> OLD.status;

    RETURN NEW;
END;
$$;

CREATE TRIGGER task_progress_create_stats
AFTER INSERT ON task_progress
FOR EACH ROW
EXECUTE FUNCTION task_progress_create_stats_fn();

CREATE TRIGGER task_progress_update_stats
AFTER UPDATE ON task_progress
FOR EACH ROW
EXECUTE FUNCTION task_progress_update_stats_fn();

INSERT INTO task_stats_event (
    workspace_id,
    task_id,
    progress_id,
    app_id,
    platform_id,
    platform_user_id,
    event_type,
    claim_mode,
    amount,
    occurred_at
)
SELECT
    progress.workspace_id,
    progress.task_id,
    progress.id,
    progress.app_id,
    progress.platform_id,
    progress.platform_user_id,
    'progress_created',
    NULL,
    0,
    progress.created_at
FROM task_progress progress
WHERE NOT EXISTS (
    SELECT 1
    FROM task_stats_event event_rows
    WHERE event_rows.progress_id = progress.id
      AND event_rows.event_type = 'progress_created'
);

INSERT INTO task_stats_event (
    workspace_id,
    task_id,
    progress_id,
    app_id,
    platform_id,
    platform_user_id,
    event_type,
    claim_mode,
    amount,
    occurred_at
)
SELECT
    progress.workspace_id,
    progress.task_id,
    progress.id,
    progress.app_id,
    progress.platform_id,
    progress.platform_user_id,
    'progress_added',
    NULL,
    progress.progress,
    progress.updated_at
FROM task_progress progress
WHERE progress.progress > 0
  AND NOT EXISTS (
      SELECT 1
      FROM task_stats_event event_rows
      WHERE event_rows.progress_id = progress.id
        AND event_rows.event_type = 'progress_added'
  );

INSERT INTO task_stats_event (
    workspace_id,
    task_id,
    progress_id,
    app_id,
    platform_id,
    platform_user_id,
    event_type,
    claim_mode,
    amount,
    occurred_at
)
SELECT
    progress.workspace_id,
    progress.task_id,
    progress.id,
    progress.app_id,
    progress.platform_id,
    progress.platform_user_id,
    progress.status,
    CASE WHEN progress.status = 'claimed' THEN definition.claim_mode ELSE NULL END,
    0,
    COALESCE(progress.claimed_at, progress.ready_at, progress.updated_at)
FROM task_progress progress
JOIN task_definition definition
  ON definition.workspace_id = progress.workspace_id
 AND definition.id = progress.task_id
WHERE progress.status IN ('ready', 'claimed')
  AND NOT EXISTS (
      SELECT 1
      FROM task_stats_event event_rows
      WHERE event_rows.progress_id = progress.id
        AND event_rows.event_type = progress.status
  );
