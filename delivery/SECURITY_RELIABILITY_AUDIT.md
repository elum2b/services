# Аудит Delivery — 2026-09-05

Проверены resolver/delivery deadline, HTTPS URL validation, DNS resolution/dial, HMAC, envelope bounds, response contract и callback marker mapping. Новых подтверждённых дефектов в проверенных путях не найдено; код сервиса не менялся.

Повторно подтверждены барьеры: HTTPS без userinfo/redirect/proxy, проверка всех resolved IP и dial уже проверенного IP, запрет private/reserved addresses, TLS >= 1.2, bounded JSON payload/envelope/response, общий deadline включает resolver, HMAC подписывает точные байты envelope, verification использует constant-time hmac.Equal. Network/timeout failures остаются retryable; отмена callback следует явному CANCELED.

Регрессионные тесты: TestDeliverSignsEnvelopeBody, TestDeliveryResponseContract, TestDeliveryTimeoutCoversResolver, TestSecureDialRejectsReservedAddressesAndPinsDNS, TestPublicAddressBlacklist, TestDeliveryValidationLimits, TestDeliveryRejectsOversizedResponse и marker/network tests.

Получатель обязан проверять подпись и обеспечивать idempotency по ключу: исходящие callback могут повторяться при неопределённом сетевом результате. Проверка внешнего получателя/его replay policy не входит в локальные тесты. Запросы к production endpoints не выполнялись.
