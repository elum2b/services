# Аудит асинхронных архивов Promo

Область: текущие async ZIP import/export и общий `internal/utils/importexport/jobs`.
Экспорт является переносом каталога, а не резервной копией.

## Риски

### 1. Потеря данных при трактовке архива как full backup

**Критичность: высокая.** `ExportPackage` содержит только promo, localization и
rewards (`repository/export_models.go:20-50`), а export читает только три
catalog-таблицы (`repository/export.go:24-55`; `sqlc/query.sql:34-50`). В нём
нет activation count, soft-delete state, redemption/reward snapshot, событий,
daily statistics и callback history, хотя эти сущности принадлежат сервису
(`service/admin/models.go:19-59`, `sqlc/schema.sql:72-163`). Восстановление в
пустой workspace даст работающий каталог, но не восстановит историю и лимиты.

Контрдовод: contract сервиса прямо описывает export как catalog-only и не
включает item metadata (`AGENT.md:31-38`).

Ремедиация: назвать API/документацию «catalog archive» и явно запретить его как
DR backup; для backup ввести версионированный полный dump с отдельной политикой
для PII, событий и callback/outbox.

### 2. Неограниченные upload и распаковка ZIP

**Критичность: высокая (DoS).** Upload без лимита копируется на диск через
`io.Copy` (`jobs/disk.go:42-57`); затем import читает весь archive в RAM
(`service/admin/archive_jobs.go:65-70`), а export сначала собирает весь ZIP в
`bytes.Buffer` (`:34-51`). Нет лимита compressed/uncompressed bytes, числа
entries или размера manifest, поэтому ZIP bomb и большой каталог исчерпают
disk/RAM.

Контрдовод: reader отменяется при отмене request context (`jobs/disk.go:51-54`,
`120-130`), а ZIP имеет только ожидаемый manifest в штатном export.

Ремедиация: ограничить upload и ZIP (compressed, uncompressed, entries и
manifest), проверять их до decode и перейти на bounded streaming/temp-file путь.

### 3. Локальный archive не работает между нодами

**Критичность: высокая при нескольких replica.** Job хранится в PostgreSQL, и
его может lease-ить любая нода (`jobs/store.go:62-68`), но archive лежит рядом
с binary конкретной ноды (`promo.go:192-199`, `jobs/disk.go:11-13`). Нода,
взявшая import/export job, может не иметь соответствующего файла; download
также зависит от локальной ноды.

Контрдовод: один process/общий volume делает текущий `DiskArchive` достаточным;
интерфейс уже допускает S3/MinIO (`jobs/README.md:3-6`).

Ремедиация: для HA использовать shared durable object storage и health-check
конфигурации; либо явно ограничить worker и download одной нодой.

### 4. Lease истекает во время import и допускает повторное применение

**Критичность: высокая.** Lease по умолчанию 10 минут (`jobs/jobs.go:18-21`) не
продлевается. После expiry job снова выбирается как `processing` (`jobs/store.go:62-68`),
пока первый worker импортирует. Import коммитит catalog transaction
(`repository/import.go:75-105`), после чего второй worker способен применить
archive повторно или получить конфликт; fencing защищает только status update
(`jobs/store.go:69-75`), а не доменную mutation.

Контрдовод: unique active-job index сериализует queue на workspace
(`jobs/store.go:130-133`), а import берёт workspace advisory lock
(`repository/import.go:75-78`). Это предотвращает параллельную запись, но не
повтор после lease expiry.

Ремедиация: установить import timeout меньше lease с запасом и heartbeat/renewal;
добавить idempotency marker, связанный с job ID, внутри import transaction.

### 5. Commit импорта и completed-status неатомарны, retry отсутствует

**Критичность: высокая.** `repository.Import` успевает commit до
`store.complete` (`repository/import.go:75-105`, `jobs/manager.go:410-420`).
Ошибка/авария между ними оставит job `processing`; после lease она будет взята
снова. Любая handler error переводит job сразу в `failed` (`jobs/manager.go:340-354`),
нет retry count, backoff или ручного безопасного retry.

Контрдовод: `complete` fenced token и history пишутся одной DB transaction
(`jobs/store.go:360-395`), поэтому stale worker не перезапишет новый status.

Ремедиация: transactional import receipt/outbox для completion, ограниченные
retry/backoff и явная операция retry с новым idempotency key.

### 6. Публичный API раскрывает внутренние job data/options

**Критичность: средняя.** Admin API возвращает alias полного `jobs.Job`
(`service/admin/models.go:72-84`) из status/history (`archive_jobs.go:147-181`).
В нём публичны `Options`, `ArchiveKey`, `LockedBy`, `LeaseToken`, внутренние
timestamps и raw error (`jobs/jobs.go:68-86`). `Options` сериализуются из
request, а `ImportRequest` допускает `Package` (`repository/export_models.go:52-55`),
поэтому caller может сохранить и затем раскрыть большой или чувствительный
payload.

Контрдовод: методы workspace-scoped (`archive_jobs.go:156-159`, `173-180`), а
фактический ZIP хранится отдельно.

Ремедиация: вернуть отдельный public DTO без options/key/lease/error details,
не сохранять package в options и ограничить/санитизировать диагностические ошибки.

### 7. Default transaction timeout непригоден для большого архива

**Критичность: средняя.** При незаданном `QueryTimeout` repository выбирает 1s
(`repository/repository.go:188-194`), и `WithTx` применяет его ко всей import
transaction (`:100-121`). Worker передаёт только свой context без отдельного
archive budget (`jobs/manager.go:399-420`). Большой валидный import будет
систематически rollback-иться; увеличение общего timeout без связи с lease
усилит риск пункта 4.

Контрдовод: deployment может передать `Options.QueryTimeout`, а batch import
уменьшает число SQL parameters.

Ремедиация: ввести явный `ArchiveImportTimeout`, применять его к decode и
transaction, и гарантировать lease > timeout + rollback/status reserve.

### 8. Orphan archive и неоднозначный manifest

**Критичность: средняя.** File записывается до создания DB job; crash между
`Archive.Store` и `queueWithOptions` оставляет orphan (`jobs/manager.go:143-168`).
Аналогично export сохраняет file до `complete` (`:371-394`). Import берёт
первый `manifest.json`, не отвергая второй manifest или лишние entries
(`service/admin/archive_jobs.go:75-95`); нет checksum/version для ZIP как
контейнера.

Контрдовод: обычная ошибка queue удаляет upload (`jobs/manager.go:166-168`),
cleanup удаляет только архивы, на которые есть DB ссылка (`:423-460`), а package
валидируется repository-level.

Ремедиация: staging+reconciler для unreferenced objects; требовать ровно один
manifest, whitelist entries и проверять container format/version/checksum.

## Уже существующие защиты

- PostgreSQL unique partial index допускает не более одной queued/processing
  job на service/workspace (`jobs/store.go:130-133`).
- Lease и completion/fail fenced worker ID + cryptographic token; stale worker
  не может завершить новую lease (`jobs/store.go:69-75`, `360-438`).
- Import валидирует package и выполняется под workspace lock в transaction;
  cache invalidируется только после успешного commit (`repository/import.go:51-105`).
- Disk keys генерируются, не происходят из имени пользователя, а path traversal
  отклоняется (`jobs/disk.go:11-13`, `63-68`, `112-117`).
- Cleanup claim использует PostgreSQL clock и token, что исключает обычное
  конкурентное удаление одного archive (`jobs/store.go:77-89`, `441-489`).
