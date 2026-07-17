-- name: CreateAccount :exec
INSERT INTO control_account (id, display_name) VALUES ($1, $2);

-- name: LockWorkspaceAuthorization :exec
SELECT pg_advisory_xact_lock(hashtextextended('control:authorization:' || $1::text, 0));

-- name: GetAccount :one
SELECT id, display_name, status, created_at, updated_at
FROM control_account WHERE id = $1 LIMIT 1;

-- name: FindAccountByIdentity :one
SELECT a.id, a.display_name, a.status, a.created_at, a.updated_at
FROM control_identity i
JOIN control_account a ON a.id = i.account_id
WHERE i.provider = $1 AND i.provider_subject = $2
LIMIT 1;

-- name: UpsertIdentity :exec
INSERT INTO control_identity (account_id, provider, provider_subject, payload)
VALUES ($1, $2, $3, $4)
ON CONFLICT (account_id, provider) DO UPDATE SET
    provider_subject = EXCLUDED.provider_subject,
    payload = EXCLUDED.payload,
    updated_at = now();

-- name: ListIdentities :many
SELECT account_id, provider, provider_subject, created_at, updated_at
FROM control_identity WHERE account_id = $1 ORDER BY provider;

-- name: CountIdentities :one
SELECT COUNT(*) AS count FROM control_identity WHERE account_id = $1;

-- name: DeleteIdentity :execrows
DELETE FROM control_identity WHERE account_id = $1 AND provider = $2;

