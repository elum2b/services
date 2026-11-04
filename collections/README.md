# Collections

Каталог коллекций и текущее наличие предметов пользователей. Количества и ячейки
принадлежат отдельному Inventory; item metadata — Reference.

- `User`: чтения своих/чужих коллекций, HasItem/HasItems.
- `Internal`: доверенные SetOwned/SetOwnedBatch.
- `Admin`: каталог, корректировки с причиной, журнал, stats, callbacks и архивы.

Полный контракт: [METHODS](METHODS.md). Инварианты и транзакции: [DESIGN](DESIGN.md).
Пример конфигурации: [catalog.json](examples/catalog.json).

```go
service := collections.New()
// Register OnCallback handlers before Run, then call Run with PostgreSQL params.
// For embedding: NewWithDatabase; prepare schema with Admin.Bootstrap.

receipt, err := service.Internal.SetOwned(ctx, model.SetOwnedParams{
    Identity: identity,
    Source: "game",
    OperationID: "purchase:123",
    OwnershipChange: model.OwnershipChange{
        ItemRef: model.ItemRef{CollectionKey: "colors", ItemKey: "color.red"},
        Owned: true,
    },
})
```

Каталог цветов и открытые цвета читаются раздельно:

```go
catalog, err := service.User.GetCollection(ctx, model.GetCollectionParams{
    ReadParams: model.ReadParams{Identity: identity, Locale: "ru"},
    CollectionKey: "colors",
    KnownVersion: cachedVersion, // empty on the first request
})
owned, err := service.User.ListOwned(ctx, model.ListOwnedParams{
    ReadParams: model.ReadParams{Identity: identity},
    CollectionKeys: []string{"colors"},
})
```

`catalog.Version` относится к составу, локали и странице. При `NotModified=true`
клиент сохраняет прежний состав; `owned` обновляет отдельно и накладывает по ItemKey.
Оба запроса пагинированы (default 100, max 1000). Identity включает workspace, app,
platform и platform_user_id; Subject позволяет читать другого пользователя того же
workspace/app. Для server cache задайте `Options{CacheEnabled: true}`;
`CatalogCacheTTL` по умолчанию 24h. Вызов GetCollection с cache hit или KnownVersion
проверяет актуальную версию одним SQL. Изменение ownership версию не меняет.

Перед использованием выбранного варианта приложение проверяет User.HasItem.
Архивация скрывает доступ, сохраняя отметку; запоздалый отзыв false допустим.
Callback consumer дедуплицирует event key. Реальные user/admin endpoints и проверки
Control actor/scope настраивает host application.

Тесты создают только уникальные временные `collections_test_*` БД и удаляют их.
Для другого PostgreSQL задайте `COLLECTIONS_TEST_POSTGRES_DSN` URL к admin/test
инстансу с правом CREATE DATABASE (не production). По умолчанию — локальный
тестовый PostgreSQL по существующему контракту репозитория.

```sh
go test -p 1 ./collections/...
go test -race -p 1 ./collections/...
go test ./collections -run '^$' -bench . -benchmem -count=3
```

Импорт требует operator attribution:

```go
params := admin.ImportParams{
    WorkspaceID: workspaceID,
    Request: model.ImportRequest{ActorID: operatorID, Package: catalog},
}
```

Close отменяет и дожидается startup/callback cleanup. После Close shell повторно
не запускается. Custom storage реализует публичный collections.Archive; metadata
аргумента Store — collections.ArchiveObject, listing — collections.ArchiveInfo.

Проверка разделения каталога/владения и замеры: [CATALOG_READS_VALIDATION](CATALOG_READS_VALIDATION.md).
