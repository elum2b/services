CREATE TABLE IF NOT EXISTS control_account (
    id VARCHAR(64) NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_account_status_chk CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE IF NOT EXISTS control_identity (
    account_id VARCHAR(64) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    provider_subject VARCHAR(255) NOT NULL,
    payload JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, provider),
    CONSTRAINT control_identity_provider_subject_uq UNIQUE (provider, provider_subject),
    CONSTRAINT control_identity_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS control_session (
    id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    ip VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(255) NOT NULL DEFAULT '',
    bind_to_ip BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_session_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT control_session_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_session_account_idx ON control_session (account_id, revoked_at, expires_at);
CREATE INDEX IF NOT EXISTS control_session_account_created_idx ON control_session (account_id, created_at);

CREATE TABLE IF NOT EXISTS control_workspace (
    id VARCHAR(36) NOT NULL,
    slug VARCHAR(128) NOT NULL,
    title VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_workspace_slug_uq UNIQUE (slug),
    CONSTRAINT control_workspace_status_chk CHECK (status IN ('active', 'archived')),
    CONSTRAINT control_workspace_creator_fk FOREIGN KEY (created_by)
        REFERENCES control_account (id)
);

CREATE TABLE IF NOT EXISTS control_workspace_member (
    workspace_id VARCHAR(36) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, account_id),
    CONSTRAINT control_member_status_chk CHECK (status IN ('active', 'removed')),
    CONSTRAINT control_member_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE,
    CONSTRAINT control_member_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_member_account_idx ON control_workspace_member (account_id, status);

CREATE TABLE IF NOT EXISTS control_workspace_invite (
    id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    created_by VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    max_uses INTEGER NULL,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_invite_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT control_invite_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE,
    CONSTRAINT control_invite_creator_fk FOREIGN KEY (created_by)
        REFERENCES control_account (id)
);

CREATE INDEX IF NOT EXISTS control_invite_workspace_idx ON control_workspace_invite (workspace_id, revoked_at, expires_at);
CREATE INDEX IF NOT EXISTS control_invite_workspace_created_idx ON control_workspace_invite (workspace_id, created_at);

UPDATE control_workspace_invite
SET revoked_at = COALESCE(revoked_at, now()),
    max_uses = NULL
WHERE max_uses IS NOT NULL AND max_uses <= 0;

UPDATE control_workspace_invite
SET max_uses = used_count
WHERE max_uses IS NOT NULL AND used_count > max_uses;

ALTER TABLE control_workspace_invite
    DROP CONSTRAINT IF EXISTS control_invite_uses_chk;

ALTER TABLE control_workspace_invite
    ADD CONSTRAINT control_invite_uses_chk CHECK (
        used_count >= 0
        AND (max_uses IS NULL OR (max_uses > 0 AND used_count <= max_uses))
    );

CREATE TABLE IF NOT EXISTS control_workspace_invite_acceptance (
    invite_id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (invite_id, account_id),
    CONSTRAINT control_invite_acceptance_invite_fk FOREIGN KEY (invite_id)
        REFERENCES control_workspace_invite (id) ON DELETE CASCADE,
    CONSTRAINT control_invite_acceptance_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_invite_acceptance_account_idx
    ON control_workspace_invite_acceptance (account_id, accepted_at);

CREATE TABLE IF NOT EXISTS control_role (
    id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    code VARCHAR(128) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    position INTEGER NOT NULL,
    is_owner BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_role_workspace_code_uq UNIQUE (workspace_id, code),
    CONSTRAINT control_role_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_role_workspace_position_idx ON control_role (workspace_id, deleted_at, position);

CREATE TABLE IF NOT EXISTS control_role_member (
    role_id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, account_id),
    CONSTRAINT control_role_member_role_fk FOREIGN KEY (role_id)
        REFERENCES control_role (id) ON DELETE CASCADE,
    CONSTRAINT control_role_member_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_role_member_account_idx ON control_role_member (account_id);

CREATE TABLE IF NOT EXISTS control_workspace_invite_role (
    invite_id VARCHAR(64) NOT NULL,
    role_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (invite_id, role_id),
    CONSTRAINT control_invite_role_invite_fk FOREIGN KEY (invite_id)
        REFERENCES control_workspace_invite (id) ON DELETE CASCADE,
    CONSTRAINT control_invite_role_role_fk FOREIGN KEY (role_id)
        REFERENCES control_role (id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS control_method (
    method_key VARCHAR(255) NOT NULL,
    service VARCHAR(64) NOT NULL,
    group_key VARCHAR(128) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (method_key)
);

CREATE INDEX IF NOT EXISTS control_method_service_idx ON control_method (service, group_key);

CREATE TABLE IF NOT EXISTS control_method_group (
    service VARCHAR(64) NOT NULL,
    group_key VARCHAR(128) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service, group_key)
);

CREATE TABLE IF NOT EXISTS control_access_service (
    service VARCHAR(64) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service)
);

CREATE TABLE IF NOT EXISTS control_localization (
    localization_key VARCHAR(255) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (localization_key, locale)
);

CREATE TABLE IF NOT EXISTS control_role_permission (
    role_id VARCHAR(64) NOT NULL,
    method_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, method_key),
    CONSTRAINT control_permission_role_fk FOREIGN KEY (role_id)
        REFERENCES control_role (id) ON DELETE CASCADE,
    CONSTRAINT control_permission_method_fk FOREIGN KEY (method_key)
        REFERENCES control_method (method_key) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_permission_method_idx ON control_role_permission (method_key);

CREATE TABLE IF NOT EXISTS control_two_factor (
    account_id VARCHAR(64) NOT NULL,
    secret VARCHAR(128) NOT NULL,
    backup_hashes JSONB NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id),
    CONSTRAINT control_two_factor_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS control_two_factor_challenge (
    id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    ip VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(255) NOT NULL DEFAULT '',
    bind_to_ip BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    session_expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_two_factor_challenge_token_uq UNIQUE (token_hash),
    CONSTRAINT control_two_factor_challenge_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

ALTER TABLE control_two_factor_challenge
    ADD COLUMN IF NOT EXISTS session_expires_at TIMESTAMPTZ;

UPDATE control_two_factor_challenge
SET session_expires_at = created_at + INTERVAL '30 days'
WHERE session_expires_at IS NULL;

ALTER TABLE control_two_factor_challenge
    ALTER COLUMN session_expires_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS control_two_factor_challenge_account_idx ON control_two_factor_challenge (account_id, expires_at);

CREATE TABLE IF NOT EXISTS control_audit_event (
    id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NULL,
    actor_id VARCHAR(64) NULL,
    method_key VARCHAR(255) NOT NULL,
    target_type VARCHAR(64) NOT NULL DEFAULT '',
    target_id VARCHAR(128) NOT NULL DEFAULT '',
    before_data JSONB NULL,
    after_data JSONB NULL,
    result VARCHAR(32) NOT NULL,
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_audit_result_chk CHECK (result IN ('succeeded', 'failed'))
);

CREATE INDEX IF NOT EXISTS control_audit_workspace_idx ON control_audit_event (workspace_id, occurred_at);
CREATE INDEX IF NOT EXISTS control_audit_actor_idx ON control_audit_event (actor_id, occurred_at);
