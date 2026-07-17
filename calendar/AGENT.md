# Calendar: правила сервиса

Наследует корневой `../AGENT.md`. Публичный API перечислен в `METHODS.md`.

## Назначение и слои

- Calendar владеет календарями активности, шагами, прогрессом пользователя,
  операциями, наградами и своей статистикой.
- Публичные слои: `service/user` и `service/admin`.
- Выдача награды создаёт callback через calendar outbox; transport callback
  остаётся вне сервиса.

## Доменные инварианты

- Позиция шага уникальна внутри календаря и определяет порядок прохождения.
- Тип, mode, interval, reset и end behavior являются typed enum и
  валидируются до SQL.
- Временные вычисления выполняются в указанной timezone календаря, но моменты
  в БД и публичных моделях хранятся как UTC.
- `Record` и `Next` идемпотентны по identity и `OperationID`.
- Конкурентные `Record`/`Next` не могут дважды пройти шаг, выдать награду или
  перескочить через недоступный шаг.
- Награда шага фиксируется в snapshot операции. Изменение или удаление
  reward после записи не меняет повторный ответ и callback.
- Target влияет на отображение календаря, но не переписывает созданный
  прогресс.
- `HideFutureRewards` скрывает только публичное представление; repository всё
  равно сохраняет корректные reward definitions и snapshots.

## Repository, cache и import/export

- `ListActive` и `GetCalendar` используют versioned catalog cache отдельно по
  методу и workspace.
- Любая mutation календаря, localization, step или reward bump-ает только
  затронутые calendar scopes после commit.
- Import remap-ит ID нового календаря и сохраняет связи `calendar -> step ->
  reward` в одной transaction.
- Export читает calendar, localization, steps и rewards из одного
  `REPEATABLE READ READ ONLY` snapshot.
- Daily stats scoped по workspace/calendar и пересчитываются только в явно
  переданном диапазоне.

## Обязательные тесты

- Все методы из `METHODS.md`: valid и invalid input.
- Порядок шагов, reset window, end behavior и timezone boundary.
- Одновременный `Record` одного шага и exactly-once callback.
- Reward snapshot после admin update/delete.
- Две cache-ноды, import/export round trip, большой import и конкурентная
  catalog mutation.

```bash
go test ./calendar
go test -run '^$' -bench . -benchmem ./calendar
```
