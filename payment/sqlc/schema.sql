CREATE TYPE payment_provider_provider_kind AS ENUM ('platform_internal', 'fiat_gateway', 'crypto_chain');
CREATE TYPE payment_asset_asset_kind AS ENUM ('fiat', 'platform_currency', 'crypto_native', 'crypto_jetton');
CREATE TYPE payment_product_quantity_mode AS ENUM ('fixed', 'flexible');
CREATE TYPE payment_product_global_interval AS ENUM ('SECOND', 'MINUTE', 'HOUR', 'DAY', 'WEEK', 'MONTH', 'ONCE', 'UNLIMITED');
CREATE TYPE payment_product_user_interval AS ENUM ('SECOND', 'MINUTE', 'HOUR', 'DAY', 'WEEK', 'MONTH', 'ONCE', 'UNLIMITED');
CREATE TYPE payment_product_item_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE payment_product_item_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');
CREATE TYPE payment_price_pricing_mode AS ENUM ('fixed', 'dynamic');
CREATE TYPE payment_product_cache_quantity_mode AS ENUM ('fixed', 'flexible');
CREATE TYPE payment_product_cache_global_interval AS ENUM ('SECOND', 'MINUTE', 'HOUR', 'DAY', 'WEEK', 'MONTH', 'ONCE', 'UNLIMITED');
CREATE TYPE payment_product_cache_user_interval AS ENUM ('SECOND', 'MINUTE', 'HOUR', 'DAY', 'WEEK', 'MONTH', 'ONCE', 'UNLIMITED');
CREATE TYPE payment_product_cache_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE payment_product_cache_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');
CREATE TYPE payment_purchase_key_status AS ENUM ('active', 'used', 'canceled', 'expired');
CREATE TYPE payment_order_status AS ENUM ('draft', 'pending_payment', 'paid', 'fulfilled', 'canceled', 'expired', 'refunded', 'chargebacked', 'failed');
CREATE TYPE payment_paid_order_index_status AS ENUM ('paid', 'fulfilled');
CREATE TYPE payment_order_item_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE payment_order_item_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');
CREATE TYPE payment_product_limit_counter_counter_scope AS ENUM ('global', 'user');
CREATE TYPE payment_attempt_status AS ENUM ('created', 'pending', 'requires_action', 'waiting_capture', 'succeeded', 'canceled', 'expired', 'refunded', 'chargebacked', 'failed');
CREATE TYPE payment_event_processing_status AS ENUM ('new', 'processed', 'ignored', 'failed');
CREATE TYPE payment_subscription_status AS ENUM ('active', 'canceled', 'refunded', 'expired');
CREATE TYPE payment_fulfillment_status AS ENUM ('pending', 'succeeded', 'revoked', 'failed');
CREATE TYPE payment_fulfillment_item_reward_type AS ENUM ('quantity', 'duration');
CREATE TYPE payment_fulfillment_item_duration_unit AS ENUM ('second', 'minute', 'hour', 'day', 'week', 'month', 'year');
CREATE TYPE payment_refund_status AS ENUM ('created', 'pending', 'succeeded', 'canceled', 'failed');
CREATE TYPE payment_stats_event_event_type AS ENUM ('purchase', 'refund');
CREATE TYPE payment_stats_order_event_event_type AS ENUM ('created', 'status');
CREATE TYPE payment_provider_transaction_status AS ENUM ('new', 'matched', 'ignored', 'failed');
CREATE TYPE payment_stats_order_event_order_status AS ENUM ('draft', 'pending_payment', 'paid', 'fulfilled', 'canceled', 'expired', 'refunded', 'chargebacked', 'failed');

CREATE TABLE IF NOT EXISTS payment_provider (
    code VARCHAR(32) NOT NULL PRIMARY KEY,
    title VARCHAR(128) NOT NULL,
    provider_kind payment_provider_provider_kind NOT NULL,
    supports_create BOOLEAN NOT NULL DEFAULT false,
    supports_redirect BOOLEAN NOT NULL DEFAULT false,
    supports_webhook BOOLEAN NOT NULL DEFAULT false,
    supports_refund BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payment_asset (
    code VARCHAR(32) NOT NULL PRIMARY KEY,
    title VARCHAR(128) NOT NULL,
    asset_kind payment_asset_asset_kind NOT NULL,
    scale SMALLINT NOT NULL DEFAULT 0,
    chain VARCHAR(32) NULL,
    network VARCHAR(32) NULL,
    contract_address VARCHAR(128) NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_asset_chain_contract_uq UNIQUE (chain, network, contract_address)
);

CREATE TABLE IF NOT EXISTS payment_provider_asset (
    provider_code VARCHAR(32) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    min_amount_minor BIGINT NULL,
    max_amount_minor BIGINT NULL,
    merchant_account VARCHAR(128) NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_code, asset_code),
    CONSTRAINT payment_provider_asset_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_provider_asset_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code)
);

CREATE TABLE IF NOT EXISTS payment_asset_rate (
    asset_code VARCHAR(32) NOT NULL,
    reference_asset_code VARCHAR(32) NOT NULL,
    reference_per_asset_minor BIGINT NOT NULL,
    source VARCHAR(64) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    auto_update_enabled BOOLEAN NOT NULL DEFAULT false,
    auto_update_source VARCHAR(32) NULL,
    source_chain_id VARCHAR(32) NULL,
    source_token_address VARCHAR(128) NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    lease_owner VARCHAR(64) NULL,
    lease_until TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (asset_code, reference_asset_code),
    CONSTRAINT payment_asset_rate_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_asset_rate_reference_asset_fk
        FOREIGN KEY (reference_asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_asset_rate_positive_chk CHECK (reference_per_asset_minor > 0)
);

CREATE TABLE IF NOT EXISTS payment_product_group (
    workspace_id VARCHAR(36) NOT NULL,
    code VARCHAR(64) NOT NULL,
    title_key VARCHAR(255) NULL,
    description_key VARCHAR(255) NULL,
    position INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, code)
);

CREATE TABLE IF NOT EXISTS payment_localization (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    localization_key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_localization_locale_key_uq UNIQUE (workspace_id, locale, localization_key)
);

