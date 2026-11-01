-- Upgrade old databases once and restore streaks from successful claim history.
DO $migration$
BEGIN
    LOCK TABLE calendar_progress, calendar_operation IN SHARE ROW EXCLUSIVE MODE;
    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'calendar_progress'::regclass
          AND attname = 'streak_count' AND NOT attisdropped
    ) THEN
        ALTER TABLE calendar_progress ADD COLUMN streak_count BIGINT NOT NULL DEFAULT 0 CHECK (streak_count >= 0);
        ALTER TABLE calendar_operation ADD COLUMN streak_count BIGINT NOT NULL DEFAULT 0 CHECK (streak_count >= 0);

        CREATE TEMP TABLE calendar_streak_backfill ON COMMIT DROP AS
        WITH grants AS (
            SELECT o.*, c.interval_type, c.timezone,
                c.interval_count * ('1 ' || c.interval_unit)::interval AS period,
                lag(o.next_claim_at) OVER (
                    PARTITION BY o.workspace_id, o.calendar_id, o.app_id, o.platform_id, o.platform_user_id
                    ORDER BY o.occurred_at, o.id
                ) AS previous_next
            FROM calendar_operation o
            JOIN calendar_definition c ON c.workspace_id = o.workspace_id AND c.id = o.calendar_id
            WHERE o.granted
        ), windows AS (
            SELECT *, CASE WHEN interval_type = 'calendar'
                THEN ((next_claim_at AT TIME ZONE timezone) + period) AT TIME ZONE timezone
                ELSE next_claim_at + period END AS window_end,
                CASE WHEN interval_type = 'calendar'
                THEN ((previous_next AT TIME ZONE timezone) + period) AT TIME ZONE timezone
                ELSE previous_next + period END AS previous_end
            FROM grants
        ), islands AS (
            SELECT *, sum(CASE WHEN previous_end IS NULL OR occurred_at >= previous_end THEN 1 ELSE 0 END) OVER (
                PARTITION BY workspace_id, calendar_id, app_id, platform_id, platform_user_id ORDER BY occurred_at, id
            ) AS island FROM windows
        )
        SELECT *, row_number() OVER (
            PARTITION BY workspace_id, calendar_id, app_id, platform_id, platform_user_id, island ORDER BY occurred_at, id
        ) AS streak FROM islands;

        UPDATE calendar_operation o SET streak_count = CASE
            WHEN o.occurred_at >= d.window_end THEN 0 ELSE d.streak END
        FROM calendar_streak_backfill d
        WHERE o.workspace_id = d.workspace_id AND o.calendar_id = d.calendar_id
          AND o.app_id = d.app_id AND o.platform_id = d.platform_id AND o.platform_user_id = d.platform_user_id
          AND o.last_claim_at = d.occurred_at;

        UPDATE calendar_progress p SET streak_count = d.streak
        FROM calendar_streak_backfill d
        WHERE p.workspace_id = d.workspace_id AND p.calendar_id = d.calendar_id
          AND p.app_id = d.app_id AND p.platform_id = d.platform_id AND p.platform_user_id = d.platform_user_id
          AND p.last_claim_at = d.occurred_at;
    END IF;
END
$migration$;
