# Проверка исправлений аудита Collections

4 ноября 2026. Все восемь COL findings закрыты положительными регрессионными тестами.
Исторические симптомы и приоритеты: [SECURITY_RELIABILITY_AUDIT.md](SECURITY_RELIABILITY_AUDIT.md).

## Выполненные проверки

- `go test -p 1 ./...` — прошёл для всего репозитория после изменений shared SQL constructor.
- `go test -race -p 1 ./collections/... ./internal/utils/sql -count=1` — прошёл.
- Дополнительный `go test -race ./collections -run '^TestRegression' -count=1` — прошёл,
  включая rollback при отказе catalog audit и concurrent Close восьми callers.
- `python3 collections/audit/check_archive_api.py` — внешний consumer успешно
  реализует Archive и ArchiveLister без internal imports.
- `go vet ./...`, `go build ./...`, `git diff --check` — прошли; SQLC regenerated.

Изолированные PostgreSQL fixtures удалены штатным cleanup. Рабочие БД не изменялись.
Callback, inventory-independent ownership, scope/replay/revision и import/export
регрессии остались в общем test suite. Роли User/Admin/Internal не объединялись.

## Регрессии исправленных контрактов

- Close отменяет startup ещё при ожидании DDL lock и ждёт его завершения; shell
  не оживает. Повторный Run после Close получает ErrServiceClosed.
- Direct embedding callback получает отмену и завершает cleanup до возврата Close.
- NewWithDatabase завершает ожидание занятого pool по deadline caller, не дожидаясь
  освобождения соединения. Legacy sqlwrap.New остаётся совместимым wrapper.
- User locale read возвращает одну строку из ста, не загружая прочие описания.
- Reorder проверяет весь состав и делает 5 SQL statements для 20 и 1001 entries,
  без BEGIN/COMMIT. Ошибка хотя бы одного key откатывает всю перестановку.
- Sync import требует actor; queue options сохраняют actor до worker. Catalog audit
  содержит actor/hash/result/job ID и коммитится с конфигурацией. Trigger failure
  audit вставки откатывает изменения active/состава. Async round-trip проверяет actor.
- Public custom archive сохраняет object metadata и optional listing через adapter;
  DB queue → Store → Open/download проходят через публичную реализацию.
- Missing callback возвращает ErrNotFound; workspace validation остаётся прежней.

## Before/after: чтение одной из 100 локалей

Apple M4, Darwin/arm64, Go 1.27.1, локальная PostgreSQL. Одна опубликованная коллекция,
100 валидных локалей с description 64KiB, результат — одна локаль. Выполнено три
чередующихся before/after пары, `-benchtime=200ms -benchmem -count=1` на пару.
До — overlay только двух user query calls на прежний ListLocalizations без locale
predicate; остальной исправленный код и fixtures одинаковы. Это контролируемое
сравнение политики SQL-read, а не полный checkout прежней версии.

| Метод | Версия | мс/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| ListCollections | before | 16.426 | 6690672 | 1277 |
| ListCollections | after | 2.256 | 76044 | 170 |
| GetCollection | before | 16.955 | 6689099 | 1318 |
| GetCollection | after | 2.636 | 83010 | 206 |

Это примерно 80–88× меньше выделенных Go bytes на данном тяжёлом locale fixture.
B/op — суммарные выделения, не peak RSS. Точечные small-catalog timings из старого
baseline несопоставимы с этой fixture. p95/p99 и production SLO не измерялись.

## Bulk reorder после исправления

`go test ./collections -run '^$' -bench '^BenchmarkCollectionsReorder$' -benchmem -benchtime=200ms -count=3`.
Setup вне таймера, отдельная fixture на калибровку.

| Entries | мс/op, медиана | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| 20 | 1.961 | 16036 | 291 |
| 1001 | 5.950 | 424078 | 5253 |

До исправления измерено 24 SQL calls для 20 entries; после — 5 и для 20, и для 1001.
Исходный метод вообще не принимал 1001 entry, поэтому выдуманного latency speedup
для него нет. Лимит catalog/reorder теперь согласован на 100000; batch владения
по-прежнему ограничен 1000, это отдельный доменный лимит.

## Ограничения

Реальный S3/MinIO и deployment не проверялись; встроенные archive adapters используют
прежние общие utilities. External compile и memory archive round-trip подтверждают
новый публичный контракт и сохранение listing, но не заменяют live storage test.
Другие сервисы не мигрировались с legacy sqlwrap.New на NewWithContext автоматически.
Root facade references публикуются при запуске: host должен дождаться IsReady перед
доступом к ним, как и при существующем lifecycle API.

Последующее разделение catalog/ownership добавило один transactional SQL version bump:
Reorder теперь использует 6 statements. Исторические замеры выше относятся к этапу
исправлений до разделения. Актуальные чтения: [CATALOG_READS_VALIDATION](CATALOG_READS_VALIDATION.md).