CREATE TABLE IF NOT EXISTS payment_product (
    workspace_id VARCHAR(36) NOT NULL,
    id VARCHAR(64) NOT NULL,
    group_code VARCHAR(64) NULL,
    title_key VARCHAR(255) NOT NULL,
    description_key VARCHAR(255) NULL,
    target JSONB NULL,
    image_url VARCHAR(512) NULL,
    link_url VARCHAR(512) NULL,
    size_label VARCHAR(64) NULL,
    period_seconds BIGINT NULL,
    trial_duration_seconds BIGINT NULL,
    quantity_mode payment_product_quantity_mode NOT NULL DEFAULT 'fixed',
    position INTEGER NOT NULL DEFAULT 0,
    global_limit INTEGER NOT NULL DEFAULT 0,
    global_interval payment_product_global_interval NOT NULL DEFAULT 'UNLIMITED',
    global_interval_count INTEGER NOT NULL DEFAULT 0,
    user_limit INTEGER NOT NULL DEFAULT 0,
    user_interval payment_product_user_interval NOT NULL DEFAULT 'UNLIMITED',
    user_interval_count INTEGER NOT NULL DEFAULT 0,
    available_from TIMESTAMPTZ NOT NULL DEFAULT '2024-01-01 00:00:00',
    available_until TIMESTAMPTZ NOT NULL DEFAULT '2124-01-01 00:00:00',
    is_visible BOOLEAN NOT NULL DEFAULT true,
    is_closed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id),
    CONSTRAINT payment_product_group_fk
        FOREIGN KEY (workspace_id, group_code) REFERENCES payment_product_group (workspace_id, code)
);

CREATE TABLE IF NOT EXISTS payment_product_item (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    reward_type payment_product_item_reward_type NOT NULL DEFAULT 'quantity',
    quantity BIGINT NOT NULL DEFAULT 0,
    scale SMALLINT NOT NULL DEFAULT 0,
    duration_unit payment_product_item_duration_unit NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_product_item_uq UNIQUE (workspace_id, product_id, item_id),
    CONSTRAINT payment_product_item_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id)
            ON UPDATE CASCADE ON DELETE CASCADE,
    CONSTRAINT payment_product_item_quantity_chk CHECK (quantity > 0),
    CONSTRAINT payment_product_item_reward_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL) OR
        (reward_type = 'duration' AND duration_unit IS NOT NULL)
    )
);

ALTER TABLE payment_product_item
    DROP CONSTRAINT IF EXISTS payment_product_item_item_fk;

CREATE TABLE IF NOT EXISTS payment_price (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    list_amount_minor BIGINT NOT NULL,
    discount_amount_minor BIGINT NOT NULL DEFAULT 0,
    pricing_mode payment_price_pricing_mode NOT NULL DEFAULT 'fixed',
    reference_asset_code VARCHAR(32) NULL,
    reference_list_amount_minor BIGINT NULL,
    reference_discount_amount_minor BIGINT NULL,
    coefficient DECIMAL(24,12) NULL,
    is_promotion BOOLEAN NOT NULL DEFAULT false,
    starts_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at TIMESTAMPTZ NOT NULL DEFAULT '2124-01-01 00:00:00',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_price_window_uq UNIQUE (workspace_id, product_id, asset_code, is_promotion, starts_at, ends_at),
    CONSTRAINT payment_price_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id)
            ON UPDATE CASCADE ON DELETE CASCADE,
    CONSTRAINT payment_price_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_price_reference_asset_fk
        FOREIGN KEY (reference_asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_price_reference_discount_chk CHECK (
        reference_discount_amount_minor IS NULL
        OR reference_list_amount_minor IS NULL
        OR reference_discount_amount_minor <= reference_list_amount_minor
    ),
    CONSTRAINT payment_price_dynamic_chk CHECK (
        (pricing_mode = 'fixed'
            AND reference_asset_code IS NULL
            AND reference_list_amount_minor IS NULL
            AND reference_discount_amount_minor IS NULL
            AND coefficient IS NULL)
        OR
        (pricing_mode = 'dynamic'
            AND reference_asset_code IS NOT NULL
            AND reference_list_amount_minor IS NOT NULL
            AND reference_discount_amount_minor IS NOT NULL
            AND coefficient IS NOT NULL
            AND coefficient > 0)
    )
);

CREATE TABLE IF NOT EXISTS payment_product_cache (
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    price_id BIGINT NOT NULL,
    item_id VARCHAR(64) NOT NULL DEFAULT '',
    link_url VARCHAR(512) NULL,
    size_label VARCHAR(64) NULL,
    group_code VARCHAR(64) NULL,
    target JSONB NULL,
    product_title TEXT NOT NULL,
    product_description TEXT NOT NULL,
    image_url VARCHAR(512) NULL,
    period_seconds BIGINT NULL,
    trial_duration_seconds BIGINT NULL,
    quantity_mode payment_product_cache_quantity_mode NOT NULL DEFAULT 'fixed',
    product_position INTEGER NOT NULL DEFAULT 0,
    global_limit INTEGER NOT NULL DEFAULT 0,
    global_interval payment_product_cache_global_interval NOT NULL DEFAULT 'UNLIMITED',
    global_interval_count INTEGER NOT NULL DEFAULT 0,
    user_limit INTEGER NOT NULL DEFAULT 0,
    user_interval payment_product_cache_user_interval NOT NULL DEFAULT 'UNLIMITED',
    user_interval_count INTEGER NOT NULL DEFAULT 0,
    is_visible BOOLEAN NOT NULL DEFAULT true,
    is_closed BOOLEAN NOT NULL DEFAULT false,
    available_from TIMESTAMPTZ NOT NULL,
    available_until TIMESTAMPTZ NOT NULL,
    list_amount_minor BIGINT NOT NULL,
    discount_amount_minor BIGINT NOT NULL DEFAULT 0,
    is_promotion BOOLEAN NOT NULL DEFAULT false,
    price_starts_at TIMESTAMPTZ NOT NULL,
    price_ends_at TIMESTAMPTZ NOT NULL,
    item_quantity BIGINT NOT NULL DEFAULT 0,
    item_scale SMALLINT NOT NULL DEFAULT 0,
    reward_type payment_product_cache_reward_type NOT NULL DEFAULT 'quantity',
    duration_unit payment_product_cache_duration_unit NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, product_id, asset_code, locale, price_id, item_id),
    CONSTRAINT payment_product_cache_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id)
            ON UPDATE CASCADE ON DELETE CASCADE,
    CONSTRAINT payment_product_cache_price_fk
        FOREIGN KEY (price_id) REFERENCES payment_price (id)
            ON DELETE CASCADE,
    CONSTRAINT payment_product_cache_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code)
);

