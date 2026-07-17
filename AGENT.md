# Общие правила разработки сервисов

Этот файл обязателен для всего репозитория. `AGENT.md` внутри конкретного
сервиса дополняет эти правила доменными требованиями, но не отменяет их. При
расхождении действуют более строгие требования.

## 1. Технологический контракт

- Язык: Go в версии, указанной в `go.mod`.
- Основная БД: PostgreSQL. Новый MySQL-код, MySQL-синтаксис и dual-driver
  compatibility не добавлять.
- Драйвер PostgreSQL: `pgx` через существующий `database/sql`/`sqlwrap` слой.
- Статические SQL-запросы описывать в SQLC. Сгенерированный код вручную не
  редактировать.
- JSON кодировать через `github.com/goccy/go-json`, а не `encoding/json`.
- Денежные значения и количества не хранить и не вычислять через float.
- Публичные enum и статусы оформлять отдельными типами и typed constants.
- Для HTTP-интеграций использовать существующие клиенты и utilities проекта;
  не создавать второй инфраструктурный слой без необходимости.

## 2. Границы сервисов

- Каждый бизнес-сервис владеет своей схемой, repository, моделями и
  lifecycle.
- Между таблицами разных сервисов нет внешних ключей и JOIN.
- Один бизнес-сервис не импортирует repository или SQLC другого.
- Связь между сервисами неявная: хранится стабильный ключ, а подробными
  данными владеет исходный сервис.
- `reference` — единственный владелец метаданных item. Остальные сервисы
  используют только непрозрачный item/reward key и собственные параметры
  выдачи.
- Общими могут быть публичные корневые модели и технические utilities из
  `internal/utils`.
- Контроллеры HTTP/WebSocket/Fiber находятся во внешнем приложении. Этот
  репозиторий предоставляет Go API, но не привязывает сервисы к transport.

## 3. Публичные слои и контракты

- `service/user` содержит только действия конечного пользователя.
- `service/admin` содержит действия оператора и управление каталогом.
- `service/internalapi`, `service/integration` и `service/operational`
  добавляются только для системных сценариев, которые не являются admin API.
- Адаптеры провайдеров не смешивать с user/admin service-кодом.
- Control является внутренним административным сервисом и не имеет
  искусственного `User` слоя.
- Системные callback/webhook/worker-действия не являются RBAC access и не
  должны появляться среди административных разрешений.
- Один публичный метод принимает `ctx` и один предметный `Params` объект.
  Не смешивать params со множеством дополнительных позиционных аргументов в
  новых и изменяемых контрактах.
- Не сохранять deprecated wrappers и обратную совместимость при согласованной
  унификации API, если это отдельно не требуется.
- Публичные сигнатуры не содержат `sqlc`, `sql.Null*`, `database/sql` и типы
  из `internal`.
- Общие сущности (`Identity`, `Actor`, `Reward`) использовать из корневого
  пакета, если их смысл совпадает. Не создавать несовместимые копии.

## 4. Identity и workspace

- Go-поле называется `WorkspaceID`, JSON/SQL поле — `workspace_id`.
- Публичное Go-представление workspace остаётся `string`.
- Workspace ID — канонический lowercase UUID длиной 36 символов.
- Пробелы, uppercase UUID, aliases вроде `main` и автоматический `TrimSpace`
  не допускаются: некорректное значение отклоняется.
- Каноническая форма обязательна также для cache keys, advisory locks и
  callback scopes, чтобы один workspace не получал несколько технических
  представлений.
- Все публичные user/admin/internal entry points и repository boundaries,
  доступные отдельно, проверяют workspace через общий validator.
- SQL-колонка workspace имеет тип `VARCHAR(36)` и участвует во всех
  business unique keys, FK и lookup, где данные scoped по workspace.
- `Identity` должна иметь валидный workspace, положительные `AppID` и
  `PlatformID`, непустой `PlatformUserID`; дополнительные поля target не
  меняют identity пользователя.
- Числового ID сущности недостаточно для admin/repository lookup: вместе с
  ним передаётся workspace scope, кроме явно global catalog в Control или
  Payment.

## 5. Валидация и ошибки

- Внешний API слой проверяет transport/UI форму запроса. Сервис не дублирует
  проектно-зависимые правила интерфейса.
- Service/repository всегда защищают доменные и storage-инварианты:
  обязательные ключи, enum, длины PostgreSQL-полей, числовые границы,
  корректность JSON, интервалы времени, взаимозависимые поля, workspace
  isolation и idempotency.
- Import выполняет полную preflight-валидацию всех вложенных объектов до
  открытия write transaction.
