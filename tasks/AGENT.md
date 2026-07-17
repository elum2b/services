# Tasks: правила сервиса

Наследует корневой `../AGENT.md`. Клиентское поведение и поля описаны в
`README.md`, публичные Go-методы и Lua contract — в `METHODS.md`.

## Назначение и слои

- Tasks владеет groups, sequences, task catalog, conditions complex tasks,
  progress, events, rewards, integration config, partner issues, Lua scripts,
  partner statistics и callback.
- `User` возвращает/стартует/проверяет/claim-ит задания.
- `Integration` выполняет встроенные системные проверки.
- `Internal` принимает partner webhook/callback. Эти операции не являются
  user/admin access.
- `Admin` управляет catalog, integrations, partner runtime и import/export.

## Обычные и комплексные задания

- User list возвращает `[]TaskGroupModel`, внутри группы локализованные tasks.
  `GroupKey` ограничивает выдачу одной группой.
- `action_kind` и `task_kind` являются typed constants и согласованы со
  schema. Новый kind требует SQL enum/migration, models, validation, client
  contract, import/export и tests.
- `start_mode=required` запрещает progress/check/claim до успешного start;
  `none` не создаёт лишний start step.
- `claim_mode` определяет только способ claim, но не ослабляет условие ready.
- Task event может одновременно продвигать обычную задачу и conditions
  нескольких complex tasks. Обновление выполняется bulk/transactional без
  потери параллельного прогресса.
- Conditions complex task выполняются в любом порядке, если sequence явно не
  задана. Parent становится ready только после всех required conditions.
- Награда condition опциональна и выдаётся отдельно; parent reward выдаётся
  один раз за весь комплекс.
- Reset windows, limits и sequence state применяются отдельно к каждой задаче
  и condition progress.
- Все claim используют immutable reward snapshot, включая повторный ответ и
  callback после изменения catalog.

## Integration checks

- Custom application event и системный integration check — разные входы.
- Универсальный `Integration.Check` сам определяет checker по сохранённому
  task config. Клиент не выбирает внутренний checker и не перебирает варианты.
- `channel_subscribe` поддерживает Telegram/VK; `channel_boost` только
  Telegram. Private payload хранит platform, channel/chat ID, tokens и token
  strategy.
- За одну проверку используется ровно один token. Round-robin/random selection
  и общий limiter не превышают 30 requests/s на token между subscribe/boost.
- Private integration payload и tokens никогда не возвращаются фронту и не
  попадают в обычный export без явного secret flow.
- External HTTP выполняется через Resty вне DB transaction; после ответа
  repository атомарно применяет результат.

## Partner Lua runtime

- Provider scripts хранятся в PostgreSQL и экспортируют функции `list`,
  `start`, `check`, `callback` без общего handler и compatibility wrappers.
- Runtime расположен в `runtime`, компилирует/cache-ит scripts, использует pool
  Lua states, ограничивает ресурсы и поддерживает graceful shutdown.
- Go/Lua boundary использует ограниченный JSON event/result contract. Lua не
  получает произвольный доступ к БД, filesystem или сети вне разрешённых Go
  helpers.
- Partner config scoped по workspace/group/platform. Неизменившийся provider
  pool не пересоздаётся только из-за TTL; version change закрывает старый pool.
- Единый webhook `/webhook/{workspace_id}/{secret}/` находит config по
  workspace+secret, передаёт headers/query/body в `callback(event)` и применяет
  complete либо revoke.
- Webhook completion сильнее последующего stale `check`: terminal completed/
  claimed state нельзя понизить. Revoke до claim блокирует награду; revoke
  после claim создаёт компенсационный callback и сохраняет историю.
- Partner `start_mode=required` и `external_click_id` сохраняются в issue до
  внешнего start/check/callback.

## Cache и import/export

- Task catalog, group list, integration config, partner config и script имеют
  отдельные versioned workspace/provider scopes.
- Catalog/config mutation bump-ает только затронутые версии после commit.
- Export JSON сохраняет иерархию group -> tasks -> localization/rewards/
  conditions и включает `start_mode`.
- Secrets экспортируются только при `IncludeSecrets=true`; иначе package
  содержит secret keys/placeholders, а import принимает `ImportRequest.Secrets`.
- Examples не содержат реальные tokens. Для private tokens используются
  placeholders или отдельная secret map.

## Обязательные тесты

- Все User/Admin/Integration/Internal методы: valid и invalid paths.
- Обычные, sequence, reset, limits, start required и manual/automatic claim.
- Complex conditions в разном порядке, параллельный общий event, condition
  reward и single parent reward.
- Subscribe/boost token selection, общий rate limit и отсутствие secret leak.
- Partner list/start/check/callback complete/revoke, stale check после webhook,
  runtime pool/version replacement и graceful shutdown.
- Snapshot, callback exactly-once, две cache-ноды и полный import/export.
- Live partner tests с настоящими tokens не запускаются по умолчанию.

```bash
go test ./tasks
go test -run '^$' -bench . -benchmem ./tasks
```
