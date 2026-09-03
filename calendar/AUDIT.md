# Аудит асинхронного archive import/export (calendar)

Проверка: 2026-09-03. Область: `calendar` и общий `internal/utils/importexport/jobs`.

## Высокий

### Не ограничены размер ZIP, распаковка и JSON manifest

- **Код:** ingress сохраняет dump неограниченным `io.Copy` (`internal/utils/importexport/jobs/disk.go:42-57`); worker полностью читает ZIP в RAM (`calendar/service/admin/archive_jobs.go:65-70`) и без лимита декодирует `manifest.json` (`:75-90`).
- **Последствие:** аутентифицированный автор archive import может занять диск/RAM/CPU ZIP-bomb-ом или oversized JSON.
- **Контрдоказательства и защиты:** сервер сам генерирует ключ и блокирует traversal (`jobs/disk.go:11-13,112-117`), обработчик принимает только точное имя manifest (`calendar/service/admin/archive_jobs.go:75-77`). Нет квот на upload, compressed/uncompressed size, число ZIP entry либо лимита JSON.
- **Исправление:** ограничить bytes на ingress и disk, разрешить один manifest, валидировать размеры/ratio до чтения, применить `io.LimitReader` к entry и deadline на декодирование/import.

### Lease не продлевается и не ограничивает долгую обработку

- **Код:** default lease — 10 минут (`internal/utils/importexport/jobs/jobs.go:18-24,65-67`), выдается разовым `locked_until` без renewal (`store.go:62-68`), а `handle` передает worker ctx без job-timeout (`manager.go:336-420`). После expiry job вновь leaseable (`store.go:62-64`).
- **Последствие:** два хоста могут одновременно выполнять один долгий import/export. Lease token fencing отвергнет просроченное `complete`/`fail` (`store.go:69-75,360-438`), но не остановит первый handler и не откатит его внешние побочные эффекты.
- **Контрдоказательства и защиты:** `SKIP LOCKED`, token и unique active job на workspace защищают штатную конкуренцию (`store.go:62-68,130-133`); это покрыто fencing-тестом (`jobs_integration_test.go:74-157`). Heartbeat, cancellation при потере lease и idempotent apply отсутствуют.
- **Исправление:** renew lease до expiry, запускать handler с deadline, отменять при fencing-loss; сделать import идемпотентным по job ID и добавить тест превышения lease.

### Общая очередь сочетается с локальным storage и без retry

- **Код:** `importexport_job` берется любой репликой через общую БД (`internal/utils/importexport/jobs/store.go:62-68`), но архив настроен на `filepath.Dir(os.Executable())/calendar/importexport` (`calendar/calendar.go:199-206`) и хранится локально (`jobs/disk.go:42-71`). Ошибка handler сразу становится `failed` (`jobs/manager.go:340-355`).
- **Последствие:** node B не откроет import, загруженный на A; download export с B также не найдет файл. Перезапуск/временный storage outage не имеют backoff или автоматического retry, а после частичной операции ручной requeue может повторить эффект.
- **Контрдоказательства и защиты:** temp+rename обеспечивает атомарность на одном узле (`jobs/disk.go:42-71`); retention и cleanup защищают lifecycle dump (`jobs/manager.go:423-460`); DB polling transient failures повторяет (`:303-337`). Это не делает archive shared и не повторяет failed job.
- **Исправление:** использовать shared durable `Archive` (object storage/общий volume), либо node affinity как временную меру; хранить attempts/next_attempt_at, retry transient errors с backoff, обеспечить идемпотентность и дать явный безопасный retry для terminal job.
