# Публичные методы Collections

Все бизнес-методы принимают context.Context и один Params из collections/model
или service/admin. Фасад выбирается доверенным приложением, transport сюда не входит.

## User — только чтение

| Метод | Params | Результат |
| --- | --- | --- |
| HasItem | HasItemParams | ItemState, включая effective owned и сохранённую revision |
| HasItems | HasItemsParams | 1–1000 уникальных пар, порядок ответа совпадает с запросом; один SQL |
| ListCollections | ReadParams | Опубликованные коллекции с локалью, без приватной истории |
| GetCollection | GetCollectionParams | Общий опубликованный состав и Version, без ownership; entries пагинированы |
| ListOwned | ListOwnedParams | Только доступные owned=true, фильтр по набору коллекций |

`colors` — CollectionKey (категория коллекции). Ключи её элементов остаются
непрозрачными Reference keys. Состав и отметки пользователя запрашиваются отдельно:

- `GetCollection{CollectionKey: "colors"}` возвращает доступные элементы и Version;
  ответ одинаков для всех пользователей workspace, без owned/revision/changed_at.
- `ListOwned{CollectionKeys: []string{"colors"}}` возвращает открытые элементы
  caller/Subject с revision/changed_at. Пустой фильтр означает все коллекции.

GetCollection.KnownVersion — ранее полученная Version для той же коллекции,
локали и страницы (opaque строка из 64 hex символов). При совпадении ответ содержит
NotModified=true, Key и Version; Entries отсутствуют, остальные metadata zero-value.
Клиент сохраняет прежний состав. Новая версия возвращает полный состав страницы.
Удалённая/неопубликованная коллекция возвращает ErrNotFound даже с KnownVersion.
Версия меняется при catalog write/import в workspace, но не при выдаче/отзыве.
Каждая страница/локаль имеет собственный token. Каждый вызов читает отдельный
snapshot; атомарный снимок нескольких страниц этот метод не предоставляет.

ReadParams.Identity — caller; Subject — optional target того же workspace/app.
Locale возвращается ровно запрошенный; отсутствующая локализация — пустой Text.
GetCollection проверяет caller/Subject scope, но не использует пользователя в cache key.
Page: default Limit=100, max=1000, отрицательный Offset приводится к 0.
Порядок catalog — position/key, entries — position/item_key, ListOwned — collection/item.

## Internal — доверенная запись

| Метод | Params | Результат |
| --- | --- | --- |
| SetOwned | SetOwnedParams | Immutable Receipt одного изменения |
| SetOwnedBatch | SetOwnedBatchParams | Один Receipt атомарного batch <=1000, без duplicate pairs |

Source и OperationID обязательны, source=admin зарезервирован. Размер ключей до
191 UTF-8 bytes, без краевых пробелов/NUL. ExpectedRevision optional, >=0.
Отсутствие отметки = false/revision0. No-op не меняет revision/changed_at и не
публикует событие. Одинаковый operation scope/payload возвращает исходный receipt,
другой payload — ErrConflict. Single и batch различаются в fingerprint.

Скрытый entry/collection даёт HasItem=false, но не стирает owned. Повторное
добавление сохраняет owned/revision; late false допускается для существующей row.
Новое true требует доступного состава. Batch откатывается при любой ошибке.

## Admin — управление и коррекции

| Методы | Params | Access |
| --- | --- | --- |
| ListCatalog / GetCollection | CatalogParams | collections.catalog.read |
| CreateCollection | SaveCollectionParams | collections.collection.create |
| UpdateCollection | SaveCollectionParams | collections.collection.update |
| ArchiveCollection | ArchiveCollectionParams | collections.collection.archive |
| AddEntry | EntryParams | collections.entry.add |
| RemoveEntry | EntryParams | collections.entry.remove |
| ReorderEntries | ReorderEntriesParams | collections.entry.reorder |
| CorrectOwned | CorrectOwnedParams | collections.ownership.correct |
| ListJournal | JournalParams | collections.journal.read |
| GetStats / ListDailyStats | StatsParams / DailyParams | collections.stats.read |
| RefreshDailyStats | DailyParams | collections.stats.refresh |

