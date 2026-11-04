# План Collections и Inventory

Дата: 4 ноября 2026. Статус: Collections реализован и проверен; Inventory остаётся в плане.
Контракт Collections: ../collections/METHODS.md; результаты: ../collections/VALIDATION.md.
Порядок: согласовать контракт → Collections целиком → Inventory.

Исходные спецификации:
- `/Volumes/CLOUD/GitHub/elum-games/game-backend/docs/collections-service.md`
- `/Volumes/CLOUD/GitHub/elum-games/game-backend/docs/inventory-service.md`

Правила реализации: корневой `AGENT.md`, существующие lifecycle/service/repository/SQLC
и utilities репозитория. Спецификации рассматриваются как требования предметной
области; примеры игровых сценариев не расширяют сервисы игровой логикой.

## 1. Подтверждённые границы

| Область | Collections | Inventory |
| --- | --- | --- |
| Состояние | Текущий bool owned для пользователя, коллекции и item key | Количество, entry ID, ячейка и ревизии |
| Каталог | Коллекции, локализации и состав | Группы, связи item keys и операционные политики |
| Запись системой | SetOwned / SetOwnedBatch | AddItems / RemoveItems / ApplyChanges |
| Пользовательские действия | Чтение своих/чужих коллекций | Чтение, Move/Swap/Split/Merge/Sort/Transfer/RequestAction |
| Внешние метаданные | Только непрозрачный item key; metadata владеет Reference | То же |
| События | collections.ownership_changed | Изменения содержимого и запросы действий; точные имена фиксируются в METHODS |
| Архив конфигурации | collections.export.v1, без пользовательских отметок | inventory.export.v1, без runtime-содержимого и истории |

Collections и Inventory не вызывают друг друга и не синхронизируют данные
автоматически. Игровое приложение принимает решения и связывает операции
устойчивыми ключами. Callback ack означает доставку, а не завершение игрового эффекта.
Inventory v1 не ограничивает вместимость; игровые эффекты и будущие лимиты
веса/слотов не входят в эту реализацию.

## 2. Общий каркас и зависимости

Для каждого нового пакета:

```text
<service>/
  AGENT.md
  DESIGN.md
  METHODS.md
  <service>.go
  config.go
  errors.go
  models.go
  callback.go
  <service>_test.go
  <service>_bench_test.go
  repository/
  service/{user,admin,internalapi}/
  sqlc/{schema.sql,query.sql,bootstrap.go,...generated}
  sqlc.yaml
```

- Go 1.26.1 по текущему go.mod, pgx/database/sql через sqlwrap, SQLC,
  goccy/go-json. Отдельные go.mod, HTTP-серверы и новые инфраструктурные библиотеки
  этим пакетам не нужны.
- Корневые Collections / Inventory собирают фасады User/Admin/Internal;
  New/Run/NewWithDatabase/Close/IsReady, bootstrap и worker lifecycle повторяют
  действующий контракт. NewWithDatabase не закрывает переданную БД.
- Общие Identity/WorkspaceID используются без потери app/platform/user scope.
  Root Actor сейчас описывает связанного пользователя; его нельзя автоматически
  считать моделью административных полномочий. Operator ID/причина коррекции
  фиксируются отдельно по существующему admin-контракту.
- Repository владеет SQLC, транзакциями, блокировками, проверкой ревизий,
  receipt/journal/stats/outbox. Service преобразует публичные модели и контексты.
- Используются contextutil, goroutine manager, callback Store/worker, sqlwrap
  cache/version/codec/mutex и importexport/jobs. Отдельные таблицы callback
  `collections_clb_event` / `inventory_clb_event`; общие utilities расширяются
  только там, где существующая параметризация недостаточна.
- Admin actions получают статические access keys своего namespace и локализации
  RU/EN по контракту Control.RegisterManifest. Internal/worker операции не являются
  пользовательскими RBAC actions; transport авторизует вызов доверенного фасада.
- Архивы: диск по умолчанию, опциональный общий S3/MinIO, бюджеты ZIP/JSON,
  heartbeat/fencing/retry, transactional receipt, reconciliation и безопасный DTO.
- Изменения существующих Control-файлов в рабочем дереве не относятся к этому
  плану; реализация новых сервисов не должна их перезаписывать.

## 3. Collections — выполненные этапы реализации

### C1. Зафиксировать публичный контракт

До SQL описать Params/Result, typed errors, модель ревизии и точные scopes:

