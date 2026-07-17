# CPA: правила сервиса

Наследует корневой `../AGENT.md`. Методы перечислены в `METHODS.md`.

## Назначение

- CPA владеет офферами, localization, target, reward definitions, shared или
  personal codes, assignments, completion events, statistics и callback.
- `User.ListActive` показывает доступный каталог; `GetCode` выдаёт либо
  возвращает стабильное назначение; `GetStatus` читает его состояние.
- Admin управляет каталогом, пулами кодов, assignments, статистикой и
  import/export.

## Доменные инварианты

- `Identity.Validate` обязателен до user repository operation.
- Один пользователь получает не более одного assignment на оффер. Повторный
  `GetCode` возвращает тот же code и reward snapshot.
- Personal code назначается атомарно и не может принадлежать двум identity.
- Shared code обязателен только для соответствующего code mode; generated и
  imported pools валидируют длину, alphabet и uniqueness.
- Удаление issued/completed code row не удаляет assignment и не разрешает
  повторную выдачу. Исторический code и выданная награда остаются доступны.
- Completion идемпотентен, использует отдельный event type/status contract и
  создаёт reward callback ровно один раз.
- Assignment хранит immutable reward snapshot. Текущий reward catalog не
  используется в повторном status/completion response.
- Lifetime/global limits и выдача кода защищены constraint/lock, но разные
  пользователи одного оффера не сериализуются общей блокировкой без причины.

## Cache и import/export

- Active catalog, offer bundle и связанные reads имеют независимые versioned
  workspace scopes.
- Mutation offer/localization/reward/code catalog bump-ает затронутые версии
  после commit; invalidation error отправляется в diagnostic callback.
- Export не содержит `items`: `reward.key` является непрозрачной ссылкой на
  Reference.
- Export читает полный каталог без pagination limit и в одном consistent
  snapshot.
- Import preflight сообщает `offers[index].field`, пишет bulk batches и при
  `update_existing` полностью заменяет localization/reward/code snapshot.

## Обязательные тесты

- Каждый public method с valid/invalid params и typed errors.
- Issue -> delete code row -> повторный issue возвращает прежнее назначение.
- Concurrent issue, pool exhaustion, limits и exactly-once completion.
- Reward snapshot после изменения каталога.
- Export/import 1001+ offers и выше PostgreSQL parameter limit.
- Две cache-ноды с общим L2 и независимыми L1.

```bash
go test ./cpa
go test -run '^$' -bench . -benchmem ./cpa
```
