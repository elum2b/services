# Control methods

Только методы слоя `admin`, которые можно использовать как основу будущего API.

## admin

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `Admin.CompleteAuth(ctx, params)` | `AuthIdentityParams{Provider, Subject, DisplayName, Payload, IP, UserAgent, BindToIP, ExpiresAt}`. | Принимает identity от уже проверенного OAuth-adapter-а, находит или создаёт account и создаёт session либо 2FA challenge. |
| `Admin.CompleteTwoFactor(ctx, challenge, code, ip)` | Одноразовый challenge token, TOTP/backup-код, IP. | Завершает вход с 2FA и выдаёт session token. |
| `Admin.CreateAccount(ctx, id, displayName)` | `id`, `displayName`. | Создаёт account оператора напрямую; нужен для административной инициализации или ручного bootstrap-сценария. |
| `Admin.GetAccount(ctx, accountID)` | `accountID` оператора. | Возвращает профиль оператора. |
| `Admin.ListIdentities(ctx, accountID)` | `accountID` текущего оператора. | Возвращает внешние аккаунты, привязанные к оператору. |
| `Admin.BindIdentity(ctx, accountID, params)` | `accountID`, проверенная `AuthIdentityParams`. | Привязывает дополнительный аккаунт GitHub, GitLab, Google, VK или Yandex. |
| `Admin.UnbindIdentity(ctx, accountID, provider)` | `accountID`, provider. | Отвязывает provider, если у account останется хотя бы один способ входа. |
| `Admin.ValidateSession(ctx, token, ip)` | Session token, IP. | Проверяет сессию, срок действия и IP binding. |
| `Admin.BeginTwoFactor(ctx, accountID, issuer)` | `accountID`, issuer. | Создаёт TOTP secret и URI для подключения 2FA. |
| `Admin.ConfirmTwoFactor(ctx, accountID, code)` | `accountID`, первый TOTP-код. | Активирует 2FA и возвращает одноразовые backup-коды. |
| `Admin.DisableTwoFactor(ctx, accountID, code)` | `accountID`, TOTP или backup-код. | Отключает 2FA после подтверждения. |
| `Admin.ListSessions(ctx, accountID)` | `accountID`. | Возвращает активные сессии оператора без токенов. |
| `Admin.RevokeSession(ctx, accountID, sessionID)` | `accountID`, `sessionID`. | Отзывает одну сессию оператора. |
| `Admin.RevokeAllSessions(ctx, accountID, exceptSessionID)` | `accountID`, опциональный `exceptSessionID`. | Отзывает все сессии, кроме при необходимости текущей. |
| `Admin.ListWorkspaces(ctx, accountID, limit, offset)` | `accountID`, пагинация. | Возвращает workspace, в которых оператор является активным участником. |
| `Admin.AcceptInvite(ctx, accountID, token)` | `accountID`, исходный invite token. | Принимает invite и добавляет оператора в workspace с заданными ролями. |
| `Admin.CreateWorkspace(ctx, params)` | `CreateWorkspaceParams{ActorID, ID, Slug, Title}`. | Создаёт workspace, добавляет создателя и системную owner-роль. |
| `Admin.GetWorkspace(ctx, workspaceID)` | `workspaceID`. | Возвращает workspace. |
| `Admin.UpdateWorkspace(ctx, params)` | `UpdateWorkspaceParams{ActorID, WorkspaceID, Slug, Title, Status}`. | Обновляет реквизиты или архивирует workspace после проверки права. |
| `Admin.ListMembers(ctx, workspaceID, limit, offset)` | `workspaceID`, пагинация. | Возвращает активных участников и назначенные роли. |
| `Admin.RemoveMember(ctx, actorID, workspaceID, accountID)` | Actor, workspace и участник. | Удаляет участника при наличии метода и строгом превосходстве роли actor. |
| `Admin.CreateInvite(ctx, params)` | `CreateInviteParams{ActorID, WorkspaceID, RoleIDs, ExpiresAt, MaxUses}`. | Создаёт invite с ролями, сроком действия и лимитом использований. |
| `Admin.ListInvites(ctx, workspaceID, limit, offset)` | `workspaceID`, пагинация. | Возвращает приглашения workspace без исходного токена. |
| `Admin.RevokeInvite(ctx, actorID, workspaceID, inviteID)` | Actor, workspace и invite. | Отзывает invite после проверки права и иерархии его ролей. |
| `Admin.CreateRole(ctx, params)` | `CreateRoleParams{ActorID, WorkspaceID, Code, Title, Description, Position}`. | Создаёт роль строго ниже высшей роли actor. |
| `Admin.UpdateRole(ctx, params)` | `UpdateRoleParams{ActorID, WorkspaceID, RoleID, Title, Description, Position}`. | Обновляет роль только если она строго ниже actor. |
| `Admin.DeleteRole(ctx, actorID, workspaceID, roleID)` | Actor, workspace и роль. | Удаляет роль и её назначения, если она строго ниже actor. |
| `Admin.ListRoles(ctx, workspaceID)` | `workspaceID`. | Возвращает роли workspace с количеством участников и включёнными methods. |
| `Admin.SetRoleMember(ctx, params)` | `SetRoleMemberParams{ActorID, WorkspaceID, AccountID, RoleID}`. | Назначает роль, если target account и role строго ниже actor. |
| `Admin.RemoveRoleMember(ctx, params)` | `RemoveRoleMemberParams{ActorID, WorkspaceID, AccountID, RoleID}`. | Снимает роль с участника при той же проверке иерархии. |
| `Admin.ListRolePermissions(ctx, workspaceID, roleID)` | `workspaceID`, `roleID`. | Возвращает включённые method keys роли. |
| `Admin.SetRolePermission(ctx, params)` | `SetRolePermissionParams{ActorID, WorkspaceID, RoleID, MethodKey, Enabled}`. | Включает или выключает method key у роли, если роль строго ниже actor. |
| `Admin.ClearRolePermissions(ctx, params)` | `ClearRolePermissionsParams{ActorID, WorkspaceID, RoleID}`. | Удаляет все включённые methods роли, если роль строго ниже actor. |
| `Admin.ListMethods(ctx)` | Нет параметров. | Возвращает все зарегистрированные методы для административного интерфейса. |
| `Admin.GetMethod(ctx, methodKey)` | `methodKey`. | Возвращает публичные метаданные зарегистрированного метода. |
| `Admin.ListAccess(ctx, locale)` | `locale`. | Возвращает локализованный каталог access: сервисы, группы и access-keys в правильном порядке для UI управления ролями. |
| `Admin.ListAudit(ctx, workspaceID, page)` | `workspaceID`, `Page{Limit, Offset}`. | Возвращает аудит workspace с пагинацией. |