- User: HasItem, HasItems, ListOwned, ListCollections, GetCollection.
- Internal: SetOwned, SetOwnedBatch; source, OperationID и optional ExpectedRevision.
- Admin: создание/изменение/архив коллекций, локализации, состав и перестановка,
  журналируемая корректировка наличия, чтение журнала/статистики/callback store,
  archive queue/status/history/download.
- Чтение чужих коллекций получает отдельно caller identity и target subject.
  Target разрешается приложением в той же workspace/app; нельзя подменять caller
  или использовать произвольный cross-workspace scope.
- Предложение: OperationID уникален в workspace + полной identity + source
  во всём write API Collections. Method и весь семантический payload входят
  в fingerprint; повтор через другой метод не создаёт вторую операцию.
- Batch ограничен по числу изменений, атомарен, с одним receipt и результатами
  по элементам. Предложение: повторяющиеся пары collection/item внутри одного
  batch отклоняются. Численные лимиты запросов фиксируются в конфигурации/контракте.
- Предложение: отсутствующая отметка имеет revision=0; фактическое изменение
  увеличивает ревизию на 1, повтор того же состояния не меняет revision/changed_at.
  No-op всё равно получает receipt, без ownership_changed и изменения счётчиков.
- На этом этапе v1 предполагает плоские коллекции; группы коллекций — возможное
  расширение, в спецификации они не заданы обязательной сущностью.

Правила скрытия, повторного добавления и отзыва подтверждены пользователем
и перечислены в разделе 6. Остальные предложения C1 остаются проектными решениями
для фиксации в METHODS перед реализацией.

### C2. PostgreSQL и SQLC

Предварительные сущности:

- `collections_collection`: workspace/key, статус/активность, порядок, timestamps.
- `collections_localization`: workspace/collection/locale.
- `collections_entry`: workspace/collection/item_key, порядок, активность и
  soft-delete/активность без удаления пользовательских отметок. Повторное
  добавление восстанавливает тот же логический collection/item key.
- `collections_ownership`: полная identity + collection/item, owned, revision,
  changed_at. Уникальный ключ не позволяет создать две отметки одного элемента.
- `collections_operation`: scope операции, fingerprint, immutable result и metadata.
- `collections_change`: before/after, ревизии, source/operator/operation,
  timestamps; stats raw/daily и callback таблица по общему механизму.

Все FK локальные и содержат workspace. Никаких FK к Reference или account.
Индексы по точечной HasItem/HasItems, owned-list с пагинацией, составу и журналу.
Пагинация имеет стабильный tie-breaker по key/ID. SQLC generation и идемпотентный
bootstrap включаются с первого этапа схемы.

### C3. Надёжный write path

1. Проверить форму и все вложенные значения до write transaction.
2. В transaction сериализовать scope операции и пользователя; для batch
   блокировки брать в стабильном порядке. Первый вариант — пользовательская
   advisory lock, чтобы защитить также ещё не существующие ownership rows.
3. Сначала проверить receipt: одинаковый fingerprint возвращает исходный
   результат, другой payload — typed conflict. Изменившийся каталог не должен
   делать уже выполненный повтор недостижимым.
4. Для новой операции проверить актуальный состав/доступность и ExpectedRevision.
   Транзакция согласует эти проверки с admin/import изменением состава через
   общий порядок catalog/entry locks; не читать joined snapshot до lock wait.
5. Атомарно изменить только требуемые отметки, записать receipt и change history,
   stats и ownership_changed для реальных изменений. Один сбой откатывает batch.
6. После commit обновить cache versions; cache outage/diagnostic callback не
   превращает committed операцию в ложную ошибку записи.

Схема блокировок документируется до реализации: catalog scopes → owner scopes
→ receipt/ownership rows в сортированном порядке; все пересекающиеся write paths
используют один порядок. Конкретная гранулярность проверяется интеграционными тестами.

### C4. Чтения и cache

- HasItem/HasItems читают актуальное состояние PostgreSQL, без stale ownership L1.
  HasItems выполняет bulk query, включая отсутствующие/скрытые элементы как false. Проверка права использует
  одновременно актуальные collection/entry visibility и сохранённый owned;
  кэш каталога не участвует в решении о наличии права.
- Каталог кэшируется отдельно от пользовательских данных. Любой кэш владения для
  отображения получает явную допустимую устарелость; до такого решения v1 читает
  владение из БД. Two-node cache tests обязательны для используемых cache paths.
