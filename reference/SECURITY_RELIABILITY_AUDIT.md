# Reference: аудит безопасности и надёжности

Дата: 2026-09-05. Проверена текущая рабочая копия, включая уже внесённые исправления общих utilities после аудита CPA. Ниже сохранён исходный аудит; текущий статус исправлений приведён отдельным разделом.

Область: User/Admin/Resource, PostgreSQL, cache, media storage, GC, асинхронные ZIP import/export и общий jobs manager. Transport/Fiber и его проверкам входных параметров доверяем; отсутствие повторной проверки обычного API input само по себе не считается уязвимостью. Данные внутри архива, состояние БД, конкурентные записи и ошибки инфраструктуры рассматриваются отдельно.

Каждая находка прошла повторную проверку по вызывающему коду, SQL, транзакциям и существующим тестам. P1 — риск потери данных; P2 — существенная функциональная ошибка или отказ в определённой конфигурации. Это приоритет исправления, не CVSS.

## Уточнённый контракт пользователя — 2026-09-05

- После физического удаления media допустима выдача ранее закэшированных bytes до истечения TTL. Немедленная инвалидация не требуется.
- Основной сценарий импорта — пустая целевая база. Если данные уже есть, приоритет имеет импортируемый пакет: соответствующие данные перезаписываются, включая type item без отдельного confirmation token. Это не означает удаление всего содержимого workspace, отсутствующего в пакете.
- Тестовые пароли и секреты не используются в production. Это подтверждение пользователя, а не результат проверки production-конфигурации.
- Эти ответы закрывают все три вопроса аудита и применены при последующих исправлениях.

## Исправления — 2026-09-05

Девять основных находок исправлены. Исходные описания ниже сохранены как история воспроизведения, а не как описание текущего кода.

| Находка | Исправление | Регрессионная проверка |
|---|---|---|
| REF-01 | GC удаляет links только действительно удаляемого resource; purge согласован workspace lock | `TestReferenceArchiveLinksAndActiveGCRegression` |
| REF-02 | При ошибке durability check compensation сохраняет media; проверка ждёт workspace lock предшествующей transaction; обычные Create/Update также проверяют durability, cleanup использует независимый context | `TestCleanupPreservesMediaWhenDurabilityUnknown`, `TestReferenceDurabilityWaitsForPendingCommit`; существующий commit-failure/rollback test |
| REF-03/04 | Пустые links очищаются по keys импортируемых items; skipped items сохраняют links | `TestReferenceArchiveLinksAndActiveGCRegression` |
| REF-05 | Весь manifest читается в одной read-only repeatable-read transaction | `TestReferenceArchiveManifestConcurrentMutation`; существующий ZIP round trip |
| REF-06 | Transaction context передаётся всем SQL/upload closures; archive budget отделён | `TestReferenceTransactionTimeoutAndShutdownRegression` |
| REF-07 | Queue/status/history/download и Resource methods объединяют root/request context | `TestReferenceTransactionTimeoutAndShutdownRegression`, `TestReferenceGCKeepsCachedMediaUntilTTL` |
| REF-08 | S3 upload использует seekable bounded spool; добавлен ArchiveLister для orphan archive sweep | `TestReferenceS3ArchiveUsesSeekableUpload` с настоящим SDK/HTTP |
| REF-09 | Invalidation error диагностируется после commit без ложной API error | `TestReferenceCommittedWriteSurvivesCacheFailure` |

Согласованный импорт по умолчанию проверяется `TestReferenceImportDefaultMergesCatalog`: A/B/C + B/C/D даёт A/B/C/D, A не меняется, B/C обновляются, D добавляется; type и пустые localization импортируемого B заменяются. Явный fail_on_conflict остаётся отдельной опцией. TTL grace после GC закреплён `TestReferenceGCKeepsCachedMediaUntilTTL`.

