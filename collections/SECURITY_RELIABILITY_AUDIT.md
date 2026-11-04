# Collections: повторный аудит безопасности и надёжности

Дата: 4 ноября 2026. Статус: все восемь подтверждённых дефектов исправлены.
Ниже сохранено историческое описание состояния до исправлений; прежние номера
строк относятся к тому снимку. Текущие гарантии и проверки перечислены в разделе
«Исправления». Отрицательные probes сохранены как история, обычные regression
тесты проверяют уже правильное поведение.

Проверены User/Admin/Internal, repository/SQLC, locks/revisions/receipt, cache,
import/export, callback и lifecycle. Внешней transport validation доверяем;
нет гипотез об обходе API через невалидный HTTP input. Все DB-пробы выполнялись
только в уникальных collections_test_* базах со штатным cleanup.

## Результат

| ID | Приоритет | Подтверждённый недостаток |
| --- | --- | --- |
| COL-01 | P1 | Незавершённый Run оживляет сервис после Close |
| COL-02 | P2 | Close в embedding не ждёт активный callback handler |
| COL-03 | P2 | NewWithDatabase игнорирует ctx при ожидании соединения |
| COL-04 | P2 | User-read одной локали загружает все локализации |
| COL-05 | P2 | Reorder несовместим с крупным каталогом и выполняет N+1 writes |
| COL-06 | P2 | Import меняет каталог без ActorID и catalog audit |
| COL-07 | P2 | Custom Archive нельзя реализовать из внешнего Go module |
| COL-08 | P3 | GetCallbackEvent отдаёт raw sql.ErrNoRows вместо typed not-found |

P1 — приоритет исправления lifecycle, не утверждение удалённого захвата сервиса.
P2 — воспроизводимая ошибка надёжности/API/логики; P3 — нарушение контракта ошибок.
Обхода workspace isolation, User→Internal escalation, SQL injection или повторной
записи ownership/outbox в проверенных путях не подтверждено.

## Исправления

| ID | Изменение | Регрессионная проверка |
| --- | --- | --- |
| COL-01 | Терминальный closed state, отмена и ожидание startup, неизменный root lifetime, общий итог concurrent Close | TestRegressionCloseDuringStartup, TestRegressionLifecycleConcurrentClose |
| COL-02 | Direct callback runs регистрируются до unlock и входят в shutdown wait | TestRegressionCloseWaitsEmbeddingCallback |
| COL-03 | NewWithDatabase/open используют sqlwrap.NewWithContext; legacy New сохраняет контракт | TestRegressionConstructorDeadline, общий SQL test suite |
| COL-04 | Отдельный SQLC ListUserLocalizations с locale predicate | TestRegressionUserLocaleRead (100 locales → 1 row) |
| COL-05 | Count + bulk UPDATE FROM unnest WITH ORDINALITY, согласованный предел catalog/reorder 100000 | TestRegressionBulkReorder: 20/1001 entries → 6 SQL calls (после добавления persistent catalog version), rollback неправильного состава |
| COL-06 | Обязательный ActorID в sync/async import, hash/job/result audit в той же транзакции | TestRegressionImportAttribution, TestRegressionImportAuditRollback, TestArchiveJobsRoundTrip |
| COL-07 | Public Archive/Object/Info/Lister, private двусторонний adapter с сохранением listing | TestRegressionPublicArchive и внешний compiler consumer |
| COL-08 | Shared store absence нормализуется в ErrNotFound через repository.DomainError | TestRegressionCallbackNotFound |

User/Admin/Internal остались раздельными. Internal source=admin по-прежнему запрещён.
Новая запись большого каталога через AddEntry проверяет тот же предел, что import/
reorder. Коррекция не меняет исторические receipts. Архивы всё ещё каталожные.

**Изменение контракта:** ImportRequest.ActorID теперь обязателен для применения
импорта и queue import. Host получает actor из своего auth context. Preview остаётся
чтением. Старые callers, не передающие ActorID, получают typed invalid argument.
Run после Close возвращает ErrServiceClosed; Run поверх NewWithDatabase не допускается.

