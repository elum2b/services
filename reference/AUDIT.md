# Аудит Reference

Область проверки: только доменный сервис `reference`. Отчёт фиксирует
наблюдаемые функциональные пробелы и риски после внедрения асинхронных архивов
и media. Для каждого пункта приведены контраргументы и уже существующие
механизмы защиты: изменение не считается обязательным, пока не подтверждён
ожидаемый контракт.

## 1. Archive Jobs Запускались До Run

Статус: исправлено.

По уточнённому контракту `NewWithDatabase` только инициализирует объект;
сервис начинает работать только после `Run`. Поэтому Resource GC корректно
запускается только из `Run` (`reference.go:124-125`,
`reference.go:335-367`).

Ранее `configureArchiveJobs` запускал `Admin.StartArchiveJobs` ещё в
`NewWithDatabase`. Этот вызов удалён: конструктор только конфигурирует queue, а
`Reference.startWorkers` запускает archive jobs вместе с GC после `Run`.

Регрессионный test `TestReferenceArchiveJobsWaitForRun` ставит export job до
`Run`, проверяет, что он остаётся `queued`, затем запускает отдельный service
через `Run` и ожидает `completed`.

## 2. Ошибка При Записи Resource Оставляла Orphan Media

Статус: исправлено.

Раньше `prepare` записывал media во внешнее storage до database mutation. Теперь
`CreateResourceWithSave` и `UpdateResourceWithSave` внутри workspace-locked DB
transaction сначала записывают некоммитированные resource metadata и
media-version bookkeeping, затем вызывают storage upload, сохраняют полученные
object refs и только после этого коммитят transaction
(`repository/resource.go`). Ошибка storage откатывает БД.

External storage не участвует в DB transaction. Поэтому при частичной ошибке
`Replace`, а также при ошибке записи refs или commit после успешной upload,
сервис выполняет compensating `DeleteVersion`
(`service/resource/resource.go`). Failure-injection tests проверяют rollback
create/update, сохранение старой active version и вызовы cleanup.

Остаточный риск: процесс может аварийно завершиться между успешным upload и
compensating delete. Для защиты именно от crash-window потребуется отдельный
storage reconciliation/mark-and-sweep process.

## 3. Импорт Media Архива Мог Частично Восстановить Данные

Статус: исправлено.

Manifest больше не сериализует source `media_version`; import генерирует новую
target version для каждого resource. `ImportArchiveWithMedia` выполняет
items/localizations, resources, media bookkeeping, storage upload, object refs
и links в одной workspace-locked DB transaction. Ошибка upload, refs, links или
commit откатывает DB mutation; созданные target versions удаляются
compensating cleanup. Duplicate resource keys, links, link positions и ссылки
на отсутствующие items/resources валидируются до transaction.

Весь участок от начала transaction до upload и commit ограничен
`ArchiveImportTimeout`, default `15m`. Job lease default `20m`; если custom
lease не превышает import timeout, он автоматически поднимается до timeout плюс
пять минут. Это оставляет время на rollback, storage cleanup и запись job
status до lease expiry.

Остаточный риск: external storage не поддерживает distributed transaction.
Crash между upload и DB commit/compensation может оставить orphan version.
Для защиты от этого crash-window нужен отдельный storage reconciliation process.

## 4. PostgreSQL Password Для Тестов Находится В Исходниках

Критичность: высокая, если credential используется вне локальных тестов; иначе
средний security/hygiene debt.

`reference/reference_test.go:1129` содержит непустой PostgreSQL password, и
test connection setup его использует. Исходный код нужно считать раскрытием,
если credential когда-либо был валиден не только для изолированной disposable
test database.

Контраргументы: CI workflow поднимает ephemeral PostgreSQL service с тем же
test credential, поэтому значение может быть намеренно публичными test data.
Это снижает прямой production impact, но не оправдывает повторное использование
пароля где-либо ещё.

Нужно решение: подтвердить, что password test-only; ротировать его, если он
когда-либо использовался повторно; предпочтительно передавать тестовую
конфигурацию через CI environment или явно несекретное fixture value.

## 5. Queued Export Мог Экспортировать Удалённые Items

Статус: исправлено.

`repository.Export` включает soft-deleted items, когда
`OnlyNotDeleted=false`. Queue не передавала этот filter, поэтому async archive
зависел от zero value и мог включать удалённые items.

Archive boundary теперь принудительно вызывает `repository.Export` с
`OnlyNotDeleted=true` (`service/admin/archive.go`). `ArchiveExportRequest`
содержит только `IncludeMedia`; public queue API и `METHODS.md` больше не
обещают filter. Даже старые job options не могут изменить этот инвариант.

## 6. GC Не Инвалидирует Media Cache

Критичность: средняя, зависит от контракта.

`CollectGarbage` удаляет storage versions и database bookkeeping
(`service/resource/resource.go:149-202`), а cache содержит доступный API
`DeletePrefix` (`service/resource/cache/cache.go:93-102`). GC его не вызывает.
Закешированная старая версия может возвращаться через `GetContent` до окончания
TTL.

Контраргументы: `GetContent` явно считает version публичной media identity и
комментирует, что old/deleted versions остаются читаемыми
(`service/resource/resource.go:205-206`). Ограниченный TTL после физического
удаления может быть намеренным availability grace period. Default TTL составляет
один час, поэтому при намеренном поведении это нужно документировать.

Нужно решение: инвалидировать закешированные bytes при физическом удалении для
strict retention или документировать cache TTL как допустимое окно чтения после
retention.

## 7. Исправлено: Семантическое Упорядочивание Resource Position

`Resource.InsertAfter` и `Resource.MoveAfter` выполняются в workspace-locked
transaction, проверяют anchors и attachment state, а затем через временный
положительный диапазон безопасно перенумеровывают только нужный item в `0..n-1`.
Schema запрещает отрицательные positions. Legacy `Attach` сохраняется, но его
collision преобразуется в domain conflict; новым клиентам рекомендованы
семантические методы.