Дополнительно: safe ArchiveJob DTO, согласованные ZIP budgets, disk spool вместо RAM ZIP, проверка JSON shape, отрицательных/повторных links и DB string boundaries, receipt reconciliation без source blob. Статические SQL затронутого media import переведены в SQLC; неиспользуемый ImportArchiveResources удалён. Resource existence проверяется одним bulk read. Старые root test-файлы объединены в reference_test.go без удаления их проверок.

Оставшиеся ограничения исходного отчёта: полноценный mark-and-sweep media объектов без DB bookkeeping после process crash ещё не реализован; unknown commit теперь сохраняет bytes, но не обещает автоматическое удаление orphan media. При недоступном distributed cache другая нода может держать старые данные до восстановления invalidation/TTL. Media import всё ещё выполняет записи resources поштучно внутри ограниченной transaction; shared storage не участвует в PostgreSQL transaction. Это не закрывается тестами штатного rollback. Реальный MinIO и потеря сетевого ответа COMMIT не проверялись.

Финальные проверки исправлений: полный `go test ./... -count=1` прошёл (Reference 20.173 s); затем после усиления durability barrier прошёл финальный `go test -race ./reference/... -count=1` (13.985 s). Race-прогон общих archive jobs также прошёл. `go vet ./...`, `go build ./...`, генерация всех восьми SQLC-конфигураций, `gofmt` и `git diff --check` прошли.

## Подтверждённые находки

| ID | Приоритет | Проблема | Подтверждение |
|---|---|---|---|
| REF-01 | P1 | GC старой версии удаляет связи активного ресурса с items | PostgreSQL, вызов настоящего CollectGarbage |
| REF-02 | P1 | Компенсация удаляет media при неизвестном результате проверки durability | Fault injection в cleanup; полный lost-COMMIT сценарий не моделировался |
| REF-03 | P2 | update_existing с пустыми links сохраняет старые связи | PostgreSQL, media import repository path |
| REF-04 | P2 | skip_existing меняет links пропущенного item | PostgreSQL, media import repository path |
| REF-05 | P2 | ZIP экспорт объединяет несовместимые снимки каталога и media | Детерминированное чередование реальных стадий экспорта на PostgreSQL |
| REF-06 | P2 | QueryTimeout не отменяет выполняющийся SQL внутри transaction | PG trigger с pg_sleep; 50 мс превращаются примерно в 503–508 мс |
| REF-07 | P2 | QueueArchiveExport принимает задачу после Close | Публичный API, PostgreSQL |
| REF-08 | P2 | S3 archive upload не работает с обычным HTTP MinIO endpoint | Настоящий AWS SDK, локальный HTTP fixture, 0 HTTP-запросов |
| REF-09 | P2 | Ошибка кэша превращает успешный commit в API error | PostgreSQL + cache Set fault injection |

### REF-01. GC удаляет связи действующего ресурса

Места: `sqlc/query.sql:270` (ResourcePurgeDeleted), `repository/resource.go:431`, `service/resource/resource.go:181`.

После Update прежняя media_version становится retired. Через retention CollectGarbage удаляет её storage и bookkeeping, затем вызывает PurgeDeletedResource. CTE `removed_links` удаляет все связи по workspace/resource_key **без проверки deleted_at**. Условие удаления только soft-deleted ресурса находится в следующем DELETE и не ограничивает CTE.

Воспроизведение: активный resource прикреплён к item; выполнен Update на новую version; старая состарена на два часа; CollectGarbage удаляет одну старую version. Текущий resource остаётся active, но `reference_item_resource` становится пустой.

Повторная проверка: обычный Update не удаляет links; существующие GC-тесты проверяют media и resource rows, но не этот эффект. PostgreSQL выполняет изменяющий CTE, даже если основной DELETE затрагивает ноль строк. Более того, PurgeDeletedResource при rows=0 не инвалидирует cache: часть клиентов некоторое время видит уже удалённые связи.

