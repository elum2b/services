# Аудит Control — 2026-09-05

## Исправлено

**Высокая: восстановление старого MCP-токена после удаления участника.** RemovePlatformMember отзывал sessions/challenges, но не control_mcp_token. ValidateAndTouchMCPToken блокирует removed account/member, однако после приглашения и активации старый токен снова проходит проверку. CreateMCPToken уже использует account-authentication lock, поэтому добавлен RevokeAllMCPTokens в ту же транзакцию удаления под тем же lock. Тест TestControlPlatformRemovalRevokesSessionsAndDefersTwoFactorReactivation проверяет отзыв после полного цикла с 2FA и persisted revoked_at.

**Средняя: транзакции обходили QueryTimeout.** Queries вне транзакции используют timeout wrapper, а sqlwrap.WithTx создавал plain Queries. Audited callbacks теперь получают bounded context; прямые auth/invite/2FA/method-registry transactions тоже получают deadline. TestControlMutationDeadlineCancelsLockWait удерживает authentication lock, проверяет окончание по 100 ms repository deadline до parent deadline и отсутствие записанного токена.

## Повторная проверка защит

Account-authentication serialization, одноразовые 2FA challenges/counters/backup codes, identity bind/unbind lock order, invitation scope lock order, строгая hierarchy, workspace isolation и audit consistency уже реализованы и покрыты существующими тестами. Криптографическая проверка внешних identity остаётся обязанностью adapters; transport identity не считается самостоятельным доказательством входа.

Удаление workspace membership не отзывает account-wide MCP token: права вычисляются для активного членства, токен сам не выдаёт workspace access. Отзыв при удалении platform membership обязателен и теперь атомарен.

Результаты проверок и ограничения — в [общем отчёте](../SECURITY_RELIABILITY_AUDIT.md).

## Уточнение первого входа — 2026-09-06

Подтверждён контракт: первый CompleteAuth с проверенной identity создаёт owner
без отдельного вызова Initialize. В рабочей копии уже была добавлена эта ветка;
повторная проверка сузила обработку гонки до ErrAlreadyInitialized вместо любого
CONFLICT. После проигранной гонки выполняется обычная авторизация, поэтому другой
пользователь без invitation не становится участником.

Добавлен TestControlConcurrentFirstAuthentication: два запроса ждут initialization
lock, проверяются один owner/account, повторный вход той же identity и отказ другой.
DESIGN/AGENT согласованы с METHODS. Build и vet прошли. После запуска пользователем
локальной PostgreSQL выполнен `go test -race -p 1 ./control/... -count=1`: все тесты
Control, adapters и repository прошли, включая первый вход и конкурентную
инициализацию одной и разных identities. Ошибок race detector нет. Реальный OAuth
обмен с Yandex и развёрнутая версия приложения этим прогоном не проверялись.