ALTER TABLE payment_product_cache
    DROP COLUMN IF EXISTS item_type,
    DROP COLUMN IF EXISTS item_title,
    DROP COLUMN IF EXISTS item_description,
    DROP COLUMN IF EXISTS item_rarity,
    DROP COLUMN IF EXISTS item_position;

-- payment_item belongs to the retired embedded item catalog. Payment now uses
-- item keys owned by Reference, but an existing installation can still have
-- foreign keys to the legacy table. Do not drop it during bootstrap: removing
-- it requires an explicit, data-aware migration rather than DROP ... CASCADE.

CREATE TABLE IF NOT EXISTS payment_purchase_key (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    key_hash CHAR(64) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    internal_user_id BIGINT NULL,
    product_id VARCHAR(64) NOT NULL,
    status payment_purchase_key_status NOT NULL DEFAULT 'active',
    max_uses INTEGER NOT NULL DEFAULT 1,
    used_count INTEGER NOT NULL DEFAULT 0,
    reserved_count INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_purchase_key_hash_uq UNIQUE (key_hash),
    CONSTRAINT payment_purchase_key_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id)
            ON UPDATE CASCADE ON DELETE CASCADE,
    CONSTRAINT payment_purchase_key_uses_chk
        CHECK (
            max_uses > 0
            AND used_count >= 0
            AND reserved_count >= 0
            AND used_count + reserved_count <= max_uses
        )
);

ALTER TABLE payment_purchase_key
    ADD COLUMN IF NOT EXISTS reserved_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE payment_purchase_key
    DROP CONSTRAINT IF EXISTS payment_purchase_key_uses_chk;

ALTER TABLE payment_purchase_key
    ADD CONSTRAINT payment_purchase_key_uses_chk CHECK (
        max_uses > 0
        AND used_count >= 0
        AND reserved_count >= 0
        AND used_count + reserved_count <= max_uses
    );

CREATE TABLE IF NOT EXISTS payment_order (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    public_id CHAR(36) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    internal_user_id BIGINT NULL,
    payer_platform_id BIGINT NULL,
    payer_platform_user_id VARCHAR(128) NULL,
    payer_internal_user_id BIGINT NULL,
    purchase_key_id BIGINT NULL,
    product_id VARCHAR(64) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 1,
    price_id BIGINT NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    locale VARCHAR(16) NOT NULL DEFAULT 'ru',
    list_amount_minor BIGINT NOT NULL,
    discount_amount_minor BIGINT NOT NULL DEFAULT 0,
    payable_amount_minor BIGINT NOT NULL,
    status payment_order_status NOT NULL DEFAULT 'draft',
    reserved_until TIMESTAMPTZ NULL,
    global_limit_snapshot INTEGER NOT NULL DEFAULT 0,
    global_interval_snapshot VARCHAR(16) NOT NULL DEFAULT 'UNLIMITED',
    global_interval_count_snapshot INTEGER NOT NULL DEFAULT 0,
    global_window_start_snapshot TIMESTAMPTZ NULL,
    global_window_end_snapshot TIMESTAMPTZ NULL,
    user_limit_snapshot INTEGER NOT NULL DEFAULT 0,
    user_interval_snapshot VARCHAR(16) NOT NULL DEFAULT 'UNLIMITED',
    user_interval_count_snapshot INTEGER NOT NULL DEFAULT 0,
    user_window_start_snapshot TIMESTAMPTZ NULL,
    user_window_end_snapshot TIMESTAMPTZ NULL,
    paid_at TIMESTAMPTZ NULL,
    fulfilled_at TIMESTAMPTZ NULL,
    canceled_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_order_public_id_uq UNIQUE (public_id),
    CONSTRAINT payment_order_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id),
    CONSTRAINT payment_order_price_fk
        FOREIGN KEY (price_id) REFERENCES payment_price (id),
    CONSTRAINT payment_order_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_order_purchase_key_fk
        FOREIGN KEY (purchase_key_id) REFERENCES payment_purchase_key (id),
    CONSTRAINT payment_order_payable_chk
        CHECK (
            discount_amount_minor <= list_amount_minor
            AND payable_amount_minor = list_amount_minor - discount_amount_minor
        )
);

ALTER TABLE payment_order
    ADD COLUMN IF NOT EXISTS global_limit_snapshot INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS global_interval_snapshot VARCHAR(16) NOT NULL DEFAULT 'UNLIMITED',
    ADD COLUMN IF NOT EXISTS global_interval_count_snapshot INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS global_window_start_snapshot TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS global_window_end_snapshot TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS user_limit_snapshot INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_interval_snapshot VARCHAR(16) NOT NULL DEFAULT 'UNLIMITED',
    ADD COLUMN IF NOT EXISTS user_interval_count_snapshot INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_window_start_snapshot TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS user_window_end_snapshot TIMESTAMPTZ NULL;

CREATE TABLE IF NOT EXISTS payment_paid_order_index (
    order_id BIGINT NOT NULL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    internal_user_id BIGINT NULL,
    payer_platform_id BIGINT NULL,
    payer_platform_user_id VARCHAR(128) NULL,
    payer_internal_user_id BIGINT NULL,
    purchase_key_id BIGINT NULL,
    product_id VARCHAR(64) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 1,
    price_id BIGINT NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    list_amount_minor BIGINT NOT NULL,
    discount_amount_minor BIGINT NOT NULL,
    payable_amount_minor BIGINT NOT NULL,
    status payment_paid_order_index_status NOT NULL DEFAULT 'paid',
    paid_at TIMESTAMPTZ NOT NULL,
    fulfilled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_paid_order_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id)
            ON DELETE CASCADE,
    CONSTRAINT payment_paid_order_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id),
    CONSTRAINT payment_paid_order_price_fk
        FOREIGN KEY (price_id) REFERENCES payment_price (id),
    CONSTRAINT payment_paid_order_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_paid_order_purchase_key_fk
        FOREIGN KEY (purchase_key_id) REFERENCES payment_purchase_key (id)
);

