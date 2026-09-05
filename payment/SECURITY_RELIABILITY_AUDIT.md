# Аудит payment — 2026-09-05

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

**Исправлено, средняя:** частичный диапазон затирал суточные агрегаты; stats/overview выполнялись независимо. Теперь пересчёт полных UTC-дней в одной транзакции. Полный payment stats workflow проверяет первый refresh с from == until.

Повторная проверка: order/attempt/fulfillment/refund используют сохранённые snapshots и transactional transitions; import меняет каталог, а не исторические заказы. TestPaymentDefaultImportMergesCatalog проверяет union групп; существующие round-trip, provider duplicate, refund/subscription, limits и snapshot tests сохранены.

## Повторная оценка старого AUDIT.md

1–3,7–8. ZIP, S3, lease/retry/receipt/orphan recovery закрыты общими механизмами и текущими callers.
4. Уже есть dependency preflight assets/rates/Reference через настроенный checker, и проверки отсутствующих зависимостей/skip.
5. Старое массовое удаление shared localization уже отсутствует; import обновляет переданные ключи. Это важно: localization keys могут использовать другие продукты.
6. Уже есть normalizeExportPackage/canonicalIdentifier и тест отклонения whitespace. Повторно менять canonical contract не потребовалось.

Reference checker нужно конфигурировать в composition root: архив не содержит внешний item catalog. Реальные payment providers и blockchain в этой проверке не вызываются.

## Открытая граница формата

Пользователю задан общий вопрос: нужен ли перенос пользовательской истории/прогресса в архиве или остаётся каталог. До ответа существующий формат не расширяется; импорт в пустую БД не восстанавливает runtime history.
