# Аудит асинхронного archive import/export (tasks)

Проверка: 2026-09-03. Область: `tasks` и общий `internal/utils/importexport/jobs`.

## Критично

### Секреты импорта хранятся открытым текстом и возвращаются в Jobs

- **Код:** `tasks/service/admin/archive_jobs.go:130-143` сериализует весь `ImportRequest` в `options`; `ImportRequest` содержит `Secrets` (`tasks/repository/models.go`, тип запроса). `importexport_job.options` — обычный `JSONB` (`internal/utils/importexport/jobs/store.go:119-128`), а `Job.Options` публично возвращается через `ArchiveJob` из `ArchiveJob`/`ArchiveHistory` (`tasks/service/admin/archive_jobs.go:147-181`, `internal/utils/importexport/jobs/jobs.go:68-86`).
- **Последствие:** значения токенов доступны читателям БД, бэкапам/репликации и вызывающему публичный Admin Jobs API данного workspace; запись сохраняется после очистки dump (`internal/utils/importexport/jobs/manager.go:423-460`).
- **Контрдоказательства и защиты:** секреты не попадают в обычный export по умолчанию (`tasks/repository/export.go:53-62`); при применении конфигурации они шифруются (`tasks/repository/import.go:821-829`). Это не защищает значение до выполнения job и не скрывает `Options`.
- **Исправление:** хранить секреты вне `options` (одноразовое защищенное хранилище/шифрованный столбец с KMS); в DTO статуса и истории исключить/redact `Options`; ограничить DB-доступ и удалить секретный материал при terminal-status.

## Высокий

### Не ограничены размер ZIP, распаковка и JSON manifest

- **Код:** загрузка целиком на диск выполняется через неограниченный `io.Copy` (`internal/utils/importexport/jobs/disk.go:42-57`); worker читает весь ZIP в память (`tasks/service/admin/archive_jobs.go:65-70`) и декодирует manifest без лимита (`:75-90`).
- **Последствие:** автор импорта может исчерпать диск, RAM или CPU (в том числе ZIP-bomb); отсутствие других entry не меняет риск единственного сжатого manifest.
- **Контрдоказательства и защиты:** ключ файла генерируется сервером и защищен от traversal (`internal/utils/importexport/jobs/disk.go:11-13,112-117`); ZIP ищется по точному имени (`tasks/service/admin/archive_jobs.go:75-77`). Лимитов размера, числа entry, compressed/uncompressed ratio и глубины JSON нет.
- **Исправление:** лимитировать прием и хранение dump, разрешить ровно один `manifest.json`, проверить его declared/uncompressed size и ratio до чтения; декодировать через `io.LimitReader`, ограничить JSON/число объектов и установить контекстный deadline.

### Lease истекает во время неограниченной операции

- **Код:** lease фиксирован на 10 минут (`internal/utils/importexport/jobs/jobs.go:18-24,65-67`), берется единственным `locked_until` без heartbeat (`store.go:62-68`); `handle` передает корневой ctx без deadline в export/import (`manager.go:336-420`). Просроченная `processing` job снова leaseable (`store.go:62-64`).
- **Последствие:** второй worker может одновременно повторно применить долгий import. Fencing защищает только запись terminal-state (`store.go:69-75,360-438`), не транзакционные побочные эффекты handler; первый worker после истечения не сможет корректно завершить/пометить ошибку.
- **Контрдоказательства и защиты:** `FOR UPDATE SKIP LOCKED`, уникальная active-job на workspace и lease token предотвращают обычный параллелизм/устаревший terminal update (`store.go:62-68,130-133`; `jobs_integration_test.go:74-157`). Они не продлевают lease и не отменяют уже исполняющийся handler.
- **Исправление:** запускать каждый handler с timeout меньше lease и периодически renew lease; при потере fencing отменять ctx и не допускать повторного применения без идемпотентного ключа/журнала прогресса.

### Локальный archive недоступен другому хосту

