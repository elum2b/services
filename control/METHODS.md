# Control API

У `control` нет user-слоя. `Admin` используется административным приложением,
`Internal` - доверенным orchestration-слоем для проверки access и аудита.

Публичные статусы и audit result берутся из `control/model`. Create-методы
возвращают сохранённые БД timestamps; duplicate slug/code возвращает
`repository.ErrAlreadyExists`.

## Авторизация

| Метод | Принимает | Что делает |
|---|---|---|
| `Admin.IsInitialized` | `context` | Проверяет, создан ли первый владелец платформы. |
| `Admin.Initialize` | Проверенную identity и metadata session | Атомарно создает первого глобального владельца. Доступен один раз. |
| `Admin.CompleteAuth` | Проверенную identity, optional `InviteToken`, metadata session | Авторизует существующий account или регистрирует новый только по одноразовому invite. |
| `Admin.CompleteTwoFactor` | Challenge, TOTP/backup code, IP | Завершает 2FA, атомарно расходует TOTP counter либо backup code, принимает отложенный invite и создает session. |
| `Admin.GetAccount` | Account ID | Возвращает account без секретных данных. |
| `Admin.BindIdentity` / `UnbindIdentity` | Account и provider identity | Добавляет или удаляет способ входа; последний способ удалить нельзя. Операции сериализуются с auth, identity другого account отклоняется. |
| `Admin.ValidateSession` | Token и IP | Проверяет session, account и platform membership одним запросом. |
| `Admin.ListSessions` / `RevokeSession` / `RevokeAllSessions` | Account/session IDs | Управляет сессиями account; изменения записываются в audit. |
| `Admin.BeginTwoFactor` / `ConfirmTwoFactor` / `DisableTwoFactor` | Account, issuer, code | Управляет TOTP и одноразовыми backup codes. |

## Global

| Метод | Принимает | Что делает |
|---|---|---|
| `Admin.CreateGlobalInvite` | Actor, global role IDs, expiry | Создает одноразовое приглашение на платформу. |
| `Admin.ListGlobalInvites` / `RevokeInvite` | Cursor либо invite ID | Показывает или отзывает приглашения. |
| `Admin.ListPlatformMembers` / `RemovePlatformMember` | Cursor либо account ID | Управляет участниками платформы. Владельца platform/workspace удалить нельзя; удаление отзывает sessions, 2FA challenges, созданные member pending invites и pending workspace-limit request. |
| `Admin.TransferGlobalOwnership` | Actor и target account | Передает глобальное владение активному участнику. |
| `Admin.CreateGlobalRole` | Actor и role | Создает глобальную роль ниже позиции actor. |
| `Admin.ListGlobalRoles` | `context` | Возвращает глобальные роли. |
| `Admin.UpdateGlobalRole` / `DeleteGlobalRole` | Actor и role | Изменяет или удаляет роль с проверкой иерархии. |
| `Admin.AssignGlobalRole` / `RemoveGlobalRole` | Actor, account, role | Назначает или снимает глобальную роль. |
| `Admin.ReplaceGlobalRolePermissions` | Actor, role, method keys | Атомарно заменяет global accesses роли. |
| `Admin.RequestWorkspaceLimit` | Account, новый лимит, причина | Создает запрос на увеличение числа принадлежащих account workspace. |
| `Admin.ListLimitRequests` / `ResolveLimitRequest` | Actor, фильтр/cursor либо решение | Просматривает, одобряет или отклоняет запросы лимитов. |
| `Admin.CancelLimitRequest` | Requester и request ID | Отменяет собственный pending request и записывает audit в scope этого запроса. |

## Workspace

| Метод | Принимает | Что делает |
|---|---|---|
| `Admin.CreateWorkspace` | Actor, UUID, slug, title | Проверяет global access и ownership limit, затем создает workspace. |
| `Admin.GetWorkspace` / `ListWorkspaces` | Workspace ID либо account + cursor | Возвращает workspace или memberships account. |
| `Admin.UpdateWorkspace` / `ArchiveWorkspace` | Actor и workspace | Изменяет либо архивирует workspace. Архивация освобождает ownership slot и отменяет pending employee-limit request. |
| `Admin.TransferWorkspaceOwnership` | Actor, workspace, target account | Передает workspace активному участнику с проверкой его ownership limit. |
| `Admin.CreateWorkspaceInvite` | Actor, workspace, role IDs, expiry | Создает одноразовое приглашение и резервирует employee slot. |
| `Admin.ListWorkspaceInvites` / `RevokeInvite` | Workspace + cursor либо invite ID | Просматривает или отзывает приглашения. |
| `Admin.ListMembers` / `RemoveMember` | Workspace + cursor либо account | Управляет сотрудниками; owner удалить нельзя. Удаление отзывает созданные member pending invites этого workspace. |
| `Admin.CreateWorkspaceRole` | Actor, workspace, role | Создает роль ниже позиции actor. |
| `Admin.ListWorkspaceRoles` | Workspace ID | Возвращает роли workspace. |
| `Admin.UpdateWorkspaceRole` / `DeleteWorkspaceRole` | Actor и role | Изменяет или удаляет роль с проверкой иерархии. |
| `Admin.AssignWorkspaceRole` / `RemoveWorkspaceRole` | Actor, member, role | Назначает или снимает роль только у активного member. |
| `Admin.ReplaceWorkspaceRolePermissions` | Actor, role, method keys | Атомарно заменяет workspace accesses роли. |
| `Admin.RequestEmployeeLimit` | Owner, workspace, новый лимит, причина | Создает запрос на увеличение лимита сотрудников. |

## Каталог и аудит

| Метод | Принимает | Что делает |
|---|---|---|
| `Admin.ListMethods` / `GetMethod` | Optional method key | Читает статичный registry access keys. |
| `Admin.ListAccess` | Locale и optional scope | Возвращает кэшированное локализованное дерево прав. |
| `Admin.ListGlobalAudit` / `ListWorkspaceAudit` | Scope cursor | Возвращает аудит выбранного scope. |
| `Internal.RegisterManifest` | Methods service | Атомарно регистрирует keys существующих групп в namespace service, затем инвалидирует access cache; чужой key перехватить нельзя. |
| `Internal.CheckGlobalAccess` | Account и method key | Проверяет глобальное право. |
| `Internal.CheckWorkspaceAccess` | Account, workspace, method key | Проверяет право внутри workspace. |
| `Internal.GetAuthorizedGlobalMethods` | Account | Возвращает разрешенные global methods. |
| `Internal.GetAuthorizedWorkspaceMethods` | Account и workspace | Возвращает разрешенные workspace methods. |
| `Internal.AppendAudit` | Typed audit event | Записывает доверенное событие global/workspace scope. |