CREATE TABLE IF NOT EXISTS payment_order_item (
    order_id BIGINT NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    reward_type payment_order_item_reward_type NOT NULL DEFAULT 'quantity',
    quantity BIGINT NOT NULL,
    scale SMALLINT NOT NULL DEFAULT 0,
    duration_unit payment_order_item_duration_unit NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, workspace_id, item_id),
    CONSTRAINT payment_order_item_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id)
            ON DELETE CASCADE,
    CONSTRAINT payment_order_item_quantity_chk CHECK (quantity > 0),
    CONSTRAINT payment_order_item_reward_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL) OR
        (reward_type = 'duration' AND duration_unit IS NOT NULL)
    )
);

ALTER TABLE payment_order_item
    DROP CONSTRAINT IF EXISTS payment_order_item_item_fk;

CREATE TABLE IF NOT EXISTS payment_product_limit_counter (
    workspace_id VARCHAR(36) NOT NULL,
    platform_id BIGINT NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    counter_scope payment_product_limit_counter_counter_scope NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL DEFAULT '',
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    paid_count BIGINT NOT NULL DEFAULT 0,
    reserved_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (
        workspace_id,
        platform_id,
        product_id,
        counter_scope,
        platform_user_id,
        window_start,
        window_end
    ),
    CONSTRAINT payment_product_limit_counter_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id)
            ON UPDATE CASCADE ON DELETE CASCADE
);

ALTER TABLE payment_product_limit_counter
    ADD COLUMN IF NOT EXISTS reserved_count BIGINT NOT NULL DEFAULT 0;

-- Global limits are workspace-wide. Merge counters created by older versions
-- that partitioned the global scope by platform_id.
WITH removed AS (
    DELETE FROM payment_product_limit_counter
    WHERE counter_scope = 'global'
      AND platform_id <> 0
    RETURNING *
), aggregated AS (
    SELECT
        workspace_id,
        product_id,
        counter_scope,
        platform_user_id,
        window_start,
        window_end,
        SUM(paid_count)::bigint AS paid_count,
        SUM(reserved_count)::bigint AS reserved_count
    FROM removed
    GROUP BY
        workspace_id,
        product_id,
        counter_scope,
        platform_user_id,
        window_start,
        window_end
)
INSERT INTO payment_product_limit_counter (
    workspace_id,
    platform_id,
    product_id,
    counter_scope,
    platform_user_id,
    window_start,
    window_end,
    paid_count,
    reserved_count
)
SELECT
    workspace_id,
    0,
    product_id,
    counter_scope,
    platform_user_id,
    window_start,
    window_end,
    paid_count,
    reserved_count
FROM aggregated
ON CONFLICT (
    workspace_id,
    platform_id,
    product_id,
    counter_scope,
    platform_user_id,
    window_start,
    window_end
) DO UPDATE SET
    paid_count = payment_product_limit_counter.paid_count + EXCLUDED.paid_count,
    reserved_count = payment_product_limit_counter.reserved_count + EXCLUDED.reserved_count,
    updated_at = now();

CREATE TABLE IF NOT EXISTS payment_attempt (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    order_id BIGINT NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    amount_minor BIGINT NOT NULL,
    status payment_attempt_status NOT NULL DEFAULT 'created',
    provider_payment_id VARCHAR(128) NULL,
    provider_invoice_id VARCHAR(128) NULL,
    provider_charge_id VARCHAR(128) NULL,
    provider_subscription_id VARCHAR(128) NULL,
    idempotency_key VARCHAR(128) NULL,
    request_fingerprint CHAR(64) NOT NULL DEFAULT '',
    confirmation_url TEXT NULL,
    return_url TEXT NULL,
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_attempt_idempotency_uq UNIQUE (workspace_id, provider_code, idempotency_key),
    CONSTRAINT payment_attempt_provider_payment_uq UNIQUE (workspace_id, provider_code, provider_payment_id),
    CONSTRAINT payment_attempt_provider_charge_uq UNIQUE (workspace_id, provider_code, provider_charge_id),
    CONSTRAINT payment_attempt_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id)
            ON DELETE CASCADE,
    CONSTRAINT payment_attempt_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_attempt_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code)
);

ALTER TABLE payment_attempt
    ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS request_fingerprint CHAR(64) NOT NULL DEFAULT '';

UPDATE payment_attempt pa
SET workspace_id = po.workspace_id
FROM payment_order po
WHERE po.id = pa.order_id
  AND pa.workspace_id IS NULL;

ALTER TABLE payment_attempt
    ALTER COLUMN workspace_id SET NOT NULL;

-- Telegram Stars charge identifiers can exceed 128 characters.
ALTER TABLE payment_attempt
    ALTER COLUMN provider_charge_id TYPE VARCHAR(256);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_attempt'::regclass
          AND conname = 'payment_attempt_idempotency_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, idempotency_key%'
    ) THEN
        ALTER TABLE payment_attempt
            DROP CONSTRAINT payment_attempt_idempotency_uq;
        ALTER TABLE payment_attempt
            ADD CONSTRAINT payment_attempt_idempotency_uq UNIQUE (workspace_id, provider_code, idempotency_key);
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_attempt'::regclass
          AND conname = 'payment_attempt_provider_payment_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, provider_payment_id%'
    ) THEN
        ALTER TABLE payment_attempt
            DROP CONSTRAINT payment_attempt_provider_payment_uq;
        ALTER TABLE payment_attempt
            ADD CONSTRAINT payment_attempt_provider_payment_uq UNIQUE (workspace_id, provider_code, provider_payment_id);
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_attempt'::regclass
          AND conname = 'payment_attempt_provider_charge_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, provider_charge_id%'
    ) THEN
        ALTER TABLE payment_attempt
            DROP CONSTRAINT payment_attempt_provider_charge_uq;
        ALTER TABLE payment_attempt
            ADD CONSTRAINT payment_attempt_provider_charge_uq UNIQUE (workspace_id, provider_code, provider_charge_id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS payment_event (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    attempt_id BIGINT NULL,
    order_id BIGINT NULL,
    provider_event_id VARCHAR(128) NULL,
    provider_payment_id VARCHAR(128) NULL,
    event_type VARCHAR(128) NOT NULL,
    event_status VARCHAR(64) NULL,
    payload_hash CHAR(64) NOT NULL,
    signature_valid BOOLEAN NULL,
    processing_status payment_event_processing_status NOT NULL DEFAULT 'new',
    processing_error TEXT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ NULL,
    CONSTRAINT payment_event_provider_event_uq UNIQUE (workspace_id, provider_code, provider_event_id),
    CONSTRAINT payment_event_payload_hash_uq UNIQUE (workspace_id, provider_code, payload_hash),
    CONSTRAINT payment_event_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_event_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id),
    CONSTRAINT payment_event_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id)
);

