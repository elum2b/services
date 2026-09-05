# CPA: аудит безопасности и надёжности

Дата: 2026-09-05. Проверяется текущий локальный код, а не старые номера строк из `AUDIT.md`.
После исходного аудита пользователь разрешил исправления. Доказательства до исправления сохранены ниже; актуальное состояние описано в следующем разделе.

## Исправления

Все пять воспроизведённых проблем исправлены. Бывшие PoC преобразованы в регрессионные тесты безопасного поведения.

| ID | Исправление | Регрессия |
| --- | --- | --- |
| CPA-01 | Completed assignment и его ID/code/rewards/events сохраняются даже при отсутствии в архиве. Complete берёт workspace read lock. Первое завершение импортированного issued создаёт одно событие. | TestCPAAuditRestorePreventsSecondCompletionCallback; TestCPAImportedIssuedAssignmentCompletesOnce; TestCPA_AdminImportUpdatePreservesCompletedRuntimeSnapshot |
| CPA-02 | Исторические связи проверяются отдельно от текущей конфигурации. | TestCPAAuditSharedCodeChangePreservesArchiveRoundTrip; TestCPAArchiveRuntimeBulkAndHistoricalMode |
| CPA-03 | Под lock пересчитываются полные затронутые UTC-дни. | TestCPAAuditPartialRefreshPreservesWholeDay |
| CPA-04 | Request/root contexts объединяются; stream удерживает context до Close. Уже отменённый root отменяет merged context синхронно. | TestCPAAuditArchiveQueueRejectsWorkAfterClose |
| CPA-05 | Каждый claim получает уникальный fencing owner в locked_by. Worker подтверждает owner из claim; ACK/Fail/Reject проверяют и срок аренды. | TestCPAAuditStaleCallbackWorkerCannotAckNewLease |

Остатки прежних восьми пунктов устранены либо заменены согласованным эксплуатационным контрактом: [AUDIT.md](AUDIT.md). Конфигурация: [ARCHIVES.md](ARCHIVES.md).

Дополнительно: SQL получает deadline транзакции; runtime import использует bulk SQL; generated collision обрабатывается ON CONFLICT DO NOTHING без прерывания транзакции. Паника фонового handler преобразуется в ошибку через goroutine manager. Receipt/status recovery не зависит от исходного архива и восстанавливает cache invalidation.

Проверки исправлений: полный `go test ./... -count=1`, `go vet ./...`, `go build ./...`, генерация SQLC во всех конфигурациях, `gofmt` и `git diff --check` прошли. Финальный `go test -race ./cpa ./internal/utils/importexport/jobs ./internal/utils/callback -count=1` также прошёл. S3 API fixture не заменяет проверку production credentials/MinIO.

## Исходное обнаружение

Описания ниже фиксируют поведение **до исправления**, включая исходные имена PoC и номера строк на момент аудита.

## Граница проверки

Внешнему API доверяем: аутентификация, права оператора и валидация внешнего запроса не рассматриваются как отсутствующие защиты CPA. Проверяются штатные бизнес-операции, PostgreSQL, транзакции, история, кэш, callback и архивные workers. Импорт `update_existing` в действующий workspace разрешён — подтверждено пользователем в этой задаче.

Для каждой находки выполнен второй проход: публичный вызов → repository → реально выполняемый SQL → constraints/locks → возможная компенсирующая защита. Подтверждение на PostgreSQL не означает подтверждения начислений в приложении-потребителе callback. Production, Fiber и внешние обработчики не запускались.

## Подтверждённые находки

| ID | Приоритет | Проблема | Доказательство |
| --- | --- | --- | --- |
| CPA-01 | P1 | Восстановление архива позволяет повторное завершение одного пользовательского оффера с новым callback key | PostgreSQL, два completion outbox events |
| CPA-02 | P1 | Штатная смена общего кода делает собственный экспорт непригодным для импорта | PostgreSQL, отказ импорта собственного пакета |
| CPA-03 | P2 | Частичный временной интервал перезаписывает статистику целого дня | PostgreSQL, дневной счётчик 2 → 1 |
| CPA-04 | P2 | После закрытия CPA архивный API продолжает принимать задания | PostgreSQL, новая persistent queued row после Close |
| CPA-05 | P2 | Старый callback worker может подтвердить новую аренду того же события | PostgreSQL, устаревший ACK принят |

