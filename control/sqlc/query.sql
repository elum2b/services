-- name: LockInitialization :exec
SELECT pg_advisory_xact_lock(hashtextextended('control:initialize', 0));

-- name: GetPlatform :one
SELECT id, owner_account_id, initialized_by, initialized_at, updated_at
FROM control_platform
WHERE id = 1;

-- name: CreatePlatform :exec
INSERT INTO control_platform (id, owner_account_id, initialized_by)
VALUES (1, $1, $1);

-- name: CreateAccount :exec
INSERT INTO control_account (id, display_name)
VALUES ($1, $2);

-- name: GetAccount :one
SELECT id, display_name, status, created_at, updated_at
FROM control_account
WHERE id = $1;

-- name: FindAuthPrincipalByIdentity :one
SELECT
    a.id,
    a.display_name,
    a.status,
    a.created_at,
    a.updated_at,
    pm.status AS platform_status,
    pm.workspace_limit,
    EXISTS (
        SELECT 1
        FROM control_two_factor tf
        WHERE tf.account_id = a.id
          AND tf.activated_at IS NOT NULL
    ) AS two_factor_enabled
FROM control_identity i
JOIN control_account a ON a.id = i.account_id
JOIN control_platform_member pm ON pm.account_id = a.id
WHERE i.provider = $1
  AND i.provider_subject = $2;

-- name: UpsertIdentity :exec
INSERT INTO control_identity (account_id, provider, provider_subject, payload)
VALUES ($1, $2, $3, $4)
ON CONFLICT (account_id, provider) DO UPDATE SET
    provider_subject = EXCLUDED.provider_subject,
    payload = EXCLUDED.payload,
    updated_at = now();

-- name: ListIdentities :many
SELECT account_id, provider, provider_subject, created_at, updated_at
FROM control_identity
WHERE account_id = $1
ORDER BY provider;

-- name: GetIdentity :one
SELECT account_id, provider, provider_subject, payload, created_at, updated_at
FROM control_identity
WHERE account_id = $1 AND provider = $2;

-- name: CountIdentities :one
SELECT COUNT(*) FROM control_identity WHERE account_id = $1;

-- name: DeleteIdentity :execrows
DELETE FROM control_identity
WHERE account_id = $1 AND provider = $2;

-- name: AddPlatformMember :exec
INSERT INTO control_platform_member (account_id, invited_by)
VALUES ($1, $2)
ON CONFLICT (account_id) DO UPDATE SET
    status = 'active',
    invited_by = COALESCE(control_platform_member.invited_by, EXCLUDED.invited_by),
    updated_at = now();

-- name: GetPlatformMember :one
SELECT account_id, status, workspace_limit, invited_by, joined_at, updated_at
FROM control_platform_member
WHERE account_id = $1;

-- name: GetPlatformMemberForUpdate :one
SELECT account_id, status, workspace_limit, invited_by, joined_at, updated_at
FROM control_platform_member
WHERE account_id = $1
FOR UPDATE;

-- name: ListPlatformMembers :many
SELECT
    pm.account_id,
    a.display_name,
    pm.status,
    pm.workspace_limit,
    pm.invited_by,
    pm.joined_at,
    pm.updated_at,
    COUNT(w.id) AS owned_workspace_count
FROM control_platform_member pm
JOIN control_account a ON a.id = pm.account_id
LEFT JOIN control_workspace w
    ON w.owner_account_id = pm.account_id
   AND w.status = 'active'
