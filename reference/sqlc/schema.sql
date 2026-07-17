CREATE TABLE IF NOT EXISTS reference_item (
    workspace_id VARCHAR(36) NOT NULL,
    key VARCHAR(128) NOT NULL,
    item_type VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, key),
    CONSTRAINT reference_item_type_chk CHECK (item_type IN ('quantity', 'duration'))
);

CREATE INDEX IF NOT EXISTS reference_item_list_idx
    ON reference_item (workspace_id, deleted_at, is_active, item_type, key);

CREATE TABLE IF NOT EXISTS reference_localization (
    workspace_id VARCHAR(36) NOT NULL,
    item_key VARCHAR(128) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, item_key, locale),
    CONSTRAINT reference_localization_item_fk
        FOREIGN KEY (workspace_id, item_key)
        REFERENCES reference_item (workspace_id, key)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS reference_localization_locale_idx
    ON reference_localization (workspace_id, locale, item_key);
