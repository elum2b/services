# Аудит promo — 2026-09-05

Проверены бизнес-логика, PostgreSQL, фоновые процессы и старый AUDIT.md. Transport validation доверенная. Итоговые команды и ограничения — в [общем отчёте](../SECURITY_RELIABILITY_AUDIT.md).

## Общие исправления архивов и транзакций

- Контекст `WithTx` теперь передаётся в callback и все SQL-вызовы; ожидание блокировки отменяется deadline транзакции. Для archive import/export выделено 5 минут вместо короткого hot-path бюджета.
- Queue/status/history/download объединяют request и root context. Поток download проверяет отмену и освобождает context при Close.
- ZIP обрабатывается через защищённый временный файл: один manifest, проверка CRC/ratio/размеров, до 32 MiB распакованного JSON, ограничение глубины и структурных элементов до typed decode. Экспорт использует ZIP STORE, проверяет footprint в том же REPEATABLE READ snapshot до загрузки строк и проверяет структурный бюджет перед выдачей архива.
- Footprint — консервативный лимит всего каталога workspace: 100000 строк и 32 MiB JSON-представления строк; это не точный размер конечного архива. Для Tasks действует и при экспорте отдельных sections.
- `ArchiveS3` подключает общий S3/MinIO; без него остаётся диск. Префикс по умолчанию `<service>/importexport/`. Все реплики должны использовать один bucket/prefix. `ArchiveS3` несовместим с явными `Archive`/`ArchiveDirectory`.
- Публичный `ArchiveJob` больше не является `jobs.Job`: options, object key, lease tokens, receipt и raw error не выдаются; ошибка наружу обобщённая. Это изменение Go DTO для callers.
- Общая очередь уже имеет heartbeat/fencing, bounded retry/backoff, transactional import receipt и sweep orphan objects. Добавлен сервисный ReconcileImport для восстановления cache invalidation при повторе уже committed job. Custom archive должен поддерживать ArchiveLister для orphan sweep.
- Пустой conflict_strategy означает update_existing: каталог объединяется по ключам, отсутствующие в импорте сущности сохраняются. Explicit skip/fail остаются. Runtime history не заменяется каталогом.

## Доменные проверки

**Исправлено, средняя:** JOIN redemption в SELECT FOR UPDATE использовал snapshot до ожидания блокировки offer. Детерминированный тест четырёх заблокированных Apply одного identity до исправления получил одну активацию и три SQLSTATE 23505. Unique constraint предотвращал двойную награду, но API нарушал idempotency contract. Теперь LockApplyOffer выполняется отдельным statement перед чтением bundle, и повтор возвращает already_applied. Это также перечитывает rewards после ожидания concurrent catalog update. Тест: TestPromoConcurrentSameIdentity.

**Исправлено, средняя:** частичный день в RefreshDailyStats заменял суточный итог. Теперь полные UTC-дни; TestPromoApplyLifecycleAndCallback проверяет нулевой диапазон первого refresh, историю reward snapshot и callback. TestPromoConcurrentLifetimeLimit проверяет общий лимит; TestPromoDefaultImportMergesCatalog — union каталога.

**Исправлено по решению пользователя 2026-09-06:** импорт не уменьшает activation_count. UPSERT использует GREATEST текущего и импортированного значений под блокировкой строки PostgreSQL. TestPromoImportNeverDecreasesActivationCount до исправления воспроизвёл сброс трёх фактических активаций в ноль; проверяет меньший/нулевой/равный счётчик, сохранение redemption и лимита, применение настроек и большего счётчика, включая пустой target.

## Повторная оценка старого AUDIT.md

1. Архив — каталог и aggregate activation_count, а не full backup: redemption/callback history не переносится. В непустом workspace имеющаяся история остаётся. Восстановление в пустую БД не восстанавливает персональную дедупликацию — это граница формата, а не исправленная гарантия.
2–8. ZIP bounds, S3, heartbeat, receipt/retry, безопасный DTO, отдельный timeout и orphan sweep теперь присутствуют. Общие jobs-защиты существовали до этого прохода, caller-specific исправления внесены сейчас.

Проверка исправления счётчика 2026-09-06: `go test -race -p 1 ./promo/... -count=1`, `go vet ./promo/...`, `go build ./promo/...` и `git diff --check` прошли.
