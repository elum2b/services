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

CREATE TABLE IF NOT EXISTS reference_resource (
    workspace_id VARCHAR(36) NOT NULL,
    key VARCHAR(128) NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deleted_at TIMESTAMPTZ NULL,
    format VARCHAR(16) NOT NULL,
    content_type VARCHAR(128) NOT NULL,
    source_size BIGINT NOT NULL,
    source_sha256 CHAR(64) NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    original_ref TEXT NOT NULL,
    preview_61_ref TEXT NOT NULL,
    preview_128_ref TEXT NOT NULL,
    preview_256_ref TEXT NOT NULL,
    preview_512_ref TEXT NOT NULL,
    placeholder_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, key),
    CONSTRAINT reference_resource_dimensions_chk CHECK (width > 0 AND height > 0),
    CONSTRAINT reference_resource_size_chk CHECK (source_size > 0)
);

CREATE INDEX IF NOT EXISTS reference_resource_list_idx
    ON reference_resource (workspace_id, deleted_at, is_active, resource_type, key);

CREATE TABLE IF NOT EXISTS reference_item_resource (
    workspace_id VARCHAR(36) NOT NULL,
    item_key VARCHAR(128) NOT NULL,
    resource_key VARCHAR(128) NOT NULL,
    position INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, item_key, resource_key),
    UNIQUE (workspace_id, item_key, position),
    CONSTRAINT reference_item_resource_item_fk
        FOREIGN KEY (workspace_id, item_key)
        REFERENCES reference_item (workspace_id, key)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT reference_item_resource_resource_fk
        FOREIGN KEY (workspace_id, resource_key)
        REFERENCES reference_resource (workspace_id, key)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS reference_item_resource_resource_idx
    ON reference_item_resource (workspace_id, resource_key, item_key);