### CPA-01. Импорт снимает защиту от повторного завершения

Места: `repository/import.go:1116` (`replaceImportedOfferRuntime`), `repository/import.go:1077` (создание нового assignment), `repository/assignments.go:375` (`Complete`), `repository/assignments.go:772` (ключ callback на основе assignment ID).

Сценарий: выдать код → экспортировать issued assignment → завершить назначение → импортировать прежний пакет в тот же workspace с `update_existing` → снова вызвать `Admin.Complete` с той же полной identity и CPAID. Все запросы предметно валидны. Импорт удаляет прежнее назначение и создаёт новое с другим serial ID и статусом issued. Второе завершение возвращает `AlreadyDone=false`; в outbox остаются два `cpa.completed` с различными event/idempotency keys.

Воспроизведение: `TestCPAAuditRestoreAllowsSecondCompletionCallback`. Получено: assignment ID `1 → 2`, два completion события для одного пользователя/оффера. Тест включает награду в snapshot.

Повторная проверка: UNIQUE identity защищает только существующую строку, которая удаляется импортом. UNIQUE callback event key использует новый ID. `ClaimImportReceipt` защищает повторное выполнение одного job, но не повторный пользовательский импорт и не откат состояния новым job. Workspace lock сериализует часть записей, но не сохраняет необратимую историю начислений. Кроме того, `Complete` не берёт workspace catalog lock, поэтому импорт не образует с ним единый протокол блокировок.

Последствие: CPA повторно публикует событие награды. Повторное фактическое начисление зависит от ключа дедупликации внешнего обработчика; оно здесь не проверено. Старые pending callback также остаются доступными для доставки после замены runtime.

Подтверждённое пользователем требование: для импортированного issued assignment первое завершение должно отправлять callback; уже отправленные уведомления повторять нельзя. Сам импорт не должен создавать повторных уведомлений.

Направление исправления: сохранять постоянную историю завершения/начисления вне заменяемого snapshot и стабильный бизнес-ключ идемпотентности. Учесть завершённые и отсутствующие в архиве assignments, существующие pending callback. Одной смены event key недостаточно: повторный `CompleteResult` также может использоваться приложением. Не удалять старый outbox как способ устранения дублей. При переносе между базами сведения о прошлой доставке должны переноситься либо быть доступны по постоянному бизнес-ключу; текущий архив такой delivery history не содержит.

### CPA-02. Историческое назначение ошибочно сверяется с текущей конфигурацией

Места: `repository/offers.go:358`, `sqlc/query.sql:1`, `repository/export.go:12`, `repository/import.go:641` (`validateRuntimePackage`, проверки code mode/source/shared code).

Сценарий: создать shared offer → выдать код → изменить SharedCode через `Admin.UpsertOffer` → экспортировать → импортировать пакет в пустой другой workspace. Изменение каталога разрешено сервисом, а назначение правильно хранит прежний код. Но import validator требует равенства assignment.Code текущему offer.SharedCode.

Воспроизведение: `TestCPAAuditSharedCodeChangeBreaksArchiveRoundTrip`. Импорт собственного экспорта отклоняется с `cpa import assignments[0]: invalid shared code linkage`.

Повторная проверка: проверка UpsertOffer не запрещает смену кода при существующих assignments; snapshot намеренно не обновляется. Export читает согласованный snapshot, поэтому ошибка не объясняется конкурентной записью или устаревшим кэшем. ZIP-валидация не исправляет противоречие внутри пакета. Аналогичные проверки mode/source заслуживают тех же регрессионных сценариев, но отдельно динамически не проверялись.

