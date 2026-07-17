# Promo: правила сервиса

Наследует корневой `../AGENT.md`. Публичный API перечислен в `METHODS.md`.

## Назначение

- Promo владеет промокодами, localization, target, rewards, redemptions,
  лимитами, audit events, daily statistics и callback.
- User применяет code; Admin управляет catalog, rewards, аудитом, статистикой
  и import/export.

## Доменные инварианты

- Code нормализуется одним общим алгоритмом до lookup, uniqueness и import
  conflict check. Исходная и normalized формы не должны расходиться между
  слоями.
- Один пользователь не может активировать одну и ту же promo повторно, если
  доменный режим явно не разрешает иное.
- Global, per-user и time-window limits проверяются атомарно под lock/constraint.
- Successful Apply создаёт redemption, reward snapshot, stats/raw event и
  callback в одной transaction.
- Повторный Apply возвращает исходный redemption reward snapshot, даже если
  Admin уже изменил или удалил текущие rewards.
- Target определяет видимость/доступность до первой активации и не меняет
  исторический redemption.
- `start_at`, `end_at` и status проверяются на одном `Now`, переданном в
  transaction path.

## Cache и import/export

- Catalog lookup/list cache разделён по методу и workspace version.
- Mutation promo/localization/reward bump-ает связанные scopes после commit.
- Export не содержит item metadata; reward key разрешается через Reference во
  внешнем приложении.
- `update_existing` полностью заменяет вложенные localization/rewards, а не
  смешивает старый и импортированный catalog.
- Export и conflict preview соблюдают общий consistent snapshot/advisory lock
  contract.

## Обязательные тесты

- Apply success, duplicate, inactive/expired, target mismatch и каждый limit.
- Concurrent last-use и exactly-once callback/statistics.
- Reward snapshot после admin update/delete.
- Valid/invalid тест каждого Admin метода.
- Две cache-ноды, большой import, conflict strategies и export consistency.

```bash
go test ./promo
go test -run '^$' -bench . -benchmem ./promo
```
