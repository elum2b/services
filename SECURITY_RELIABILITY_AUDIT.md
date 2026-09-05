# Аудит оставшихся сервисов — 2026-09-05

Объём: Calendar, Promo, Tasks, Payment, Control и Delivery. CPA и Reference проверены отдельно ранее. Проверка локальная: business logic, PostgreSQL, concurrency, cache, lifecycle и фоновые процессы. Валидация внешнего API доверенная. Это не проверка production infrastructure или внешних провайдеров.

## Результат повторной проверки

| Сервис | Подтверждённые исправления | Отчёт |
|---|---|---|
| Calendar | transaction context, архивы/S3/DTO/reconciliation, default merge, полные дни статистики | [Calendar](calendar/SECURITY_RELIABILITY_AUDIT.md) |
| Promo | те же общие исправления, конкурентный Apply одного identity, полные дни статистики | [Promo](promo/SECURITY_RELIABILITY_AUDIT.md) |
| Tasks | общие исправления, skip_existing без ненужных secrets, атомарная суточная статистика, защита diagnostic callback | [Tasks](tasks/SECURITY_RELIABILITY_AUDIT.md) |
| Payment | общие исправления, default merge, атомарная суточная статистика | [Payment](payment/SECURITY_RELIABILITY_AUDIT.md) |
| Control | окончательный отзыв MCP-токенов при удалении platform member, deadline транзакций | [Control](control/SECURITY_RELIABILITY_AUDIT.md) |
| Delivery | новых подтверждённых дефектов в проверенных путях нет; существующие защиты повторно проверены | [Delivery](delivery/SECURITY_RELIABILITY_AUDIT.md) |

Старые AUDIT.md сохранены как исторические документы со ссылкой на актуальные статусы. Не переносились автоматически устаревшие claims: jobs уже имеет heartbeat/fencing/retry/receipt/sweep; Tasks уже шифрует secrets и включает skipped tasks в ID-карту conditions; Payment уже исправил shared localization и canonical identifiers, содержит dependency preflight.

## Воспроизведения и регрессии

- Promo: четыре Apply одного identity принудительно ожидали одну row lock. До исправления — 1 success и 3 PostgreSQL 23505; unique constraint не допускал двойной выплаты, но ломался idempotent response. Теперь отдельный LockApplyOffer и свежий bundle; TestPromoConcurrentSameIdentity.
- Control: MCP-токен создаётся до удаления участника, затем account повторно активируется через приглашение/2FA. Тест проверяет, что токен отозван навсегда и revoked_at записан. Отдельный тест удерживает authentication lock и проверяет repository deadline/rollback.
- Calendar/Promo/Tasks/Payment: Test<Service>DefaultImportMergesCatalog проверяет A/B/C + B/C/D → A/B/C/D и приоритет полей импорта. Полные workflow tests теперь выполняют первый refresh статистики на from == until и проверяют полные суточные итоги.
- Tasks: TestRequireImportSecrets дополнен skip_existing без secrets и проверкой сохранённого secret. Archive round-trip проверяет безопасный публичный JSON и шифрование options в хранилище.
- Архивы: сохранены все round-trip tests; общие tests проверяют ZIP bounds/bomb/entries, S3 через HTTP fake, receipt rollback/recovery, fencing, service isolation, запрет download import. Добавлен тест запрета экспорта manifest, не проходящего import shape budget.
- Root archive_jobs_test.go объединены с основными service_test.go по правилам AGENT.md; тесты не удалены.

## Проверки

- `go test -p 1 ./...` — прошёл, включая локальные PostgreSQL integration tests всех сервисов.
- `go vet ./...`, `go build ./...`, `git diff --check` — прошли.
- SQLC regenerated для Calendar/Promo/Tasks/Payment/Control, gofmt выполнен.
- `go test -race -p 1 ./calendar ./promo ./tasks ./payment ./control ./delivery ./internal/utils/importexport/jobs -count=1` — прошёл, включая новый lock/deadline test Control.

При параллельном первом прогоне были deadline failures в трёх timing/lock tests Tasks/Control. Изолированный повтор и последовательный полный прогон прошли. Дефолтные product timeout не увеличивались ради тестов.

## Открытые решения

1. **Решено 2026-09-06:** уменьшать activation_count импортом запрещено. Исправлено атомарным GREATEST текущего/архивного значения в UPSERT; добавлен регрессионный тест потраченного лимита.
2. Требуется ли перенос пользовательской истории/прогресса в архивах Calendar/Tasks/Payment/Promo? Текущий формат каталожный; в непустой БД история сохраняется, в пустой не восстанавливается (Promo переносит только общий activation_count). Ответ ожидается. Расширение формата без решения не выполнялось.

## Границы проверки

- Реальные MinIO/S3, несколько production реплик, сетевой обрыв именно в COMMIT и внешние payment/blockchain endpoints не проверялись. S3 проверен общими HTTP fake tests; это не production qualification.
- Диск подходит одной реплике; нескольким нужен общий ArchiveS3 либо общий volume. ArchiveS3 использует отдельный service prefix. Custom Archive без ArchiveLister не даёт orphan object sweep.
- Гарантия callback — повторная доставка с idempotency key, не exactly-once внешнего эффекта. Получатель обязан дедуплицировать.
- Зафиксированные ранее ограничения Reference (crash-orphan media, недоступность межнодового cache до recovery/TTL) этим проходом не закрывались.
- Остаточные гипотезы без доказанного пути не выдаются за уязвимости. Наличие прошедших тестов не означает отсутствие всех возможных дефектов.