Последствие: успешный экспорт не гарантирует возможность переноса/восстановления. Импорт отклоняется до записи, частичного переноса в этом сценарии нет.

Исправление: валидировать историческое назначение по его собственному mode/code и связи с историческим pool record, а не требовать совпадения со сменяемым каталогом. Не переписывать уже выданные коды ради прохождения импорта.

### CPA-03. Пересчёт части дня занижает итог дня

Места: `service/admin/stats.go:50`, `repository/stats.go:89`, `sqlc/query.sql:360`, `sqlc/query.sql.go:1477`.

Сценарий: два issued события одного дня → RefreshDailyStats за полный UTC-день даёт 2 → RefreshDailyStats с границей между событиями и концом дня даёт 1 в той же дневной строке. WHERE ограничивает события точным временем, GROUP BY сворачивает их в день, ON CONFLICT заменяет полный дневной результат частичным.

Воспроизведение: `TestCPAAuditPartialRefreshOverwritesWholeDay`. Граница берётся через `clock_timestamp()` PostgreSQL, чтобы не зависеть от рассинхронизации часов приложения и БД.

Повторная проверка: repository проверяет только непустой упорядоченный диапазон; приведения к полным суткам нет ни в публичном методе, ни в generated SQL. Это не ошибка timezone. Существующий тест также использует интервалы меньше суток, поэтому такой вызов не исключён текущим контрактом.

Исправление: определять затронутые UTC-дни и полностью пересчитывать каждый либо явно отклонять неполные сутки. Согласовать, должен ли until быть исключительной границей; чтение статистики сейчас использует включительные DATE-границы.

### CPA-04. Архивный API не связан с lifecycle context

Места: `service/admin/archive_jobs.go:97`, `:121`, `:147`, `:162`, `:184`, `:199`; для сравнения `service/admin/complete.go:26` объединяет контексты.

Сценарий: создать CPA через NewWithDatabase → Close → QueueArchiveExport с живым request context. Метод напрямую передаёт контекст в jobs.Manager, не объединяя его с root context CPA. Чужая БД закономерно остаётся открытой.

Воспроизведение: `TestCPAAuditArchiveQueueAcceptsWorkAfterClose`: после Close создана строка queued с ID 1.

Повторная проверка: Manager.Close останавливает workers, но queue/status методы не проверяют состояние закрытия. Это не нарушение правила владения БД — чужую БД закрывать нельзя. При обычном Run закрытие собственной БД ограничивает окно, но не устраняет нарушение для embedding и запросов во время shutdown.

Последствие: закрытый экземпляр принимает обязательство обработать новую работу; обработка зависит от другого/будущего worker. Аналогично архивные reads/uploads не отменяются root context.

Исправление: применять a.withContext ко всем публичным архивным методам и передавать объединённый контекст в manager. Проверить отмену долгого upload и отсутствие queue row после Close.

### CPA-05. Callback ACK не отличает поколения lease

Места: `internal/utils/callback/store.go:415`, `:533`, `:545`, `:558`; `internal/utils/callback/worker.go:17`, `:240`.

Сценарий: worker A арендует событие → lease истекает → worker B с тем же WorkerID арендует его заново → A отправляет старый ACK. Условие UPDATE проверяет только ID/status/locked_by; уникального lease token и проверки срока в ACK нет. Default WorkerID одинаков у всех экземпляров. Повторное lease возможно и в одном scheduler после истечения срока долгого handler.

Воспроизведение: `TestCPAAuditStaleCallbackWorkerCanAckNewLease`. Два Store на одной CPA БД, истечение lease моделируется изменением locked_until только тестового события. Старый MarkOK успешно меняет статус новой аренды на ok. Реальное ожидание минуты не требуется.

Повторная проверка: SKIP LOCKED защищает одновременный claim, а не смену владельца после expiry. Ограничение четырёх обработчиков на route также не является fencing. Архивный jobs.Store уже имеет token fence, но это другой store: callback этим механизмом не пользуется. Уникальные WorkerID разных реплик ослабляют сценарий между репликами, но не дают поколений аренды внутри одного worker.

