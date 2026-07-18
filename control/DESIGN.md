# Control

`control` - внутренний сервис авторизации административной платформы. HTTP,
WebSocket, Fiber-контроллеры и проверка provider proof остаются снаружи.

## Инициализация

Пока строка `control_platform(id = 1)` отсутствует, внешний API может показать
единственный сценарий `Admin.Initialize`. Метод под advisory lock атомарно
создает первый account, identity, platform member, владельца платформы и
session. Повторная и параллельная инициализация возвращает
`ErrAlreadyInitialized`.

После инициализации новая identity не создает account без одноразового
глобального или workspace invitation. Существующие identities продолжают
входить без приглашения. При включенной 2FA принятие приглашения откладывается
до успешного завершения challenge. Удаление platform member отзывает все его
sessions, незавершенные challenges и выданные им pending invitations в той же
транзакции. Вход, выпуск session и удаление сериализуются по account, поэтому
session не может появиться после отзыва. Повторная активация такого account с
2FA выполняется только после успешного второго фактора. Использованный TOTP
counter сохраняется атомарно, как и расход backup code.

Bind и unbind способов входа сериализуются по account identities, конкретным
provider subjects и account authentication. Поэтому identity нельзя
одновременно перенести или удалить и использовать для выпуска новой session.
Попытка привязать identity другого account возвращает domain forbidden без
публичного PostgreSQL conflict.

Telegram WebApp init data проверяется адаптером и по умолчанию действительно
пять минут. После успешной проверки `control` выпускает собственную session,
поэтому Telegram init data не используется для последующих запросов.

## Два уровня доступа

Глобальный уровень управляет платформой:

- глобальные роли и права;
- приглашения на платформу;
- право `control.global.workspace.create`;
- запросы на расширение лимитов;
- передача владельца платформы.

Workspace-уровень изолирован по `workspace_id`:

- сотрудники и одноразовые приглашения;
- роли и права рабочей области;
- изменение и архивация workspace;
- передача владельца workspace.

RBAC работает в allow-only режиме. Наличие точного access key хотя бы у одной
роли разрешает действие. Владелец соответствующего scope имеет все права этого
scope. Actor не может управлять пользователем или ролью с равной либо более
высокой позицией. Позиция владельца равна `0`.

## Владение и лимиты

`control_platform_member.workspace_limit` считает только активные workspace,
где account указан в `owner_account_id`. Членство и работа в чужих workspace
лимит не расходуют. Значение по умолчанию - `1`.

`control_workspace.employee_limit` считает активных участников, кроме текущего
владельца. Неиспользованные и непросроченные workspace invitations резервируют
места, поэтому массовое создание ссылок не позволяет превысить лимит. Значение
по умолчанию - `10`.

Увеличение обоих лимитов выполняется через `control_limit_request`. Запрос
создает сам account либо владелец workspace, а одобряет пользователь с
глобальным access. Передача workspace новому владельцу также проверяет его
ownership limit.

Отменить pending limit request может только создавший его account. Отмена
account workspace-limit request записывается в global audit, а отмена
workspace employee-limit request - в audit соответствующего workspace.
Удаление platform member автоматически отменяет его pending workspace-limit
request, а архивация workspace - pending employee-limit request. После
деактивации scope такой request нельзя одобрить, и он не блокирует будущий
запрос unique-индексом.

Статусы account, membership, workspace и limit request, а также audit result
публикуются typed constants из `control/model`. Неизвестный status filter или
audit result отклоняется до обращения к PostgreSQL.

## Приглашения

Каждое приглашение содержит случайный token, в БД хранится только SHA-256 hash.
Token предназначен одному account и принимается ровно один раз. Global invite
может назначить глобальные роли. Workspace invite атомарно активирует membership
и назначает workspace-роли. Проверка срока, лимита сотрудников, запись
membership, ролей и acceptance выполняются в одной транзакции.
Role IDs перед записью нормализуются и удаляются дубликаты. При удалении
platform member отзываются все созданные им pending invitations; при удалении
workspace member отзываются его pending invitations внутри этого workspace.
Workspace invitation acceptance и revoke всегда берут workspace lock раньше
invite lock. Это сохраняет единый порядок с member, role и archive mutations и
не допускает deadlock между принятием и административным изменением scope.

Create session, workspace и role используют `INSERT ... RETURNING`, поэтому
публичная create-модель содержит сохранённые timestamps и `last_used_at`.
Ожидаемые duplicate slug/code conflicts нормализуются в `ErrAlreadyExists`.

## Реестр методов

`control` единственный хранит полный каталог административных действий.
`control.global.*` автоматически относится к global scope, остальные keys - к
workspace scope. Manifest key обязан начинаться с namespace заявленного
service, ссылаться на существующую группу этого service и не может перехватить
key, уже принадлежащий другому service. Права роли принимают только
существующие keys своего scope.

`Admin.ListAccess(locale, scope)` возвращает локализованное дерево
service -> groups -> accesses, отсортированное по position. Результат хранится в
версионном cache scope `control/access-catalog`.
Успешная регистрация manifest bump-ает версию этого scope, поэтому новый key
становится видимым на всех нодах без ожидания TTL.

## Владельцы

Владелец хранится напрямую в `control_platform.owner_account_id` и
`control_workspace.owner_account_id`, а не в специальной роли. Передать
глобальное владение может только текущий глобальный владелец. Передать workspace
может только его текущий владелец активному участнику. Предыдущий владелец
остается участником.