- GetCollection возвращает общий опубликованный состав без owned, с Version и
  KnownVersion/NotModified для долгого versioned cache. Версия и тело согласованы
  одним read-only snapshot; ownership выдача/отзыв версию не меняет.
- Для категории `colors` клиент вызывает GetCollection(CollectionKey="colors")
  и отдельно ListOwned(CollectionKeys=["colors"]). Отсутствующие отметки не создаются.
- ListOwned поддерживает выбранную коллекцию/набор коллекций, лимиты и пагинацию.
  Item metadata и account profile не загружаются N+1 из соседних сервисов.

### C5. Admin, outbox, статистика и архивы

- Коррекция оператора использует тот же write engine, обязательную причину и audit.
- Callback snapshot содержит identity, collection/item, before/after, revision,
  source и устойчивые event/operation IDs. Повтор доставки не создаёт новую запись.
- Предложенные stats: текущие owned, реальные grants/revokes, уникальные владельцы;
  no-op/replay не учитываются как изменение. Точные публичные метрики согласуются
  в METHODS, дневные итоги считают полный UTC-день.
- Catalog admin/import используют workspace advisory lock. Экспорт конфигурации —
  consistent snapshot; импорт объединяет A/B/C + B/C/D → A/B/C/D, B/C по архиву,
  заменяет переданный вложенный состав. Пользовательские отметки и receipts не
  стираются; ID/ключи коллекций сохраняют привязку истории.
- Поддержать fail_on_conflict, skip_existing, update_existing (по умолчанию),
  полную preflight validation и крупный bulk import. Отсутствующий во вложенном импортированном
  составе entry становится недоступным для HasItem, но его сохранённые
  owned/revision и история остаются для возможного повторного добавления.

### C6. Завершение Collections

Выполнить матрицу раздела 5, записать результаты benchmarks/EXPLAIN,
повторно проверить достижимость найденных проблем и исправить подтверждённые.
Передать готовые AGENT/DESIGN/METHODS, примеры вызовов/архива, результаты проверок
и известные ограничения. Только после этого переходить к Inventory.

## 4. Inventory — этапы после Collections

### I1. Контракт, политики и схема

Зафиксировать все методы исходной спецификации; типы quantities/revisions/slots,
operation scope, выбор стеков, auto-slot, совместимость, видимость групп,
transfer/action policies. Default inventory_key=main; вместимость v1 не ограничена.

Предварительные таблицы:
- inventory владельца (полная identity + inventory_key, revision);
- entry (стабильный ID, item_key, quantity, slot, revision);
- group/localization/item-group связи;
- policy разрешённых action/transfer;
- operation/participants/change journal, statistics и outbox.

Уникальная ячейка scoped по inventory, quantity>0, безопасные integer bounds.
История не удаляется вместе с опустевшим стеком. Решить deferrable unique constraint
либо другой проверенный способ перестановки слотов без промежуточных конфликтов.

### I2. Единый transactional engine

Сначала AddItems/RemoveItems/ApplyChanges: validate → locks → receipt replay →
актуальные revisions/policies → расчёт итогового состояния → write → snapshot /
journal / stats / outbox → commit / cache invalidation. Не вводить обходные SQL
write paths для admin, transfer или последующих лимитов.

Блокировки inventories берутся в каноническом порядке полной identity/inventory_key,
затем entries. Создание отсутствующего inventory защищается lock и unique key.
Все проверки количества и переполнений выполняются до записи итогового состояния.

### I3. Организация ячеек

Move, Swap, Split, Merge, Sort используют engine и expected revisions.
Проверить свободные/занятые цели, сохранение общего количества, отсутствие потери
стабильных entry IDs при move/sort, детерминированную сортировку и корректность
явного списка IDs. Новые IDs при split и судьба поглощённого ID при merge
фиксируются контрактом и immutable result.

### I4. Передача между владельцами

Transfer по умолчанию запрещён. Разрешённый transfer атомарно меняет отправителя
и получателя внутри Inventory, пишет один operation с обоими участниками и
событиями, соблюдает назначения и политики. Ни один trusted path не обходит
owner/quantity/revision/compatibility проверки. Cross-workspace/app/platform
разрешения требуют отдельного решения и не включаются молча.

### I5. Запросы действий

RequestAction проверяет актуальную запись, policy и revision; сохраняет snapshot
и outbox. Количество при запросе не списывается и не резервируется. GetOperation
возвращает собственную операцию пользователя, без раскрытия чужих приватных данных.
Приложение применяет последствия через идемпотентный ApplyChanges с проверкой
актуального состояния; callback ack не используется как бизнес-результат.