WHERE (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (pm.joined_at, pm.account_id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
GROUP BY pm.account_id, a.display_name
ORDER BY pm.joined_at DESC, pm.account_id DESC
LIMIT sqlc.arg(page_limit);

-- name: RemovePlatformMember :execrows
UPDATE control_platform_member
SET status = 'removed', updated_at = now()
WHERE account_id = $1
  AND status = 'active';

-- name: RemoveAllGlobalRoleMemberships :execrows
DELETE FROM control_global_role_member
WHERE account_id = $1;

-- name: RemoveAllWorkspaceRoleMemberships :execrows
DELETE FROM control_workspace_role_member
WHERE account_id = $1;

-- name: RemoveAllWorkspaceMemberships :execrows
UPDATE control_workspace_member
SET status = 'removed', updated_at = now()
WHERE account_id = $1
  AND status = 'active';

-- name: CreateSession :one
INSERT INTO control_session (
    id, account_id, token_hash, ip, user_agent, bind_to_ip, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, account_id, token_hash, ip, user_agent, bind_to_ip,
          expires_at, revoked_at, last_used_at, created_at;

-- name: ValidateAndTouchSession :one
UPDATE control_session s
SET last_used_at = CASE
    WHEN s.last_used_at < now() - INTERVAL '5 minutes' THEN now()
    ELSE s.last_used_at
END
FROM control_account a
JOIN control_platform_member pm ON pm.account_id = a.id
WHERE s.token_hash = $1
  AND s.account_id = a.id
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND a.status = 'active'
  AND pm.status = 'active'
  AND (s.bind_to_ip = FALSE OR s.ip = $2)
RETURNING s.id, s.account_id, s.token_hash, s.ip, s.user_agent, s.bind_to_ip,
          s.expires_at, s.revoked_at, s.last_used_at, s.created_at;

-- name: ListSessions :many
SELECT id, account_id, token_hash, ip, user_agent, bind_to_ip,
       expires_at, revoked_at, last_used_at, created_at
FROM control_session
WHERE account_id = $1
ORDER BY created_at DESC, id DESC;

-- name: RevokeSession :execrows
UPDATE control_session
SET revoked_at = now()
WHERE id = $1
  AND account_id = $2
  AND revoked_at IS NULL;

-- name: RevokeAllSessions :execrows
UPDATE control_session
SET revoked_at = now()
WHERE account_id = $1
  AND revoked_at IS NULL
  AND ($2 = '' OR id <> $2);

-- name: GetGlobalAuthorizationForUpdate :one
SELECT
    p.owner_account_id = sqlc.arg(actor_id)::text AS actor_is_owner,
    pm.status = 'active' AS actor_is_active,
    (
        p.owner_account_id = sqlc.arg(actor_id)::text
        OR EXISTS (
            SELECT 1
            FROM control_global_role_member rm
            JOIN control_global_role_permission rp ON rp.role_id = rm.role_id
            JOIN control_method m
                ON m.method_key = rp.method_key
               AND m.scope = 'global'
            WHERE rm.account_id = sqlc.arg(actor_id)::text
              AND rp.method_key = sqlc.arg(method_key)::text
        )
    ) AS allowed,
    CASE
        WHEN p.owner_account_id = sqlc.arg(actor_id)::text THEN 0
        ELSE COALESCE((
            SELECT MIN(r.position)
            FROM control_global_role_member rm
            JOIN control_global_role r ON r.id = rm.role_id
            WHERE rm.account_id = sqlc.arg(actor_id)::text
        ), 2147483647)
    END AS actor_position,
    CASE
        WHEN p.owner_account_id = sqlc.arg(target_account_id)::text THEN 0
        ELSE COALESCE((
            SELECT MIN(r.position)
            FROM control_global_role_member rm
            JOIN control_global_role r ON r.id = rm.role_id
            WHERE rm.account_id = sqlc.arg(target_account_id)::text
        ), 2147483647)
    END AS target_position,
    EXISTS (
        SELECT 1
        FROM control_platform_member target
        WHERE target.account_id = sqlc.arg(target_account_id)::text
          AND target.status = 'active'
    ) AS target_is_active
FROM control_platform p
JOIN control_platform_member pm ON pm.account_id = sqlc.arg(actor_id)::text
WHERE p.id = 1
FOR UPDATE OF p;

-- name: CheckGlobalAccess :one
SELECT EXISTS (
    SELECT 1
    FROM control_platform p
    JOIN control_platform_member pm
        ON pm.account_id = $1
       AND pm.status = 'active'
    WHERE p.id = 1
      AND (
          p.owner_account_id = $1
          OR EXISTS (
              SELECT 1
              FROM control_global_role_member rm
              JOIN control_global_role_permission rp ON rp.role_id = rm.role_id
              JOIN control_method m
                  ON m.method_key = rp.method_key
                 AND m.scope = 'global'
              WHERE rm.account_id = $1
                AND rp.method_key = $2
          )
      )
) AS allowed;

-- name: TransferGlobalOwnership :execrows
UPDATE control_platform
SET owner_account_id = sqlc.arg(new_owner_account_id), updated_at = now()
WHERE id = 1
  AND owner_account_id = sqlc.arg(current_owner_account_id);

-- name: CreateGlobalRole :one
INSERT INTO control_global_role (id, code, title, description, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, code, title, description, position, created_at, updated_at;

-- name: GetGlobalRole :one
SELECT id, code, title, description, position, created_at, updated_at
FROM control_global_role
WHERE id = $1;

-- name: ListGlobalRoles :many
SELECT r.id, r.code, r.title, r.description, r.position, r.created_at, r.updated_at,
       COUNT(rm.account_id) AS member_count
FROM control_global_role r
LEFT JOIN control_global_role_member rm ON rm.role_id = r.id
GROUP BY r.id
ORDER BY r.position, r.id;

-- name: UpdateGlobalRole :execrows
UPDATE control_global_role
SET title = $1, description = $2, position = $3, updated_at = now()
WHERE id = $4;

-- name: DeleteGlobalRole :execrows
DELETE FROM control_global_role WHERE id = $1;

-- name: DeleteGlobalInviteRoleReferences :execrows
DELETE FROM control_invite_global_role
WHERE role_id = $1;

-- name: AddGlobalRoleMember :execrows
INSERT INTO control_global_role_member (role_id, account_id)
VALUES ($1, $2)
ON CONFLICT (role_id, account_id) DO NOTHING;

-- name: RemoveGlobalRoleMember :execrows
DELETE FROM control_global_role_member
WHERE role_id = $1 AND account_id = $2;

-- name: ListGlobalRolePermissions :many
SELECT method_key
FROM control_global_role_permission
WHERE role_id = $1
ORDER BY method_key;

-- name: ReplaceGlobalRolePermissions :exec
WITH deleted AS (
    DELETE FROM control_global_role_permission WHERE role_id = sqlc.arg(role_id)
)
INSERT INTO control_global_role_permission (role_id, method_key)
SELECT sqlc.arg(role_id), requested.method_key
FROM unnest(sqlc.arg(method_keys)::text[]) AS requested(method_key)
JOIN control_method m
    ON m.method_key = requested.method_key
   AND m.scope = 'global';

-- name: GetWorkspaceCreationBundleForUpdate :one
SELECT
    pm.workspace_limit,
    (
        SELECT COUNT(*)
        FROM control_workspace w
        WHERE w.owner_account_id = pm.account_id
          AND w.status = 'active'
    ) AS owned_workspace_count,
    (
        p.owner_account_id = pm.account_id
        OR EXISTS (
            SELECT 1
            FROM control_global_role_member rm
            JOIN control_global_role_permission rp ON rp.role_id = rm.role_id
            WHERE rm.account_id = pm.account_id
              AND rp.method_key = 'control.global.workspace.create'
        )
    ) AS allowed
FROM control_platform_member pm
JOIN control_platform p ON p.id = 1
WHERE pm.account_id = $1
  AND pm.status = 'active'
FOR UPDATE OF pm;

-- name: GetWorkspaceOwnershipCapacityForUpdate :one
SELECT
    pm.workspace_limit,
    (
        SELECT COUNT(*)
        FROM control_workspace w
        WHERE w.owner_account_id = pm.account_id
          AND w.status = 'active'
    ) AS owned_workspace_count
FROM control_platform_member pm
WHERE pm.account_id = $1
  AND pm.status = 'active'
FOR UPDATE OF pm;

-- name: CreateWorkspace :one
INSERT INTO control_workspace (
    id, slug, title, created_by, owner_account_id
)
VALUES ($1, $2, $3, $4, $4)
RETURNING id, slug, title, status, created_by, owner_account_id,
          employee_limit, created_at, updated_at;

-- name: GetWorkspace :one
SELECT id, slug, title, status, created_by, owner_account_id, employee_limit,
       created_at, updated_at
FROM control_workspace
WHERE id = $1;

-- name: AddWorkspaceMember :exec
INSERT INTO control_workspace_member (workspace_id, account_id)
VALUES ($1, $2)
ON CONFLICT (workspace_id, account_id) DO UPDATE SET
    status = 'active',
    updated_at = now();

-- name: ListWorkspacesForAccount :many
SELECT w.id, w.slug, w.title, w.status, w.created_by, w.owner_account_id,
       w.employee_limit, w.created_at, w.updated_at
FROM control_workspace w
JOIN control_workspace_member wm
    ON wm.workspace_id = w.id
   AND wm.account_id = $1
   AND wm.status = 'active'
WHERE (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (w.created_at, w.id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY w.created_at DESC, w.id DESC
LIMIT sqlc.arg(page_limit);

-- name: GetWorkspaceAuthorizationForUpdate :one
SELECT
    w.owner_account_id = sqlc.arg(actor_id)::text AS actor_is_owner,
    wm.status = 'active' AS actor_is_active,
    (
        w.owner_account_id = sqlc.arg(actor_id)::text
        OR EXISTS (
            SELECT 1
            FROM control_workspace_role_member rm
            JOIN control_workspace_role_permission rp
                ON rp.role_id = rm.role_id
               AND rp.workspace_id = rm.workspace_id
            JOIN control_method m
                ON m.method_key = rp.method_key
               AND m.scope = 'workspace'
            WHERE rm.workspace_id = w.id
              AND rm.account_id = sqlc.arg(actor_id)::text
              AND rp.method_key = sqlc.arg(method_key)::text
        )
    ) AS allowed,
    CASE
        WHEN w.owner_account_id = sqlc.arg(actor_id)::text THEN 0
        ELSE COALESCE((
            SELECT MIN(r.position)
            FROM control_workspace_role_member rm
            JOIN control_workspace_role r
                ON r.id = rm.role_id
               AND r.workspace_id = rm.workspace_id
            WHERE rm.workspace_id = w.id
              AND rm.account_id = sqlc.arg(actor_id)::text
        ), 2147483647)
    END AS actor_position,
    CASE
        WHEN w.owner_account_id = sqlc.arg(target_account_id)::text THEN 0
        ELSE COALESCE((
            SELECT MIN(r.position)
            FROM control_workspace_role_member rm
            JOIN control_workspace_role r
                ON r.id = rm.role_id
               AND r.workspace_id = rm.workspace_id
            WHERE rm.workspace_id = w.id
              AND rm.account_id = sqlc.arg(target_account_id)::text
        ), 2147483647)
    END AS target_position,
    EXISTS (
        SELECT 1
        FROM control_workspace_member target
        WHERE target.workspace_id = w.id
          AND target.account_id = sqlc.arg(target_account_id)::text
          AND target.status = 'active'
    ) AS target_is_active,
    w.employee_limit,
    (
        SELECT COUNT(*)
        FROM control_workspace_member employee
        WHERE employee.workspace_id = w.id
          AND employee.status = 'active'
          AND employee.account_id <> w.owner_account_id
    ) AS employee_count,
    (
        SELECT COUNT(*)
        FROM control_invite invite
        WHERE invite.workspace_id = w.id
          AND invite.kind = 'workspace'
          AND invite.accepted_at IS NULL
          AND invite.revoked_at IS NULL
          AND (invite.expires_at IS NULL OR invite.expires_at > now())
    ) AS pending_invite_count
FROM control_workspace w
JOIN control_workspace_member wm
    ON wm.workspace_id = w.id
   AND wm.account_id = sqlc.arg(actor_id)::text
WHERE w.id = sqlc.arg(workspace_id)::text
  AND w.status = 'active'
FOR UPDATE OF w;

-- name: CheckWorkspaceAccess :one
SELECT EXISTS (
    SELECT 1
    FROM control_workspace w
    JOIN control_workspace_member wm
        ON wm.workspace_id = w.id
       AND wm.account_id = sqlc.arg(account_id)::text
       AND wm.status = 'active'
    JOIN control_platform_member pm
        ON pm.account_id = wm.account_id
       AND pm.status = 'active'
    WHERE w.id = sqlc.arg(workspace_id)::text
      AND w.status = 'active'
      AND (
          w.owner_account_id = sqlc.arg(account_id)::text
          OR EXISTS (
              SELECT 1
              FROM control_workspace_role_member rm
              JOIN control_workspace_role_permission rp
                  ON rp.role_id = rm.role_id
                 AND rp.workspace_id = rm.workspace_id
              JOIN control_method m
                  ON m.method_key = rp.method_key
                 AND m.scope = 'workspace'
              WHERE rm.workspace_id = w.id
                AND rm.account_id = sqlc.arg(account_id)::text
                AND rp.method_key = sqlc.arg(method_key)::text
          )
      )
) AS allowed;

-- name: GetWorkspaceCapacityForUpdate :one
SELECT
    w.employee_limit,
    (
        SELECT COUNT(*)
        FROM control_workspace_member employee
        WHERE employee.workspace_id = w.id
          AND employee.status = 'active'
          AND employee.account_id <> w.owner_account_id
    ) AS employee_count
FROM control_workspace w
WHERE w.id = $1
  AND w.status = 'active'
FOR UPDATE;

-- name: IsActiveWorkspaceMember :one
SELECT EXISTS (
    SELECT 1
    FROM control_workspace_member
    WHERE workspace_id = $1
      AND account_id = $2
      AND status = 'active'
) AS active;

-- name: UpdateWorkspace :execrows
UPDATE control_workspace
SET slug = $1, title = $2, updated_at = now()
WHERE id = $3 AND status = 'active';

-- name: ArchiveWorkspace :execrows
UPDATE control_workspace
SET status = 'archived', updated_at = now()
WHERE id = $1 AND status = 'active';

-- name: TransferWorkspaceOwnership :execrows
UPDATE control_workspace
SET owner_account_id = sqlc.arg(new_owner_account_id), updated_at = now()
WHERE id = sqlc.arg(workspace_id)
  AND owner_account_id = sqlc.arg(current_owner_account_id)
  AND status = 'active';

-- name: ListWorkspaceMembers :many
SELECT wm.workspace_id, wm.account_id, a.display_name, wm.status,
       wm.joined_at, wm.updated_at,
       wm.account_id = w.owner_account_id AS is_owner
FROM control_workspace_member wm
JOIN control_account a ON a.id = wm.account_id
JOIN control_workspace w ON w.id = wm.workspace_id
WHERE wm.workspace_id = sqlc.arg(workspace_id)
  AND wm.status = 'active'
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (wm.joined_at, wm.account_id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY wm.joined_at DESC, wm.account_id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListWorkspaceMemberRoles :many
SELECT rm.account_id, rm.role_id
FROM control_workspace_role_member rm
WHERE rm.workspace_id = $1
  AND rm.account_id = ANY(sqlc.arg(account_ids)::text[])
ORDER BY rm.account_id, rm.role_id;

-- name: RemoveWorkspaceMemberRoles :execrows
DELETE FROM control_workspace_role_member
WHERE workspace_id = $1 AND account_id = $2;

-- name: RemoveWorkspaceMember :execrows
UPDATE control_workspace_member
SET status = 'removed', updated_at = now()
WHERE workspace_id = $1
  AND account_id = $2
  AND status = 'active';

-- name: CreateWorkspaceRole :one
INSERT INTO control_workspace_role (
    id, workspace_id, code, title, description, position
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, code, title, description, position,
          created_at, updated_at;

-- name: GetWorkspaceRole :one
SELECT id, workspace_id, code, title, description, position, created_at, updated_at
FROM control_workspace_role
WHERE id = $1 AND workspace_id = $2;

-- name: ListWorkspaceRoles :many
SELECT r.id, r.workspace_id, r.code, r.title, r.description, r.position,
       r.created_at, r.updated_at, COUNT(rm.account_id) AS member_count
FROM control_workspace_role r
LEFT JOIN control_workspace_role_member rm ON rm.role_id = r.id
WHERE r.workspace_id = $1
GROUP BY r.id
ORDER BY r.position, r.id;

-- name: UpdateWorkspaceRole :execrows
UPDATE control_workspace_role
SET title = $1, description = $2, position = $3, updated_at = now()
WHERE id = $4 AND workspace_id = $5;

-- name: DeleteWorkspaceRole :execrows
DELETE FROM control_workspace_role
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteWorkspaceInviteRoleReferences :execrows
DELETE FROM control_invite_workspace_role
WHERE role_id = $1 AND workspace_id = $2;

-- name: AddWorkspaceRoleMember :execrows
INSERT INTO control_workspace_role_member (role_id, workspace_id, account_id)
VALUES ($1, $2, $3)
ON CONFLICT (role_id, account_id) DO NOTHING;

-- name: RemoveWorkspaceRoleMember :execrows
DELETE FROM control_workspace_role_member
WHERE role_id = $1 AND workspace_id = $2 AND account_id = $3;

-- name: ListWorkspaceRolePermissions :many
SELECT method_key
FROM control_workspace_role_permission
WHERE workspace_id = $1 AND role_id = $2
ORDER BY method_key;

-- name: ReplaceWorkspaceRolePermissions :exec
WITH deleted AS (
    DELETE FROM control_workspace_role_permission
    WHERE workspace_id = sqlc.arg(workspace_id)
      AND role_id = sqlc.arg(role_id)
)
INSERT INTO control_workspace_role_permission (role_id, workspace_id, method_key)
SELECT sqlc.arg(role_id), sqlc.arg(workspace_id), requested.method_key
FROM unnest(sqlc.arg(method_keys)::text[]) AS requested(method_key)
JOIN control_method m
    ON m.method_key = requested.method_key
   AND m.scope = 'workspace';

-- name: CreateInvite :exec
INSERT INTO control_invite (
    id, kind, workspace_id, created_by, token_hash, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: AddInviteGlobalRoles :exec
INSERT INTO control_invite_global_role (invite_id, role_id)
SELECT sqlc.arg(invite_id), role_id
FROM unnest(sqlc.arg(role_ids)::text[]) AS roles(role_id);

-- name: AddInviteWorkspaceRoles :exec
INSERT INTO control_invite_workspace_role (invite_id, workspace_id, role_id)
SELECT sqlc.arg(invite_id), sqlc.arg(workspace_id), role_id
FROM unnest(sqlc.arg(role_ids)::text[]) AS roles(role_id);

-- name: GetInviteByHashForUpdate :one
SELECT id, kind, workspace_id, created_by, token_hash, expires_at,
       accepted_by, accepted_at, revoked_at, created_at
FROM control_invite
WHERE token_hash = $1
FOR UPDATE;

-- name: GetInviteByHash :one
SELECT id, kind, workspace_id, created_by, token_hash, expires_at,
       accepted_by, accepted_at, revoked_at, created_at
FROM control_invite
WHERE token_hash = $1;

-- name: GetInvite :one
SELECT id, kind, workspace_id, created_by, token_hash, expires_at,
       accepted_by, accepted_at, revoked_at, created_at
FROM control_invite
WHERE id = $1;

-- name: GetInviteForUpdate :one
SELECT id, kind, workspace_id, created_by, token_hash, expires_at,
       accepted_by, accepted_at, revoked_at, created_at
FROM control_invite
WHERE id = $1
FOR UPDATE;

-- name: ListInviteGlobalRoles :many
SELECT role_id FROM control_invite_global_role
WHERE invite_id = $1 ORDER BY role_id;

-- name: ListInviteWorkspaceRoles :many
SELECT role_id FROM control_invite_workspace_role
WHERE invite_id = $1 ORDER BY role_id;

-- name: AcceptInvite :execrows
UPDATE control_invite
SET accepted_by = $1, accepted_at = now()
WHERE id = $2
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: RevokeInvite :execrows
UPDATE control_invite
SET revoked_at = now()
WHERE id = $1
  AND accepted_at IS NULL
  AND revoked_at IS NULL;

-- name: ListGlobalInvites :many
SELECT id, kind, workspace_id, created_by, expires_at, accepted_by,
       accepted_at, revoked_at, created_at
FROM control_invite
WHERE kind = 'global'
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListWorkspaceInvites :many
SELECT id, kind, workspace_id, created_by, expires_at, accepted_by,
       accepted_at, revoked_at, created_at
FROM control_invite
WHERE kind = 'workspace'
  AND workspace_id = sqlc.arg(workspace_id)::text
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: AddGlobalRolesFromInvite :exec
INSERT INTO control_global_role_member (role_id, account_id)
SELECT role_id, sqlc.arg(account_id)
FROM control_invite_global_role
WHERE invite_id = sqlc.arg(invite_id)
ON CONFLICT (role_id, account_id) DO NOTHING;

-- name: AddWorkspaceRolesFromInvite :exec
INSERT INTO control_workspace_role_member (role_id, workspace_id, account_id)
SELECT role_id, workspace_id, sqlc.arg(account_id)
FROM control_invite_workspace_role
WHERE invite_id = sqlc.arg(invite_id)
ON CONFLICT (role_id, account_id) DO NOTHING;

-- name: CreateLimitRequest :execrows
INSERT INTO control_limit_request (
    id, kind, account_id, workspace_id, current_limit, requested_limit,
    reason, requested_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT DO NOTHING;

-- name: GetLimitRequestForUpdate :one
SELECT id, kind, account_id, workspace_id, current_limit, requested_limit,
       approved_limit, reason, status, requested_by, reviewed_by,
       review_comment, created_at, reviewed_at
FROM control_limit_request
WHERE id = $1
FOR UPDATE;

-- name: ListLimitRequests :many
SELECT id, kind, account_id, workspace_id, current_limit, requested_limit,
       approved_limit, reason, status, requested_by, reviewed_by,
       review_comment, created_at, reviewed_at
FROM control_limit_request
WHERE (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter))
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ResolveLimitRequest :execrows
UPDATE control_limit_request
SET status = $1,
    approved_limit = $2,
    reviewed_by = $3,
    review_comment = $4,
    reviewed_at = now()
WHERE id = $5 AND status = 'pending';

-- name: CancelLimitRequest :execrows
UPDATE control_limit_request
SET status = 'cancelled', reviewed_at = now()
WHERE id = $1
  AND requested_by = $2
  AND status = 'pending';

-- name: CancelPendingAccountLimitRequests :execrows
UPDATE control_limit_request
SET status = 'cancelled',
    review_comment = 'account platform membership removed',
    reviewed_at = now()
WHERE kind = 'account_workspace'
  AND account_id = $1
  AND status = 'pending';

-- name: CancelPendingWorkspaceLimitRequests :execrows
UPDATE control_limit_request
SET status = 'cancelled',
    review_comment = 'workspace archived',
    reviewed_at = now()
WHERE kind = 'workspace_employee'
  AND workspace_id = $1
  AND status = 'pending';

-- name: UpdateAccountWorkspaceLimit :execrows
UPDATE control_platform_member
SET workspace_limit = $1, updated_at = now()
WHERE account_id = $2
  AND status = 'active';

-- name: UpdateWorkspaceEmployeeLimit :execrows
UPDATE control_workspace
SET employee_limit = $1, updated_at = now()
WHERE id = $2
  AND status = 'active';

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
    position = CASE
        WHEN EXCLUDED.position > 0 THEN EXCLUDED.position
        ELSE control_method_group.position
    END,
    updated_at = now();

-- name: MethodGroupExists :one
SELECT EXISTS (
    SELECT 1
    FROM control_method_group
    WHERE service = $1 AND group_key = $2
) AS exists;

-- name: LockMethodRegistry :exec
SELECT pg_advisory_xact_lock(hashtextextended('control:method-registry', 0));

-- name: ListMethodGroups :many
SELECT service, group_key, position, created_at, updated_at
FROM control_method_group
ORDER BY service, group_key;

-- name: ListAccessCatalog :many
SELECT g.service, service_catalog.position AS service_position,
       g.group_key, g.position AS group_position,
       COALESCE(service_title.value, g.service) AS service_title,
       COALESCE(service_description.value, '') AS service_description,
       COALESCE(group_title.value, g.group_key) AS group_title,
       COALESCE(group_description.value, '') AS group_description,
       m.method_key, m.scope, m.position,
       COALESCE(access_title.value, m.method_key) AS access_title,
       COALESCE(access_description.value, '') AS access_description
FROM control_method_group g
JOIN control_method m ON m.service = g.service AND m.group_key = g.group_key
JOIN control_access_service service_catalog ON service_catalog.service = g.service
LEFT JOIN control_localization service_title
    ON service_title.localization_key = 'control.access_service.' || g.service || '.title'
   AND service_title.locale = sqlc.arg(locale)
LEFT JOIN control_localization service_description
    ON service_description.localization_key = 'control.access_service.' || g.service || '.description'
   AND service_description.locale = sqlc.arg(locale)
LEFT JOIN control_localization group_title
    ON group_title.localization_key = 'control.method_group.' || g.service || '.' || g.group_key
   AND group_title.locale = sqlc.arg(locale)
LEFT JOIN control_localization group_description
    ON group_description.localization_key = 'control.method_group.' || g.service || '.' || g.group_key || '.description'
   AND group_description.locale = sqlc.arg(locale)
LEFT JOIN control_localization access_title
    ON access_title.localization_key = 'control.method.' || m.method_key
   AND access_title.locale = sqlc.arg(locale)
LEFT JOIN control_localization access_description
    ON access_description.localization_key = 'control.method.' || m.method_key || '.description'
   AND access_description.locale = sqlc.arg(locale)
WHERE sqlc.arg(scope)::text = '' OR m.scope = sqlc.arg(scope)::text
ORDER BY service_catalog.position, g.position, m.position, m.method_key;

-- name: GetMethod :one
SELECT method_key, service, group_key, scope, position, created_at, updated_at
FROM control_method WHERE method_key = $1;

-- name: ListMethods :many
SELECT method_key, service, group_key, scope, position, created_at, updated_at
FROM control_method
ORDER BY scope, service, group_key, position, method_key;

-- name: CountMethodsByScope :one
SELECT COUNT(DISTINCT method_key)
FROM control_method
WHERE scope = sqlc.arg(scope)::text
  AND method_key = ANY(sqlc.arg(method_keys)::text[]);

-- name: ListAuthorizedGlobalMethods :many
SELECT DISTINCT m.method_key, m.service, m.group_key, m.scope, m.position
FROM control_method m
JOIN control_platform_member pm
    ON pm.account_id = $1 AND pm.status = 'active'
JOIN control_platform p ON p.id = 1
LEFT JOIN control_global_role_member rm ON rm.account_id = pm.account_id
LEFT JOIN control_global_role_permission rp
    ON rp.role_id = rm.role_id AND rp.method_key = m.method_key
WHERE m.scope = 'global'
  AND (p.owner_account_id = pm.account_id OR rp.method_key IS NOT NULL)
ORDER BY m.service, m.group_key, m.position, m.method_key;

-- name: ListAuthorizedWorkspaceMethods :many
SELECT DISTINCT m.method_key, m.service, m.group_key, m.scope, m.position
FROM control_method m
JOIN control_workspace_member wm
    ON wm.workspace_id = $1
   AND wm.account_id = $2
   AND wm.status = 'active'
JOIN control_workspace w ON w.id = wm.workspace_id AND w.status = 'active'
LEFT JOIN control_workspace_role_member rm
    ON rm.workspace_id = wm.workspace_id
   AND rm.account_id = wm.account_id
LEFT JOIN control_workspace_role_permission rp
    ON rp.role_id = rm.role_id
   AND rp.workspace_id = rm.workspace_id
   AND rp.method_key = m.method_key
WHERE m.scope = 'workspace'
  AND (w.owner_account_id = wm.account_id OR rp.method_key IS NOT NULL)
ORDER BY m.service, m.group_key, m.position, m.method_key;

-- name: CreateAuditEvent :exec
INSERT INTO control_audit_event (
    id, scope, workspace_id, actor_id, method_key, target_type, target_id,
    before_data, after_data, result, request_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ListAuditEvents :many
SELECT id, scope, workspace_id, actor_id, method_key, target_type, target_id,
       COALESCE(before_data, '{}'::jsonb) AS before_data,
       COALESCE(after_data, '{}'::jsonb) AS after_data,
       result, request_id, occurred_at
FROM control_audit_event
WHERE scope = sqlc.arg(scope)
  AND (sqlc.arg(workspace_id)::text = '' OR workspace_id = sqlc.arg(workspace_id))
  AND (sqlc.narg(cursor_at)::timestamptz IS NULL
       OR (occurred_at, id) < (sqlc.narg(cursor_at), sqlc.arg(cursor_id)::text))
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: UpsertTwoFactor :exec
INSERT INTO control_two_factor (
    account_id, secret, backup_hashes, activated_at, last_totp_counter
)
VALUES ($1, $2, $3, NULL, NULL)
ON CONFLICT (account_id) DO UPDATE SET
    secret = EXCLUDED.secret,
    backup_hashes = EXCLUDED.backup_hashes,
    activated_at = NULL,
    last_totp_counter = NULL,
    updated_at = now();

-- name: GetTwoFactor :one
SELECT account_id, secret, backup_hashes, activated_at, last_totp_counter,
       created_at, updated_at
FROM control_two_factor WHERE account_id = $1;

-- name: GetTwoFactorForUpdate :one
SELECT account_id, secret, backup_hashes, activated_at, last_totp_counter,
       created_at, updated_at
FROM control_two_factor WHERE account_id = $1 FOR UPDATE;

-- name: ActivateTwoFactor :execrows
UPDATE control_two_factor
SET activated_at = now(), last_totp_counter = sqlc.arg(last_totp_counter)
WHERE account_id = sqlc.arg(account_id)
  AND activated_at IS NULL;

-- name: DeleteTwoFactor :execrows
DELETE FROM control_two_factor WHERE account_id = $1;

-- name: HasActiveTwoFactor :one
SELECT EXISTS (
    SELECT 1 FROM control_two_factor
    WHERE account_id = $1 AND activated_at IS NOT NULL
) AS active;

-- name: CreateTwoFactorChallenge :exec
INSERT INTO control_two_factor_challenge (
    id, account_id, invite_id, token_hash, ip, user_agent, bind_to_ip,
    expires_at, session_expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetTwoFactorChallengeForUpdate :one
SELECT id, account_id, invite_id, token_hash, ip, user_agent, bind_to_ip,
       expires_at, session_expires_at, created_at
FROM control_two_factor_challenge
WHERE token_hash = $1 AND expires_at > now()
FOR UPDATE;

-- name: GetTwoFactorChallengeAccount :one
SELECT account_id
FROM control_two_factor_challenge
WHERE token_hash = $1 AND expires_at > now();

-- name: GetTwoFactorChallengeWithFactorForUpdate :one
SELECT c.id AS challenge_id, c.account_id, c.invite_id, c.ip, c.user_agent,
       c.bind_to_ip, c.expires_at, c.session_expires_at,
       f.secret, f.backup_hashes, f.activated_at, f.last_totp_counter
FROM control_two_factor_challenge c
JOIN control_two_factor f ON f.account_id = c.account_id
WHERE c.token_hash = $1 AND c.expires_at > now()
FOR UPDATE OF c, f;

-- name: DeleteTwoFactorChallenge :execrows
DELETE FROM control_two_factor_challenge WHERE id = $1;

-- name: DeleteTwoFactorChallengesForAccount :execrows
DELETE FROM control_two_factor_challenge
WHERE account_id = $1;

-- name: UpdateTwoFactorBackupHashes :execrows
UPDATE control_two_factor
SET backup_hashes = $1, updated_at = now()
WHERE account_id = $2 AND activated_at IS NOT NULL;

-- name: UpdateTwoFactorLastCounter :execrows
UPDATE control_two_factor
SET last_totp_counter = sqlc.arg(last_totp_counter), updated_at = now()
WHERE account_id = sqlc.arg(account_id)
  AND activated_at IS NOT NULL
  AND (
      last_totp_counter IS NULL
      OR last_totp_counter < sqlc.arg(last_totp_counter)
  );

-- name: UpdatePendingTwoFactorBackupHashes :execrows
UPDATE control_two_factor
SET backup_hashes = $1, updated_at = now()
WHERE account_id = $2 AND activated_at IS NULL;

-- name: RevokePendingInvitesByCreator :execrows
UPDATE control_invite
SET revoked_at = now()
WHERE created_by = $1
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: RevokePendingWorkspaceInvitesByCreator :execrows
UPDATE control_invite
SET revoked_at = now()
WHERE workspace_id = $1
  AND created_by = $2
  AND kind = 'workspace'
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());