Исправление: удалять links только для реально выбранного к окончательному purge soft-deleted resource без оставшихся versions; selection, блокировка, удаление links и resource должны быть атомарны и согласованы с update/import. Проверить GC retired version при активном ресурсе и конкуренцию с import.

### REF-02. Неопределённость БД считается разрешением удалить media

Места: `service/admin/archive.go:522`, особенно условие `err == nil && durable` на строке 536; `service/resource/resource.go:88`, `:117`, `:508`.

cleanupArchiveVersions пропускает удаление только после успешного ответа БД «version существует». Если проверка завершилась ошибкой, вызывается DeleteVersion. После успешного commit с потерянным ответом или после commit + ошибки cache invalidation это может удалить актуальные media, уже записанные в metadata. Обычные Resource.Create/Update при ошибке commit вообще не проверяют durability перед compensating delete.

Воспроизведение: repository с недоступной БД и доступный fake storage; вызов настоящего cleanupArchiveVersions всё равно вызывает DeleteVersion. Это подтверждает опасную ветку, но не является end-to-end моделированием сетевого обрыва после COMMIT.

Повторная проверка: durable=true действительно защищён; shared import receipt предотвращает повторный DB import, но не возвращает удалённые bytes и не останавливает компенсацию внутри handler. Transaction rollback безопасен только при достоверно известном rollback, а не при любом error. Ошибка самой durability-проверки не доказывает отсутствие записи.

Исправление: unknown result сохраняет media и отправляет операцию на reconciliation; удалять только заведомо незафиксированные версии. Применить тот же контракт к обычным Create/Update, использовать независимый ограниченный cleanup context и диагностировать failure. Проверить lost COMMIT response, недоступную БД при проверке и успешный commit + cache error.

### REF-03. Пустой список links не заменяет старый

Место: `repository/archive.go:440` (аналогичный старый path на строке 185).

При update_existing DELETE выполняется только при `len(links)>0`, причём item keys берутся из самих links. Item, у которого в архиве больше нет ресурсов, не попадает в DELETE. Импорт сообщает об успехе, а прежние связи сохраняются. Это воспроизводится и когда links целиком пусты, и для отдельного item без links внутри непустого архива.

Повторная проверка: importBulk корректно заменяет localizations, но не links; soft-delete/обновление metadata не исправляет связи. Валидатор допускает item с нулём ресурсов — это обычный достижимый архив.

Исправление: для media import определять заменяемые связи по принятым items из manifest, включая items с пустым набором ресурсов. Не менять связи items вне импортируемого пакета.

### REF-04. skip_existing изменяет пропущенные сущности

Место: `repository/archive.go:456` (старый path: 201).

Items и resources могут быть пропущены как существующие, но цикл links выполняется без проверки conflict strategy/набора принятых items. `ON CONFLICT ... DO UPDATE SET position=EXCLUDED.position` меняет связь существующего item. Возможны добавление новых связей и unique conflict по position.

Воспроизведение: существующая связь position=0, импорт skip_existing с той же связью position=5; item пропущен, position становится 5.

Повторная проверка: workspace lock гарантирует последовательность, но не правильную семантику skip. SQL уникальность исключает дубликаты, однако не запрещает обновление пропущенной сущности.

Исправление с учётом уточнения пользователя: основной импорт должен последовательно перезаписывать соответствующие items, resources и links. Сейчас код при отсутствии strategy выбирает fail_on_conflict, что расходится с этим целевым контрактом. REF-04 описывает отдельно существующую явно выбираемую ветку skip_existing: если она сохраняется как опция, она должна сохранять полный агрегат skipped item, а не частично менять links. Уточнение приоритета импорта не делает текущее смешанное поведение согласованным.

### REF-05. ZIP собственного экспорта может не импортироваться из-за разных снимков

Места: `service/admin/archive.go:30`, `repository/export.go:10`, `repository/archive.go:13`.

