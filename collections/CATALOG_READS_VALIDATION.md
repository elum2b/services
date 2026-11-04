# Раздельное чтение каталога и владения — 5 ноября 2026

## Контракт

`colors` — ключ коллекции (категория). `User.GetCollection` возвращает общий
опубликованный состав без owned/revision/changed_at. `User.ListOwned` с
CollectionKeys=["colors"] возвращает доступные открытые элементы caller/Subject.
Оба метода пагинированы: default 100, max 1000. Item metadata остаются в Reference.

GetCollection возвращает Version и принимает KnownVersion. Совпадение даёт
NotModified=true без Entries; клиент сохраняет ранее полученную страницу.
Version привязана к workspace/collection/locale/нормализованной странице.
Caller/Subject и ownership не меняют версию. Страницы имеют отдельные tokens;
несколько вызовов не являются одним snapshot.

`Options.CacheEnabled` включает серверный L1/L2. `CatalogCacheTTL` по умолчанию
24h. Каждое чтение проверяет публикацию и durable revision в PostgreSQL. Версия
повышается атомарно с CRUD/import/audit, поэтому outage кэша или ошибка postcommit
invalidation не оставляют старый public catalog под актуальным ключом. Версия и
тело на cache miss читаются одним REPEATABLE READ READ ONLY snapshot.
Счётчик общий для workspace: изменение другой коллекции тоже меняет токен.

## Проверено

- Каталог одинаков для разных пользователей и не содержит полей ownership.
- Отдельные L1 и общий L2: cold/hit, L2 hit, default 24h TTL; ответ не позволяет
  испортить shared slice через изменение Entries.
- Выдача/отзыв не меняют catalog Version; ListOwned и HasItem сразу видят изменение.
  Полная Identity и разрешённый Subject учитываются только в пользовательском чтении.
- KnownVersion не обходит валидацию scope, publication или ErrNotFound. Token
  другой коллекции/локали/страницы/workspace не даёт NotModified.
- Add/hide/remove/re-add/reorder/update/import видны другой ноде даже при отказе L2.
  Archive/inactive скрывают каталог, unarchive сохраняет прежнее владение.
- Неуспешный audit откатывает данные и version bump. Multi-collection import
  повышает revision один раз; skip-only/failed import не повышает её.
- Новый instance и повторный Bootstrap сохраняют durable version.
- Управляемая гонка read/remove: тело и версия из одного snapshot, следующий
  запрос не возвращает удалённый элемент из старого cache key.
- SQL trace: hit/NotModified — 1 SQL, cold — 5 SQL; ListOwned — 1 SQL.
  Счёт включает SET TRANSACTION, исключает BEGIN/COMMIT/ROLLBACK.
- EXPLAIN (ANALYZE, BUFFERS): publication/version, ordered catalog page и ListOwned,
  включая 1000 entries и 1000 owned rows до/после ANALYZE.

Успешно выполнены:

```sh
sqlc generate -f collections/sqlc.yaml
go test -p 1 ./...
go test -race -p 1 ./collections/... ./internal/utils/sql/...
go vet ./...
go build ./...
git diff --check
```

SQLC v1.30.0. Проверены форматирование Go и whitespace новых файлов, которые ещё
не входят в обычный git diff. Тесты используют только уникальные временные БД.

## Бенчмарки

Apple M4, darwin/arm64, Go 1.27.1, локальная PostgreSQL. Одна коллекция, 1000
доступных элементов и 1000 owned=true. Таймер исключает setup/import/grant/ANALYZE.
Медианы трёх запусков, `-benchtime=300ms -benchmem -count=3`:

| Ветка | мс/op | B/op | allocs/op | SQL/op |
| --- | ---: | ---: | ---: | ---: |
| GetCollection, L1 hit, 1000 entries | 0.215 | 28385 | 65 | 1 |
| GetCollection, KnownVersion совпала | 0.218 | 3264 | 58 | 1 |
| GetCollection, cache miss | 2.256 | 409543 | 8930 | 5 |
| ListOwned, 1000, без статистики | 38.705 | 775619 | 11116 | 1 |
| ListOwned, 1000, после ANALYZE | 3.102 | 775432 | 11115 | 1 |

```sh
go test ./collections -run '^$' -bench '^BenchmarkCollectionsPublishedCatalog$' -benchmem -benchtime=300ms -count=3
```

Cache miss создаётся новым locale variant с пустой локализацией; состав одинаков.
Для воспроизводимого сравнения статистики autovacuum отключается только у трёх
таблиц внутри временной benchmark-БД. Затем ANALYZE выполняется явно. Production
настройки этим кодом не меняются. B/op — суммарные Go allocations, не peak RSS.
Это локальные microbenchmarks; p95/p99 и production load не измерялись.
Прежние цифры combined GetCollection относятся к другому контракту и не являются
прямым before/after сравнением этих веток.

## Наблюдение о плане ListOwned

Повторно подтверждённая причина медленного сценария — отсутствие статистики после
массовой загрузки в пустую БД. EXPLAIN до ANALYZE оценивал 1000 строк как одну,
выбирал nested loop и отбрасывал 499500 пар join filter. После ANALYZE использован
hash join; SQL execution на той же fixture уменьшился с 80.603 до 0.783 ms.
Это время server EXPLAIN из отдельного запуска, не end-to-end benchmark выше.

ListOwned SQL в этом изменении не переписывался. После крупной загрузки нужна
актуальная статистика PostgreSQL; ожидание фонового autoanalyze может временно
давать медленные чтения. Каталог с cache hit/NotModified этого join не выполняет.
