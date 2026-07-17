# Calendar methods

Только методы слоев `user` и `admin`, которые можно использовать как основу будущего API.

## user

| Метод | Что принимаем | Что делает |
| --- | --- | --- |
| `User.ListActive(ctx, params)` | `ListActiveParams{WorkspaceID, Locale, Now}`. | Возвращает активные календари рабочей области на момент `Now`. |
| `User.GetCalendar(ctx, params)` | `GetCalendarParams{Identity, Ref, Locale}`; `Identity{WorkspaceID, AppID, PlatformID, Platform, PlatformUserID, IsPremium, Sex, Country}`. | Возвращает календарь с локализацией, шагами и наградами для пользователя. |
| `User.GetProgress(ctx, params)` | `GetProgressParams{Identity, CalendarID}`. | Возвращает прогресс пользователя по календарю. |
| `User.Record(ctx, params)` | `RecordParams{Identity, CalendarID, OperationID, Now}`. | Фиксирует операцию пользователя, обновляет прогресс и возвращает результат выдачи награды. |
| `User.Next(ctx, params)` | `NextParams{Identity, CalendarID, OperationID, Now}`. | Записывает переход пользователя к следующему доступному шагу календаря. |

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
| `Admin.Export(ctx, workspaceID, req)` | `workspaceID`, `ExportRequest{Now}`. | Экспортирует календари workspace в `calendar.export.v1`: определения, локализации, шаги и награды. |
| `Admin.PreviewImport(ctx, workspaceID, pkg)` | `workspaceID`, `ExportPackage`. | Проверяет пакет импорта, считает элементы и возвращает конфликты по `calendar.Type` без записи данных. |
| `Admin.Import(ctx, workspaceID, req)` | `ImportRequest{Package, ConflictStrategy}`; стратегии `fail_on_conflict`, `skip_existing`, `update_existing`. | Импортирует календари в workspace пачками в транзакции; при новом workspace генерирует новые `calendarID` и переносит связи шагов/наград. |
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
