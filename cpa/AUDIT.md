# Аудит асинхронных архивов CPA

Область: текущие async ZIP import/export и общий `internal/utils/importexport/jobs`.
Экспорт предназначен для переноса каталога, не для полного восстановления CPA.

## Риски

### 1. Потеря данных при трактовке архива как full backup

**Критичность: высокая.** Package содержит только offers, localization и rewards
(`repository/export_models.go:20-54`, `repository/export.go:24-81`). Он не
сохраняет personal-code pools, code statuses, assignments, immutable reward
snapshots, completion events, statistics и callback history, хотя это данные
CPA (`service/admin/models.go:44-91`, `sqlc/schema.sql:107-187`). После import
каталог восстановится, но выдачи и их история утрачены.

Контрдовод: contract явно называет export catalog-only и исключает Reference
items (`AGENT.md:31-42`); перенос истории и PII может быть намеренно запрещён.

Ремедиация: документировать archive как catalog migration, не DR backup; если
нужен backup, создать отдельный версионированный dump с политикой PII, pools,
assignments/events и callback outbox.

### 2. Неограниченные upload и распаковка ZIP

**Критичность: высокая (DoS).** Upload без лимита копируется на disk
(`jobs/disk.go:42-57`), import читает архив полностью в RAM
(`service/admin/archive_jobs.go:65-70`), export держит весь ZIP в
`bytes.Buffer` (`:34-51`). Нет пределов compressed/uncompressed bytes, числа
entries или manifest; ZIP bomb и большой catalog могут исчерпать disk/RAM.

Контрдовод: context прерывает копирование (`jobs/disk.go:51-54`, `120-130`), а
штатный export формирует только manifest.

Ремедиация: до decode лимитировать upload, compressed/uncompressed size, entries
и manifest; заменить memory buffering bounded streaming/temp-file обработкой.

### 3. Локальный archive не работает между нодами

**Критичность: высокая при нескольких replica.** Queue общая в PostgreSQL и
lease доступна любой ноде (`jobs/store.go:62-68`), тогда как archive хранится
возле executable (`cpa.go:196-203`). Другой worker или download-node не найдёт
file.

Контрдовод: single-node deployment или общий mounted volume безопасны; абстракция
`Archive` поддерживает S3/MinIO (`jobs/README.md:3-6`).

Ремедиация: использовать shared object storage в HA и проверить его на startup;
иначе явно ограничить обработку и download одной нодой.

### 4. Lease истекает во время import и допускает повторное применение

**Критичность: высокая.** Default lease 10m (`jobs/jobs.go:18-21`) не
heartbeats. Expired `processing` job снова lease-ится (`jobs/store.go:62-68`),
пока первый worker делает commit import (`repository/import.go:118-150`).
Token fence защищает только update status, не повторную business mutation
(`jobs/store.go:69-75`).

Контрдовод: один active job на workspace (`jobs/store.go:130-133`) и workspace
mutation lock (`repository/import.go:118-121`) сериализуют writers, но не
последовательный duplicate после expiry.

Ремедиация: import deadline < lease с резервом, lease renewal и transactional
idempotency receipt по job ID.

### 5. Commit импорта и completed-status неатомарны, retry отсутствует

**Критичность: высокая.** Domain transaction завершён до `store.complete`
(`repository/import.go:118-150`, `jobs/manager.go:410-420`). Crash/ошибка в
этом окне оставляет `processing`, который снова выполнится после lease. Любая
ошибка становится terminal `failed` (`jobs/manager.go:340-354`): нет retry
count, backoff и контролируемого resume.

Контрдовод: completion и history атомарны и fenced (`jobs/store.go:360-395`).

Ремедиация: outbox/receipt, записываемый с import mutation, bounded retry with
backoff и отдельный idempotent retry API.

### 6. Публичный API раскрывает внутренние job data/options

**Критичность: средняя.** `ArchiveJob` -- alias `jobs.Job`
(`service/admin/models.go:103-115`) и возвращается без projection
(`archive_jobs.go:147-181`). Клиент получает raw `Options`, `ArchiveKey`,
`LockedBy`, `LeaseToken`, raw `Error` (`jobs/jobs.go:68-86`). Options содержат
сериализованный request; `ImportRequest` включает `Package`
(`repository/export_models.go:56-59`), что позволяет сохранение/раскрытие
лишних payload.

Контрдовод: lookup ограничен service/workspace (`archive_jobs.go:156-159`), ZIP
не встроен в job row.

Ремедиация: public DTO без options/key/lease/debug error, не сохранять package
в options и ограничивать/санитизировать ошибки.

### 7. Default transaction timeout непригоден для большого архива

**Критичность: средняя.** Нулевой `QueryTimeout` превращается в 1s
(`repository/repository.go:292-298`); `WithTx` применяет его к import
transaction (`:135-161`). Worker не задаёт archive-specific timeout
(`jobs/manager.go:399-420`). В production default большой valid import будет
rollback-иться; рост общего timeout без lease coordination создаст duplicate
риск.

Контрдовод: service options могут задать иной `QueryTimeout`, batch insert
снижает parameter pressure.

Ремедиация: явный `ArchiveImportTimeout` для parse+transaction и lease больше
timeout с резервом на rollback/status.

### 8. Orphan archive и неоднозначный manifest

**Критичность: средняя.** Import file появляется до DB row (`jobs/manager.go:143-168`),
export file -- до `complete` (`:371-394`); crash создаёт orphan, который
retention cleanup не видит. Import принимает первый `manifest.json`, игнорируя
дубли и extra entries (`service/admin/archive_jobs.go:75-95`), без container
checksum/version.

Контрдовод: обычная queue error компенсируется Delete (`jobs/manager.go:166-168`),
cleanup безопасно claim-ит referenced archives (`:423-460`), package validation
проверяет domain format/service.

Ремедиация: staging namespace + reconciler, ровно один manifest, whitelist
entries и checksum/container version.

## Уже существующие защиты

- Unique partial index сериализует queued/processing job для service/workspace
  (`jobs/store.go:130-133`).
- Lease token и worker ID fenced status transitions (`jobs/store.go:69-75`,
  `360-438`), а PostgreSQL clock используется для lease/cleanup claim.
- Import валидирует package, выполняет workspace-locked transaction и только
  после успеха инвалидирует CPA cache (`repository/import.go:94-150`).
- DiskArchive генерирует ключи и запрещает traversal (`jobs/disk.go:11-13`,
  `63-68`, `112-117`).
- Cleanup claim token защищает от обычного конкурентного удаления archive
  (`jobs/store.go:77-89`, `441-489`).