-- name: CreateSession :exec
INSERT INTO control_session (id, account_id, token_hash, ip, user_agent, bind_to_ip, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetActiveSessionByHash :one
SELECT id, account_id, token_hash, ip, user_agent, bind_to_ip, expires_at, revoked_at, last_used_at, created_at
FROM control_session
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
LIMIT 1;

-- name: RevokeSession :execrows
UPDATE control_session SET revoked_at = now()
WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: RevokeAllSessions :execrows
UPDATE control_session SET revoked_at = now()
WHERE account_id = $1 AND revoked_at IS NULL AND ($2 = '' OR id <> $3);

-- name: TouchSession :execrows
UPDATE control_session SET last_used_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: ListSessions :many
SELECT id, account_id, token_hash, ip, user_agent, bind_to_ip, expires_at, revoked_at, last_used_at, created_at
FROM control_session WHERE account_id = $1 ORDER BY created_at DESC;

-- name: CreateWorkspace :exec
INSERT INTO control_workspace (id, slug, title, created_by) VALUES ($1, $2, $3, $4);

-- name: GetWorkspace :one
SELECT id, slug, title, status, created_by, created_at, updated_at
FROM control_workspace WHERE id = $1 LIMIT 1;

-- name: ListWorkspacesForAccount :many
SELECT w.id, w.slug, w.title, w.status, w.created_by, w.created_at, w.updated_at
FROM control_workspace w
JOIN control_workspace_member m ON m.workspace_id = w.id
WHERE m.account_id = $1 AND m.status = 'active'
ORDER BY w.created_at DESC LIMIT $2 OFFSET $3;

-- name: AddWorkspaceMember :exec
INSERT INTO control_workspace_member (workspace_id, account_id)
VALUES ($1, $2)
ON CONFLICT (workspace_id, account_id) DO UPDATE SET
    status = 'active',
    updated_at = now();

-- name: ListWorkspaceMembers :many
SELECT m.workspace_id, m.account_id, m.status, m.joined_at, m.updated_at,
       a.display_name, COALESCE(MIN(r.position), 2147483647) AS position
FROM control_workspace_member m
JOIN control_account a ON a.id = m.account_id
LEFT JOIN control_role_member rm ON rm.account_id = m.account_id
LEFT JOIN control_role r ON r.id = rm.role_id AND r.workspace_id = m.workspace_id AND r.deleted_at IS NULL
WHERE m.workspace_id = $1 AND m.status = 'active'
GROUP BY m.workspace_id, m.account_id, m.status, m.joined_at, m.updated_at, a.display_name
ORDER BY position, m.joined_at
LIMIT $2 OFFSET $3;

-- name: RemoveWorkspaceMember :execrows
UPDATE control_workspace_member SET status = 'removed'
WHERE workspace_id = $1 AND account_id = $2 AND status = 'active';

-- name: RemoveWorkspaceMemberRoles :execrows
DELETE FROM control_role_member rm
USING control_role r
WHERE r.id = rm.role_id
  AND r.workspace_id = $1
  AND rm.account_id = $2;

-- name: UpdateWorkspace :execrows
UPDATE control_workspace SET slug = $1, title = $2, status = $3 WHERE id = $4;

-- name: UpdateWorkspaceAsActiveMember :execrows
UPDATE control_workspace w
SET slug = $1, title = $2, status = $3
WHERE w.id = $4
  AND EXISTS (
      SELECT 1 FROM control_workspace_member m
      WHERE m.workspace_id = w.id AND m.account_id = $5 AND m.status = 'active'
  );

-- name: CreateInvite :exec
INSERT INTO control_workspace_invite (id, workspace_id, created_by, token_hash, max_uses, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: AddInviteRole :exec
INSERT INTO control_workspace_invite_role (invite_id, role_id) VALUES ($1, $2);

-- name: GetInviteByHashForUpdate :one
SELECT id, workspace_id, created_by, token_hash, max_uses, used_count, expires_at, revoked_at, created_at
FROM control_workspace_invite
WHERE token_hash = $1
LIMIT 1 FOR UPDATE;

-- name: ListInviteRoles :many
SELECT role_id FROM control_workspace_invite_role WHERE invite_id = $1 ORDER BY role_id;

-- name: CreateInviteAcceptance :execrows
INSERT INTO control_workspace_invite_acceptance (invite_id, account_id)
VALUES ($1, $2)
ON CONFLICT (invite_id, account_id) DO NOTHING;

-- name: IncrementInviteUse :execrows
UPDATE control_workspace_invite SET used_count = used_count + 1
WHERE id = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
  AND (max_uses IS NULL OR used_count < max_uses);

-- name: ListInvites :many
SELECT id, workspace_id, created_by, token_hash, max_uses, used_count, expires_at, revoked_at, created_at
FROM control_workspace_invite WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: RevokeInvite :execrows
UPDATE control_workspace_invite SET revoked_at = now()
WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL;

-- name: RevokeInviteAsActiveMember :execrows
UPDATE control_workspace_invite i
SET revoked_at = now()
WHERE i.id = $1 AND i.workspace_id = $2 AND i.revoked_at IS NULL
  AND EXISTS (
      SELECT 1 FROM control_workspace_member m
      WHERE m.workspace_id = i.workspace_id AND m.account_id = $3 AND m.status = 'active'
  );

-- name: CreateRole :exec
INSERT INTO control_role (id, workspace_id, code, title, description, position, is_owner)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: AddRoleMember :exec
INSERT INTO control_role_member (role_id, account_id)
VALUES ($1, $2)
ON CONFLICT (role_id, account_id) DO NOTHING;

-- name: RemoveRoleMember :execrows
DELETE FROM control_role_member WHERE role_id = $1 AND account_id = $2;

-- name: UpdateRole :execrows
UPDATE control_role SET title = $1, description = $2, position = $3
WHERE id = $4 AND workspace_id = $5 AND deleted_at IS NULL AND is_owner = FALSE;

-- name: DeleteRole :execrows
UPDATE control_role SET deleted_at = now()
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL AND is_owner = FALSE;

-- name: ListRoles :many
SELECT r.id, r.workspace_id, r.code, r.title, r.description, r.position, r.is_owner, r.deleted_at, r.created_at, r.updated_at,
       COUNT(rm.account_id) AS member_count
FROM control_role r
LEFT JOIN control_role_member rm ON rm.role_id = r.id
WHERE r.workspace_id = $1 AND r.deleted_at IS NULL
GROUP BY r.id
ORDER BY r.position, r.id;

-- name: ListRolePermissions :many
SELECT p.role_id, p.method_key, p.created_at
FROM control_role_permission p
JOIN control_role r ON r.id = p.role_id
WHERE r.workspace_id = $1 AND p.role_id = $2 AND r.deleted_at IS NULL
ORDER BY p.method_key;

-- name: UpsertMethod :exec
INSERT INTO control_method (method_key, service, group_key, position)
VALUES ($1, $2, $3, $4)
ON CONFLICT (method_key) DO UPDATE SET
    service = EXCLUDED.service,
    group_key = EXCLUDED.group_key,
    position = EXCLUDED.position,
    updated_at = now();

-- name: UpsertMethodGroup :exec
INSERT INTO control_method_group (service, group_key, position)
VALUES ($1, $2, $3)
ON CONFLICT (service, group_key) DO UPDATE SET
    position = EXCLUDED.position,
    updated_at = now();

-- name: LockMethodRegistry :exec
SELECT pg_advisory_xact_lock(hashtextextended('control:method-registry', 0));

-- name: ListMethodGroups :many
SELECT service, group_key, position, created_at, updated_at
FROM control_method_group
ORDER BY service, group_key;

-- name: ListAccessCatalog :many
SELECT g.service, service_catalog.position AS service_position, g.group_key, g.position AS group_position,
       COALESCE(service_title.value, g.service) AS service_title,
       COALESCE(service_description.value, '') AS service_description,
       COALESCE(group_loc.value, g.group_key) AS group_title,
       COALESCE(group_description.value, '') AS group_description,
       m.method_key, m.position,
       COALESCE(access_loc.value, m.method_key) AS access_title,
       COALESCE(access_description.value, '') AS access_description
FROM control_method_group g
JOIN control_method m ON m.service = g.service AND m.group_key = g.group_key
JOIN control_access_service service_catalog ON service_catalog.service = g.service
LEFT JOIN control_localization service_title
    ON service_title.localization_key = 'control.access_service.' || g.service || '.title'
   AND service_title.locale = $1
LEFT JOIN control_localization service_description
    ON service_description.localization_key = 'control.access_service.' || g.service || '.description'
   AND service_description.locale = $2
LEFT JOIN control_localization group_loc
    ON group_loc.localization_key = 'control.method_group.' || g.service || '.' || g.group_key
   AND group_loc.locale = $3
LEFT JOIN control_localization group_description
    ON group_description.localization_key = 'control.method_group.' || g.service || '.' || g.group_key || '.description'
   AND group_description.locale = $4
LEFT JOIN control_localization access_loc
    ON access_loc.localization_key = 'control.method.' || m.method_key
   AND access_loc.locale = $5
LEFT JOIN control_localization access_description
    ON access_description.localization_key = 'control.method.' || m.method_key || '.description'
   AND access_description.locale = $6
ORDER BY service_catalog.position, g.position, m.position, m.method_key;

-- name: GetMethod :one
SELECT method_key, service, group_key, position, created_at, updated_at
FROM control_method WHERE method_key = $1 LIMIT 1;

-- name: ListMethods :many
SELECT method_key, service, group_key, position, created_at, updated_at
FROM control_method ORDER BY service, group_key, method_key;

-- name: SetRolePermission :exec
INSERT INTO control_role_permission (role_id, method_key)
VALUES ($1, $2)
ON CONFLICT (role_id, method_key) DO NOTHING;

-- name: DeleteRolePermission :execrows
DELETE FROM control_role_permission WHERE role_id = $1 AND method_key = $2;

-- name: ClearRolePermissions :execrows
DELETE FROM control_role_permission WHERE role_id = $1;

-- name: CheckAccess :one
SELECT EXISTS(
    SELECT 1
    FROM control_workspace_member m
    JOIN control_method cm ON cm.method_key = $1
    JOIN control_role_member rm ON rm.account_id = m.account_id
    JOIN control_role r ON r.id = rm.role_id
    LEFT JOIN control_role_permission p ON p.role_id = r.id AND p.method_key = cm.method_key
    WHERE m.workspace_id = $2 AND m.account_id = $3 AND m.status = 'active'
      AND r.workspace_id = m.workspace_id AND r.deleted_at IS NULL
      AND (r.is_owner = TRUE OR p.method_key IS NOT NULL)
) AS allowed;

-- name: ListAuthorizedMethods :many
SELECT DISTINCT cm.method_key, cm.service, cm.group_key, cm.position
FROM control_method cm
JOIN control_workspace_member m ON m.workspace_id = $1 AND m.account_id = $2 AND m.status = 'active'
JOIN control_role_member rm ON rm.account_id = m.account_id
JOIN control_role r ON r.id = rm.role_id AND r.workspace_id = m.workspace_id AND r.deleted_at IS NULL
LEFT JOIN control_role_permission p ON p.role_id = r.id AND p.method_key = cm.method_key
WHERE r.is_owner = TRUE OR p.method_key IS NOT NULL
ORDER BY cm.service, cm.group_key, cm.method_key;

-- name: GetAccountPosition :one
SELECT COALESCE((
    SELECT r.position
    FROM control_workspace_member m
    JOIN control_role_member rm ON rm.account_id = m.account_id
    JOIN control_role r ON r.id = rm.role_id
    WHERE m.workspace_id = $1 AND m.account_id = $2 AND m.status = 'active'
      AND r.workspace_id = m.workspace_id AND r.deleted_at IS NULL
    ORDER BY r.position
    LIMIT 1
), 2147483647) AS position;

-- name: IsActiveWorkspaceMember :one
SELECT EXISTS(
    SELECT 1 FROM control_workspace_member
    WHERE workspace_id = $1 AND account_id = $2 AND status = 'active'
) AS active;

-- name: GetRole :one
SELECT id, workspace_id, code, title, description, position, is_owner, deleted_at, created_at, updated_at
FROM control_role WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL LIMIT 1;

-- name: CreateAuditEvent :exec
INSERT INTO control_audit_event (
    id, workspace_id, actor_id, method_key, target_type, target_id,
    before_data, after_data, result, request_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UpsertTwoFactor :exec
INSERT INTO control_two_factor (account_id, secret, backup_hashes, activated_at)
VALUES ($1, $2, $3, NULL)
ON CONFLICT (account_id) DO UPDATE SET
    secret = EXCLUDED.secret,
    backup_hashes = EXCLUDED.backup_hashes,
    activated_at = NULL,
    updated_at = now();

-- name: GetTwoFactor :one
SELECT account_id, secret, backup_hashes, activated_at, created_at, updated_at
FROM control_two_factor WHERE account_id = $1 LIMIT 1;

-- name: GetTwoFactorForUpdate :one
SELECT account_id, secret, backup_hashes, activated_at, created_at, updated_at
FROM control_two_factor WHERE account_id = $1 LIMIT 1 FOR UPDATE;

-- name: ActivateTwoFactor :execrows
UPDATE control_two_factor SET activated_at = now()
WHERE account_id = $1 AND activated_at IS NULL;

-- name: DeleteTwoFactor :execrows
DELETE FROM control_two_factor WHERE account_id = $1;

-- name: HasActiveTwoFactor :one
SELECT EXISTS(
    SELECT 1 FROM control_two_factor WHERE account_id = $1 AND activated_at IS NOT NULL
) AS active;

-- name: CreateTwoFactorChallenge :exec
INSERT INTO control_two_factor_challenge (
    id,
    account_id,
    token_hash,
    ip,
    user_agent,
    bind_to_ip,
    expires_at,
    session_expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetTwoFactorChallengeForUpdate :one
SELECT id, account_id, token_hash, ip, user_agent, bind_to_ip, expires_at,
       session_expires_at, created_at
FROM control_two_factor_challenge
WHERE token_hash = $1 AND expires_at > now()
LIMIT 1 FOR UPDATE;

-- name: GetTwoFactorChallengeWithFactorForUpdate :one
SELECT c.id AS challenge_id, c.account_id, c.ip, c.user_agent, c.bind_to_ip,
       c.expires_at, c.session_expires_at,
       f.secret, f.backup_hashes, f.activated_at
FROM control_two_factor_challenge c
JOIN control_two_factor f ON f.account_id = c.account_id
WHERE c.token_hash = $1 AND c.expires_at > now()
LIMIT 1 FOR UPDATE;

-- name: DeleteTwoFactorChallenge :execrows
DELETE FROM control_two_factor_challenge WHERE id = $1;

-- name: UpdateTwoFactorBackupHashes :execrows
UPDATE control_two_factor SET backup_hashes = $1 WHERE account_id = $2 AND activated_at IS NOT NULL;

-- name: UpdatePendingTwoFactorBackupHashes :execrows
UPDATE control_two_factor SET backup_hashes = $1 WHERE account_id = $2 AND activated_at IS NULL;

-- name: ListAuditEvents :many
SELECT id, workspace_id, actor_id, method_key, target_type, target_id,
       COALESCE(before_data, '{}'::jsonb) AS before_data, COALESCE(after_data, '{}'::jsonb) AS after_data, result, request_id, occurred_at
FROM control_audit_event
WHERE workspace_id = $1
ORDER BY occurred_at DESC, id DESC LIMIT $2 OFFSET $3;

-- name: ListAuditEventsFiltered :many
SELECT id, workspace_id, actor_id, method_key, target_type, target_id,
       COALESCE(before_data, '{}'::jsonb) AS before_data, COALESCE(after_data, '{}'::jsonb) AS after_data, result, request_id, occurred_at
FROM control_audit_event
WHERE workspace_id = $1
  AND ($2 = '' OR method_key = $3)
  AND ($4 = '' OR actor_id = $5)
  AND ($6 IS NULL OR occurred_at >= $7)
  AND ($8 IS NULL OR occurred_at < $9)
ORDER BY occurred_at DESC, id DESC LIMIT $10 OFFSET $11;
