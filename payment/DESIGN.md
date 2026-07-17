# Payment Service Design

## Цель

Сервис платежей должен хранить один каталог товаров и один жизненный цикл покупки для разных способов оплаты:

- VK Mini Apps / Direct Games: `VOTE`
- Telegram Bot API: `XTR` / Stars
- TON: native `TON`
- TON Jettons: например `USDT_TON`, `MEMCOIN_TON` и любые jettons из TON
- YooKassa: `RUB`
- Platega: `RUB`, а позже и другие поддерживаемые методы

Главная граница: каталог и выдача товара не должны зависеть от провайдера. Провайдеры создают или подтверждают платежные попытки, а продукт выдается только после атомарного перевода бизнес-заказа в оплаченный статус.

## Принципы провайдеров

### VKMA / VOTE

- Клиент открывает окно покупки через VK Bridge `VKWebAppShowOrderBox`.
- Сервер обязан отвечать на `get_item`, возвращая товар, цену в голосах и метаданные.
- Выдача товара допустима только на серверном `order_status_change` со статусом успешного списания (`chargeable`).
- Внешние ключи идемпотентности: `order_id`, `subscription_id`, `item`, `user_id`, `app_id`.
- Подписки и возвраты приходят отдельными статусами, поэтому возвраты и отмены должны быть отдельными событиями, а не перезаписью заказа без истории.

### Telegram Stars / XTR

- Для цифровых товаров внутри Telegram используется только Stars с валютой `XTR`.
- Инвойс может быть многоразовым, поэтому payload не должен сам по себе означать уникальную покупку.
- `pre_checkout_query` нужно быстро подтвердить или отклонить, но это не доказательство оплаты.
- Выдача товара допустима только после `successful_payment`.
- Нужно хранить `telegram_payment_charge_id`, потому что он нужен для возврата через Bot API.

### TON / TON

- Создается локальный ожидающий заказ с суммой в nanotons и уникальным payload/comment/query id.
- Кошелек пользователя подписывает и отправляет транзакцию через TON Connect.
- Возвращенный BoC не является достаточным доказательством оплаты: backend должен найти транзакцию on-chain.
- Подтверждение должно сверять recipient, sender при наличии, amount, asset, payload/order id, network и успешность без bounce.

### TON Jettons

- Jetton переводится через jetton wallet пользователя по TEP-74, а не как простой TON transfer.
- Хранить нужно не только ticker, но и `jetton_master_address`, decimals и network.
- `scale` в `payment_asset` равен decimals конкретного jetton master: например, TON default часто `9`, а USDT в TON обычно `6`.
- Подтверждение должно сверять jetton master, sender wallet, recipient/merchant wallet, amount в elementary units и payload/query id.
- Для jettons часто возникает несколько internal messages, поэтому учет должен опираться на разобранное on-chain событие, а не только на одну внешнюю транзакцию кошелька.

### YooKassa / RUB

- Платеж создается только с сервера.
- При создании передается сумма, валюта, `capture`, confirmation/return URL и idempotence key.
- Пользователь уходит на `confirmation_url`; `return_url` не является подтверждением оплаты.
- Успех наступает только после статуса `succeeded` из webhook или после серверной проверки платежа.
- Перед выдачей нужно сверить `payment.id`, status, paid, amount, currency и локальный order metadata.

### Platega / RUB

- Платеж создается серверным запросом с merchant headers и возвращает transaction id, статус, payment URL и TTL.
- Callback отправляет статус транзакции: `CONFIRMED`, `CANCELED`, `CHARGEBACKED`.
- Callback нужно проверять по merchant headers/секрету, сохранять raw payload и обрабатывать идемпотентно.
- Перед выдачей нужно сверить transaction id, amount, currency и локальный order payload.

## Модель данных

### 1. Catalog

`payment_product_group`, `payment_product`, `payment_product_item`, `payment_localization`, `payment_price`.

`payment_product_item.item_id` хранит непрозрачный ключ. Метаданными item владеет
сервис `reference`; payment не дублирует название, описание, редкость или тип.

Этот слой сохраняет текущий функционал: группы, товары, предметы выдачи, локализация, периоды доступности, скидки и цены в разных активах. Суммы всегда хранятся в minor units:

- `RUB`: копейки, `scale = 2`
- `TON`: nanotons, `scale = 9`
- `XTR` и `VOTE`: целые единицы, `scale = 0`
- TON jetton: elementary units по decimals конкретного jetton master, например `USDT_TON scale = 6`, `MEMCOIN_TON scale = 9`

### 2. Order

`payment_order` - бизнес-заказ на покупку конкретного продукта конкретным пользователем. В заказ копируется снимок цены: `list_amount_minor`, `discount_amount_minor`, `payable_amount_minor`, `price_id`, `asset_code`, `locale`.

Заказ не равен платежу. Один заказ может иметь несколько `payment_attempt`: пользователь мог открыть новую ссылку, платеж мог истечь, провайдер мог вернуть другой transaction id.

### 2.1 Purchase Key

`payment_purchase_key` позволяет оплатить товар для другого пользователя без раскрытия его id покупателю. Сервис или владелец создает одноразовый/многоразовый ключ, в базе хранится только `key_hash`, а ключ указывает на скрытого получателя и product.

Публичный метод `Product.GetByKey` возвращает только информацию о товаре и лимитах. Лимиты считаются по получателю ключа, а не по покупателю. Создание order по такому ключу позже должно сохранить buyer отдельно от beneficiary.