Export(items/localizations) использует REPEATABLE READ, но завершает transaction до ExportArchiveData. Последний отдельно читает resources, затем links, уже без общего snapshot. Если между стадиями создать item и прикрепить resource, manifest получает link к item, отсутствующему в package. validateArchiveResources при импорте отклоняет такой собственный архив как unknown item.

Воспроизведение: выполнены те же repository-стадии, что у ZIP handler, с допустимой административной записью между ними; links содержит отсутствующий в первой стадии key. Планировщик самого worker не инструментировался.

Повторная проверка: snapshot JSON-каталога действительно существует и защищает items/localizations, поэтому JSON-only export этой конкретной ошибке не подвержен. Он не распространяется на последующие media-запросы. Workspace lock на mutation не блокирует export reads.

Исправление: собрать весь manifest в одной read-only repeatable-read transaction; затем выгружать immutable media versions вне DB transaction. Отдельно защищать экспортируемые версии от GC на время выгрузки.

### REF-06. Внутренний timeout не передаётся запросам transaction

Место: `repository/repository.go:122`, `:150`; вызывающие closures в `repository/admin.go`, `resource.go`, `import.go`, `archive.go`.

sqlwrap.Transaction создаёт ограниченный context, но WithTx/WithTxTimeout вызывают `fn(txRepo)` и теряют этот context. Closures используют исходный ctx для SQL и storage upload. database/sql откатывает transaction после отмены, однако выполняющийся запрос использует другой context и продолжает занимать соединение/locks до своего завершения.

Воспроизведение: QueryTimeout=50ms, INSERT trigger с pg_sleep(0.5); API возвращает error примерно через 503–508ms, строка не коммитится. Таким образом нарушение атомарности не обнаружено, но обещанная граница времени не работает.

Повторная проверка: deadline от внешнего request действительно отменяет SQL; проблема воспроизводится при обычном context.Background и собственном timeout сервиса. Queued media import дополнительно имеет внешний 15-minute deadline, ограничивающий часть сценариев. JSON-only import всё ещё использует обычный короткий transaction timeout. Storage внутри workspace transaction увеличивает влияние задержки на остальные записи workspace.

Исправление: передавать transaction context во все closures/SQL/upload; отдельный бюджет archive operations и короткий бюджет обычных запросов. Проверить медленную БД и медленное storage.

### REF-07. Архивные методы обходят lifecycle

Место: `service/admin/admin.go:79–217`.

QueueArchiveExport/Import, Status/History/Download передают request ctx напрямую в jobs manager, без a.withContext. Close останавливает workers, но borrowed database остаётся открытой по правильному ownership-контракту; поэтому после Close новая задача успешно записывается и остаётся queued либо подхватывается другой репликой.

Воспроизведение: NewWithDatabase → Close → публичный QueueArchiveExport с context.Background возвращает Job ID=1.

Повторная проверка: jobs.Manager.Close останавливает runtime, но не является запретом записи queue; исправление contextutil после CPA не помогает, если Merge вообще не вызывается. Обычные User/Admin методы root context объединяют. Resource methods также требуют отдельной проверки lifecycle: переданный конструктору root ctx не участвует в большинстве операций.

Исправление: использовать root/request context во всех entry points; для download удерживать merged context до Close reader. Проверить queue и cached media reads после shutdown.

### REF-08. HTTP MinIO не принимает архив через текущий SDK path

Место: `archive_s3.go:63`, особенно Body на строке 84.

archiveContextReader скрывает Seek/известную длину даже у seekable source. Настоящий AWS SDK с используемой конфигурацией HTTP endpoint возвращает до отправки запроса:

```text
compute input header checksum failed, unseekable stream is not supported without TLS and trailing checksum
```

Локальный HTTP fixture получил 0 запросов. Тесты archive_s3_test.go используют fakeArchiveClient и обходят signing/checksum middleware, поэтому проходят.

