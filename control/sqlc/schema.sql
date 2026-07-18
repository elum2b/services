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

CREATE TABLE IF NOT EXISTS control_platform (
    id SMALLINT NOT NULL DEFAULT 1,
    owner_account_id VARCHAR(64) NOT NULL,
    initialized_by VARCHAR(64) NOT NULL,
    initialized_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_platform_singleton_chk CHECK (id = 1),
    CONSTRAINT control_platform_owner_fk FOREIGN KEY (owner_account_id)
        REFERENCES control_account (id) ON DELETE RESTRICT,
    CONSTRAINT control_platform_initializer_fk FOREIGN KEY (initialized_by)
        REFERENCES control_account (id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS control_platform_member (
    account_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    workspace_limit INTEGER NOT NULL DEFAULT 1,
    invited_by VARCHAR(64) NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id),
    CONSTRAINT control_platform_member_status_chk CHECK (status IN ('active', 'removed')),
    CONSTRAINT control_platform_member_workspace_limit_chk CHECK (workspace_limit >= 0),
    CONSTRAINT control_platform_member_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE,
    CONSTRAINT control_platform_member_inviter_fk FOREIGN KEY (invited_by)
        REFERENCES control_account (id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS control_platform_member_status_idx
    ON control_platform_member (status, joined_at DESC, account_id DESC);

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

CREATE INDEX IF NOT EXISTS control_session_account_created_idx
    ON control_session (account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS control_access_service (
    service VARCHAR(64) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service)
);

CREATE TABLE IF NOT EXISTS control_method_group (
    service VARCHAR(64) NOT NULL,
    group_key VARCHAR(128) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service, group_key)
);

CREATE TABLE IF NOT EXISTS control_method (
    method_key VARCHAR(255) NOT NULL,
    service VARCHAR(64) NOT NULL,
    group_key VARCHAR(128) NOT NULL,
    scope VARCHAR(16) GENERATED ALWAYS AS (
        CASE
            WHEN method_key LIKE 'control.global.%' THEN 'global'
            ELSE 'workspace'
        END
    ) STORED,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (method_key),
    CONSTRAINT control_method_group_fk FOREIGN KEY (service, group_key)
        REFERENCES control_method_group (service, group_key) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_method_catalog_idx
    ON control_method (scope, service, group_key, position, method_key);

CREATE TABLE IF NOT EXISTS control_localization (
    localization_key VARCHAR(255) NOT NULL,
    locale VARCHAR(16) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (localization_key, locale)
);

CREATE TABLE IF NOT EXISTS control_global_role (
    id VARCHAR(64) NOT NULL,
    code VARCHAR(128) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_global_role_code_uq UNIQUE (code),
    CONSTRAINT control_global_role_position_chk CHECK (position > 0)
);

CREATE INDEX IF NOT EXISTS control_global_role_position_idx
    ON control_global_role (position, id);

CREATE TABLE IF NOT EXISTS control_global_role_member (
    role_id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, account_id),
    CONSTRAINT control_global_role_member_role_fk FOREIGN KEY (role_id)
        REFERENCES control_global_role (id) ON DELETE CASCADE,
    CONSTRAINT control_global_role_member_account_fk FOREIGN KEY (account_id)
        REFERENCES control_platform_member (account_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_global_role_member_account_idx
    ON control_global_role_member (account_id, role_id);

CREATE TABLE IF NOT EXISTS control_global_role_permission (
    role_id VARCHAR(64) NOT NULL,
    method_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, method_key),
    CONSTRAINT control_global_permission_role_fk FOREIGN KEY (role_id)
        REFERENCES control_global_role (id) ON DELETE CASCADE,
    CONSTRAINT control_global_permission_method_fk FOREIGN KEY (method_key)
        REFERENCES control_method (method_key) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_global_permission_method_idx
    ON control_global_role_permission (method_key, role_id);

CREATE TABLE IF NOT EXISTS control_workspace (
    id VARCHAR(36) NOT NULL,
    slug VARCHAR(128) NOT NULL,
    title VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_by VARCHAR(64) NOT NULL,
    owner_account_id VARCHAR(64) NOT NULL,
    employee_limit INTEGER NOT NULL DEFAULT 10,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_workspace_slug_uq UNIQUE (slug),
    CONSTRAINT control_workspace_status_chk CHECK (status IN ('active', 'archived')),
    CONSTRAINT control_workspace_employee_limit_chk CHECK (employee_limit >= 0),
    CONSTRAINT control_workspace_creator_fk FOREIGN KEY (created_by)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT,
    CONSTRAINT control_workspace_owner_fk FOREIGN KEY (owner_account_id)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_workspace_owner_idx
    ON control_workspace (owner_account_id, status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS control_workspace_member (
    workspace_id VARCHAR(36) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, account_id),
    CONSTRAINT control_workspace_member_status_chk CHECK (status IN ('active', 'removed')),
    CONSTRAINT control_workspace_member_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE,
    CONSTRAINT control_workspace_member_account_fk FOREIGN KEY (account_id)
        REFERENCES control_platform_member (account_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_workspace_member_account_idx
    ON control_workspace_member (account_id, status, joined_at DESC, workspace_id DESC);

CREATE TABLE IF NOT EXISTS control_workspace_role (
    id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    code VARCHAR(128) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_workspace_role_scope_uq UNIQUE (id, workspace_id),
    CONSTRAINT control_workspace_role_code_uq UNIQUE (workspace_id, code),
    CONSTRAINT control_workspace_role_position_chk CHECK (position > 0),
    CONSTRAINT control_workspace_role_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_workspace_role_position_idx
    ON control_workspace_role (workspace_id, position, id);

CREATE TABLE IF NOT EXISTS control_workspace_role_member (
    role_id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, account_id),
    CONSTRAINT control_workspace_role_member_role_fk FOREIGN KEY (role_id, workspace_id)
        REFERENCES control_workspace_role (id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT control_workspace_role_member_account_fk FOREIGN KEY (workspace_id, account_id)
        REFERENCES control_workspace_member (workspace_id, account_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS control_workspace_role_member_account_idx
    ON control_workspace_role_member (workspace_id, account_id, role_id);

CREATE TABLE IF NOT EXISTS control_workspace_role_permission (
    role_id VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    method_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, method_key),
    CONSTRAINT control_workspace_permission_role_fk FOREIGN KEY (role_id, workspace_id)
        REFERENCES control_workspace_role (id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT control_workspace_permission_method_fk FOREIGN KEY (method_key)
        REFERENCES control_method (method_key) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_workspace_permission_method_idx
    ON control_workspace_role_permission (workspace_id, method_key, role_id);

CREATE TABLE IF NOT EXISTS control_invite (
    id VARCHAR(64) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    workspace_id VARCHAR(36) NULL,
    created_by VARCHAR(64) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    accepted_by VARCHAR(64) NULL,
    accepted_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT control_invite_token_hash_uq UNIQUE (token_hash),
    CONSTRAINT control_invite_kind_uq UNIQUE (id, kind),
    CONSTRAINT control_invite_kind_scope_uq UNIQUE (id, kind, workspace_id),
    CONSTRAINT control_invite_kind_chk CHECK (kind IN ('global', 'workspace')),
    CONSTRAINT control_invite_scope_chk CHECK (
        (kind = 'global' AND workspace_id IS NULL)
        OR (kind = 'workspace' AND workspace_id IS NOT NULL)
    ),
    CONSTRAINT control_invite_acceptance_chk CHECK (
        (accepted_by IS NULL AND accepted_at IS NULL)
        OR (accepted_by IS NOT NULL AND accepted_at IS NOT NULL)
    ),
    CONSTRAINT control_invite_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE,
    CONSTRAINT control_invite_creator_fk FOREIGN KEY (created_by)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT,
    CONSTRAINT control_invite_acceptor_fk FOREIGN KEY (accepted_by)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_invite_workspace_pending_idx
    ON control_invite (workspace_id, expires_at, created_at DESC, id DESC)
    WHERE kind = 'workspace' AND accepted_at IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS control_invite_global_created_idx
    ON control_invite (created_at DESC, id DESC)
    WHERE kind = 'global';

CREATE TABLE IF NOT EXISTS control_invite_global_role (
    invite_id VARCHAR(64) NOT NULL,
    invite_kind VARCHAR(16) NOT NULL DEFAULT 'global',
    role_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (invite_id, role_id),
    CONSTRAINT control_invite_global_role_kind_chk CHECK (invite_kind = 'global'),
    CONSTRAINT control_invite_global_role_invite_fk FOREIGN KEY (invite_id, invite_kind)
        REFERENCES control_invite (id, kind) ON DELETE CASCADE,
    CONSTRAINT control_invite_global_role_role_fk FOREIGN KEY (role_id)
        REFERENCES control_global_role (id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS control_invite_workspace_role (
    invite_id VARCHAR(64) NOT NULL,
    invite_kind VARCHAR(16) NOT NULL DEFAULT 'workspace',
    workspace_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (invite_id, role_id),
    CONSTRAINT control_invite_workspace_role_kind_chk CHECK (invite_kind = 'workspace'),
    CONSTRAINT control_invite_workspace_role_invite_fk FOREIGN KEY (invite_id, invite_kind, workspace_id)
        REFERENCES control_invite (id, kind, workspace_id) ON DELETE CASCADE,
    CONSTRAINT control_invite_workspace_role_role_fk FOREIGN KEY (role_id, workspace_id)
        REFERENCES control_workspace_role (id, workspace_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS control_two_factor (
    account_id VARCHAR(64) NOT NULL,
    secret VARCHAR(128) NOT NULL,
    backup_hashes JSONB NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    last_totp_counter BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id),
    CONSTRAINT control_two_factor_account_fk FOREIGN KEY (account_id)
        REFERENCES control_account (id) ON DELETE CASCADE
);

ALTER TABLE control_two_factor
    ADD COLUMN IF NOT EXISTS last_totp_counter BIGINT NULL;

CREATE TABLE IF NOT EXISTS control_two_factor_challenge (
    id VARCHAR(64) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    invite_id VARCHAR(64) NULL,
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
        REFERENCES control_account (id) ON DELETE CASCADE,
    CONSTRAINT control_two_factor_challenge_invite_fk FOREIGN KEY (invite_id)
        REFERENCES control_invite (id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS control_two_factor_challenge_account_idx
    ON control_two_factor_challenge (account_id, expires_at);

CREATE TABLE IF NOT EXISTS control_limit_request (
    id VARCHAR(64) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    account_id VARCHAR(64) NULL,
    workspace_id VARCHAR(36) NULL,
    current_limit INTEGER NOT NULL,
    requested_limit INTEGER NOT NULL,
    approved_limit INTEGER NULL,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    requested_by VARCHAR(64) NOT NULL,
    reviewed_by VARCHAR(64) NULL,
    review_comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ NULL,
    PRIMARY KEY (id),
    CONSTRAINT control_limit_request_kind_chk CHECK (
        kind IN ('account_workspace', 'workspace_employee')
    ),
    CONSTRAINT control_limit_request_scope_chk CHECK (
        (kind = 'account_workspace' AND account_id IS NOT NULL AND workspace_id IS NULL)
        OR (kind = 'workspace_employee' AND account_id IS NULL AND workspace_id IS NOT NULL)
    ),
    CONSTRAINT control_limit_request_value_chk CHECK (
        current_limit >= 0
        AND requested_limit > current_limit
        AND (approved_limit IS NULL OR approved_limit >= current_limit)
    ),
    CONSTRAINT control_limit_request_status_chk CHECK (
        status IN ('pending', 'approved', 'rejected', 'cancelled')
    ),
    CONSTRAINT control_limit_request_account_fk FOREIGN KEY (account_id)
        REFERENCES control_platform_member (account_id) ON DELETE CASCADE,
    CONSTRAINT control_limit_request_workspace_fk FOREIGN KEY (workspace_id)
        REFERENCES control_workspace (id) ON DELETE CASCADE,
    CONSTRAINT control_limit_request_requester_fk FOREIGN KEY (requested_by)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT,
    CONSTRAINT control_limit_request_reviewer_fk FOREIGN KEY (reviewed_by)
        REFERENCES control_platform_member (account_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS control_limit_request_account_pending_uq
    ON control_limit_request (account_id)
    WHERE kind = 'account_workspace' AND status = 'pending';

CREATE UNIQUE INDEX IF NOT EXISTS control_limit_request_workspace_pending_uq
    ON control_limit_request (workspace_id)
    WHERE kind = 'workspace_employee' AND status = 'pending';

CREATE INDEX IF NOT EXISTS control_limit_request_status_created_idx
    ON control_limit_request (status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS control_audit_event (
    id VARCHAR(64) NOT NULL,
    scope VARCHAR(16) NOT NULL,
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
    CONSTRAINT control_audit_scope_chk CHECK (
        (scope = 'global' AND workspace_id IS NULL)
        OR (scope = 'workspace' AND workspace_id IS NOT NULL)
    ),
    CONSTRAINT control_audit_result_chk CHECK (result IN ('succeeded', 'failed'))
);

CREATE INDEX IF NOT EXISTS control_audit_workspace_idx
    ON control_audit_event (workspace_id, occurred_at DESC, id DESC)
    WHERE scope = 'workspace';

CREATE INDEX IF NOT EXISTS control_audit_global_idx
    ON control_audit_event (occurred_at DESC, id DESC)
    WHERE scope = 'global';

CREATE INDEX IF NOT EXISTS control_audit_actor_idx
    ON control_audit_event (actor_id, occurred_at DESC, id DESC);
