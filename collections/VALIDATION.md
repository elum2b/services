# Проверка Collections — 4 ноября 2026

Дополнение после повторного аудита: все восемь COL findings исправлены и покрыты
регрессионными тестами. Обновлённые гарантии и before/after замеры находятся в
[AUDIT_FIXES_VALIDATION.md](AUDIT_FIXES_VALIDATION.md). Ниже — исходный baseline,
не описание неисправленных lifecycle/API ограничений.

Актуальное разделение catalog/ownership, проверки и замеры:
[CATALOG_READS_VALIDATION.md](CATALOG_READS_VALIDATION.md).

## Результаты

- PostgreSQL интеграционные тесты: владение/видимость/revisions/replay, atomic batch,
  scopes и разделение User/Admin/Internal, callback delivery, архивы, preflight,
  import 11000 entries, cancel/rollback, Run/Close и две cache-ноды.
- `go test -p 1 ./...` — прошёл для всего репозитория.
- `go test -race -p 1 ./collections/... ./control ./internal/utils/sql -count=1` — прошёл.
- `go vet ./...`, `go build ./...`, `git diff --check` — прошли.
- Финальный повтор `go test -race -p 1 ./collections/... -count=1` после callback validation — прошёл.
- SQLC regenerated с v1.30.0 для Collections/Control. Code generation — без ручной
  правки generated rows/queries. Runtime инструментирования в production не добавлено.
- TestControlCollectionsAccessCatalog проверяет actual Control registration и
  service/group/access localization RU/EN. Existing Control caller changes сохранены.

## SQL round-trips

TestBulkSQLRoundTrips измеряет pgx QueryTracer без BEGIN/COMMIT/ROLLBACK:

| Операция | 1 элемент | 1000 элементов |
| --- | ---: | ---: |
| HasItems | 1 | 1 |
| SetOwnedBatch с изменениями | 8 | 8 |
| Replay batch | 3 | 3 |

SetOwnedBatch: shared catalog lock, owner lock, lookup receipt, bulk states,
3 bulk writes ownership/change/outbox и receipt. No-op пропускает 3 bulk writes.
Foreign receipt FK deferred до commit; запись callback exactly-once проверяется
по количеству строк, доставка остаётся at-least-once.

## Бенчмарки

Дополнительное сравнение на 300ms × 3 с Promo/Calendar: [BENCHMARK_COMPARISON.md](BENCHMARK_COMPARISON.md). На более длинном прогоне batch/1000 дал ≈176ms; приведённый ниже 50ms baseline не является устойчивой оценкой этого тяжёлого пути.

Команда: `go test ./collections -run '^$' -bench . -benchmem -benchtime=50ms -count=3`.
Darwin/arm64, Go 1.27.1 (go.mod 1.26.1), локальная PostgreSQL. Короткий baseline,
не production latency SLO. Каталог 1000 entries. Setup вне таймера; write keys
уникальны между калибровками. Ниже медианы трёх повторов.

Admin.GetStats измерен до роста таблиц от write benchmarks. Cache miss fixture
изменяется вне таймера. shared_owner parallel смешивает change/no-op в зависимости
от порядка блокировок; independent owners создают новые grants.

| Операция | мкс/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| User.HasItem/hit | 382.29 | 5631 | 142 |
| User.HasItem/miss | 391.85 | 5529 | 141 |
| User.HasItems/1 | 367.20 | 5577 | 141 |
| User.HasItems/100 | 645.85 | 163667 | 1710 |
| User.HasItems/1000 | 2735.97 | 1773732 | 15273 |
| User.GetCollection | 3122.05 | 228130 | 2613 |
| User.ListCollections | 1193.40 | 7667 | 156 |
| User.ListOwned | 330.49 | 4256 | 73 |
| Admin.GetStats | 302.64 | 2912 | 48 |
| Internal.SetOwned/change | 2326.79 | 40579 | 820 |
| Internal.SetOwned/noop | 1587.58 | 15548 | 305 |
| Internal.SetOwned/replay | 1309.40 | 9237 | 155 |
| Internal.SetOwned/conflict | 1262.39 | 8671 | 149 |
| Internal.SetOwnedBatch/1 | 2306.39 | 39741 | 820 |
| Internal.SetOwnedBatch/100 | 10582.16 | 2070320 | 8737 |
| Internal.SetOwnedBatch/1000 | 89558.38 | 21296104 | 78152 |
| Admin.ListCatalog/cache_hit_true | 2.44 | 1881 | 30 |
| Admin.ListCatalog/cache_hit_false | 749.43 | 9999 | 190 |
| Admin.Import | 32599.50 | 2981992 | 19906 |
| Admin.Export | 6438.65 | 900451 | 17999 |
| Internal.SetOwnedParallel/shared_owner_false | 717.28 | 39295 | 821 |
| Internal.SetOwnedParallel/shared_owner_true | 1925.21 | 34669 | 734 |
| Admin.CreateCollection | 1055.49 | 6514 | 108 |

## EXPLAIN (ANALYZE, BUFFERS)

TestExplainHotQueries использует actual generated SQL, не переписанный SQL.
Ниже планы на точечной fixture (1 entry/ownership/receipt); они подтверждают
SQL/index compatibility, но не заменяют profiling большого production каталога.

- `hasItems`: execution 0.042 ms; indexes: collections_collection_pkey, collections_entry_order, collections_ownership_pkey.
- `getReceipt`: execution 0.017 ms; indexes: collections_operation_journal.
- `listOwned`: execution 0.034 ms; indexes: collections_owned_list, collections_collection_pkey, collections_entry_pkey.

## Повторная проверка рисков

- Receipt lookup выполняется до состава: архивирование/удаление не ломает replay.
- No-op не меняет revision/events/stats. false без row не создаёт фиктивное владение.
- Hidden raw ownership не даёт права через HasItem/HasItems или user lists.
- Soft-delete/import сохраняют ownership, исправлено отсутствие фильтра ListOwned
  при nil keys (COALESCE cardinality). Порядок bulk results собирается по паре ключей.
- Read aggregates/export используют consistent snapshot; entries страницируются.
- Cache error и panic диагностического callback после commit не дают ложную ошибку.
- Callback uint64 IDs ограничены PostgreSQL BIGINT; status/reason валидируются.
- SQLC static queries и import placeholders исключают интерполяцию user input.
- Close borrowed wrapper останавливает L1 workers, не закрывая caller DB/shared L2.

Ограничения: реальный MinIO/S3, production transport/auth wiring и live consumer
игровых эффектов этим прогоном не проверялись. Для S3 используется уже проверенный
общий adapter. Runtime ownership не включён в config archive. Каталожный cache
может устареть при L2 outage до recovery/TTL; авторизационные HasItem всегда из DB.
Дневная статистика materialized по явному refresh, GetStats — актуальный DB aggregate.
Inventory остаётся на этапе плана.
