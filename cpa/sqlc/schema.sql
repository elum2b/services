CREATE TYPE cpa_code_mode AS ENUM ('shared_code', 'personal_code');
CREATE TYPE cpa_code_source AS ENUM ('generated', 'pool');
CREATE TYPE cpa_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE cpa_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');
CREATE TYPE cpa_code_status AS ENUM ('available', 'issued', 'completed', 'deleted');
CREATE TYPE cpa_assignment_status AS ENUM ('issued', 'completed');
CREATE TYPE cpa_assignment_event_type AS ENUM ('issued', 'completed');

CREATE TABLE IF NOT EXISTS cpa_offer (
    workspace_id VARCHAR(36) NOT NULL,
    id VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    target JSONB NULL,
    code_mode cpa_code_mode NOT NULL,
    code_source cpa_code_source NULL,
    shared_code VARCHAR(512) NULL,
    generated_length SMALLINT NULL,
    generated_alphabet VARCHAR(512) NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    start_at TIMESTAMPTZ NULL,
    end_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id),
    CONSTRAINT cpa_offer_code_config_chk CHECK (
        (
            code_mode = 'shared_code'
            AND shared_code IS NOT NULL
            AND shared_code <> ''
            AND code_source IS NULL
            AND generated_length IS NULL
            AND generated_alphabet IS NULL
        )
        OR
        (
            code_mode = 'personal_code'
            AND shared_code IS NULL
            AND (
                (
                    code_source = 'pool'
                    AND generated_length IS NULL
                    AND generated_alphabet IS NULL
                )
                OR
                (
                    code_source = 'generated'
                    AND generated_length BETWEEN 1 AND 512
                    AND char_length(generated_alphabet) >= 2
                )
            )
        )
    ),
    CONSTRAINT cpa_offer_window_chk CHECK (
        start_at IS NULL OR end_at IS NULL OR start_at < end_at
    )
);

CREATE INDEX IF NOT EXISTS cpa_offer_active_idx
    ON cpa_offer (workspace_id, is_active, start_at, end_at);
CREATE INDEX IF NOT EXISTS cpa_offer_list_idx
    ON cpa_offer (workspace_id, created_at DESC, id);
CREATE INDEX IF NOT EXISTS cpa_offer_active_list_idx
    ON cpa_offer (workspace_id, is_active, created_at DESC, id);

CREATE TABLE IF NOT EXISTS cpa_localization (
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, cpa_id, locale),
    CONSTRAINT cpa_localization_offer_fk
        FOREIGN KEY (workspace_id, cpa_id)
        REFERENCES cpa_offer (workspace_id, id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cpa_reward (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    reward_key VARCHAR(128) NOT NULL,
    reward_type cpa_reward_type NOT NULL DEFAULT 'quantity',
    quantity BIGINT NOT NULL DEFAULT 1,
    scale INTEGER NOT NULL DEFAULT 0,
    duration_unit cpa_duration_unit NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, cpa_id, reward_key),
    CONSTRAINT cpa_reward_offer_fk
        FOREIGN KEY (workspace_id, cpa_id)
        REFERENCES cpa_offer (workspace_id, id)
        ON DELETE CASCADE,
    CONSTRAINT cpa_reward_quantity_chk CHECK (quantity > 0),
    CONSTRAINT cpa_reward_scale_chk CHECK (scale BETWEEN 0 AND 65535),
    CONSTRAINT cpa_reward_type_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL)
        OR (reward_type = 'duration' AND duration_unit IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS cpa_reward_list_idx
    ON cpa_reward (workspace_id, cpa_id, id);

CREATE TABLE IF NOT EXISTS cpa_code (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    code VARCHAR(512) NOT NULL,
    source cpa_code_source NOT NULL,
    status cpa_code_status NOT NULL DEFAULT 'available',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ NULL,
    UNIQUE (workspace_id, cpa_id, code),
    CONSTRAINT cpa_code_offer_fk
        FOREIGN KEY (workspace_id, cpa_id)
        REFERENCES cpa_offer (workspace_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS cpa_code_available_idx
    ON cpa_code (workspace_id, cpa_id, status, id);

CREATE TABLE IF NOT EXISTS cpa_assignment (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(255) NOT NULL,
    code_id BIGINT NULL UNIQUE,
    code VARCHAR(512) NOT NULL,
    code_mode cpa_code_mode NOT NULL,
    rewards_snapshot JSONB NOT NULL DEFAULT '[]'::jsonb,
    status cpa_assignment_status NOT NULL DEFAULT 'issued',
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, cpa_id, app_id, platform_id, platform_user_id),
    CONSTRAINT cpa_assignment_offer_fk
        FOREIGN KEY (workspace_id, cpa_id)
        REFERENCES cpa_offer (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cpa_assignment_code_fk
        FOREIGN KEY (code_id)
        REFERENCES cpa_code (id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS cpa_assignment_status_idx
    ON cpa_assignment (workspace_id, cpa_id, status, issued_at);
CREATE INDEX IF NOT EXISTS cpa_assignment_user_idx
    ON cpa_assignment (workspace_id, app_id, platform_id, platform_user_id, issued_at);

CREATE TABLE IF NOT EXISTS cpa_assignment_event (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    assignment_id BIGINT NOT NULL,
    event_type cpa_assignment_event_type NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (assignment_id, event_type),
    CONSTRAINT cpa_assignment_event_assignment_fk
        FOREIGN KEY (assignment_id)
        REFERENCES cpa_assignment (id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS cpa_assignment_event_stats_idx
    ON cpa_assignment_event (workspace_id, cpa_id, occurred_at, event_type);

CREATE TABLE IF NOT EXISTS cpa_stats_daily (
    workspace_id VARCHAR(36) NOT NULL,
    cpa_id VARCHAR(128) NOT NULL,
    stats_date DATE NOT NULL,
    issued_count BIGINT NOT NULL DEFAULT 0,
    completed_count BIGINT NOT NULL DEFAULT 0,
    unique_users BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, cpa_id, stats_date),
    CONSTRAINT cpa_stats_daily_issued_chk CHECK (issued_count >= 0),
    CONSTRAINT cpa_stats_daily_completed_chk CHECK (completed_count >= 0),
    CONSTRAINT cpa_stats_daily_unique_chk CHECK (unique_users >= 0)
);

CREATE INDEX IF NOT EXISTS cpa_stats_daily_date_idx
    ON cpa_stats_daily (workspace_id, stats_date, cpa_id);