- **Код:** jobs лежат в общей PostgreSQL, поэтому lease может взять любая реплика (`store.go:62-68`), но archive создается в `filepath.Dir(os.Executable())/tasks/importexport` (`tasks/tasks.go:198-205`) через локальный `DiskArchive` (`jobs/disk.go:42-71`).
- **Последствие:** job, поставленная на node A, на node B завершится `open import dump`; export, записанный на A, не скачается с B. Ошибка становится terminal failed без автоматической повторной доставки.
- **Контрдоказательства и защиты:** атомарная локальная запись через temp+rename (`jobs/disk.go:42-71`), opaque keys и 24-часовая retention (`jobs.go:20-21`, `manager.go:423-460`) полезны на одном хосте. Общего volume/object storage и node affinity нет.
- **Исправление:** production `Archive` должен быть shared/durable (object storage или гарантированно общий volume); до миграции закрепить worker и download за node-владельцем и мониторить `open import dump`.

## Средний

### `skip_existing` теряет сложные условия при смешанном наборе задач

- **Код:** конфликтующая parent-задача исключается (`tasks/repository/import.go:743-748`); condition добавляется только если ее child есть в `taskIDs` (`:750-772`). `taskIDs` заполняется только для не-skipped задач (`:523-620`).
- **Последствие:** если новый complex parent ссылается на уже существующую condition-задачу, ссылка валидна в пакете, но при `skip_existing` child отсутствует в `taskIDs`, condition молча не импортируется. Аналогично пропадает ссылка нового parent на любой skipped child.
- **Контрдоказательства и защиты:** валидация требует, чтобы child был в пакете, и запрещает циклы (`:1439-1453`); skipped parent намеренно не изменяется. Это не сохраняет условия нового parent и нет предупреждения/счетчика потерянных связей.
- **Исправление:** построить ID-карту и для существующих skipped задач, затем импортировать связи нового parent; либо явно отклонять такую комбинацию с conflict/warning. Добавить тест mixed new/skipped complex conditions.

### Секреты требуются для конфигураций, которые будут пропущены

- **Код:** `requireImportSecrets` вызывается до Preview и стратегии (`tasks/repository/import.go:167-169`) и проходит по всем configs (`:1458-1484`); только позднее `importPartnerConfigsBulk` пропускает конфликтующие config при `skip_existing` (`:814-819`).
- **Последствие:** `skip_existing` не может импортировать остальную часть архива без секретов для уже существующей и не изменяемой config; это повышает давление передавать лишние секреты, которое в async path превращается в критичный plaintext-риск выше.
- **Контрдоказательства и защиты:** обязательность секрета защищает создаваемые/обновляемые configs; embedded secret поддержан (`:1466-1468,1487-1505`). Нет фильтрации required secrets по preview и стратегии.
- **Исправление:** вычислять конфликты до проверки и требовать секреты лишь для configs, которые будут применены; `PreviewImport` должен возвращать strategy-aware RequiredSecrets.

## Восстановление и повтор

- **Код:** polling DB-ошибок повторяется (`internal/utils/importexport/jobs/manager.go:303-337`), а просроченная lease автоматически доступна следующему worker (`store.go:62-68`). Но ошибка handler сразу переводит job в `failed` (`manager.go:340-355`), активная уникальность снимается, и API retry/requeue отсутствует.
- **Риск:** временные ошибки storage/DB/сети требуют ручной постановки нового import; повтор может применить импорт после частичного внешнего эффекта. История статусов есть (`store.go:341-354,390-438`), но не содержит попыток, retry-budget или классификации ошибок.
- **Защиты и меры:** import repository выполняется в транзакции и блокирует workspace (`tasks/repository/import.go:173-198`), что снижает частичную запись БД. Нужны bounded retry с backoff для transient errors, счетчик attempts/next_attempt_at, явный admin retry с новым idempotency key и тесты crash-after-apply/lease-expiry.