### I6. Чтения, эксплуатация и финальная проверка

Bulk GetCounts, фильтры групп/item keys, пагинация, cache versions, admin corrections,
stats, access manifest, config archives и lifecycle. Затем полная матрица тестов,
бенчмарки каждого метода/ветки, EXPLAIN горячих запросов и повторный аудит.

## 5. Обязательная финальная матрица для обоих сервисов

Тесты пишутся вместе с функциональностью; завершающий этап обязательно включает
полный фактический прогон и измерения, а не только наличие файлов.

### Общие тесты

- Каждый публичный метод: valid/invalid input, typed error, workspace и полная
  app/platform/user isolation. Не использовать один только PlatformUserID.
- Реальная PostgreSQL: schema/bootstrap twice, constraints, rollback, locks,
  cancellation/deadline и отсутствие partial writes.
- Same OperationID/payload replay; payload mismatch; одинаковый ключ из разных
  source/owner/workspace; retry после commit и изменения/удаления каталога.
- Immutable receipt/callback; отсутствие повторных stats/outbox записей.
  Это exactly-once запись события в локальной БД, не exactly-once доставка.
- Callback lease/retry/fencing/restart и idempotent consumer на тестовом примере.
- Две ноды с разными L1 и общей L2; version bump и cache outage после commit.
- New без соединения, Run cancellation, Close останавливает workers и ожидания,
  embedding не закрывает чужую БД, nil/not-ready сценарии.
- Archive round trip и все strategies; preflight без partial writes; invalid/bomb
  ZIP и структурные бюджеты; конкуренция admin/import, recovery receipt,
  пакет выше PostgreSQL parameter limit. S3 fake отдельно от live MinIO проверки.

### Collections

- false при отсутствии row, true→false→true, no-op true/false и ревизии.
- Один item в разных коллекциях, разные владельцы и target/caller isolation.
- Атомарный batch, duplicate pairs, partial-invalid batch, stale expected revision.
- Конкурентное создание первой отметки, два противоположных изменения, replay,
  пересекающиеся batches, concurrent catalog removal/import.
- Удаление/архивация/re-add и поздний отзыв: полная последовательность из
  раздела 6, сохранение revision, no-op/replay и атомарный mixed batch.
- HasItem/HasItems после удаления, архивирования и конфигурационного импорта:
  немедленный false на второй ноде даже при прогретом catalog cache.
- Полный состав/только owned, пагинация/locale, HasItems без N+1,
  актуальный HasItem после SetOwned(false) с другой ноды.

### Inventory

- Add/remove и auto-slot; insufficient quantity, overflow, zero-stack removal.
- Move/Swap/Split/Merge/Sort, stale revision, конфликт цели и валидность явного
  порядка IDs; сумма количеств до/после неизменна у перестановок/разделений.
- Одновременные списания, встречные transfers, retries, пересекающиеся sorts,
  отсутствие deadlock/частичного debit-credit и double-spend.
- Transfer запрещён/разрешён политикой, изоляция scopes и допустимое назначение.
- RequestAction ничего не тратит; после изменения entry stale callback snapshot
  не позволяет ApplyChanges повторно использовать предмет.

### Бенчмарки

Во всех случаях -benchmem, fixture setup вне таймера, проверка ошибок и результатов,
уникальные identity/operation keys для stateful success веток.

| Collections | Inventory |
| --- | --- |
| HasItem true/false, HasItems 1/100/1000 | GetEntry, ListEntries, GetCounts на разных размерах |
| ListCollections/GetCollection/ListOwned, catalog hit/miss | Группы/фильтры, hit/miss |
| SetOwned change/no-op/replay/conflict | Add/Remove/ApplyChanges success/replay/conflict |
| SetOwnedBatch на разных размерах | Move/Swap/Split/Merge/Sort по отдельности |
| Параллельные записи одного/разных owners | Transfer: разные пары/общий sender |
| Admin reads/writes, import/export | RequestAction/GetOperation, admin, import/export |

Выделить contention и throughput независимых владельцев. Зафиксировать среду,
размеры fixtures, ns/op, B/op, allocs/op, SQL round-trips; при оптимизации — до/после.
Порогов latency без измерений сейчас не обещать. Горячие SQL проверить EXPLAIN
(ANALYZE, BUFFERS); уменьшение числа запросов не должно ослаблять consistency.

