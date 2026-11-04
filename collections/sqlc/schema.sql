CREATE TABLE IF NOT EXISTS collections_collection (
 workspace_id varchar(36) NOT NULL, key varchar(191) NOT NULL,
 position integer NOT NULL DEFAULT 0, active boolean NOT NULL DEFAULT true,
 archived boolean NOT NULL DEFAULT false,
 PRIMARY KEY(workspace_id,key)
);
CREATE TABLE IF NOT EXISTS collections_localization (
 workspace_id varchar(36) NOT NULL, collection_key varchar(191) NOT NULL,
 locale varchar(32) NOT NULL, title text NOT NULL, description text NOT NULL,
 PRIMARY KEY(workspace_id,collection_key,locale),
 FOREIGN KEY(workspace_id,collection_key) REFERENCES collections_collection(workspace_id,key)
);
CREATE TABLE IF NOT EXISTS collections_entry (
 workspace_id varchar(36) NOT NULL, collection_key varchar(191) NOT NULL,
 item_key varchar(191) NOT NULL, position integer NOT NULL, active boolean NOT NULL,
 deleted boolean NOT NULL DEFAULT false,
 PRIMARY KEY(workspace_id,collection_key,item_key),
 FOREIGN KEY(workspace_id,collection_key) REFERENCES collections_collection(workspace_id,key)
);
CREATE INDEX IF NOT EXISTS collections_entry_order ON collections_entry(workspace_id,collection_key,position,item_key);
CREATE TABLE IF NOT EXISTS collections_ownership (
 workspace_id varchar(36) NOT NULL, app_id bigint NOT NULL CHECK(app_id>0),
 platform_id bigint NOT NULL CHECK(platform_id>0), platform_user_id varchar(191) NOT NULL,
 collection_key varchar(191) NOT NULL, item_key varchar(191) NOT NULL,
 owned boolean NOT NULL, revision bigint NOT NULL CHECK(revision>0), changed_at timestamptz NOT NULL,
 PRIMARY KEY(workspace_id,app_id,platform_id,platform_user_id,collection_key,item_key),
 FOREIGN KEY(workspace_id,collection_key,item_key) REFERENCES collections_entry(workspace_id,collection_key,item_key)
);
CREATE INDEX IF NOT EXISTS collections_owned_list ON collections_ownership(workspace_id,app_id,platform_id,platform_user_id,collection_key,item_key) WHERE owned;
CREATE TABLE IF NOT EXISTS collections_operation (
 id uuid NOT NULL UNIQUE, workspace_id varchar(36) NOT NULL,
 app_id bigint NOT NULL, platform_id bigint NOT NULL, platform_user_id varchar(191) NOT NULL,
 source varchar(191) NOT NULL, operation_id varchar(191) NOT NULL,
 fingerprint bytea NOT NULL, result jsonb NOT NULL, actor_id varchar(191) NOT NULL,
 reason text NOT NULL, created_at timestamptz NOT NULL,
 PRIMARY KEY(workspace_id,app_id,platform_id,platform_user_id,source,operation_id)
);
CREATE INDEX IF NOT EXISTS collections_operation_journal ON collections_operation(workspace_id,created_at DESC,id);
CREATE TABLE IF NOT EXISTS collections_change (
 id bigserial PRIMARY KEY, receipt_id uuid NOT NULL REFERENCES collections_operation(id) DEFERRABLE INITIALLY DEFERRED,
 workspace_id varchar(36) NOT NULL, collection_key varchar(191) NOT NULL, item_key varchar(191) NOT NULL,
 before_owned boolean NOT NULL, after_owned boolean NOT NULL, revision bigint NOT NULL,
 occurred_at timestamptz NOT NULL,
 UNIQUE(receipt_id,collection_key,item_key)
);
CREATE INDEX IF NOT EXISTS collections_change_stats ON collections_change(workspace_id,occurred_at,collection_key);
CREATE TABLE IF NOT EXISTS collections_stats_daily (
 workspace_id varchar(36) NOT NULL, stats_date date NOT NULL, grants bigint NOT NULL, revokes bigint NOT NULL,
 PRIMARY KEY(workspace_id,stats_date)
);
CREATE TABLE IF NOT EXISTS collections_catalog_audit (
 id bigserial PRIMARY KEY, workspace_id varchar(36) NOT NULL, actor_id varchar(191) NOT NULL,
 method varchar(64) NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS collections_clb_event (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    routing_key VARCHAR(96) NOT NULL,
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
    CONSTRAINT collections_clb_event_status_chk CHECK (status IN ('pending', 'processing', 'ok', 'reject')),
    CONSTRAINT collections_clb_event_key_uq UNIQUE (source_service, event_key),
    CONSTRAINT collections_clb_event_idempotency_uq UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS collections_clb_event_due_idx
    ON collections_clb_event (status, next_attempt_at, locked_until, id);

CREATE INDEX IF NOT EXISTS collections_clb_event_route_due_idx
    ON collections_clb_event (routing_key, status, next_attempt_at, locked_until, id);

CREATE INDEX IF NOT EXISTS collections_clb_event_type_idx
    ON collections_clb_event (workspace_id, source_service, event_type, status, created_at);

CREATE TABLE IF NOT EXISTS collections_catalog_version (
 workspace_id varchar(36) PRIMARY KEY,
 revision bigint NOT NULL CHECK(revision>0)
);
