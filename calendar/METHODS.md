# Calendar methods

Только методы слоев `user` и `admin`, которые можно использовать как основу будущего API.

## user

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `User.ListActive(ctx, params)` | `ListActiveParams{WorkspaceID, Locale, Now}`. | Возвращает активные календари рабочей области на момент `Now`. |
| `User.GetCalendar(ctx, params)` | `GetCalendarParams{Identity, Ref, Locale, Now}`; `Identity{WorkspaceID, AppID, PlatformID, Platform, PlatformUserID, IsPremium, Sex, Country}`. | Возвращает активный на момент `Now` календарь с локализацией, шагами и наградами для пользователя. `Ref` может быть UUID календаря или его `Type`. |
| `User.GetProgress(ctx, params)` | `GetProgressParams{Identity, CalendarID}`. | Возвращает прогресс пользователя по календарю. |
| `User.Record(ctx, params)` | `RecordParams{Identity, CalendarRef, OperationID, Now}`. | Идемпотентно фиксирует попытку получения награды, при доступности шага обновляет прогресс и создает callback `calendar.reward_granted`. `CalendarRef` может быть UUID календаря или его `Type`. |
| `User.Next(ctx, params)` | `NextParams{Identity, CalendarRef, Locale, Now}`. | Без записи в БД рассчитывает, что вернул бы `Record` на момент `Now`. Используется для предпросмотра доступности следующего шага; награду не выдает и callback не создает. |

## admin

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `Admin.CreateCalendar(ctx, params)` | `SaveCalendarParams{ID, WorkspaceID, Type, Mode, IntervalType, IntervalUnit, IntervalCount, ResetAfterIntervals, EndBehavior, Timezone, HideFutureRewards, IsActive, StartAt, EndAt}`. | Создает календарь; при пустом `ID` генерирует UUID. |
| `Admin.UpdateCalendar(ctx, params)` | `SaveCalendarParams` с обязательным `ID`. | Обновляет календарь. |
| `Admin.GetCalendar(ctx, workspaceID, id)` | `workspaceID`, `id`. | Возвращает календарь с локализациями, шагами и наградами. |
| `Admin.ListCalendars(ctx, workspaceID, page)` | `workspaceID`, `Page{Limit, Offset}`. | Возвращает список календарей. |
| `Admin.SetCalendarActive(ctx, workspaceID, id, active)` | `workspaceID`, `id`, `active bool`. | Включает или выключает календарь. |
| `Admin.DeleteCalendar(ctx, workspaceID, id)` | `workspaceID`, `id`. | Удаляет календарь. |
| `Admin.CreateStep(ctx, params)` | `SaveStepParams{WorkspaceID, CalendarID, Position}`. | Создает шаг календаря. |
| `Admin.UpdateStep(ctx, params)` | `SaveStepParams{WorkspaceID, CalendarID, ID, Position}`. | Обновляет шаг календаря. |
| `Admin.DeleteStep(ctx, workspaceID, calendarID, id)` | `workspaceID`, `calendarID`, `id`. | Удаляет шаг календаря. |
| `Admin.CreateReward(ctx, params)` | `SaveRewardParams{WorkspaceID, CalendarID, StepID, Key, Type, Quantity, Scale, Unit, Position}`. | Создает награду шага. `Scale` задает точность дробной валюты, например `25/scale=2` = `0.25`. |
| `Admin.UpdateReward(ctx, params)` | `SaveRewardParams` с обязательным `ID`. | Обновляет награду шага. |
| `Admin.GetReward(ctx, workspaceID, calendarID, id)` | `workspaceID`, `calendarID`, `id`. | Возвращает награду. |
| `Admin.DeleteReward(ctx, workspaceID, calendarID, id)` | `workspaceID`, `calendarID`, `id`. | Удаляет награду. |
| `Admin.QueueArchiveExport(ctx, params)` | `QueueArchiveExportParams{WorkspaceID, FileName, ExportRequest}`. | Ставит ZIP-экспорт в асинхронную очередь. |
| `Admin.QueueArchiveImport(ctx, params)` | `QueueArchiveImportParams{WorkspaceID, FileName, ImportRequest, Archive}`. | Ставит ZIP-импорт в асинхронную очередь; асинхронный импорт не предоставляет `PreviewImport`. |
| `Admin.ArchiveJob(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Возвращает текущий статус задачи. |
| `Admin.ArchiveHistory(ctx, workspaceID, page)` | `workspaceID`, `Page`. | Возвращает историю задач workspace. |
| `Admin.DownloadArchive(ctx, workspaceID, id)` | `workspaceID`, job `id`. | Скачивает результат завершенного export job. |
| `Admin.ArchiveJobHistory(ctx, workspaceID, id, page)` | `workspaceID`, job `id`, `Page`. | Возвращает историю состояний задачи. |
| `Admin.UpsertLocalization(ctx, params)` | `SaveLocalizationParams{WorkspaceID, CalendarID, Locale, Title, Description}`. | Создает или обновляет локализацию календаря. |
| `Admin.GetLocalization(ctx, workspaceID, calendarID, locale)` | `workspaceID`, `calendarID`, `locale`. | Возвращает локализацию. |
| `Admin.ListLocalizations(ctx, workspaceID, calendarID)` | `workspaceID`, `calendarID`. | Возвращает локализации календаря. |
| `Admin.DeleteLocalization(ctx, workspaceID, calendarID, locale)` | `workspaceID`, `calendarID`, `locale`. | Удаляет локализацию. |
| `Admin.ListOperations(ctx, workspaceID, calendarID, page)` | `workspaceID`, `calendarID`, `Page`. | Возвращает журнал операций календаря. |
| `Admin.GetStats(ctx, workspaceID, calendarID)` | `workspaceID`, `calendarID`. | Возвращает агрегированную статистику календаря. |
| `Admin.ListDailyStats(ctx, workspaceID, calendarID, from, until)` | `workspaceID`, `calendarID`, `from`, `until`. | Возвращает дневную статистику за период. |
| `Admin.RefreshDailyStats(ctx, workspaceID, from, until)` | `workspaceID`, `from`, `until`. | Пересчитывает дневную статистику только указанной workspace. |
| `Admin.ListCallbackEvents(ctx, params)` | `CallbackEventListParams{WorkspaceID, Status, Page}`. | Возвращает callback-события календаря только указанной workspace. |
| `Admin.GetCallbackEvent(ctx, workspaceID, id)` | `workspaceID`, `id`. | Возвращает callback-событие указанной workspace. |
| `Admin.RetryCallbackEventNow(ctx, workspaceID, id)` | `workspaceID`, `id`. | Отправляет callback-событие workspace на повторную обработку. |
| `Admin.MarkCallbackEventOK(ctx, workspaceID, id)` | `workspaceID`, `id`. | Помечает callback-событие workspace успешным. |
| `Admin.MarkCallbackEventReject(ctx, workspaceID, id, reason)` | `workspaceID`, `id`, `reason`. | Помечает callback-событие workspace отклоненным. |
| `Admin.ResetExpiredCallbackProcessing(ctx, workspaceID)` | `workspaceID`. | Сбрасывает зависшие callback-события workspace. |