Повторная проверка: это подтверждено для поддержанного HTTP MinIO endpoint; работоспособность/отказ HTTPS S3 этим тестом не доказаны. Локальный disk mode не затронут. Исправленный общий jobs.S3Archive из CPA здесь не используется: у Reference свой adapter.

Исправление: ограниченный seekable spool с сохранением context/длины либо reuse общего S3 adapter; тестировать настоящим SDK оба режима endpoint и retry, затем реальным MinIO.

### REF-09. Cache failure возвращается после успешной записи БД

Места: `repository/cache.go:49–73`, `repository/admin.go:48`, `repository/import.go:153`, аналогичные mutations.

bumpReferenceCacheVersions вызывает диагностический callback, но возвращает исходную cache error наружу. Mutation уже committed, клиент получает failure и может повторить запрос или принять неверное решение о состоянии операции.

Воспроизведение: cache.Set возвращает `audit cache offline`; публичный Admin.CreateItem возвращает эту error, но SELECT подтверждает одну committed строку.

Повторная проверка: local version bump происходит до ошибки, но общая версия может не обновиться, поэтому другая нода может продолжать читать старый cache. Наличие OnCacheInvalidationError не подавляет публичную ошибку. У SoftDeleteResource ошибка уже игнорируется, поэтому дефект не одинаков для всех методов.

Исправление: не превращать успешный commit в failure; отдельно диагностировать и восстанавливать invalidation, определить cache-degraded поведение других нод. Это также уменьшит ошибочные запуски компенсации REF-02.

## Повторная проверка старого AUDIT.md

| Старый пункт | Текущий результат |
|---|---|
| 1. Workers до Run | Исправление сохранено; startWorkers вызывается из Run, существующий regression test проходит. |
| 2. Orphan после Resource write | Обычные storage/transaction failures покрыты тестами; crash-window остаётся. Компенсация не решает unknown commit и сама опасна: REF-02. |
| 3. Частичный media import | DB mutation и receipt атомарны; новый target media_version сохранён. Остались REF-02/03/04/05 и storage crash-window. |
| 4. Пароль тестовой PostgreSQL | Пользователь подтвердил, что тестовые секреты не используются в production. Пункт закрыт как production security finding; значение в отчёт не копируется. |
| 5. Export deleted items | Queue принудительно использует OnlyNotDeleted=true, исправление сохранено. |
| 6. Cache после GC | Пользователь разрешил выдачу bytes до истечения TTL. Пункт закрыт как допустимое поведение. REF-01 — отдельная безусловная потеря links и остаётся актуальным. |
| 7. Position ordering | InsertAfter/MoveAfter сохраняют workspace lock и безопасное перенумерование. Импорт использует другой путь: REF-04. |

## Дополнительные подтверждённые ограничения и технический долг

Эти пункты не смешиваются с подтверждением внешней эксплуатации, но требуют учёта при исправлениях.

