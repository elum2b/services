# Аудит асинхронного archive import/export Payment

Проверка актуального кода `payment` и общего `internal/utils/importexport/jobs`.
Ниже «защиты» означают уже существующие барьеры, а не подтверждение отсутствия
риска.

## 1. Неограниченный ZIP и manifest

**Критичность: высокая.** `DiskArchive.Store` копирует вход без лимита
(`internal/utils/importexport/jobs/disk.go:42-71`), а import затем полностью
читает его в RAM (`payment/service/admin/archive_jobs.go:65-70`). Нет лимита
на compressed/uncompressed ZIP, число entries, размер `manifest.json` или
точно одного manifest. Большой upload либо ZIP bomb может исчерпать диск/RAM;
первый подходящий manifest принимается (`archive_jobs.go:75-93`).

**Защиты и контрдоводы:** ключ файла генерируется сервером, path traversal
отсекается (`disk.go:63-68`, `112-117`); JSON и package валидируются до DB
mutation (`repository/import.go:92-95`). Это не ограничивает объём до
декодирования.

**Ремедиация:** ввести лимиты upload, compressed/uncompressed entry и entries,
стримингово читать единственный manifest через `io.LimitReader`; покрыть
превышения и ZIP bomb тестами.

## 2. Локальный archive storage при нескольких нодах

**Критичность: высокая.** Job lease хранится в общей PostgreSQL
(`jobs/store.go:62-70`), но archive всегда находится рядом с executable
конкретной ноды (`payment/payment.go:242-249`). После постановки import на A
job может leased нодой B, где файла нет; то же относится к export/download и
rolling restart. Job станет `failed` при `Open` (`jobs/manager.go:399-411`).

**Защиты и контрдоводы:** `SKIP LOCKED`, lease token и DB unique index дают
корректный выбор одного активного job (`jobs/store.go:62-70`, `130-133`). Это
защищает очередь, но не делает локальный файл доступным другим нодам.

**Ремедиация:** использовать shared durable object storage/volume для всех
воркеров либо закреплять обработку за нодой-владельцем; добавить two-node
integration test и restart test.

## 3. Истёкший lease допускает повторный import

**Критичность: высокая.** Default lease равен 10 минутам
(`jobs/jobs.go:18-21`), продления lease нет. Пока первый handler выполняет
долгий import, второй worker получает тот же `processing` job после
`locked_until` (`jobs/store.go:62-68`). Fencing защищает только финальные
`complete`/`fail` (`jobs/store.go:69-75`, `360-438`), не вызов
`handler.Import`; оба могут применить package. Workspace advisory lock
сериализует DB mutations (`payment/repository/import.go:112-154`, `170-185`),
но не устраняет повторное, в том числе destructive, применение.

**Защиты и контрдоводы:** transaction import атомарна, а stale worker не может
завершить job. Короткий package обычно укладывается в десять минут, поэтому
риск зависит от объёма и задержек DB.

**Ремедиация:** heartbeat/renew lease на время handler, отдельный import
deadline короче lease с запасом, либо idempotent application, привязанная к
job ID; тестировать lease expiry посреди import с двумя workers.

## 4. Внешние зависимости package не переносит

**Критичность: средняя.** Export содержит catalog, prices и wallet, но не
assets, rates или Reference item catalog (`payment/AGENT.md:54-56`,
`repository/export_models.go:22-103`). `payment_price` ссылается на
`payment_asset` (`payment/sqlc/schema.sql:178-202`); dynamic price при import
ищет текущий rate target workspace и завершается `ErrAssetRateNotFound`
(`repository/import.go:305-447`). `product_item.item_id` импортируется как
внешний ключ предметной области без проверки существования Reference
(`repository/import.go:774-821`).

**Защиты и контрдоводы:** rate проверяется до записи catalog, поэтому import
откатывается; отсутствие Reference в export явно задокументировано. Однако
порядок подготовки target (assets, rates, Reference) не выражен контрактом и
не проверяется preview.

