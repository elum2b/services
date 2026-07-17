CREATE TYPE promo_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE promo_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');

CREATE TABLE IF NOT EXISTS promo_offer (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    code VARCHAR(255) NOT NULL,
    code_normalized VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    target JSONB NULL,
    max_activations BIGINT NOT NULL DEFAULT 0,
    activation_count BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    start_at TIMESTAMPTZ NULL,
    end_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, code_normalized),
    UNIQUE (workspace_id, id),
    CONSTRAINT promo_offer_window_chk CHECK (
        start_at IS NULL OR end_at IS NULL OR start_at < end_at
    ),
    CONSTRAINT promo_offer_activation_chk CHECK (
        max_activations >= 0 AND activation_count >= 0
    )
);

CREATE INDEX IF NOT EXISTS promo_offer_list_idx
    ON promo_offer (workspace_id, created_at DESC, id);

CREATE TABLE IF NOT EXISTS promo_localization (
    workspace_id VARCHAR(36) NOT NULL,
    promo_id BIGINT NOT NULL,
    locale VARCHAR(16) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, promo_id, locale),
    CONSTRAINT promo_localization_offer_fk
        FOREIGN KEY (workspace_id, promo_id)
        REFERENCES promo_offer (workspace_id, id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS promo_reward (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    promo_id BIGINT NOT NULL,
    reward_key VARCHAR(128) NOT NULL,
    reward_type promo_reward_type NOT NULL DEFAULT 'quantity',
    quantity BIGINT NOT NULL,
    scale SMALLINT NOT NULL DEFAULT 0,
    duration_unit promo_duration_unit NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, promo_id, reward_key),
    CONSTRAINT promo_reward_offer_fk
        FOREIGN KEY (workspace_id, promo_id)
        REFERENCES promo_offer (workspace_id, id) ON DELETE RESTRICT,
    CONSTRAINT promo_reward_quantity_chk CHECK (quantity > 0),
    CONSTRAINT promo_reward_scale_chk CHECK (scale >= 0),
    CONSTRAINT promo_reward_type_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL)
        OR (reward_type = 'duration' AND duration_unit IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS promo_reward_list_idx
    ON promo_reward (workspace_id, promo_id, id);

CREATE TABLE IF NOT EXISTS promo_redemption (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    promo_id BIGINT NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(255) NOT NULL,
    reward_snapshot JSONB NOT NULL,
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, promo_id, app_id, platform_id, platform_user_id),
    UNIQUE (workspace_id, promo_id, id),
    CONSTRAINT promo_redemption_offer_fk
        FOREIGN KEY (workspace_id, promo_id)
        REFERENCES promo_offer (workspace_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS promo_redemption_stats_idx
    ON promo_redemption (workspace_id, promo_id, redeemed_at);
CREATE INDEX IF NOT EXISTS promo_redemption_user_idx
    ON promo_redemption (workspace_id, app_id, platform_id, platform_user_id, redeemed_at);

CREATE TABLE IF NOT EXISTS promo_redemption_event (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    promo_id BIGINT NOT NULL,
    redemption_id BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (redemption_id),
    CONSTRAINT promo_redemption_event_redemption_fk
        FOREIGN KEY (workspace_id, promo_id, redemption_id)
        REFERENCES promo_redemption (workspace_id, promo_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS promo_redemption_event_stats_idx
    ON promo_redemption_event (workspace_id, promo_id, occurred_at);
CREATE INDEX IF NOT EXISTS promo_redemption_event_occurred_idx
    ON promo_redemption_event (occurred_at, workspace_id, promo_id);

CREATE TABLE IF NOT EXISTS promo_stats_daily (
    workspace_id VARCHAR(36) NOT NULL,
    promo_id BIGINT NOT NULL,
    stats_date DATE NOT NULL,
    redemption_count BIGINT NOT NULL DEFAULT 0,
    unique_users BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, promo_id, stats_date)
);

CREATE TABLE IF NOT EXISTS promo_clb_event (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    source_service VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    payload BYTEA NOT NULL,
    payload_content_type VARCHAR(64) NOT NULL DEFAULT 'application/json',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by VARCHAR(128) NULL,
    locked_until TIMESTAMPTZ NULL,
    delivered_at TIMESTAMPTZ NULL,
    rejected_at TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    reject_reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT promo_clb_event_status_chk CHECK (status IN ('pending', 'processing', 'ok', 'reject')),
    CONSTRAINT promo_clb_event_event_key_uq UNIQUE (source_service, event_key),
    CONSTRAINT promo_clb_event_idempotency_key_uq UNIQUE (idempotency_key)
);

-- Existing installations may have created this table before workspace scoping.
ALTER TABLE promo_clb_event
    ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36) NOT NULL
        DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE promo_clb_event
    ALTER COLUMN workspace_id DROP DEFAULT;

CREATE INDEX IF NOT EXISTS promo_clb_event_due_idx
    ON promo_clb_event (status, next_attempt_at, locked_until, id);

CREATE INDEX IF NOT EXISTS promo_clb_event_type_idx
    ON promo_clb_event (workspace_id, event_type, status, created_at);
