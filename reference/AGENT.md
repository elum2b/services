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

## Resources, cache и export

- `Get`, `Resolve` и `List` имеют отдельные versioned cache scopes.
- Mutation item/localization/soft-delete/restore/type bump-ает все реально
  затронутые scopes workspace после commit.
- Reference — единственный сервис, чей export/import содержит полный `items`
  catalog и localization.
- Import пишет items раньше localization; `update_existing` не оставляет
  устаревшие вложенные localization.
- Resource media имеет immutable `media_version` из восьми ASCII букв. Create и
  update пишут новый version prefix; старые и soft-deleted versions остаются
  доступными до policy-based garbage collection.
- Media bytes cache находится в `service/resource/cache`, ограничен числом
  entries и bytes, использует LRU/TTL и coalesces concurrent miss одного
  versioned variant. Cache key обязательно содержит workspace, resource key,
  media version и variant.
- `Resource.GetContent` возвращает bytes и content type, а transport сам
  формирует REST/WebSocket response. Original запрашивается с `Size=0`, PNG
  previews доступны только в размерах `61`, `128`, `256`, `512`.
- TGS хранится как исходный gzip, но проверяется и рендерится после
  ограниченной распаковки как Lottie JSON. SVG хранит только original и
  generated placeholder, без raster preview variants.
- Backend не рендерит vector/animated media. Admin frontend передаёт
  проверенный PNG/WebP `FirstFrame`; backend валидирует original Lottie/TGS,
  ограниченно проверяет Rive container и генерирует delivery previews и
  placeholder только из `FirstFrame`.
- Resource export/import в текущем виде не расширяется. Целевой контракт —
  asynchronous ZIP dump job с постепенной подготовкой и download retention 24
  hours; этот workflow не смешивается с текущим синхронным import/export API.
- GC удаляет только unreferenced immutable media version prefixes старше 24
  hours, ограниченным batch за запуск. Update и soft-delete никогда не
  удаляют storage synchronously.

## Обязательные тесты

- Каждый User/Admin метод с valid/invalid params.
- Resolve 0/1/100+ keys, порядок, duplicates, missing и locale fallback.
- Soft delete/restore и подтверждённая/неподтверждённая type change.
- Workspace isolation и cache invalidation на двух нодах.
- Import/export round trip, большой пакет и concurrent admin mutation.
- Resource create/update/delete, links, historical version reads, bounded media
  cache, media processing and batch GC.

```bash
go test ./reference
go test -run '^$' -bench . -benchmem ./reference
```