Кэш закрывается по согласованному владению: у каждого Run отдельный L2 client.
Borrowed embedding DB/L2 не закрываются библиотекой. Структурно удалены фиктивный
NewPreparedWithOptions/fallback и неиспользуемый selectText; методы фасадов разделены
на catalog, ownership, journal, stats и reads.

Проверки и новые замеры: [AUDIT_FIXES_VALIDATION.md](AUDIT_FIXES_VALIDATION.md).

## Историческое описание до исправлений

## COL-01 — Run после завершившегося Close снова делает сервис рабочим

**Код:** collections.go:71–106, 290–298, 336–374.

Run устанавливает running, затем вызывает open без lifecycle lock. Close отменяет
старый root context и закрывает старые ресурсы, не останавливая startup и не
устанавливая терминальный closed state. После завершения open метод adopt
подменяет rootCtx/rootCancel, фасады, client и goroutine manager новыми живыми
объектами. Закрытие shell не связано с ctx, переданным open.

**Воспроизведение:** TestAuditProbeCloseDuringRun удерживает ACCESS EXCLUSIVE lock
на importexport_job, ждёт блокировки Run в bootstrap, вызывает и завершает Close,
снимает DB lock. После этого shell.IsReady() становится true. Это действительный
публичный путь Run/Close, а не прямой вызов private adopt из теста.

**Последствие:** host считает сервис остановленным, но новый root/worker может
продолжить работу; возможны потеря владения ресурсами и нарушение shutdown.
Если host отдельно отменяет именно Run ctx, этот конкретный путь не оживляет
активный root. Но Close сам по себе этого не гарантирует.

**Повторная проверка:** running защищает только от второго Run; lifecycleMu в
adopt не помогает, потому что Close не синхронизирует завершение startup.
Проба запускалась и с -race: логическое нарушение осталось, Go race detector
в этой детерминированной последовательности data race не зарегистрировал.
Не выдаём эту проверку за доказательство отсутствия всех data races Run/Close.

**Исправление:** явные starting/running/closing/closed состояния; startup context
должен отменяться Close. Публикация результатов open и решение об их закрытии —
под одним lifecycle protocol. Close ждёт startup и не допускает поздний adopt.
Не удерживать mutex на сетевом/DB ожидании.

## COL-02 — embedding callbacks не входят в ожидание Close

**Код:** callback.go:71–74, 87–113; collections.go:336–374.

Для NewWithDatabase OnCallback непосредственно вызывает runCallback. Рабочие
handler goroutines принадлежат локальному manager внутри callback.Store.On,
а не p.goroutines. Close отменяет root, но ждёт только p.goroutines и архивные
jobs. Возврат Store.On с ожиданием handlers происходит в чужой вызывающей goroutine.

**Воспроизведение:** TestAuditProbeEmbeddedCloseDoesNotJoinCallback запускает handler,
который получает отмену и ещё выполняет cleanup. Close успевает вернуть nil, пока
handler продолжает работу. После разрешения cleanup handler и OnCallback завершаются.

**Последствие:** приложение может закрыть внешнюю БД/другие зависимости после Close,
пока callback ещё их использует. Exactly-once внешнего эффекта не обещается;
это ошибка graceful shutdown, не доказательство двойной награды.

**Контрдоказательство:** pre-registered callback при обычном Run обёрнут в
p.goroutines.Go; там ожидание работает. Проблема относится к embedding/direct
OnCallback и не распространяется автоматически на все modes.

**Исправление:** регистрировать все активные callback runs в lifecycle wait group
под синхронизацией с Close; сначала отмена, затем join, затем закрытие зависимостей.
Обязательны отдельные тесты embedding и Run.

## COL-03 — deadline конструктора не ограничивает ожидание pool connection