Create/Update изменяют definition/localization, Entries в них запрещены:
состав меняется отдельными Admin-методами. Update требует существующий key;
Archived=false позволяет восстановление. AddEntry — upsert, включая восстановление
удалённого item. RemoveEntry мягко удаляет; Reorder требует полный уникальный
порядок текущего состава (до 100000 entries), включая неактивные entries.
Все catalog writes требуют ActorID и создают audit. ListCatalog возвращает definition/
localization без entries; Admin.GetCollection пагинирует текущие entries.

CorrectOwned использует тот же transactional engine, требует ActorID/Reason,
принудительно source=admin, пишет actor/reason в operation journal. User такой
операции не имеет. ListJournal фильтрует optional identity и никогда не доступен User.

## Admin — callback

ListCallbackEvents, GetCallbackEvent, RetryCallbackEventNow, MarkCallbackEventOK,
MarkCallbackEventReject, ResetExpiredCallbackProcessing принимают CallbackParams.
Соответствующие access: collections.callback.read/retry/accept/reject/reset.
Get/изменение scoped WorkspaceID+ID. Retry не оживляет terminal ok/reject.
Публичный DTO не раскрывает lease tokens. Нет отдельного admin access для worker.

Корень OnCallback(ctx, handler, options...) регистрирует/выполняет callback handler
по существующему lifecycle контракту. Context.Ownership содержит ChangeEvent;
Successful/FailedWithError/CanceledWithReason управляют delivery status.
EventType: collections.ownership_changed.

## Admin — конфигурационные архивы

| Метод | Params | Access |
| --- | --- | --- |
| Export / QueueArchiveExport | ExportParams / QueueArchiveExportParams | collections.archive.export |
| PreviewImport / Import / QueueArchiveImport | ImportParams / QueueArchiveImportParams | collections.archive.import |
| ArchiveJob | ArchiveStatusParams | collections.archive.read |
| ArchiveHistory / ArchiveJobHistory | ArchiveHistoryParams | collections.archive.read |
| DownloadArchive | ArchiveStatusParams | collections.archive.download |

Format collections.export.v1, без ownership/history/Reference metadata.
ConflictStrategy: update_existing (default), skip_existing, fail_on_conflict.
ImportParams.Request.Package содержит каталог. Для Import и QueueArchiveImport
обязателен ImportRequest.ActorID; host берёт его из авторизованного operator context,
не из неподтверждённых клиентских данных. Импорт записывает operator/job/hash/result
в catalog audit в той же transaction. Preview не требует ActorID. QueueImport получает ZIP с одним
manifest.json; вложенный Package в queue options не сохраняется. Download доступен
только завершённому export. Архивные status/history возвращают безопасный DTO.

## Infrastructure

New/Run/NewWithDatabase/Close/IsReady и Admin.Bootstrap, ConfigureArchiveJobs,
StartArchiveJobs нужны composition root, не являются удалёнными operator actions.
NewWithDatabase не закрывает переданную БД; schema bootstrap выполняет embedding caller.

Доступы внесены в Control/sqlc/catalog.sql с RU/EN service/group/access localization.
AccessManifest предоставляет перечень для composition root. Наличие ключа не
заменяет проверку Control actor/scope в transport.

Root errors: ErrInvalidArgument, ErrNotFound, ErrConflict, ErrForbidden и lifecycle
errors. Ошибка receipt mismatch и stale revision — ожидаемый typed conflict.

После Close сервис терминален: повторный Run/регистрация callback возвращают
ErrServiceClosed. Close отменяет и ждёт startup и все зарегистрированные/embedding
callback runs перед освобождением зависимостей. Run допустим только у shell New(),
а не поверх уже подготовленного NewWithDatabase.

Custom Archive реализуется через публичные collections.Archive/ArchiveObject/
ArchiveInfo/ArchiveLister. Никакие imports из internal/utils для этого не нужны.
Optional ArchiveLister сохраняется adapter и участвует в orphan cleanup.