Финальные команды (подставить имя сервиса):

```sh
(cd collections && sqlc generate)
go test -p 1 ./collections/...
go test -race -p 1 ./collections/...
go test ./collections -run '^$' -bench . -benchmem -count=3
go vet ./...
go build ./...
go test -p 1 ./...
git diff --check
```

Для Inventory — аналогичный набор с ./inventory. PostgreSQL fixtures изолированы
в отдельных тестовых БД, не рабочие данные. gofmt проверяется для всех изменённых Go.
Результаты реальной БД, S3 fake и live deployment отмечаются отдельно.

## 6. Решения до реализации

### Collections: решения пользователя от 4 ноября 2026

1. **Скрытие не стирает отметку, но блокирует право.** Если коллекция архивирована
   или неактивна, либо элемент удалён из состава/скрыт, HasItem возвращает false
   даже при сохранённом owned=true. HasItems применяет то же правило.
2. **Повторное добавление сохраняет состояние.** Возврат того же item key в ту же
   коллекцию сохраняет owned и его revision. Если во время скрытия право не
   отозвали, после публикации HasItem снова возвращает true. Если отозвали — false.
3. **Поздний отзыв разрешён.** Internal.SetOwned(false) разрешён для существующей
   отметки после удаления элемента или архивации коллекции. Установка нового
   owned=true требует действующей коллекции и элемента состава. Скрытый
   произвольный key без существующей отметки не становится допустимым для записи.

Следствия для реализации:

- Хранимое owned и эффективное право HasItem различаются. Чтение скрытого состава
  не переписывает owned=false в БД и не изменяет revision отметки.
- Soft-delete и import сохраняют стабильную логическую привязку ownership к
  collection/item key; удаление состава не запускает cascade delete ownership.
- Реальный отзыв true→false меняет ревизию и создаёт change/outbox даже во время
  скрытия. Архивация/повторная публикация сами по себе не являются SetOwned и
  не создают фиктивных ownership-change операций.
- ExpectedRevision сравнивается с сохранённой ревизией отметки, а не с
  вычисленным false от HasItem. После скрытия stale writer не обходит проверку.
- Replay receipt проверяется до текущей видимости: повтор уже выполненного
  SetOwned(true) после архивации возвращает исходный receipt, но не выдаёт право
  заново и не меняет актуальный ответ HasItem.
- Политика применяется к single, batch, admin correction и import одинаково.
  Batch с недопустимым новым true откатывается целиком, включая допустимые отзывы.
- В stats нужно различать сохранённые отметки и доступные сейчас элементы;
  archive/unpublish не должны выглядеть как пользовательский revoke в журнале.

Приёмочные последовательности:

| Действия | HasItem | Сохранённое состояние |
| --- | --- | --- |
| Выдать true → скрыть entry | false | owned=true, прежняя revision |
| Вернуть entry без отзыва | true | owned=true, прежняя revision |
| Архивировать коллекцию с owned=true | false | owned=true, прежняя revision |
| После скрытия выполнить SetOwned(false) | false | owned=false, revision увеличена |
| После отзыва вернуть entry/коллекцию | false | owned=false, ревизия отзыва сохранена |
| Новый SetOwned(true) в скрытом составе | false | typed error, изменений нет |
| Повторить старый OperationID после скрытия | false | исходный receipt, новых записей нет |


Остальные предложенные технические контракты C1 фиксируются перед кодом:
scope OperationID, no-op revision, batch limits/duplicates, admin metrics и отсутствие
collection groups в v1. Если обсуждение меняет предложение — обновить план/METHODS.

### Inventory: уточнить перед этапом I1

- Нумерация слотов и auto-allocation: первый свободный или append; numeric bounds.
- Разрешённые режимы auto-stack, лимит стека, детерминированный выбор при RemoveItems.
- Может ли item входить в несколько групп; default/fallback order при Sort.
- Scope transfer (workspace/app/platform/inventory), детализация policy и приоритеты
  item/group rules; никаких неявных разрешений по умолчанию.
- Operation scope для cross-owner transfer, идентификаторы split/merge результата,
  no-op и правила увеличения inventory/entry revisions.
- RequestAction: допустимость нескольких независимых запросов до применения,
  ограничения частоты; резервирование не следует из текущей спецификации.
- Точный набор admin statistics и правила локализации fallback.

Эти вопросы не требуют расширять Collections и не блокируют его планирование.
