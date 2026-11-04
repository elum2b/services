# Collections

Collections — библиотечный сервис в github.com/elum2b/services, без собственного
HTTP transport. Корень собирает User/Admin/Internal, lifecycle, SQL wrapper,
callback workers и archive jobs. Публичные модели находятся в model; repository
единственный знает SQLC, SQL transactions и DB rows.

## Схема и права

Каталог — collections_collection / localization / entry. Entry удаляется мягко.
Ownership связан полным составным ключом с entry и содержит owned/revision/changed_at.
Состояние пользователя не удаляется при смене каталога. Raw ownership не является
проверкой права: HasItem соединяет его с актуальной видимостью обеих частей каталога.
Обычные user-list/get возвращают только опубликованный состав. Admin имеет отдельные
методы чтения скрытого каталога. Удалённый entry хранится для истории/отзыва, а в
каталог возвращается через AddEntry с тем же ключом.

User API получает authenticated Identity и optional Subject. Разные workspace/app
запрещены; приложение разрешает account в полную subject identity и контролирует
доступ к фасаду. Сервис не выдаёт операторских прав на основании identity.
User API не содержит mutations. Control AccessManifest описывает только Admin actions.

## Операции

Для SetOwned транзакция берёт shared catalog lock и exclusive owner lock. Импорт
и admin writes берут exclusive catalog lock: состав не меняется между проверкой
и записью права. Ещё не существующие ownership rows защищены тем же owner lock.
Это сериализует одного владельца, но позволяет параллельные записи разных owners.

Операционный scope включает полную identity, source и OperationID. SHA-256 hash
канонического JSON семантических полей включает single/batch/correction method,
порядок changes, ExpectedRevision, actor/reason коррекции. Profile/target атрибуты
identity не меняют hash. Исходный receipt возвращается до проверки текущего каталога,
поэтому replay работает после удаления/архивации. История не переписывается.

Отсутствующая запись — false, revision=0. Фактическое изменение повышает revision.
No-op сохраняет только receipt. Начальный false не материализует ownership row.
Поздний false для скрытого entry разрешён, если ownership row уже существует.
ExpectedRevision проверяется и для no-op. Ошибка любого элемента откатывает batch.

BatchStates, SetOwnershipBatch, PutChangeBatch и PutCallbackBatch используют массивы
PostgreSQL; количество SQL-вызовов не зависит от размера batch. Изменения сначала
проверяются целиком, потом записываются. Change journal имеет deferred FK на receipt,
чтобы все изменения/outbox и сам receipt коммитились вместе. SQLC владеет запросами. ReorderEntries также делает один bulk UPDATE FROM
unnest WITH ORDINALITY и 6 SQL statements (включая persistent catalog version bump) независимо от числа элементов;
лимит состава и полной перестановки согласован на 100000 entries.

## История, статистика, callback

collections_operation хранит immutable результат; collections_change — только
реальные переходы. collections_catalog_audit фиксирует actor/payload CRUD каталога.
GetStats различает stored_owned и available_owned; скрытие каталога не становится
фиктивным revoke. Grants/revokes считаются из change journal. Суточная materialization
пересчитывает полные UTC-дни. Она отражает данные на момент refresh, не realtime.

Outbox collections_clb_event совместим с общим callback Store/worker: routing по
workspace/app/platform, устойчивые event keys из receipt ID и номера изменения.
Payload содержит before/after/revision и source/operation. Запись события ровно одна,
внешняя доставка at-least-once. Получатель дедуплицирует и обрабатывает отзыв права.
Ack подтверждает доставку, не игровой эффект. Collections не вызывает Inventory.

## Кэш и архивы

User.GetCollection возвращает только опубликованный состав с локализацией и
opaque Version; ownership читается отдельно через ListOwned с CollectionKeys.
Каталог общий для пользователей workspace, локали и страницы; пользовательских
флагов в cached DTO нет. CacheEnabled включает L1/L2 через sqlwrap.Query,
CatalogCacheTTL по умолчанию 24 часа. Возвращаемый slice отделён от L1.

collections_catalog_version хранит монотонную workspace revision. CRUD и import
повышают её в одной transaction с данными/audit (import один раз на пакет,
skip-only/replayed job не повышает). Ошибка transaction откатывает и версию.
Каждое GetCollection сначала читает publication + revision одним SQL из PostgreSQL.
SHA-256 token связывает revision с workspace/collection/locale/нормализованной
страницей; ownership, caller и Subject в него не входят. KnownVersion позволяет
вернуть NotModified без загрузки тела. Hidden/missing collection даёт ErrNotFound.
На cache miss версия и тело читаются из одного REPEATABLE READ READ ONLY snapshot.
Новая версия выбирает новый cache key, включая при L2 outage или сбое invalidation.
Чтение, пересекающееся с commit, может вернуть прежний согласованный snapshot;
следующий запрос увидит новую версию. Версия и ключи не сбрасываются при рестарте.
Workspace scope консервативно обновляет версии всех его коллекций.

Admin.ListCatalog использует отдельный sqlwrap cache version scope (metadata без
entries) с postcommit invalidation. Только этот admin cache при outage может быть
stale до recovery/TTL; ошибка cache/diagnostic callback не отменяет commit.
Ownership не кэшируется: HasItem/HasItems и ListOwned читают актуальную DB, вне
кэша каталога. User locality query содержит locale predicate; остальные переводы
не загружаются. Multi-query view и export используют REPEATABLE READ READ ONLY.
GetCollection entries пагинированы; несколько вызовов не образуют общий snapshot.

collections.export.v1 содержит коллекции, локализации и entries; runtime ownership,
receipts и Reference metadata не переносятся. Import default update_existing:
A/B/C + B/C/D → A/B/C/D, вложенный состав B/C заменяется без удаления ownership.
Explicit skip/fail поддержаны. Preflight до write transaction; bulk batches до
1000 rows/60000 params. Import job receipt коммитится с конфигурацией и catalog audit. ActorID обязателен
для sync/async import, actor/hash/result/job ID не теряются между очередью и worker.

Общие archive utilities обеспечивают ограничение ZIP/JSON, STORE export,
private staging, retries, heartbeat/fencing, recovery и safe job DTO. Диск default;
несколько реплик требуют общего S3/MinIO или Archive. Экспорт ограничен 100000
строками/32 MiB консервативного DB footprint (включая soft-deleted entries), JSON
и shape budgets. Archive timeout 5 минут отдельно от hot-path QueryTimeout.

## Lifecycle

New — shell с неизменным lifetime context/manager. Run отменяется Close даже
во время open/bootstrap и публикует зависимости только если shell не closed.
Close терминален, ждёт startup и direct embedding callbacks; concurrent Close
получают общий итог. Нет mutex на сетевых ожиданиях. Run открывает/bootstraps БД
и workers; NewWithDatabase с context-aware sqlwrap constructor использует
подготовленную caller schema (Admin.Bootstrap доступен для embedding). Close отменяет
root context и останавливает ресурсы. Borrowed DB/shared L2 не закрываются: local
L1 workers освобождаются sqlwrap.CloseLocalCache. Owned connection закрывается.

## Границы

Нет игровых категорий/selection, количества, слотов или синхронизации с Inventory.
HasItem не фиксирует право навсегда: приложение учитывает последующий отзыв при
использовании выбранной внешности. Нет общей транзакции с game-backend.
Доставка, реальный S3 и production wiring требуют проверки в окружении приложения.

Public Archive contracts объявлены в model и доступны через root aliases.
Приватный archiveadapter конвертирует queue types в обе стороны и сохраняет
optional listing. Внешний consumer проверяется отдельным compiler test.
