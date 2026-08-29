# Promo methods

Только методы слоев `user` и `admin`, которые можно использовать как основу будущего API.

## user

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `User.Apply(ctx, params)` | `ApplyParams{Identity, Code, Now}`; `Identity{WorkspaceID, AppID, PlatformID, Platform, PlatformUserID, IsPremium, Sex, Country}`. | Активирует промокод для пользователя и возвращает промо, redemption и признак повторной активации. |

## admin

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `Admin.CreatePromo(ctx, params)` | `SavePromoParams{ID, WorkspaceID, Code, Payload, MaxActivations, IsActive, StartAt, EndAt}`. | Создает промокод. |
| `Admin.UpdatePromo(ctx, params)` | `SavePromoParams` с обязательным `ID`. | Обновляет промокод. |
| `Admin.GetPromo(ctx, workspaceID, id)` | `workspaceID`, `id`. | Возвращает промокод с локализациями и наградами. |
| `Admin.ListPromos(ctx, workspaceID, page)` | `workspaceID`, `Page{Limit, Offset}`. | Возвращает список промокодов. |
| `Admin.DeletePromo(ctx, workspaceID, id)` | `workspaceID`, `id`. | Удаляет промокод. |
| `Admin.UpsertLocalization(ctx, params)` | `SaveLocalizationParams{WorkspaceID, PromoID, Locale, Title, Description}`. | Создает или обновляет локализацию промокода. |
| `Admin.GetLocalization(ctx, workspaceID, promoID, locale)` | `workspaceID`, `promoID`, `locale`. | Возвращает локализацию. |
| `Admin.ListLocalizations(ctx, workspaceID, promoID)` | `workspaceID`, `promoID`. | Возвращает локализации промокода. |
| `Admin.DeleteLocalization(ctx, workspaceID, promoID, locale)` | `workspaceID`, `promoID`, `locale`. | Удаляет локализацию. |
| `Admin.UpsertReward(ctx, params)` | `SaveRewardParams{WorkspaceID, PromoID, Key, Type, Quantity, Scale, Unit}`. | Создает или обновляет награду промокода. `Scale` задает точность дробной валюты, например `25/scale=2` = `0.25`. |
| `Admin.GetReward(ctx, workspaceID, promoID, key)` | `workspaceID`, `promoID`, `key`. | Возвращает награду. |
| `Admin.ListRewards(ctx, workspaceID, promoID)` | `workspaceID`, `promoID`. | Возвращает награды промокода. |
| `Admin.DeleteReward(ctx, workspaceID, promoID, key)` | `workspaceID`, `promoID`, `key`. | Удаляет награду. |
| `Admin.QueueArchiveExport(ctx, params)` | `QueueArchiveExportParams{WorkspaceID, FileName, ExportRequest}`. | Ставит ZIP-экспорт в асинхронную очередь. |
| `Admin.QueueArchiveImport(ctx, params)` | `QueueArchiveImportParams{WorkspaceID, FileName, ImportRequest, Archive}`. | Ставит ZIP-импорт в асинхронную очередь; асинхронный импорт не предоставляет `PreviewImport`. |
| `Admin.ArchiveJob(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Возвращает текущий статус задачи. |
| `Admin.ArchiveHistory(ctx, workspaceID, page)` | `workspaceID`, `Page`. | Возвращает историю задач workspace. |
| `Admin.DownloadArchive(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Скачивает результат завершенного export job. |
| `Admin.ArchiveJobHistory(ctx, workspaceID, id, page)` | `workspaceID`, job `id`, `Page`. | Возвращает историю состояний задачи. |
| `Admin.GetStats(ctx, workspaceID, promoID)` | `workspaceID`, `promoID`. | Возвращает статистику активаций промокода. |
| `Admin.GetUserRedemption(ctx, identity, promoID)` | `Identity`, `promoID`. | Возвращает активацию промокода конкретным пользователем. |
| `Admin.ListRedemptions(ctx, workspaceID, promoID, page)` | `workspaceID`, `promoID`, `Page`. | Возвращает список активаций. |
| `Admin.ListDailyStats(ctx, workspaceID, promoID, from, until)` | `workspaceID`, `promoID`, `from`, `until`. | Возвращает дневную статистику промокода. |
| `Admin.RefreshDailyStats(ctx, workspaceID, from, until)` | `workspaceID`, `from`, `until`. | Пересчитывает дневную статистику только указанной workspace. |
| `Admin.ListCallbackEvents(ctx, params)` | `CallbackEventListParams{WorkspaceID, Status, Page}`. | Возвращает callback-события promo только указанной workspace. |
| `Admin.GetCallbackEvent(ctx, workspaceID, id)` | `workspaceID`, `id`. | Возвращает callback-событие указанной workspace. |
| `Admin.RetryCallbackEventNow(ctx, workspaceID, id)` | `workspaceID`, `id`. | Отправляет callback-событие workspace на повторную обработку. |
| `Admin.MarkCallbackEventOK(ctx, workspaceID, id)` | `workspaceID`, `id`. | Помечает callback-событие workspace успешным. |
| `Admin.MarkCallbackEventReject(ctx, workspaceID, id, reason)` | `workspaceID`, `id`, `reason`. | Помечает callback-событие workspace отклоненным. |
| `Admin.ResetExpiredCallbackProcessing(ctx, workspaceID)` | `workspaceID`. | Сбрасывает зависшие callback-события workspace. |
