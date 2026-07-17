CREATE TABLE IF NOT EXISTS calendar_definition (
    id CHAR(36) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    type VARCHAR(64) NOT NULL,
    mode VARCHAR(32) NOT NULL,
    interval_type VARCHAR(32) NOT NULL,
    interval_unit VARCHAR(32) NOT NULL,
    interval_count INTEGER NOT NULL DEFAULT 1,
    reset_after_intervals INTEGER NOT NULL DEFAULT 1,
    end_behavior VARCHAR(32) NOT NULL DEFAULT 'stop',
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    hide_future_rewards BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    start_at TIMESTAMPTZ NULL,
    end_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT calendar_definition_type_uq UNIQUE (workspace_id, type),
    CONSTRAINT calendar_definition_workspace_id_uq UNIQUE (workspace_id, id),
    CONSTRAINT calendar_definition_mode_chk CHECK (mode IN ('interval', 'sequential', 'sequential_reset')),
    CONSTRAINT calendar_definition_interval_type_chk CHECK (interval_type IN ('calendar', 'floating')),
    CONSTRAINT calendar_definition_interval_unit_chk CHECK (interval_unit IN ('second', 'minute', 'hour', 'day', 'week', 'month')),
    CONSTRAINT calendar_definition_end_behavior_chk CHECK (end_behavior IN ('restart', 'repeat_last', 'stop')),
    CONSTRAINT calendar_definition_interval_count_chk CHECK (interval_count > 0),
    CONSTRAINT calendar_definition_reset_count_chk CHECK (reset_after_intervals > 0),
    CONSTRAINT calendar_definition_window_chk CHECK (start_at IS NULL OR end_at IS NULL OR start_at < end_at)
);

CREATE INDEX IF NOT EXISTS calendar_definition_active_idx
    ON calendar_definition (workspace_id, is_active, deleted_at, start_at, end_at, created_at);

CREATE TABLE IF NOT EXISTS calendar_localization (
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, calendar_id, locale),
    CONSTRAINT calendar_localization_definition_fk
        FOREIGN KEY (workspace_id, calendar_id)
        REFERENCES calendar_definition (workspace_id, id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS calendar_step (
    id BIGSERIAL NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    position INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT calendar_step_position_uq UNIQUE (workspace_id, calendar_id, position),
    CONSTRAINT calendar_step_workspace_id_uq UNIQUE (workspace_id, calendar_id, id),
    CONSTRAINT calendar_step_position_chk CHECK (position > 0),
    CONSTRAINT calendar_step_definition_fk
        FOREIGN KEY (workspace_id, calendar_id)
        REFERENCES calendar_definition (workspace_id, id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS calendar_reward (
    id BIGSERIAL NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    step_id BIGINT NOT NULL,
    item_key VARCHAR(128) NOT NULL,
    reward_type VARCHAR(32) NOT NULL DEFAULT 'quantity',
    item_count BIGINT NOT NULL,
    scale SMALLINT NOT NULL DEFAULT 0,
    duration_unit VARCHAR(32) NULL,
    position INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT calendar_reward_key_uq UNIQUE (workspace_id, calendar_id, step_id, item_key),
    CONSTRAINT calendar_reward_count_chk CHECK (item_count > 0),
    CONSTRAINT calendar_reward_type_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL)
        OR (reward_type = 'duration' AND duration_unit IS NOT NULL)
    ),
    CONSTRAINT calendar_reward_duration_unit_chk CHECK (
        duration_unit IS NULL OR duration_unit IN ('second', 'minute', 'hour', 'day', 'week', 'month', 'year')
    ),
    CONSTRAINT calendar_reward_position_chk CHECK (position > 0),
    CONSTRAINT calendar_reward_step_fk
        FOREIGN KEY (workspace_id, calendar_id, step_id)
        REFERENCES calendar_step (workspace_id, calendar_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS calendar_reward_list_idx
    ON calendar_reward (workspace_id, calendar_id, step_id, position, id);

CREATE TABLE IF NOT EXISTS calendar_progress (
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(255) NOT NULL,
    current_position INTEGER NOT NULL DEFAULT 0,
    claim_count BIGINT NOT NULL DEFAULT 0,
    last_claim_position INTEGER NULL,
    last_claim_at TIMESTAMPTZ NULL,
    next_claim_at TIMESTAMPTZ NULL,
    is_completed BOOLEAN NOT NULL DEFAULT FALSE,
    reset_count BIGINT NOT NULL DEFAULT 0,
    last_was_reset BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, calendar_id, app_id, platform_id, platform_user_id),
    CONSTRAINT calendar_progress_definition_fk
        FOREIGN KEY (workspace_id, calendar_id)
        REFERENCES calendar_definition (workspace_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS calendar_progress_user_idx
    ON calendar_progress (workspace_id, app_id, platform_id, platform_user_id, updated_at);

CREATE TABLE IF NOT EXISTS calendar_operation (
    id BIGSERIAL NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(255) NOT NULL,
    operation_id VARCHAR(128) NOT NULL,
    granted BOOLEAN NOT NULL,
    status VARCHAR(32) NOT NULL,
    position INTEGER NULL,
    rewards_snapshot JSONB NOT NULL,
    current_position INTEGER NOT NULL,
    claim_count BIGINT NOT NULL,
    last_claim_position INTEGER NULL,
    last_claim_at TIMESTAMPTZ NULL,
    next_claim_at TIMESTAMPTZ NULL,
    is_completed BOOLEAN NOT NULL,
    reset_count BIGINT NOT NULL,
    was_reset BOOLEAN NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT calendar_operation_idempotency_uq UNIQUE (
        workspace_id, calendar_id, app_id, platform_id, platform_user_id, operation_id
    ),
    CONSTRAINT calendar_operation_definition_fk
        FOREIGN KEY (workspace_id, calendar_id)
        REFERENCES calendar_definition (workspace_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS calendar_operation_stats_idx
    ON calendar_operation (workspace_id, calendar_id, occurred_at, granted);

CREATE INDEX IF NOT EXISTS calendar_operation_user_idx
    ON calendar_operation (workspace_id, app_id, platform_id, platform_user_id, occurred_at);

CREATE TABLE IF NOT EXISTS calendar_stats_daily (
    workspace_id VARCHAR(36) NOT NULL,
    calendar_id CHAR(36) NOT NULL,
    stats_date DATE NOT NULL,
    operation_count BIGINT NOT NULL DEFAULT 0,
    grant_count BIGINT NOT NULL DEFAULT 0,
    unique_users BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, calendar_id, stats_date)
);