Последствие: устаревший обработчик меняет состояние текущей попытки; поздний Failed/Reject может аналогично перезаписать её. Сам факт повторной доставки при at-least-once не объявляется отдельной уязвимостью; дефект здесь — принятие устаревшего подтверждения.

Исправление: случайный token или монотонное поколение на каждый lease; передавать его через callback Context, проверять при ACK/Reject/Fail. Отдельно определить heartbeat/deadline долгих handlers и сохранить дедупликацию у получателя.

## Повторная проверка прежнего AUDIT.md

| Старый пункт | Текущее состояние | Повторная проверка |
| --- | --- | --- |
| 1. Только catalog, нет runtime | Основной тезис устарел | exportRuntime экспортирует codes, assignments, snapshots и events; import восстанавливает runtime и пересчитывает stats. Outbox не переносится по явному текущему контракту. Новые CPA-01/02 рассматриваются отдельно. |
| 2. Неограниченный ZIP upload | Основной upload/ZIP-bomb сценарий закрыт | QueueImport использует copyUpload и validateZIPFile: 100 MiB compressed, 500 MiB uncompressed, предел entries/ratio; фактическая распаковка проверяется, требуется единственный manifest. Остаток: экспорт строит весь пакет и bytes.Buffer до применения maxReader; bounded streaming и предел памяти генерации отсутствуют. OOM нагрузкой не воспроизводился. |
| 3. Локальный диск между репликами | Зависит от deployment | Options.Archive и ArchiveDirectory уже доступны, есть комментарий о shared durable storage. Default остаётся local; startup не доказывает shared storage. Ответ о production-топологии пока не получен. |
| 4. Lease без heartbeat и повторный import | Основной тезис закрыт | jobs.Manager.withLease продлевает аренду; ImportJob пишет ClaimImportReceipt в бизнес-транзакции. Это не защита от нового импорта старого snapshot, см. CPA-01. |
| 5. Неатомарный статус, retry отсутствует | Частично закрыт | Receipt устраняет повторную бизнес-запись после crash; есть bounded retry/backoff. Но ошибка complete после успешного import может привести к failed при уже применённых данных, если классификатор считает её terminal. isTransient распознаёт Temporary()/network timeout, а не все PostgreSQL retryable SQLSTATE. Этот fault-injection сценарий отдельно не воспроизведён. |
| 6. Возврат внутренних job fields | Не закрыт на уровне Go API; не доказанная внешняя утечка | ArchiveJob всё ещё alias jobs.Job; Options, ArchiveKey, LeaseToken, LockedBy и Error возвращаются без projection. QueueArchiveImport сериализует весь ImportRequest, включая Package, хотя данные затем читаются из ZIP. С учётом доверенного внешнего API нельзя утверждать, что эти поля реально видит неавторизованный пользователь. |
| 7. 1s timeout архива | Не закрыт на уровне конфигурации | repository.queryTimeout по умолчанию 1s; WithTx использует его для полного import/export. HandlerTimeout=1h не переопределяет его. Runtime import делает последовательные SQL на каждую code/assignment/event, в отличие от bulk catalog. Максимально поддерживаемый размер при production timeout не измерен. |
| 8. Orphans и неоднозначный manifest | Частично закрыт | Единственный manifest обязателен, ZIP checksum проверяется чтением entries. SweepOrphans ищет unreferenced ZIP при наличии ArchiveLister. DiskArchive.List игнорирует `.archive-*`: временный файл после kill процесса до rename не попадает в sweep. Custom Archive без List вообще не участвует в orphan sweep. Реальное падение процесса/заполнение диска не моделировалось. |

Таким образом, закрывать весь прежний AUDIT.md как исправленный оснований нет; переносить все восемь прежних формулировок в список подтверждённых уязвимостей тоже нельзя.