- **Storage reconciliation отсутствует.** Media GC перечисляет только DB media_version rows. После crash между upload и rollback/compensation объекты без committed bookkeeping никогда не попадут в GC. При отменённом request обычная compensation использует тот же отменённый ctx и DeleteVersion может сразу отказаться. Это оставшийся риск старых пунктов 2/3; внешняя bucket lifecycle policy в репозитории не подтверждена. Отдельно Reference s3Archive не реализует ArchiveLister, поэтому общий SweepOrphans молча пропускает его — archive objects без job row тоже не сверяются.
- **Архивные лимиты не согласованы с экспортом.** Jobs.QueueImport по умолчанию ограничен 100 MiB; reference handler декларирует 512 MiB, manifest ограничен 16 MiB, ZIP entry count — 10 000. Export не проверяет эти бюджеты перед созданием успешного архива. Достаточно большой собственный архив окажется непринимаемым. Import держит ZIP bytes и media buffers одновременно в RAM; JSON manifest декодируется без ограничения количества объектов/глубины до typed allocation. OOM и большой production dump не воспроизводились; требуется согласованный измеренный budget/spool контракт.
- **Media import preflight неполон.** validateArchiveResources не запрещает отрицательные позиции, повтор одной пары item/resource на разных positions и превышение ряда DB string/int границ. SQL часть отклоняет поздно, уже после upload, а повтор пары может молча оставить только последнюю позицию. Проверяется hash original, но previews/placeholder и ContentType доверяются manifest; transport XSS не утверждается без проверки внешнего обработчика выдачи. Items/localizations импортируются bulk, media resources/links — поштучными SELECT/INSERT/UPDATE, что увеличивает transaction/lock duration.
- **Public archive DTO раскрывает инфраструктуру.** ArchiveJob — alias jobs.Job; наружу доступны Options/ArchiveKey/LockedBy/LeaseToken и raw Error. Workspace-scoped lookup защищает от чтения чужой job, поэтому обход workspace здесь не найден. Для API нужен safe DTO/diagnostic separation, как в CPA.
- **Повторный applied import зависит от исходного blob.** У reference archiveJobHandler нет AppliedImportReconciler. Общий manager проверяет receipt, но для этого handler продолжает путь чтения архива. Если commit состоялся, статус не записался, а blob исчез, reconciliation не завершится штатно; повторная DB-запись при этом защищена receipt.
- **Статические SQL в repository/archive.go/resource.go обходят SQLC**, локальные структуры/tests расходятся с текущими AGENT.md. Это поддерживаемость, не самостоятельная SQL injection: значения передаются параметрами, динамические bulk statements используют placeholders.

## Исключённые или не подтверждённые гипотезы

- Resolve с дубликатами теряет Resources при прямом repository-вызове: воспроизведено, но User.Resolve дедуплицирует keys до repository. При оговорённой публичной API-границе дефект недостижим; не включён в основные находки.
- SQL injection и межworkspace чтение через проверенные публичные lookup не подтверждены: workspace validation, составные ключи/FK и параметризация присутствуют. Это не доказательство отсутствия любых будущих проблем.
- Повторный DB import после успешного commit защищён transactional import receipt. Нельзя переносить CPA callback findings механически: Reference не выдаёт CPA-награды и не имеет аналогичного business callback.
- Смена type через update_existing без DangerousChangeType confirmation разрешена пользователем: импортируемые данные имеют приоритет. Это не уязвимость; подтверждение остаётся контрактом отдельного обычного метода смены type.
- Чтение старой version само по себе разрешено доменным контрактом; active/deleted ограничения каталога не обязаны запрещать исторические media URLs.

## Проверки и границы доказательств

- `go test ./reference/... -count=1` прошёл на локальной PostgreSQL; корневой пакет — 11.262 s. Этот запуск включал первоначальные временные audit probes; стандартные существующие тесты также прошли.
- Дополнительные пробы PostgreSQL повторили REF-01/03/04/05/06/07/09. Отдельные fixture tests подтвердили REF-02 и REF-08. Первая настройка cleanup fixture пыталась открыть отсутствующий Unix socket; после замены на явный NewUnavailable проверка опасной ветки прошла.
- `go vet ./reference/...` прошёл.
- Финальный `go test -race ./reference/... -count=1` без временных проб прошёл; корневой пакет — 13.963 s. `git diff --check` прошёл.
- Исходники проб сохранены в [SECURITY_RELIABILITY_REPRODUCTIONS.md](SECURITY_RELIABILITY_REPRODUCTIONS.md). Временные Go-файлы удалены: они проверяли наличие дефектов и не должны закреплять неправильное поведение в CI.
- Production, реальный MinIO, сетевой lost-COMMIT и аварийное завершение процесса не тестировались. Для этих сценариев указаны конкретная ветка кода и пределы моделирования.
- Вопросы о cache retention, type при импорте и test-only credential закрыты ответами пользователя; см. уточнённый контракт выше.