ALTER TABLE payment_event
    ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36);

UPDATE payment_event pe
SET workspace_id = COALESCE(
    (SELECT pa.workspace_id FROM payment_attempt pa WHERE pa.id = pe.attempt_id),
    (SELECT po.workspace_id FROM payment_order po WHERE po.id = pe.order_id)
)
WHERE pe.workspace_id IS NULL;

UPDATE payment_event
SET workspace_id = '00000000-0000-0000-0000-000000000000'
WHERE workspace_id IS NULL;

ALTER TABLE payment_event
    ALTER COLUMN workspace_id SET NOT NULL;

-- Provider event IDs may embed a long Telegram Stars charge identifier.
ALTER TABLE payment_event
    ALTER COLUMN provider_event_id TYPE VARCHAR(256);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_event'::regclass
          AND conname = 'payment_event_provider_event_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, provider_event_id%'
    ) THEN
        ALTER TABLE payment_event DROP CONSTRAINT payment_event_provider_event_uq;
        ALTER TABLE payment_event
            ADD CONSTRAINT payment_event_provider_event_uq UNIQUE (workspace_id, provider_code, provider_event_id);
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_event'::regclass
          AND conname = 'payment_event_payload_hash_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, payload_hash%'
    ) THEN
        ALTER TABLE payment_event DROP CONSTRAINT payment_event_payload_hash_uq;
        ALTER TABLE payment_event
            ADD CONSTRAINT payment_event_payload_hash_uq UNIQUE (workspace_id, provider_code, payload_hash);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS payment_subscription (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    provider_subscription_id VARCHAR(128) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    internal_user_id BIGINT NULL,
    product_id VARCHAR(64) NOT NULL,
    order_id BIGINT NULL,
    attempt_id BIGINT NULL,
    status payment_subscription_status NOT NULL DEFAULT 'active',
    cancel_reason VARCHAR(255) NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_subscription_provider_uq UNIQUE (workspace_id, provider_code, provider_subscription_id),
    CONSTRAINT payment_subscription_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_subscription_product_fk
        FOREIGN KEY (workspace_id, product_id) REFERENCES payment_product (workspace_id, id),
    CONSTRAINT payment_subscription_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id),
    CONSTRAINT payment_subscription_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id)
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_subscription'::regclass
          AND conname = 'payment_subscription_provider_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, provider_subscription_id%'
    ) THEN
        ALTER TABLE payment_subscription DROP CONSTRAINT payment_subscription_provider_uq;
        ALTER TABLE payment_subscription
            ADD CONSTRAINT payment_subscription_provider_uq UNIQUE (workspace_id, provider_code, provider_subscription_id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS payment_fulfillment (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    order_id BIGINT NOT NULL,
    attempt_id BIGINT NOT NULL,
    internal_user_id BIGINT NULL,
    status payment_fulfillment_status NOT NULL DEFAULT 'pending',
    error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    fulfilled_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    CONSTRAINT payment_fulfillment_order_uq UNIQUE (order_id),
    CONSTRAINT payment_fulfillment_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id),
    CONSTRAINT payment_fulfillment_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id)
);

CREATE TABLE IF NOT EXISTS payment_fulfillment_item (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    fulfillment_id BIGINT NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    reward_type payment_fulfillment_item_reward_type NOT NULL DEFAULT 'quantity',
    quantity BIGINT NOT NULL,
    scale SMALLINT NOT NULL DEFAULT 0,
    duration_unit payment_fulfillment_item_duration_unit NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_fulfillment_item_uq UNIQUE (fulfillment_id, workspace_id, item_id),
    CONSTRAINT payment_fulfillment_item_fulfillment_fk
        FOREIGN KEY (fulfillment_id) REFERENCES payment_fulfillment (id)
            ON DELETE CASCADE,
    CONSTRAINT payment_fulfillment_item_quantity_chk CHECK (quantity > 0),
    CONSTRAINT payment_fulfillment_item_reward_chk CHECK (
        (reward_type = 'quantity' AND duration_unit IS NULL) OR
        (reward_type = 'duration' AND duration_unit IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_fulfillment_attempt_uq
    ON payment_fulfillment (attempt_id);

CREATE TABLE IF NOT EXISTS payment_subscription_renewal (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    subscription_id BIGINT NOT NULL,
    order_id BIGINT NOT NULL,
    attempt_id BIGINT NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    provider_subscription_id VARCHAR(128) NOT NULL,
    provider_charge_id VARCHAR(128) NOT NULL,
    amount_minor BIGINT NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_subscription_renewal_period_uq UNIQUE (
        workspace_id,
        provider_code,
        provider_subscription_id,
        period_end
    ),
    CONSTRAINT payment_subscription_renewal_subscription_fk
        FOREIGN KEY (subscription_id) REFERENCES payment_subscription (id),
    CONSTRAINT payment_subscription_renewal_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id),
    CONSTRAINT payment_subscription_renewal_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id),
    CONSTRAINT payment_subscription_renewal_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_subscription_renewal_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_subscription_renewal_amount_chk CHECK (amount_minor > 0)
);

CREATE INDEX IF NOT EXISTS payment_subscription_renewal_order_idx
    ON payment_subscription_renewal (workspace_id, order_id, created_at DESC);

-- Telegram Stars renewal charge identifiers can exceed 128 characters.
ALTER TABLE payment_subscription_renewal
    ALTER COLUMN provider_charge_id TYPE VARCHAR(256);

WITH duplicate_renewals AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY workspace_id, provider_code, provider_charge_id
            ORDER BY id
        ) AS position
    FROM payment_subscription_renewal
)
DELETE FROM payment_subscription_renewal AS renewal
USING duplicate_renewals AS duplicate
WHERE renewal.id = duplicate.id
  AND duplicate.position > 1;