**Код:** collections.go:48–65; internal/utils/sql/client.go:59–66.

NewWithDatabase(ctx,...) вызывает sqlwrap.New, который делает PingContext с
context.Background(). Переданный ctx начинает влиять лишь на последующий archive
bootstrap. При исчерпании pool constructor продолжает ждать после deadline caller.

**Воспроизведение:** TestAuditProbeConstructorIgnoresDeadline занимает единственное
соединение, вызывает NewWithDatabase с MaxConnections=1 и ctx=50ms. Через ещё
100ms после отмены constructor всё ещё не завершён. Возврат происходит только
после освобождения соединения.

**Защиты:** операции repository ограничены timeout; openPostgres в Run использует
ctx. Они не ограничивают этот PingContext. Дефект в общей utility, Collections
наследует его, поэтому исправление потребует осторожной совместимости других сервисов.

**Исправление:** context-aware создание wrapper/ping; не выполнять background ping
в публичном конструкторе, принимающем ctx. Проверять ctx и на acquisition pool.

## COL-04 — User возвращает одну локаль, читая все

**Код:** repository/read.go:130–157, 190–207; sqlc/query.sql:11–12.

ListCollections/GetCollection вызывают ListLocalizations по collection keys без
locale predicate. Все title/description сканируются в Go и лишь затем фильтруются.
Число коллекций ограничено Page, но размер вложенного материала не ограничен
потребностью ответа. validateCollection разрешает 100 локалей, до 64KiB description
в каждой. Public list допускает до 1000 коллекций.

**Воспроизведение:** TestAuditProbeUserReadLoadsUnselectedLocales создаёт допустимые
100 локализаций одной коллекции. pgx trace фиксирует 100 SELECT rows для запроса
Locale=l00, в результате только одна. Прочитано около 6,25MiB описаний ради 64KiB.
На больших валидных каталогах этот расход умножается на размер страницы.

**Риск:** излишние DB/network/heap/GC расходы на обычных User requests; при тяжёлом
каталоге — деградация доступности. Для multi-GB оценки нужны соответствующие
stored data и отдельный stress test; такой объём реально не загружался аудитом.
Это не claim утечки других локализаций в результат: лишние rows отбрасываются.

**Защиты:** длины одной локализации и Page ограничены, QueryTimeout ограничивает
ожидание. Они не устраняют 100-кратную амплификацию и уже произведённые allocations.

**Исправление:** отдельный SQLC user query с locale predicate и bulk keys. Admin
all-locales read сохранить отдельным методом с явным бюджетом возвращаемых bytes.

## COL-05 — ReorderEntries: несогласованные лимиты и последовательные writes

**Код:** repository/catalog.go:197–240; validateCollection:26–27;
sqlc/query.sql:13–14, ReorderEntry.

Каталог и импорт принимают более 1000 entries, но ReorderEntries требует весь
состав и одновременно запрещает более model.MaxBatch=1000 keys. Большую коллекцию
этим методом атомарно переставить невозможно. Для допустимых коллекций выполняется
отдельный UPDATE на каждый entry под exclusive catalog lock всей workspace.

**Воспроизведения:**
- TestAuditProbeLargeCollectionCannotReorder: import 1001 entries успешен,
  полный reorder → ErrInvalidArgument, список 1000 → ErrConflict.
- TestAuditProbeReorderSQLRoundTrips: 20 entries → 24 SQL-вызова без BEGIN/COMMIT.
  Формула текущего пути — N+4; ownership bulk path этой проблемы не имеет.

**Последствие:** валидные большие коллекции не поддерживаются методом; сетевые
round-trips удлиняют глобальную exclusive lock, блокируя SetOwned и другие catalog
writes этой workspace. На удалённой БД это повышает риск QueryTimeout/очередей.
Таймауты большого reorder на production latency в этом проходе не измерялись.