- Ожидаемые ошибки возвращаются как typed domain errors. Raw PostgreSQL error
  не является публичным ответом для ожидаемого conflict/not-found/limit.
- Ошибка вложенного объекта содержит индекс и путь поля, пригодный для API.
- Не нормализовать silently данные, если после нормализации два разных input
  становятся одним ключом. Правило нормализации должно быть частью контракта.

## 6. Immutable snapshots

- Все данные, обещанные пользователю при выдаче/старте/создании операции,
  фиксируются в immutable snapshot.
- Snapshot включает награды, item key и параметры выдачи, цену, asset,
  quantity, scale, скидку и другие изменяемые поля, влияющие на результат.
- Admin update/delete исходного каталога не меняет уже созданные assignment,
  progress, redemption, order, fulfillment, callback и idempotent response.
- Повторный вызов читает snapshot первой успешной операции, а не актуальный
  каталог.
- Snapshot и business write создаются в одной транзакции. Callback payload
  строится из этого snapshot.
- JSON snapshot допустим как неизменяемый документ. Поля, используемые для
  поиска, блокировки и агрегации, остаются реляционными колонками.

## 7. Структура пакета

Базовая структура:

```text
<service>/
  AGENT.md
  <service>.go
  config.go
  errors.go
  callback.go              # если нужен outbox
  <service>_test.go
  <service>_bench_test.go
  repository/
  service/
    admin/
    user/                   # если есть user API
    internalapi/            # только системные операции
  sqlc/
    schema.sql
    query.sql
    bootstrap.go
  sqlc.yaml
```

- Корневая структура называется предметно: `CPA`, `Tasks`, `Payment`, а не
  `API`, `Manager` или `Service`.
- Корневой пакет собирает слои, владеет lifecycle и скрывает bootstrap.
- `repository` — единственный бизнес-слой, работающий с SQLC, транзакциями,
  locks, `sql.Null*` и плоскими DB rows.
- `service` выполняет orchestration и преобразует repository models в
  публичные модели. Он не знает о колонках и SQLC params.
- Общие публичные DTO метода размещаются в `models.go`, локальные params/result
  могут находиться рядом с методом.
- Один файл содержит один метод или тесно связанную CRUD-группу. Не собирать
  весь сервис, модели и helpers в одном файле.
- Helper создаётся при реальном повторном использовании или когда выделяет
  сложный алгоритм. Тривиальные однострочные wrappers не добавлять.

## 8. Стиль Go-кода

- Логические этапы функции отделять пустой строкой: подготовка context,
  validation, repository call, обработка ошибки, построение result.
- После открывающей `{` функции и перед закрывающей `}` оставлять пустую
  строку, когда функция содержит несколько логических этапов. Для коротких
  однострочных функций это не требуется.
- Не писать плотные literals, mapper calls и длинные сигнатуры.
- В многострочном struct literal каждое поле находится на отдельной строке.
- Если вызов функции не помещается как короткое выражение, каждый аргумент
  находится на отдельной строке. Не раскладывать по несколько аргументов на
  одну строку.
- Не использовать positional mapper с большим числом scalar arguments.
  Передавать row/model или именованный params object.
- Сложный mapper разбивать на предметные helpers, не скрывая ошибки
  преобразования.
- Импорты группировать `stdlib`, внутренние пакеты сервиса, общие пакеты,
  внешние зависимости; фактическую сортировку оставлять `gofmt`.
- Комментарии объясняют неочевидный инвариант или причину, а не пересказывают
  код.
- Имена должны отражать действие и предмет: `GetApplyBundleForUpdate`,
  `CreateRedemption`, `ExecuteRefund`.
- Не оставлять неиспользуемые fallback, compatibility path, SQL query и
  мёртвые модели после перехода на новый контракт.

Рекомендуемая форма метода:

```go
func (u *User) GetCode(ctx context.Context, params GetCodeParams) (GetCodeResult, error) {

    mergedCtx, cancel := u.withContext(ctx)
    defer cancel()

    if err := params.Identity.Validate(); err != nil {
        return GetCodeResult{}, err
    }

    result, err := u.repository.Issue(
        mergedCtx,
        scope(params.Identity, params.CPAID),
    )
    if err != nil {
        return GetCodeResult{}, err
    }

    return GetCodeResult{
        Assignment:    mapAssignment(result.Assignment),
        Rewards:       mapRewards(result.Rewards),
        AlreadyIssued: result.AlreadyIssued,
    }, nil

}
```

## 9. SQLC и repository

