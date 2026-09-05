# Reference methods

Только методы слоев `user` и `admin`, которые можно использовать как основу будущего API.

## user

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `User.Get(ctx, params)` | `GetParams{WorkspaceID, Key, Locale}`. | Возвращает активный справочный item по ключу. |
| `User.Resolve(ctx, params)` | `ResolveParams{WorkspaceID, Keys, Locale}`. | Массово разрешает ключи в items и возвращает найденные элементы плюс `MissingKeys`. |
| `User.List(ctx, params)` | `ListParams{WorkspaceID, Locale, Page}`; `Page{Limit, Offset}`. | Возвращает страницу активных справочных items. |

## admin

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `Admin.CreateItem(ctx, params)` | `SaveItemParams{WorkspaceID, Key, Type, Payload, IsActive}`. | Создает справочный item. |
| `Admin.UpdateItem(ctx, params)` | `UpdateItemParams{WorkspaceID, Key, Payload, IsActive}`. | Обновляет payload и активность item. |
| `Admin.DangerousChangeType(ctx, params)` | `DangerousChangeTypeParams{WorkspaceID, Key, CurrentType, NewType, Confirmation}`. | Меняет тип item при подтверждении `CHANGE_REFERENCE_TYPE`. |
| `Admin.GetItem(ctx, workspaceID, key)` | `workspaceID`, `key`. | Возвращает item с локализациями и служебными полями. |
| `Admin.ListItems(ctx, params)` | `ItemListParams{WorkspaceID, Type, OnlyNotDeleted, Page}`. | Возвращает список items с фильтрами. |
| `Admin.SoftDeleteItem(ctx, workspaceID, key)` | `workspaceID`, `key`. | Мягко удаляет item. |
| `Admin.RestoreItem(ctx, workspaceID, key, active)` | `workspaceID`, `key`, `active bool`. | Восстанавливает item и задает активность. |
| `Admin.UpsertLocalization(ctx, params)` | `SaveLocalizationParams{WorkspaceID, ItemKey, Locale, Title, Description}`. | Создает или обновляет локализацию item. |
| `Admin.GetLocalization(ctx, workspaceID, key, locale)` | `workspaceID`, `key`, `locale`. | Возвращает локализацию. |
| `Admin.ListLocalizations(ctx, workspaceID, key)` | `workspaceID`, `key`. | Возвращает локализации item. |
| `Admin.DeleteLocalization(ctx, workspaceID, key, locale)` | `workspaceID`, `key`, `locale`. | Удаляет локализацию. |
| `Admin.QueueArchiveExport(ctx, params)` | `QueueArchiveExportParams{WorkspaceID, FileName, IncludeMedia}`. | Ставит ZIP-экспорт только не удалённых данных в асинхронную очередь. |
| `Admin.QueueArchiveImport(ctx, params)` | `QueueArchiveImportParams{WorkspaceID, FileName, IncludeMedia, ConflictStrategy, Archive}`. | Ставит ZIP-импорт в асинхронную очередь; асинхронный импорт не предоставляет `PreviewImport`. |
| `Admin.ArchiveJob(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Возвращает текущий статус задачи. |
| `Admin.ArchiveHistory(ctx, workspaceID, page)` | `workspaceID`, `Page`. | Возвращает историю задач workspace. |
| `Admin.DownloadArchive(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Скачивает результат завершенного export job. |
| `Admin.ArchiveJobHistory(ctx, workspaceID, id, page)` | `workspaceID`, job `id`, `Page`. | Возвращает историю состояний задачи. |
| `Admin.GetStats(ctx, workspaceID)` | `workspaceID`. | Возвращает статистику справочника. |

## resource

| Метод | Что принимает | Что делает |
| --- | --- | --- |
| `Resource.Create(ctx, params)` | `SaveParams{WorkspaceID, Key, Type, Payload, IsActive, File, FirstFrame}`. | Обрабатывает media и создаёт resource; Lottie/TGS/SVG требуют PNG/WebP `FirstFrame` из admin frontend. |
| `Resource.Update(ctx, params)` | `SaveParams{WorkspaceID, Key, Type, Payload, IsActive, File, FirstFrame}`. | Записывает media в новую immutable версию и обновляет metadata resource. |
| `Resource.Get(ctx, params)` | `GetParams{WorkspaceID, Key}`. | Возвращает resource через отдельный versioned cache. |
| `Resource.List(ctx, params)` | `ListParams{WorkspaceID, Limit, Offset}`. | Возвращает страницу не удалённых resources workspace. |
| `Resource.Delete(ctx, params)` | `GetParams{WorkspaceID, Key}`. | Soft-delete resource без удаления объектов storage. |
| `Resource.CollectGarbage(ctx, params)` | `CollectGarbageParams{Limit}`. | Через час удаляет retired media versions из storage, затем физически purges полностью удалённые resources; вызывается внутренним worker. |
| `Resource.InsertAfter(ctx, workspaceID, itemKey, resourceKey, afterResourceKey)` | workspace, keys; пустой `afterResourceKey` означает начало. | Рекомендуемый atomic API: привязывает ещё не привязанный resource и нормализует порядок `0..n-1`. |
| `Resource.MoveAfter(ctx, workspaceID, itemKey, resourceKey, afterResourceKey)` | workspace, keys; пустой `afterResourceKey` означает начало. | Рекомендуемый atomic API: перемещает привязанный resource и нормализует порядок `0..n-1`. |
| `Resource.Attach(ctx, workspaceID, itemKey, resourceKey, position)` | workspace, keys, position. | Legacy low-level attach; для упорядочивания используйте `InsertAfter` и `MoveAfter`. |
| `Resource.Detach(ctx, workspaceID, itemKey, resourceKey)` | workspace, keys. | Отвязывает resource от item. |
| `Resource.ListItemResources(ctx, workspaceID, itemKey)` | workspace, item key. | Возвращает resources, привязанные к item, в порядке position. |
| `Resource.GetContent(ctx, params)` | `ContentParams{WorkspaceID, Key, Version, Format, Size}`. | Возвращает bytes original (`Size=0`) или PNG preview (`61`, `128`, `256`, `512`) через bounded in-memory media cache. |

## Контракт импорта и архивов

Без `ConflictStrategy` используется `update_existing`. Импорт добавляет новые ключи и заменяет соответствующие существующие данные: `A B C` + импорт `B C D` → `A B C D`. Данные A остаются прежними, B/C берутся из пакета, D создаётся. Отсутствие ключа в пакете не удаляет его из workspace. Type импортируемого item может измениться без confirmation token; требование подтверждения относится к отдельному `DangerousChangeType`.

Вложенные localization импортируемого item заменяются целиком, включая пустой набор. При `IncludeMedia=true` так же заменяются его links; links items вне пакета сохраняются. Явный `skip_existing` сохраняет связи пропущенного item, а `fail_on_conflict` отклоняет конфликт атомарно. Media versions всегда создаются заново, прежние versions передаются GC по retention.

ZIP manifest собирается в одном PostgreSQL snapshot. Архивные операции используют отдельный длительный transaction budget (по умолчанию 15 минут), обычные операции — `QueryTimeout`. ZIP обрабатывается через защищённые временные файлы. Лимиты экспорта/импорта согласованы: 100 MiB ZIP, 512 MiB распакованных данных, 10 000 entries, 16 MiB manifest; структура manifest дополнительно ограничена перед typed decode. Превышение лимитов завершает job ошибкой, не создавая успешный непринимаемый архив.

`ArchiveJob` возвращает публичный статус без storage key, options, worker/lease token и raw SQL errors. Методы queue/status/history/download учитывают закрытие сервиса; download reader нужно закрывать. Уже committed import может завершить согласование job status по receipt без повторной загрузки source ZIP.

GC старой version не удаляет links активного resource. После физического удаления допускается выдавать ранее закэшированные bytes до истечения TTL. При неизвестном результате проверки commit media не удаляются компенсацией; требуется последующее согласование состояния. Ошибка invalidation кэша сообщается в `OnCacheInvalidationError` и не превращает committed mutation в API failure.
