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
| `Admin.Export(ctx, workspaceID, req)` | `workspaceID`, `ExportRequest{Now, OnlyNotDeleted}`. | Экспортирует справочник workspace в `reference.export.v1`: items, payload, активность, признак удаления и локализации. |
| `Admin.PreviewImport(ctx, workspaceID, pkg)` | `workspaceID`, `ExportPackage`. | Проверяет пакет импорта, считает items/localizations и возвращает конфликты по `item.Key` без записи данных. |
| `Admin.Import(ctx, workspaceID, req)` | `ImportRequest{Package, ConflictStrategy}`; стратегии `fail_on_conflict`, `skip_existing`, `update_existing`. | Импортирует справочник пачками в транзакции: сначала items, затем localizations, после чего обновляет версии кеша reference. |
| `Admin.GetStats(ctx, workspaceID)` | `workspaceID`. | Возвращает статистику справочника. |