- Все статические SQL statements находятся в SQLC query files.
- Исключения: bootstrap runner, advisory lock и динамический bulk import SQL,
  если SQLC не может выразить переменное число `VALUES`. Исключение должно
  быть локальным repository helper, использовать только placeholders и
  статические имена таблиц/колонок.
- Не собирать значения пользователя конкатенацией SQL-строк.
- PostgreSQL placeholders — `$1`, `$2`, ...; параметры генерирует helper.
- SQLC params в repository вызовах оформлять по одному полю на строку.
- Repository не возвращает наружу generated rows/params и DB-specific enum.
- Не допускать N+1. Для вложенных агрегатов использовать плоские relational
  rows и сборку в Go либо несколько заранее ограниченных bulk reads.
- JSON aggregation не использовать как замену реляционному запросу.
- Для горячего read/idempotent path ориентир — 1 SQL round-trip, для write
  path — минимальное измеренное число round-trip без ослабления consistency.
- Индексы проектировать по реальным `WHERE`, `JOIN`, `ORDER BY`; горячие
  изменения проверять через `EXPLAIN (ANALYZE, BUFFERS)`.
- Multi-query read, который формирует один документ/export, выполняется в
  `REPEATABLE READ READ ONLY` transaction.

## 10. Транзакции и конкурентность

- Транзакцией владеет repository, не service.
- `check -> state write -> snapshot -> stats/raw event -> callback outbox`
  выполняется атомарно.
- Идемпотентность защищается `UNIQUE`/`ON CONFLICT`, а не только pre-count.
- Повтор операции не выдаёт награду, не увеличивает статистику и не создаёт
  callback второй раз.
- Лимиты защищаются подходящей row/advisory lock и повторной проверкой внутри
  transaction.
- Catalog admin writes и import используют один workspace advisory-lock
  contract, если import conflict semantics зависит от их взаимного порядка.
- Внешний HTTP/Lua/provider вызов не выполнять внутри DB transaction или
  удерживаемой DB lock. Сетевой timeout не должен становиться timeout БД.
- Триггеры допустимы только для коротких локальных атомарных последствий.
  Скрытую orchestration и сетевые вызовы в trigger не помещать.

## 11. Callback/outbox

- Callback создаётся в той же транзакции, что business event.
- `event_key` детерминирован и уникален.
- Payload содержит фактически выданный snapshot.
- Worker скрывает lease, retry и ack/reject, поддерживает graceful shutdown.
- Admin API callback store scoped по workspace.
- После claim отмена провайдером не переписывает историю молча: создаётся
  отдельное компенсационное/revoke событие согласно домену.

## 12. Кэш

- Использовать общий cache contract `sqlwrap`, не писать отдельный cache
  engine в сервисе.
- Кэшированные методы имеют version scope как минимум по service, method и
  workspace. Версия метода меняется независимо от остальных методов.
- Mutation после успешного commit bump-ает версию затронутого scope. Старые
  ключи становятся недостижимыми; сканирование и удаление concrete keys не
  используется.
- Ошибка cache invalidation после успешного DB commit не превращает успешную
  mutation в ложную API-ошибку. Ошибка уходит в диагностический callback.
- L1 может кратко кэшировать version, чтобы не удваивать Redis traffic.
- При нескольких нодах custom distributed cache должен использовать
  совместимый distributed mutex/singleflight. In-memory mutex допустим только
  для одной ноды.
- Cache miss stampede защищать mutex; lock не должен охватывать внешний HTTP.
- Multi-node поведение проверять двумя независимыми L1 и общим L2.

## 13. Import/export

- Import/export реализуется самим доменным сервисом.
- Формат имеет одно versioned имя вида `<service>.export.v1`; отдельное
  дублирующее поле `version` не добавлять.
- Не экспортировать source server/workspace: пакет предназначен для другой
  workspace.
- JSON повторяет доменную иерархию: дочерние localization/rewards/conditions
  вложены в владельца, а не вынесены в несвязанные верхнеуровневые массивы.
- Сервисы, кроме `reference`, не экспортируют item catalog и dependencies.
  Они доверяют непрозрачному ключу награды/item.
- Secrets не попадают в export по умолчанию. Явный `include_secrets` является
  осознанной операцией; import может принять отдельную map секретов.
- Import сначала полностью валидирует и компилирует package, затем в одной
  transaction пишет таблицы в FK-порядке bulk batches.
- Для bulk использовать `internal/utils/importexport`: до 1000 строк и до
  60 000 PostgreSQL parameters на statement.
- Поддерживать и тестировать `fail_on_conflict`, `skip_existing` и
  `update_existing`. Update заменяет вложенный snapshot, не оставляя старые
  дочерние rows.
