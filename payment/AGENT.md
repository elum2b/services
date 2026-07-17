# Payment: правила сервиса

Наследует корневой `../AGENT.md`. Модель платежей описана в `DESIGN.md`, методы
— в `METHODS.md`, внешний контракт — в `README.md`.

## Назначение и слои

- Payment владеет product catalog, groups, prices, product-item relations,
  orders, attempts, provider events, fulfillments, subscriptions, refunds,
  rates, merchant wallets, statistics и callback.
- `User` предоставляет каталог и создание order/attempt.
- `Admin` управляет каталогом и аудитом платежных сущностей.
- `Operational` обрабатывает доверенные provider lifecycle события; это не
  admin access.
- Provider adapters находятся в `adapters/<provider>` и скрывают особенности
  API, подписи и payload конкретной платформы.

## Деньги и каталог

- `AmountMinor`, `Quantity`, `Scale`, rates и discounts используют integer или
  точный decimal contract, никогда float.
- Asset code и provider code typed/normalized единообразно на всех слоях.
- Product хранит только item key и параметры выдачи; метаданными item владеет
  Reference.
- Flexible quantity проверяется до reserve/limit calculation. Нулевое и
  отрицательное количество запрещено.
- Limits проверяются и резервируются атомарно; expired/cancelled order
  освобождает только собственный reserve.

## Order, attempt и fulfillment

- При создании order фиксируется immutable snapshot продукта, item-ов, цены,
  asset, quantity, scale, скидки и limit config, влияющего на completion.
- Admin update каталога не меняет созданный order, callback или fulfillment.
- Provider event и attempt имеют стабильный idempotency key/external ID.
  Повтор webhook не создаёт второй fulfillment/refund/callback.
- `CompleteAttempt` строит результат и callback из order snapshot и выполняет
  state transition атомарно.
- Нельзя принимать merchant TON wallet от клиента при создании order. Wallet
  выбирается по workspace из Payment config.
- На workspace существует одна актуальная TON wallet configuration независимо
  от network; subscriber запускается/останавливается при синхронизации БД.
- Любой поддерживаемый TON address нормализуется из raw/user-friendly формы до
  единого внутреннего представления.
- Provider HTTP, blockchain RPC и price updater не выполняются внутри DB
  transaction.

## Cache, import/export и фоновые процессы

- Product catalog, completion config и rates кэшируются отдельными versioned
  scopes. Catalog mutation bump-ает только связанные scopes после commit.
- Cache completion config не заменяет order snapshot и не является источником
  исторической истины.
- Payment export содержит groups, products, localization, target,
  product-item keys, prices и merchant wallets, но не Reference item catalog.
- Secrets/private provider credentials не экспортируются без явного флага.
- Price updater, TON subscribers и callback workers запускаются через общий
  goroutine manager, защищены от panic и завершаются graceful.
- Live blockchain/provider tests не входят в обычный test suite.

## Обязательные тесты

- Каждый public method: valid/invalid, workspace isolation и typed errors.
- CreateOrder: fixed/flexible quantity, limits, expiration и snapshot.
- CompleteAttempt: success, duplicate event, invalid transition, callback и
  изменение каталога после order.
- Refund/subscription/provider event idempotency.
- TON address formats и wallet synchronization.
- Две cache-ноды, import/export round trip и concurrent catalog mutation.

```bash
go test ./payment
go test -run '^$' -bench . -benchmem ./payment
```