CREATE UNIQUE INDEX IF NOT EXISTS payment_subscription_renewal_charge_uq
    ON payment_subscription_renewal (workspace_id, provider_code, provider_charge_id);

ALTER TABLE payment_fulfillment_item
    DROP CONSTRAINT IF EXISTS payment_fulfillment_item_item_fk;

CREATE TABLE IF NOT EXISTS payment_refund (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    order_id BIGINT NOT NULL,
    attempt_id BIGINT NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    idempotency_key VARCHAR(128) NULL,
    provider_refund_id VARCHAR(128) NULL,
    amount_minor BIGINT NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    status payment_refund_status NOT NULL DEFAULT 'created',
    reason VARCHAR(255) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_refund_provider_uq UNIQUE (workspace_id, provider_code, provider_refund_id),
    CONSTRAINT payment_refund_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id),
    CONSTRAINT payment_refund_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id),
    CONSTRAINT payment_refund_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_refund_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code)
);

ALTER TABLE payment_refund
    ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36);

ALTER TABLE payment_refund
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128);

UPDATE payment_refund pr
SET workspace_id = po.workspace_id
FROM payment_order po
WHERE po.id = pr.order_id
  AND pr.workspace_id IS NULL;

ALTER TABLE payment_refund
    ALTER COLUMN workspace_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS payment_refund_idempotency_uq
    ON payment_refund (workspace_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'payment_refund'::regclass
          AND conname = 'payment_refund_provider_uq'
          AND pg_get_constraintdef(oid) NOT LIKE '%workspace_id, provider_code, provider_refund_id%'
    ) THEN
        ALTER TABLE payment_refund DROP CONSTRAINT payment_refund_provider_uq;
        ALTER TABLE payment_refund
            ADD CONSTRAINT payment_refund_provider_uq UNIQUE (workspace_id, provider_code, provider_refund_id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS payment_stats_event (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    event_type payment_stats_event_event_type NOT NULL,
    source_id BIGINT NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0,
    asset_code VARCHAR(32) NOT NULL,
    amount_minor BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_stats_event_source_uq UNIQUE (event_type, source_id)
);

CREATE TABLE IF NOT EXISTS payment_stats_order_event (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    order_id BIGINT NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    event_type payment_stats_order_event_event_type NOT NULL,
    order_status payment_stats_order_event_order_status NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_stats_order_event_uq UNIQUE (order_id, event_type, order_status),
    CONSTRAINT payment_stats_order_event_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id)
            ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS payment_stats_daily (
    workspace_id VARCHAR(36) NOT NULL,
    product_id VARCHAR(64) NOT NULL DEFAULT '',
    asset_code VARCHAR(32) NOT NULL,
    stats_date DATE NOT NULL,
    purchase_count BIGINT NOT NULL DEFAULT 0,
    purchase_quantity BIGINT NOT NULL DEFAULT 0,
    unique_buyers BIGINT NOT NULL DEFAULT 0,
    gross_amount_minor BIGINT NOT NULL DEFAULT 0,
    refund_count BIGINT NOT NULL DEFAULT 0,
    refund_amount_minor BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, product_id, asset_code, stats_date)
);

CREATE TABLE IF NOT EXISTS payment_stats_daily_overview (
    workspace_id VARCHAR(36) NOT NULL,
    stats_date DATE NOT NULL,
    products_total BIGINT NOT NULL DEFAULT 0,
    active_products BIGINT NOT NULL DEFAULT 0,
    visible_products BIGINT NOT NULL DEFAULT 0,
    orders_created BIGINT NOT NULL DEFAULT 0,
    draft_orders BIGINT NOT NULL DEFAULT 0,
    pending_payment_orders BIGINT NOT NULL DEFAULT 0,
    paid_orders BIGINT NOT NULL DEFAULT 0,
    fulfilled_orders BIGINT NOT NULL DEFAULT 0,
    canceled_orders BIGINT NOT NULL DEFAULT 0,
    expired_orders BIGINT NOT NULL DEFAULT 0,
    refunded_orders BIGINT NOT NULL DEFAULT 0,
    chargebacked_orders BIGINT NOT NULL DEFAULT 0,
    failed_orders BIGINT NOT NULL DEFAULT 0,
    purchase_count BIGINT NOT NULL DEFAULT 0,
    purchase_quantity BIGINT NOT NULL DEFAULT 0,
    unique_buyers BIGINT NOT NULL DEFAULT 0,
    refund_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, stats_date)
);

CREATE TABLE IF NOT EXISTS payment_stats_daily_buyer (
    workspace_id VARCHAR(36) NOT NULL,
    stats_date DATE NOT NULL,
    app_id BIGINT NOT NULL,
    platform_id BIGINT NOT NULL,
    platform_user_id VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (
        workspace_id,
        stats_date,
        app_id,
        platform_id,
        platform_user_id
    )
);

CREATE TABLE IF NOT EXISTS payment_provider_cursor (
    workspace_id VARCHAR(36) NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    network VARCHAR(32) NOT NULL,
    source_key VARCHAR(255) NOT NULL,
    cursor_value VARCHAR(255) NOT NULL DEFAULT '',
    cursor_sequence BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, provider_code, network, source_key),
    CONSTRAINT payment_provider_cursor_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code)
);

CREATE TABLE IF NOT EXISTS payment_ton_wallet (
    workspace_id VARCHAR(36) NOT NULL,
    network VARCHAR(32) NOT NULL,
    wallet_address VARCHAR(255) NOT NULL,
    network_config_url VARCHAR(512) NULL,
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, network, wallet_address),
    CONSTRAINT payment_ton_wallet_workspace_uq UNIQUE (workspace_id)
);