## Проверенные защиты и отклонённые гипотезы

- Issue повторно читает assignment после identity lock; UNIQUE включает workspace, CPAID, AppID, PlatformID, PlatformUserID. Параллельная штатная выдача одной identity не доказана как обход этой защиты.
- Выделение pool code использует FOR UPDATE SKIP LOCKED и UNIQUE code_id у assignment. Разные пользователи не получают одну строку pool.
- Assignment, event и callback создаются одной транзакцией через callbacks.WithTx(tx). Ошибка создания outbox не оставляет успешную частичную выдачу.
- Штатные повторные GetCode/Complete используют исторический reward snapshot; изменение reward catalog не меняет выданное обещание. Проблема CPA-01 относится к замене истории импортом.
- Удаление issued/completed code является soft delete; история и невозможность обычной повторной выдачи сохранены.
- Catalog admin writes и import используют workspace lock; catalog reads/export используют snapshot там, где собирается документ несколькими запросами.
- Cache versions меняются после commit; провал invalidation не превращает выполненную mutation в ложную бизнес-ошибку. Проверка двух cache-нод присутствует в интеграционных тестах. Окно commit→invalidation не объявляется отдельным обходом выдачи: Issue читает БД.
- Динамические import table names приходят из констант, значения передаются placeholders. Достижимая SQL injection из доверенных публичных запросов не обнаружена.
- Target регулирует отображение по AGENT.md; отсутствие target-проверки в GetCode не объявляется обходом авторизации вопреки заданному контракту.
- Generated collision retry после unique violation не использует savepoint; но пространство кодов валидируется минимум на 64 бита. Практический сценарий массовых коллизий не доказан и в подтверждённые находки не включён.

## Проверки и ограничения доказательств

Базовый запуск на локальном PostgreSQL прошёл:

```bash
GOCACHE=/private/tmp/cpa-audit-gocache go test ./cpa ./internal/utils/importexport/jobs ./internal/utils/callback -count=1
GOCACHE=/private/tmp/cpa-audit-gocache go test ./cpa -run '^TestCPAAudit' -count=1 -v
GOCACHE=/private/tmp/cpa-audit-gocache go vet ./cpa/... ./internal/utils/importexport/jobs ./internal/utils/callback
```

Пять TestCPAAudit — характеристические PoC: PASS означает воспроизведение описанного дефекта, а не его исправление. При исправлении их нужно преобразовать в проверки требуемого безопасного поведения. Они используют изолированные CPA test databases; срок callback lease меняется только в тестовой базе. Существующие данные приложения не очищались.

Первый запуск внутри sandbox не получил доступа к localhost; повторный разрешённый запуск выполнил PostgreSQL-тесты успешно. Это ограничение окружения не учитывалось как дефект продукта.

Финальная проверка прошла:

```bash
GOCACHE=/private/tmp/cpa-audit-gocache go test -race ./cpa ./internal/utils/importexport/jobs ./internal/utils/callback -count=1
git diff --check
```

Race: CPA — 12.643s, jobs — 1.882s, callback — 2.008s; все PASS. Пять PoC также прошли внутри этого запуска. Проверка остальных бизнес-сервисов не выполнялась, поскольку текущая область — CPA. SQLC не перегенерировался: SQL/schema и бизнес-код не изменялись.

## Подтверждённые решения пользователя

1. Разрешены исправления всех подтверждённых и оставшихся старых пунктов.
2. Сейчас работает монолит. Диск остаётся default; для будущих реплик подключается S3/MinIO. Хранение ZIP в PostgreSQL исключено.
3. Первое завершение импортированного issued отправляет callback; уже отправленное уведомление не повторяется.
4. Completed сохраняется целиком даже при отсутствии в архиве. Импорт не откатывает факт завершения.
5. Внешний обработчик начислений не исследован: фактическая exactly-once сетевая доставка не заявляется.

Изменения соответствуют этим решениям; действующие настройки S3 и production deployment не менялись.
