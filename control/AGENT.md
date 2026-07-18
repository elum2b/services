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
- Global и workspace RBAC хранятся раздельно. Ключи `control.global.*`
  относятся к global scope; остальные административные ключи — к workspace.
- Проверка выполняется по actor, scope и точному access key до mutation.
- Actor не может создавать, менять, удалять, назначать или снимать роль с
  position, равной или выше его максимальной роли.
- То же строгое превосходство действует для target member и ролей invite.
- `AssignRole` не делает account участником автоматически. Если target не
  является активным member workspace, возвращается domain error.
- Владельцы platform и workspace хранятся напрямую, не как специальные роли.
  Их position равна `0`; передача владения доступна только текущему владельцу.
- `workspace_limit` считает только активные workspace во владении account.
  Membership в чужих workspace лимит не расходует.
- `employee_limit` не считает владельца, но pending workspace invites
  резервируют места.
- Owner/bootstrap сценарии должны быть явными и не обходить hierarchy
  незаметным fallback.
- Access definitions статичны: CRUD для них не существует. CRUD нужен для
  workspace roles и role assignments.

## Авторизация и безопасность

- После первой инициализации новый account создаётся только по одноразовому
  global или workspace invite. При 2FA invite принимается после challenge.
- Session, challenge, invite и backup tokens не хранить в открытом виде;
  наружу возвращать исходное значение только в момент создания.
- Account, membership, workspace, limit-request statuses и audit result
  используют типы и constants из `control/model`; неизвестные значения
  отклоняются domain error до SQL.
- 2FA challenge одноразовый и ограничен сроком. TOTP counter и backup code
  расходуются атомарно на уровне account: один код не может завершить два
  challenge или повторно использоваться после активации 2FA.
- Создание session, завершение 2FA, отзыв sessions и удаление platform member
  сериализуются по account. Удаление не должно оставлять session, способную
  стать валидной после повторной активации account.
- Удаление platform member атомарно отзывает все его sessions и незавершённые
  2FA challenges, а также созданные им неиспользованные invitations. Удаление
  workspace member отзывает созданные им pending invitations этого workspace.
  Повторная активация account с 2FA происходит только после успешного второго
  фактора.
- Telegram WebApp init data без явно заданного `MaxAge` принимается не дольше
  пяти минут. После проверки provider identity сервис создаёт собственную
  session и больше не использует Telegram init data как access token.
- Unbind identity запрещён, если account останется без способа входа.
- Bind/unbind identity сериализуются по account, затем по отсортированным
  provider subjects, затем по account auth. Identity другого account
  возвращает domain forbidden, а не PostgreSQL unique error.
- IP binding проверяется только когда включён в session contract.
- Секреты, token hashes и 2FA material не попадают в list/get/audit payload.
- Каждая security-sensitive admin mutation создаёт audit event с actor,
  method key, target, request ID и результатом. Mutation и обязательный audit
  должны оставаться консистентными.
- Созданный invite записывается в audit по фактическому invite ID. Отмена
  workspace employee-limit request относится к workspace audit, а отмена
  account workspace-limit request — к global audit.
- Архивация workspace и удаление platform member автоматически отменяют
  pending limit requests соответствующего scope.
- Create workspace/role/session возвращает фактически записанную строку через
  SQL `RETURNING`; timestamps и `last_used_at` не синтезируются в service.
- Ожидаемые slug/code conflicts возвращаются как `ErrAlreadyExists`, без
  утечки PostgreSQL `23505`.

## Access catalog и cache

- Иерархия ответа: localized service -> localized groups -> localized
  accesses.
- Service/group/access сортируются по position; position наружу не выдаётся.
- Для service, group и access обязательны локализации `ru` и `en`: title и
  description.
- `ListAccess(locale)` кэшируется versioned scope. Изменение обязательных seed
  данных или успешная регистрация manifest bump-ает версию; статичный каталог
  нельзя обновлять admin CRUD.
- Каждый manifest key обязан принадлежать namespace заявленного service и
  ссылаться на существующую группу этого service.
- Callback/webhook/worker методы других сервисов не регистрировать как access.

## Обязательные тесты

- Valid/invalid auth, replay challenge, TOTP counter и backup code.
- Allow-only RBAC и отсутствие access.
- Запрет действий над равной/высшей ролью и non-member target.
- Workspace isolation для roles, members, invites и audit.
- Полный localized/sorted access tree и cache hit/version behavior.
- Конкурентные invite/session/role operations, удаление во время входа и
  одновременные Begin/Confirm 2FA.

```bash
go test ./control
go test -run '^$' -bench . -benchmem ./control
```
