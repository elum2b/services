# Аудит tasks — 2026-09-05

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

**Исправлено, средняя:** skip_existing требовал секреты для partner config, которую не меняет. Проверка учитывает strategy/conflicts до записи и повторяется под workspace lock; TestRequireImportSecrets проверяет успешный skip без секрета и сохранение существующего значения.

**Исправлено, средняя:** частичные дни и два независимых commits суточных materializations. Теперь полные UTC-дни, stats/overview в одной транзакции; существующий stats workflow проверяет первый refresh с from == until.

**Исправлено:** panic диагностического callback cache invalidation больше не превращает committed mutation в ошибку API.

Повторная проверка: plaintext job secrets из старого отчёта уже шифровались до queue. Шифрование сохранено; round-trip тест проверяет отсутствие secrets/options в публичном JSON. ID-карта mixed skip complex conditions уже включала skipped task до continue, поэтому старый пункт не воспроизводится по текущему коду. Runtime progress/claimed reward snapshots не импортируются и не удаляются. Существующие тесты concurrent Record, delta/snapshot, partner leases и двух cache-нод сохранены. TestTasksDefaultImportMergesCatalog проверяет union групп.

## Повторная оценка старого AUDIT.md

Plaintext и mixed conditions — уже закрытые исторические пункты. Required secrets исправлены сейчас. ZIP/lease/shared storage/recovery покрыты общими механизмами и обновлёнными callers. Preview без strategy по-прежнему описывает секреты полного пакета; фактический Import фильтрует применяемые configs.

## Открытая граница формата

Пользователю задан общий вопрос: нужен ли перенос пользовательской истории/прогресса в архиве или остаётся каталог. До ответа существующий формат не расширяется; импорт в пустую БД не восстанавливает runtime history.