- Cache version bump выполняется один раз после commit, а не после каждой
  строки.

## 14. Target, localization и rewards

- Target регулирует только отображение материала, не право на уже созданную
  историческую операцию.
- Поддерживаемые общие признаки: premium, sex, country, locale, platform name
  и numeric platform/application IDs. Фильтрация каталога выполняется в Go,
  чтобы не размножать cache variants тяжёлым SQL.
- Private/integration payload никогда не возвращается фронту. Public payload
  является договорённостью backend/frontend и хранит только отображаемые
  данные.
- Группы и пользовательские сущности возвращают локализацию выбранного locale.
- Reward содержит `Key`, typed `Type`, integer `Quantity`, `Scale`, `Unit`.
  Например `Quantity=25, Scale=2` означает `0.25`.

## 15. Lifecycle и фоновые задачи

- `New(DatabaseParams)` только сохраняет конфигурацию.
- `Run(ctx)` открывает PostgreSQL, выполняет идемпотентный bootstrap, собирает
  сервис, запускает workers и блокируется до shutdown/error.
- `NewWithDatabase` используется тестами/embedding и не закрывает чужую БД.
- `Close` отменяет root context, останавливает workers, ждёт их завершения и
  закрывает только принадлежащие сервису ресурсы.
- Каждый public method объединяет request context с root context.
- Фоновые операции запускать только через общий goroutine manager с panic
  recovery; naked `go func()` в сервисах не использовать.
- Pollers/subscribers синхронизируют конфигурацию из БД: появившиеся запускают,
  удалённые/disabled останавливают, неизменившиеся не пересоздают по одному TTL.

## 16. Тесты

- В корне каждого сервиса ровно два test-файла:
  `<service>_test.go` и `<service>_bench_test.go`. Папку `tests/` не создавать.
- Каждый публичный метод покрывается валидным и невалидным сценарием.
- Обязательны integration tests на реальной PostgreSQL schema для SQL,
  transaction, locks, triggers, cache и import/export.
- Негативные tests проверяют typed error и отсутствие частичной записи.
- Для mutation проверять idempotency, workspace isolation, concurrency,
  immutable snapshot и callback exactly-once.
- Для cache проверять hit/miss, version bump и две ноды.
- Для import/export проверять round trip, все conflict strategies, invalid
  preflight, согласованный snapshot, конкурентный admin write и пакет больше
  parameter limit.
- Live provider/blockchain tests, требующие токенов или внешней сети, не входят
  в обычный `go test ./...` и не могут задерживать CI по timeout.
- Не ослаблять старые тесты ради нового поведения; при намеренной смене
  контракта обновлять и реализацию, и явное ожидание.

## 17. Бенчмарки

- Измерять отдельные публичные методы и отдельные ветки, а не полный lifecycle,
  который в production может длиться часы или дни.
- Разделять successful, idempotent/already-exists, cache hit/miss и admin read.
- Всегда использовать `-benchmem`.
- Stateful benchmark генерирует уникальную identity/operation key на итерацию.
- Внешний HTTP не включать в DB benchmark; provider benchmark выделять
  отдельно.
- После оптимизации фиксировать до/после: `ns/op`, `B/op`, `allocs/op` и число
  SQL round-trip.
- Низкая скорость не является основанием убирать lock/snapshot/idempotency;
  сначала измерять запросы, планы, transport и allocations.

## 18. Access, документация и совместимость

- На каждое самостоятельное admin-действие существует отдельный static access
  key. Методы с batch/single вариантами одного действия могут делить ключ.
- Access catalog не редактируется через CRUD. Control возвращает его с RU/EN
  localization, service/group/access positions и сортирует по position, не
  выдавая position наружу.
- При изменении публичного метода обновлять соответствующий `METHODS.md`.
- При изменении пользовательских полей Tasks/Payment обновлять README/DESIGN и
  import/export example.
- TODO содержит только подтверждённые проблемы с конкретным местом и ожидаемым
  исправлением. Завершённый пункт помечается после теста, а не после намерения.

## 19. Проверка перед завершением

Для любого изменения выполнить применимую часть, а перед крупным завершением —
весь набор:

```bash
gofmt -w <изменённые-go-файлы>
test -z "$(gofmt -l .)"
find . -name sqlc.yaml -execdir sqlc generate \;
go test ./...
go vet ./...
git diff --check
```

После `sqlc generate` проверить, что generated diff ожидаем и в нём нет
ручных либо посторонних изменений. Для горячего SQL дополнительно выполнить
`EXPLAIN (ANALYZE, BUFFERS)` и соответствующий benchmark.