CREATE TABLE IF NOT EXISTS payment_provider_transaction (
    id BIGINT NOT NULL GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    provider_code VARCHAR(32) NOT NULL,
    network VARCHAR(32) NOT NULL,
    source_key VARCHAR(255) NOT NULL,
    asset_code VARCHAR(32) NOT NULL,
    external_transaction_id VARCHAR(255) NOT NULL,
    sequence_number BIGINT NOT NULL DEFAULT 0,
    source_address VARCHAR(255) NOT NULL DEFAULT '',
    destination_address VARCHAR(255) NOT NULL DEFAULT '',
    amount_minor BIGINT NOT NULL,
    payment_reference VARCHAR(255) NOT NULL DEFAULT '',
    sender_reference VARCHAR(255) NULL,
    order_id BIGINT NULL,
    attempt_id BIGINT NULL,
    status payment_provider_transaction_status NOT NULL DEFAULT 'new',
    error TEXT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_provider_transaction_external_uq UNIQUE (workspace_id, provider_code, network, source_key, external_transaction_id),
    CONSTRAINT payment_provider_transaction_provider_fk
        FOREIGN KEY (provider_code) REFERENCES payment_provider (code),
    CONSTRAINT payment_provider_transaction_asset_fk
        FOREIGN KEY (asset_code) REFERENCES payment_asset (code),
    CONSTRAINT payment_provider_transaction_order_fk
        FOREIGN KEY (order_id) REFERENCES payment_order (id),
    CONSTRAINT payment_provider_transaction_attempt_fk
        FOREIGN KEY (attempt_id) REFERENCES payment_attempt (id)
);

