# Collections

Наследует ../AGENT.md. Публичные контракты — METHODS.md, схема — DESIGN.md.

- User содержит только чтения. Никаких SetOwned, admin corrections, журнала или
  импорта в User. Internal содержит только доверенные SetOwned/SetOwnedBatch.
  Admin управляет каталогом, коррекциями, статистикой, callbacks и архивами.
- Владение scoped полной services.Identity + collection key + item key.
  Один item в двух коллекциях имеет независимые отметки.
- Хранимый owned и доступность различаются: HasItem=false для неактивной/архивной
  коллекции и скрытого/удалённого entry. HasItem/HasItems не используют cache.
- Удаление состава не удаляет ownership/revision. Повторное добавление сохраняет
  отметку; поздний SetOwned(false) разрешён для существующей записи. Новое true
  требует опубликованного состава. Идентификаторы collection/item не переименовываются.
- Переданный L2 cache client выделяется одному сервису; общий backend допустим,
  совместное владение одним client-объектом несколькими Run запрещено.
- Revision/changed_at скрытых элементов, включая чтения чужого subject, допустимы
  по подтверждению пользователя; effective owned при этом остаётся false.
- Operation scope: workspace + app + platform + user + source + OperationID.
  Method, ordered changes, expected revisions и correction actor/reason входят в hash.
  Повтор с другим payload — conflict; receipt проверяется до состава и ревизий.
- No-op сохраняет receipt, не создаёт ownership row для начального false,
  не меняет revision/changed_at, change journal или callback.
- Lock order: catalog workspace shared → owner exclusive → receipt/state.
  Admin/import берут тот же catalog lock exclusively. Все mutations атомарны.
- Batch <=1000, повтор collection/item запрещён. Bulk reads/writes не дают N+1.
- Reference metadata и account profiles не принадлежат Collections. Никаких
  межсервисных FK, SQL JOIN или импортов чужих repository.
- Конфигурационный импорт не трогает отметки/receipts; update заменяет вложенный
  состав через soft delete. Остальные коллекции сохраняются. Экспорт — snapshot.
- Operator correction требует actor/reason; source admin зарезервирован.
- Close терминален, отменяет startup и ждёт direct callback handlers. Root context
  и goroutine manager shell не заменяются при Run.
- Sync/async import требует ActorID и пишет transactional catalog audit вместе с
  import receipt. Queue options сохраняют attribution, public status скрывает options.
- User.GetCollection содержит только общий опубликованный каталог/Version, без
  ownership. ListOwned фильтруется CollectionKeys и всегда читает DB. Долгий cache
  каталогов использует persistent DB revision, не user identity. CRUD/import bump
  атомарен с данными/audit; проверка publication/version обязательна и для KnownVersion.
- User locale reads фильтруют locale в SQL. Reorder — один bulk UPDATE; предел
  catalog/reorder 100000 entries, ownership batch остаётся 1000.
- Custom Archive использует публичные root/model типы с private adapter к jobs.
- Root tests ровно collections_test.go / collections_bench_test.go.