**Ремедиация:** явно документировать/возвращать dependency preflight с
отсутствующими assets, rates и Reference items либо включить согласованный
dependency manifest; добавить tests для пустого target и несовпадающих rates.

## 5. `update_existing` удаляет shared localization

**Критичность: высокая.** Перед update удаляются все localization для title/
description keys конфликтующих groups/products
(`payment/repository/import.go:450-532`). Затем вставляются только locale из
archive (`584-667`). Неполный archive тем самым стирает отсутствующие locale;
если ключ разделён с неимпортируемой сущностью, стирается и её перевод.

**Защиты и контрдоводы:** удаление и повторная вставка находятся в одной DB
transaction; `skip_existing` этот путь не выполняет. Но unique key именно
`(workspace_id, locale, localization_key)`, а не ownership entity
(`payment/sqlc/schema.sql:111-120`), поэтому transaction не ограничивает
blast radius.

**Ремедиация:** не удалять locale, отсутствующие в archive; удалять только
явно переданный entity-owned key после проверки ownership, либо запретить
shared keys. Нужны tests partial locale и два продукта с одним key.

## 6. Нормализация проверяется, но не сохраняется

**Критичность: средняя.** Валидаторы делают `TrimSpace` у локальных копий
group/product/item/asset key (`payment/repository/import.go:1129-1146`,
`1185-1203`, `1258-1284`, `1314-1318`), но import bulk использует исходные
значения (`535-581`, `774-821`, `824-893`). Поэтому `" product "` может пройти
non-empty validation и быть сохранён как иной ключ; locale также лишь
проверяется на непустоту (`1366-1377`). Автогенерируемые localization keys
строятся до canonical trim (`1500-1539`).

**Защиты и контрдоводы:** duplicate checks используют trimmed локальные копии,
а DB constraints и `target.Validate` отсекают часть некорректных данных. Они
не гарантируют единую canonical identity между preview, dependency lookup и
записью.

**Ремедиация:** канонизировать все identifier/locale/enum поля один раз в
`normalizeExportPackage` до validate, либо отклонять неканонический input;
проверить whitespace duplicates и сохранённые значения тестами.

## 7. Archive object и DB status не образуют одну транзакцию

**Критичность: средняя.** Import сначала записывает archive, затем job в DB
(`jobs/manager.go:131-171`); export сначала записывает object, затем помечает
job completed (`357-394`). Crash между операциями оставит orphan file либо job
с отсутствующим archive. Cleanup также сначала удаляет object, потом очищает
DB key (`423-460`), что создаёт обратное окно. Local disk не даёт atomic
commit с PostgreSQL.

**Защиты и контрдоводы:** при обычной ошибке queue после import store вызывается
`Delete` (`manager.go:166-168`); retention cleanup пытается убрать завершённые
и failed dumps. Эти меры не исполнятся при crash/kill и cleanup не находит
orphan без DB record.

**Ремедиация:** зафиксировать допустимое окно и добавить reconciliation по
object metadata/age, либо staging state и recoverable outbox; тестировать
crash/failure injection между Store, queue, complete и cleanup.

## 8. Retry-семантика и покрытие отказов недостаточны

**Критичность: средняя.** Любая ошибка handler переводит job в terminal
`failed` без retry/backoff (`jobs/manager.go:340-355`); неудачная запись
`fail` игнорируется. Одновременно expiry lease является неявной повторной
доставкой. Нет явного retry count, классификации transient ошибок или
операторского requeue API.

**Защиты и контрдоводы:** job/history сохраняются, status и download
workspace-scoped (`jobs/manager.go:174-250`); базовый payment test проверяет
один успешный round trip (`payment/archive_jobs_test.go:14-70`), а общий test
проверяет fencing финального статуса (`jobs/jobs_integration_test.go:74-178`).
Они не покрывают bounds, multi-node storage, lease во время handler,
partial localization, dependencies и storage/DB failure windows.

**Ремедиация:** определить явную retry policy (attempts, backoff, requeue,
idempotency) и добавить перечисленные failure-injection/integration tests до
расширения archive API.