### 3. Attempt

`payment_attempt` - конкретная попытка оплаты у конкретного провайдера. Здесь живут provider payment id, invoice id, charge id, idempotency key, confirmation URL, expiration, raw request/response.

Уникальность:

- `idempotency_key` уникален глобально.
- `(provider_code, provider_payment_id)` уникален, когда provider id известен.
- `(provider_code, provider_charge_id)` уникален, когда charge/refund id используется провайдером.

### 4. Event

`payment_event` - сырые webhook/update/on-chain события. Событие сохраняется до бизнес-логики, чтобы можно было безопасно ретраить обработку и разбирать спорные кейсы.

### 5. Fulfillment

`payment_fulfillment` и `payment_fulfillment_item` фиксируют выдачу товара. Лимиты покупок должны считать только успешно оплаченные/выданные заказы, а не просто созданные pending-заказы. Если нужен anti-oversell для жестко лимитированных товаров, pending-заказы можно учитывать только до `reserved_until`.

### 6. Refund

`payment_refund` хранит инициированные возвраты и внешний `idempotency_key`.
Один ключ всегда соответствует одной операции и одному provider idempotency
key; неоднозначная ошибка провайдера сохраняет `pending` для безопасного retry.
Возврат не удаляет платеж и не
затирает выдачу, а создаёт отдельную запись и переводит order/fulfillment в
обратимый статус. Provider chargeback хранится как raw provider event и
`chargebacked`-состояние order/attempt; для внешнего владельца награды создаётся
идемпотентный callback `payment.order.chargebacked` со снимком выдачи.

`payment_subscription_renewal` хранит каждый подтверждённый оплаченный период
подписки. Renewal использует snapshot исходного fulfillment, продлевает одну
provider subscription и создаёт `payment.subscription.renewed`; исходный order
и fulfillment при этом не создаются повторно.

## Статусы

Order:

- `draft` - локально подготовлен, платеж еще не создан
- `pending_payment` - платежная попытка создана, ожидается оплата
- `paid` - деньги подтверждены, выдача еще не завершена
- `fulfilled` - товар выдан
- `canceled` - заказ отменен до оплаты
- `expired` - время оплаты истекло
- `refunded` - выполнен возврат
- `chargebacked` - провайдер сообщил chargeback
- `failed` - ошибка, требующая ручной диагностики или новой попытки

Attempt:

- `created`
- `pending`
- `requires_action`
- `waiting_capture`
- `succeeded`
- `canceled`
- `expired`
- `refunded`
- `chargebacked`
- `failed`

Fulfillment:

- `pending`
- `succeeded`
- `revoked`
- `failed`

## Проверка лимитов

Лимиты должны считаться по `payment_order` в состояниях `paid`, `fulfilled`, `refunded`, `chargebacked` в зависимости от бизнес-правила:

- Для запрета повторной покупки обычно считать `paid` и `fulfilled`.
- Для невозвратных расходуемых товаров возврат может не уменьшать лимит.
- Для подписок/премиума возврат или отмена может отзывать entitlement, но история покупки остается.

Для временных окон нужен единый расчет по `paid_at` или `fulfilled_at`, а не по `created_at`, иначе неоплаченные pending-заказы будут расходовать лимиты.

## Обработка успешного платежа

1. Сохранить raw event в `payment_event`.
2. Найти `payment_attempt` по provider id или payload/order id.
3. В транзакции заблокировать order и attempt.
4. Проверить, что сумма, asset, provider, product и user совпадают со snapshot заказа.
5. Перевести attempt в `succeeded`.
6. Перевести order из `pending_payment` в `paid`, если он еще не был оплачен.
7. Создать fulfillment и выдать items.
8. Перевести order в `fulfilled`.
9. Повторное событие должно вернуть уже созданный результат без повторной выдачи.

## Что переносим из старой схемы

- `product`, `price`, `item`, `item_link`, `localization`, `product_cache` идеи остаются.
- `order` делится на `payment_order` и `payment_attempt`.
- `charge_id`, `order`, `subscription_id`, `data` переезжают в provider-specific поля и JSON metadata.
- `UPDATED_BALANCE` становится fulfillment-слоем: выдача товаров должна быть идемпотентной и привязанной к `payment_fulfillment`.

## Использованные источники

- Telegram Stars: https://core.telegram.org/bots/payments-stars
- TON Connect transactions: https://docs.ton.org/applications/ton-connect/how-to/send-transaction
- TON asset processing: https://docs.ton.org/develop/dapps/asset-processing/
- YooKassa quick start: https://yookassa.ru/developers/payment-acceptance/getting-started/quick-start
- YooKassa webhooks: https://yookassa.ru/developers/using-api/webhooks
- Platega API: https://docs.platega.io/
- Platega transaction callback: https://docs.platega.io/callback-%D0%BE%D0%B1-%D0%B8%D0%B7%D0%BC%D0%B5%D0%BD%D0%B5%D0%BD%D0%B8%D0%B8-%D1%81%D1%82%D0%B0%D1%82%D1%83%D1%81%D0%B0-%D1%82%D1%80%D0%B0%D0%BD%D0%B7%D0%B0%D0%BA%D1%86%D0%B8%D0%B8-29209725e0
- VK Bridge order box reference: https://github.com/VKCOM/vk-mini-apps-api/blob/master/docs/classes/vkminiappapi.md