**Повторная проверка:** AddEntry может менять position по одному — это обходной путь,
но не атомарный reorder состава. Предел 1000 описан в текущем METHODS, поэтому
это также несогласованность возможностей API, а не скрытый bypass проверки.
ListEntries до проверки длины читает весь состав, включая крупный каталог.

**Исправление:** определить согласованный предел reorder независимо от ownership
MaxBatch; использовать bulk UPDATE FROM unnest и server-side membership/cardinality
checks, сохранив атомарность и общий lock contract.

## COL-06 — импорт обходит аудит изменений каталога

**Код:** repository/archive.go:99–181; service/admin/archive_jobs.go:290–308;
repository/catalog.go:83–117; METHODS.md:59.

CRUD идёт через catalogWrite с ActorID и CatalogAudit. Sync Import и archive handler
напрямую вызывают ImportJob; ImportParams/queue import не содержат actor и записи
collections_catalog_audit не создаются. Import receipt — техническая дедупликация
job, не operator audit.

**Воспроизведение:** TestAuditProbeImportBypassesCatalogAudit создаёт коллекцию и
entry: audit=2. Import меняет active true→false и убирает состав, audit остаётся 2.
Запрос успешен, вызов не требует ActorID.

**Защиты:** Control может отдельно разрешать import action, transaction сохраняет
атомарность. Это не обход authentication/RBAC, и приложение может вести свой
внешний аудит. Однако обещанный сервисный audit всех catalog writes отсутствует.

**Исправление:** actor и attribution metadata должны сохраняться для sync/queue
import и записываться вместе с domain import/receipt. Достаточно разумного summary,
package hash и job ID; не обязательно дублировать весь большой ZIP в audit.

## COL-07 — внешний consumer не может реализовать Custom Archive

**Код:** config.go:40–42; service/admin/admin.go:94–111;
internal/utils/importexport/jobs/jobs.go, Archive.Store/ArchiveObject.

Options.Archive экспортирует internal jobs.Archive. Его Store принимает именованный
jobs.ArchiveObject. У Collections нет публичного alias/DTO для этого аргумента.
Внешний module не может импортировать internal package, чтобы объявить метод.

**Проверка:** baseline external module с local replace импортирует Collections и
компилируется. При реализации CustomArchive с требуемым Store компилятор сообщает:
`use of internal package github.com/elum2b/services/internal/utils/importexport/jobs not allowed`.

**Контрдоказательства:** ArchiveS3Options публично aliased и встроенный S3/диск
используются без этой ошибки. DTO alias в других местах сам по себе не проблема:
здесь именно отсутствует имя для обязательного метода custom implementation.
Такой же старый паттерн в других сервисах не закрывает дефект нового API.

**Исправление:** публичные Archive/ArchiveObject/ArchiveInfo контракты и приватный
adapter к jobs. Следовать правилу корневого AGENT.md:53–54; проверить отдельным
consumer module, не тестом внутри services, которому internal доступен.

## COL-08 — ошибка отсутствующего callback не является domain error

**Код:** service/admin/callbacks.go:50–57.

GetCallbackEvent возвращает ошибку callback.Store.GetEvent без нормализации.
Для отсутствующего ID это sql.ErrNoRows, а не Collections.ErrNotFound.

**Проверка:** TestAuditProbeCallbackMissingRawError — валидные workspace/ID,
errors.Is(err, sql.ErrNoRows)=true, errors.Is(err, ErrNotFound)=false.

**Риск:** caller не может единообразно обработать not-found; transport с fallback
INTERNAL_ERROR может вернуть 500. Фактический HTTP 500 не проверялся: Collections
transport ещё не подключён. Workspace validation и isolation работают.

**Исправление:** нормализовать ожидаемое отсутствие на repository/utility boundary,
не превращая сетевые/DB ошибки в not-found. Добавить valid/missing/cross-scope tests.

## Кэш: уточнение пользователя, не дополнительная находка

