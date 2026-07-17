# Reference: правила сервиса

Наследует корневой `../AGENT.md`. Публичный API перечислен в `METHODS.md`.

## Назначение

- Reference — единственный источник истины для item key, type, payload,
  active/deleted state и localization.
- Остальные сервисы не создают item через Reference автоматически и не имеют
  DB-связей с его таблицами. Внешнее приложение явно разрешает keys через
  User API Reference.

## Доменные инварианты

- Item key стабилен внутри workspace и не переиспользуется для другого смысла.
- Обычный update не меняет type. Опасная смена type доступна только отдельным
  методом с точным confirmation token.
- Удаление soft: исторические reward/order/task snapshots продолжают хранить
  key и разрешаются согласно явно выбранному read contract.
- Restore не создаёт новый item и сохраняет identity исходного key.
- `Get`, `Resolve` и `List` одинаково применяют active/deleted и locale rules.
- `Resolve` сохраняет порядок входных keys, корректно обрабатывает duplicates
  по зафиксированному контракту и не выполняет N+1.
- Localization fallback детерминирован; отсутствие перевода не должно
  подмешивать данные другой workspace.

## Cache и import/export

- `Get`, `Resolve` и `List` имеют отдельные versioned cache scopes.
- Mutation item/localization/soft-delete/restore/type bump-ает все реально
  затронутые scopes workspace после commit.
- Reference — единственный сервис, чей export/import содержит полный `items`
  catalog и localization.
- Import пишет items раньше localization; `update_existing` не оставляет
  устаревшие вложенные localization.

## Обязательные тесты

- Каждый User/Admin метод с valid/invalid params.
- Resolve 0/1/100+ keys, порядок, duplicates, missing и locale fallback.
- Soft delete/restore и подтверждённая/неподтверждённая type change.
- Workspace isolation и cache invalidation на двух нодах.
- Import/export round trip, большой пакет и concurrent admin mutation.

```bash
go test ./reference
go test -run '^$' -bench . -benchmem ./reference
```
