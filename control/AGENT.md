# Control: правила сервиса

Наследует корневой `../AGENT.md`. Архитектура описана в `DESIGN.md`, публичные
операции — в `METHODS.md`.

## Назначение и слои

- Control — внутренний административный сервис: accounts, identities,
  sessions, 2FA, workspaces, members, invites, roles, access catalog и audit.
- У сервиса нет `User` слоя. Внешнее приложение вызывает `Admin`, а
  доверенные межсервисные проверки — `Internal`.
- Control единственный знает registry доступных действий других сервисов.
- OAuth/Telegram/TON Connect proof проверяют adapters. В Control передаётся
  уже проверенная provider identity для создания account/session.

## RBAC и иерархия

- Модель allow-only: access key присутствует у роли — действие разрешено,
  отсутствует — запрещено. Deny и приоритеты deny не добавлять.
- Проверка выполняется по actor, workspace и точному access key до mutation.
- Actor не может создавать, менять, удалять, назначать или снимать роль с
  position, равной или выше его максимальной роли.
- То же строгое превосходство действует для target member и ролей invite.
- `AssignRole` не делает account участником автоматически. Если target не
  является активным member workspace, возвращается domain error.
- Owner/bootstrap сценарии должны быть явными и не обходить hierarchy
  незаметным fallback.
- Access definitions статичны: CRUD для них не существует. CRUD нужен для
  workspace roles и role assignments.

## Авторизация и безопасность

- Session, challenge, invite и backup tokens не хранить в открытом виде;
  наружу возвращать исходное значение только в момент создания.
- 2FA challenge одноразовый, ограничен сроком и защищён от повторного
  завершения конкурентными запросами.
- Unbind identity запрещён, если account останется без способа входа.
- IP binding проверяется только когда включён в session contract.
- Секреты, token hashes и 2FA material не попадают в list/get/audit payload.
- Каждая security-sensitive admin mutation создаёт audit event с actor,
  method key, target, request ID и результатом. Mutation и обязательный audit
  должны оставаться консистентными.

## Access catalog и cache

- Иерархия ответа: localized service -> localized groups -> localized
  accesses.
- Service/group/access сортируются по position; position наружу не выдаётся.
- Для service, group и access обязательны локализации `ru` и `en`: title и
  description.
- `ListAccess(locale)` кэшируется versioned scope. Изменение обязательных seed
  данных bump-ает версию; статичный каталог нельзя обновлять admin CRUD.
- Callback/webhook/worker методы других сервисов не регистрировать как access.

## Обязательные тесты

- Valid/invalid auth, replay challenge, TOTP и backup code.
- Allow-only RBAC и отсутствие access.
- Запрет действий над равной/высшей ролью и non-member target.
- Workspace isolation для roles, members, invites и audit.
- Полный localized/sorted access tree и cache hit/version behavior.
- Конкурентные invite/session/role operations.

```bash
go test ./control
go test -run '^$' -bench . -benchmem ./control
```