Пользователь подтвердил стратегию: общий L2 backend, но отдельный клиент/соединение
для каждого сервиса. При таком контракте Run вправе закрыть собственный клиент;
один Storage-объект нельзя разделять между independently owned сервисами.

TestAuditProbeRunClosesSuppliedCache подтвердил вызов Cache.Close при shutdown Run.
Это ожидаемо для отдельного owned connection, поэтому из списка дефектов исключено.
Текущий platform wiring внешний Options.Cache не передаёт. Остальные сервисы
используют такой же sqlwrap.Client.Close, не отдельный процесс-wide shared cache.
Embedding NewWithDatabase отдельно документирует caller-owned DB/L2 cleanup.
При внедрении L2 важно не реализовать Storage.Close как глобальное закрытие всех
клиентов backend. Параметров подключения к Redis этот проход не добавляет.

## Не подтвердилось / согласованные особенности

- User не имеет SetOwned/Batch/CorrectOwned/Import. Internal source=admin запрещён;
  операторская коррекция имеет отдельный actor/reason и fingerprint.
- Полная workspace/app/platform/user isolation соблюдается; Subject разрешён только
  в той же workspace/app. Внешнее приложение разрешает account→subject.
- Shared catalog + owner lock закрывают check/write race состава; duplicate receipt
  защищён PK, revision проверяется до записи. Batch не пишет partial state.
- Bulk states собираются по ключам, не по произвольному порядку JOIN rows.
- Replay после скрытия возвращает immutable receipt; новое true требует состава,
  late false сохраняет ревизии и историю по согласованным правилам.
- Revision и changed_at скрытого элемента для чужого пользователя **разрешены
  пользователем в этом аудите**. Это не privacy finding.
- HasItem не использует stale cache. Межнодовая stale metadata при L2 outage уже
  документирована, не выдаётся за обход проверки прав.
- ZIP bounds/CRC/shape, import preflight, consistent snapshot, async receipt,
  heartbeat/fencing/retry в общих utilities существуют. Не повторяем старые
  findings об их отсутствии. Реальный MinIO/production не проверялся.
- Экспортный footprint включает soft-deleted rows: это известный консервативный
  бюджет, который может ограничить экспорт после накопления истории состава.
  Retention/компактация требует отдельного проекта без потери ownership links.

## Структурные замечания меньшего приоритета

- NewPreparedWithOptions фактически ничего не prepares и всегда возвращает nil
  error. Admin constructor содержит недостижимый fallback; selectText не используется.
  User/Internal.Close — no-op без собственного lifecycle. Стоит убрать мёртвые
  обещания и wrappers, сохраняя единый root ownership.
- Schema callback table дублирует schema/BootstrapTable общей utility. При изменении
  общей таблицы два источника DDL могут разойтись; требуется один источник/проверка parity.
- Большие Go files и плотные группы методов в service/*/methods.go затрудняют
  per-method review. Разбить по тесным CRUD/read/write группам при исправлениях,
  не смешивая это с изменением бизнес-семантики.

## Проверки и воспроизведения

```sh
go test -race -p 1 ./collections/... -count=1
python3 collections/audit/run_probes.py
python3 collections/audit/check_archive_api.py
python3 collections/audit/run_probes.py -race -run '^TestAuditProbeCloseDuringRun$' -count=1 -v
```

До исправления обычные tests/race прошли. Все 9 DB probes подтвердили описанные поведения
(включая ожидаемый Close отдельного Cache); custom Archive consumer compile error
подтверждён отдельно. Passing defect-presence probes — не признак исправления.
Исторические probes находятся в audit/defect-probes-before-fixes.go.txt. Текущий
runner запускает положительные TestRegression* из обычного root test-файла; они
входят в CI. External archive runner теперь требует успешной компиляции.

Сначала исправлять lifecycle/cancellation, затем read amplification и reorder,
далее attribution import/public archive contract/typed callback errors. Каждый
фикс закреплять обычным regression test с правильным ожидаемым поведением,
потом повторить race и релевантные benchmarks.