INSERT INTO payment_provider (code, title, provider_kind, supports_create, supports_redirect, supports_webhook, supports_refund)
VALUES
    ('vkma', 'VK Mini Apps', 'platform_internal', false, false, true, true),
    ('telegram_stars', 'Telegram Stars', 'platform_internal', true, false, true, true),
    ('ton', 'TON blockchain', 'crypto_chain', true, false, false, false),
    ('yookassa', 'YooKassa', 'fiat_gateway', true, true, true, true),
    ('platega', 'Platega', 'fiat_gateway', true, true, true, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO payment_asset (code, title, asset_kind, scale, chain, network, contract_address, is_active)
VALUES
    ('VOTE', 'VK Votes', 'platform_currency', 0, NULL, NULL, NULL, true),
    ('XTR', 'Telegram Stars', 'platform_currency', 0, NULL, NULL, NULL, true),
    ('RUB', 'Russian Ruble', 'fiat', 2, NULL, NULL, NULL, true),
    ('TON', 'Toncoin', 'crypto_native', 9, 'ton', 'mainnet', NULL, true),
    ('USDT_TON', 'Tether USD on TON', 'crypto_jetton', 6, 'ton', 'mainnet', 'EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs', true),
    ('TSTON_TON', 'Tonstakers TON', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQC98_qAmNEptUtPc7W6xdHh_ZHrBUFpw5Ft_IzNU20QAJav', true),
    ('UTYA_TON', 'Utya', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQBaCgUwOoc6gHCNln_oJzb0mVs79YG7wYoavh-o1ItaneLA', true),
    ('STON_TON', 'STON', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQA2kCVNwVsil2EM2mB0SkXytxCqQjS4mttjDpnXmwG9T6bO', true),
    ('REDO_TON', 'Resistance Dog', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQBZ_cafPyDr5KUTs0aNxh0ZTDhkpEZONmLJA2SNGlLm4Cko', true),
    ('STORM_TON', 'STORM', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQBsosmcZrD6FHijA7qWGLw5wo_aH8UN435hi935jJ_STORM', true),
    ('GEMSTON_TON', 'GEMSTON', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQBX6K9aXVl3nXINCyPPL86C4ONVmQ8vK360u6dykFKXpHCa', true),
    ('NOT_TON', 'Notcoin', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQAvlWFDxGF2lXm67y4yzC17wYKD9A0guwPkMs1gOsM__NOT', true),
    ('JETTON_TON', 'JetTon', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQAQXlWJvGbbFfE8F3oS8s87lIgdovS455IsWFaRdmJetTon', true),
    ('MAJOR_TON', 'Major', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQCuPm01HldiduQ55xaBF_1kaW_WAUy5DHey8suqzU_MAJOR', true),
    ('DOGS_TON', 'Dogs', 'crypto_jetton', 9, 'ton', 'mainnet', 'EQCvxJy4eG8hyHBFsZ7eePxrRsUQSFE_jpptRAYBmcG_DOGS', true),
    ('MEMCOIN_TON', 'Memcoin Jetton on TON', 'crypto_jetton', 9, 'ton', 'mainnet', NULL, true)
ON CONFLICT (code) DO UPDATE SET
    title = EXCLUDED.title,
    asset_kind = EXCLUDED.asset_kind,
    scale = EXCLUDED.scale,
    chain = EXCLUDED.chain,
    network = EXCLUDED.network,
    contract_address = EXCLUDED.contract_address,
    is_active = EXCLUDED.is_active,
    updated_at = now();

INSERT INTO payment_provider_asset (provider_code, asset_code, is_active)
VALUES
    ('vkma', 'VOTE', true),
    ('telegram_stars', 'XTR', true),
    ('ton', 'TON', true),
    ('ton', 'USDT_TON', true),
    ('ton', 'TSTON_TON', true),
    ('ton', 'UTYA_TON', true),
    ('ton', 'STON_TON', true),
    ('ton', 'REDO_TON', true),
    ('ton', 'STORM_TON', true),
    ('ton', 'GEMSTON_TON', true),
    ('ton', 'NOT_TON', true),
    ('ton', 'JETTON_TON', true),
    ('ton', 'MAJOR_TON', true),
    ('ton', 'DOGS_TON', true),
    ('ton', 'MEMCOIN_TON', true),
    ('yookassa', 'RUB', true),
    ('platega', 'RUB', true)
ON CONFLICT (provider_code, asset_code) DO UPDATE SET
    is_active = EXCLUDED.is_active,
    updated_at = now();

CREATE INDEX IF NOT EXISTS payment_provider_asset_asset_active_idx ON payment_provider_asset (asset_code, is_active, provider_code);
CREATE INDEX IF NOT EXISTS payment_asset_rate_reference_idx ON payment_asset_rate (reference_asset_code, asset_code);
CREATE INDEX IF NOT EXISTS payment_asset_rate_auto_lease_idx ON payment_asset_rate (auto_update_enabled, lease_until);
CREATE INDEX IF NOT EXISTS payment_product_group_idx ON payment_product (workspace_id, group_code);
CREATE INDEX IF NOT EXISTS payment_product_workspace_window_idx ON payment_product (workspace_id, is_visible, is_closed, available_from, available_until, position);
CREATE INDEX IF NOT EXISTS payment_product_window_idx ON payment_product (available_from, available_until, position);
CREATE INDEX IF NOT EXISTS payment_price_current_idx ON payment_price (workspace_id, product_id, asset_code, starts_at, ends_at, is_promotion, id);
CREATE INDEX IF NOT EXISTS payment_price_dynamic_idx ON payment_price (workspace_id, asset_code, reference_asset_code, pricing_mode);
CREATE INDEX IF NOT EXISTS payment_purchase_key_product_status_idx ON payment_purchase_key (workspace_id, product_id, status);
CREATE INDEX IF NOT EXISTS payment_purchase_key_target_idx ON payment_purchase_key (app_id, platform_id, platform_user_id);
CREATE INDEX IF NOT EXISTS payment_order_user_product_status_idx ON payment_order (workspace_id, platform_id, platform_user_id, product_id, status);
CREATE INDEX IF NOT EXISTS payment_order_payer_idx ON payment_order (app_id, payer_platform_id, payer_platform_user_id);
CREATE INDEX IF NOT EXISTS payment_order_purchase_key_idx ON payment_order (purchase_key_id);
CREATE INDEX IF NOT EXISTS payment_order_status_created_idx ON payment_order (status, created_at);
CREATE INDEX IF NOT EXISTS payment_paid_order_global_window_idx ON payment_paid_order_index (workspace_id, platform_id, product_id, paid_at);
CREATE INDEX IF NOT EXISTS payment_paid_order_user_window_idx ON payment_paid_order_index (workspace_id, platform_id, platform_user_id, product_id, paid_at);
CREATE INDEX IF NOT EXISTS payment_paid_order_purchase_key_idx ON payment_paid_order_index (purchase_key_id);
CREATE INDEX IF NOT EXISTS payment_order_item_item_idx ON payment_order_item (workspace_id, item_id);
CREATE INDEX IF NOT EXISTS payment_product_limit_counter_window_idx ON payment_product_limit_counter (window_end, workspace_id, platform_id, product_id);
CREATE INDEX IF NOT EXISTS payment_attempt_order_idx ON payment_attempt (order_id);
CREATE INDEX IF NOT EXISTS payment_attempt_provider_status_idx ON payment_attempt (provider_code, status, created_at);
CREATE INDEX IF NOT EXISTS payment_event_attempt_idx ON payment_event (attempt_id);
CREATE INDEX IF NOT EXISTS payment_event_order_idx ON payment_event (order_id);
CREATE INDEX IF NOT EXISTS payment_event_processing_idx ON payment_event (processing_status, received_at);
CREATE INDEX IF NOT EXISTS payment_subscription_user_idx ON payment_subscription (workspace_id, platform_id, platform_user_id, product_id, status);
CREATE INDEX IF NOT EXISTS payment_subscription_active_idx ON payment_subscription (workspace_id, platform_id, platform_user_id, status, ended_at);
CREATE INDEX IF NOT EXISTS payment_subscription_active_product_idx ON payment_subscription (workspace_id, platform_id, platform_user_id, product_id, status, ended_at);
CREATE INDEX IF NOT EXISTS payment_subscription_active_provider_idx ON payment_subscription (workspace_id, platform_id, platform_user_id, provider_code, status, ended_at);
CREATE INDEX IF NOT EXISTS payment_subscription_active_product_provider_idx ON payment_subscription (workspace_id, platform_id, platform_user_id, product_id, provider_code, status, ended_at);
CREATE INDEX IF NOT EXISTS payment_subscription_order_idx ON payment_subscription (order_id);
CREATE INDEX IF NOT EXISTS payment_fulfillment_user_status_idx ON payment_fulfillment (internal_user_id, status);
CREATE INDEX IF NOT EXISTS payment_refund_order_idx ON payment_refund (order_id);
CREATE INDEX IF NOT EXISTS payment_stats_daily_date_idx ON payment_stats_daily (workspace_id, stats_date, product_id);
CREATE INDEX IF NOT EXISTS payment_stats_daily_overview_date_idx ON payment_stats_daily_overview (stats_date, workspace_id);
CREATE INDEX IF NOT EXISTS payment_stats_daily_buyer_date_idx ON payment_stats_daily_buyer (stats_date, workspace_id);
CREATE INDEX IF NOT EXISTS payment_ton_wallet_enabled_idx ON payment_ton_wallet (is_enabled, network, updated_at);
CREATE INDEX IF NOT EXISTS payment_provider_transaction_order_idx ON payment_provider_transaction (order_id);
CREATE INDEX IF NOT EXISTS payment_provider_transaction_attempt_idx ON payment_provider_transaction (attempt_id);

CREATE INDEX IF NOT EXISTS payment_product_cache_get_idx ON payment_product_cache (workspace_id,
        product_id,
        asset_code,
        locale,
        is_visible,
        is_closed,
        available_from,
        available_until,
        price_starts_at,
        price_ends_at);
CREATE INDEX IF NOT EXISTS payment_product_cache_price_idx ON payment_product_cache (workspace_id,
        product_id,
        asset_code,
        locale,
        is_promotion,
        price_starts_at,
        price_id);
CREATE INDEX IF NOT EXISTS payment_stats_event_workspace_idx ON payment_stats_event (workspace_id, occurred_at, event_type, asset_code);
CREATE INDEX IF NOT EXISTS payment_stats_event_product_idx ON payment_stats_event (workspace_id, product_id, occurred_at, event_type, asset_code);
CREATE INDEX IF NOT EXISTS payment_stats_event_user_idx ON payment_stats_event (workspace_id, platform_id, platform_user_id, occurred_at);
CREATE INDEX IF NOT EXISTS payment_stats_order_event_workspace_idx ON payment_stats_order_event (workspace_id, occurred_at, order_status);
CREATE INDEX IF NOT EXISTS payment_stats_order_event_product_idx ON payment_stats_order_event (workspace_id, product_id, occurred_at, order_status);
CREATE INDEX IF NOT EXISTS payment_provider_cursor_provider_idx ON payment_provider_cursor (provider_code, network, updated_at);
CREATE INDEX IF NOT EXISTS payment_provider_transaction_sequence_idx ON payment_provider_transaction (workspace_id, provider_code, network, source_key, sequence_number);
CREATE INDEX IF NOT EXISTS payment_provider_transaction_reference_idx ON payment_provider_transaction (workspace_id, provider_code, asset_code, payment_reference);
