# Сравнение бенчмарков Collections / Promo / Calendar

4 ноября 2026. Существующий production-код не изменялся ради сравнения.

## Условия

- Apple M4, Darwin/arm64, Go 1.27.1, один локальный PostgreSQL.
- Последовательный запуск пакетов, `-p 1 -run '^$' -benchmem -benchtime=300ms -count=3`.
- Выбраны существующие read/write/replay benchmarks трёх сервисов.
- Для Promo/Calendar использован Go overlay: только имена тестовых БД заменены
  на уникальные collections_compare_<service>_<suffix>; добавлен cleanup этих БД.
  Их штатные helpers пересоздают фиксированные *_test базы, поэтому напрямую
  для этого сравнения они не запускались. Overlay и scratch БД удалены.
- Медианы ns/op в таблице переведены в миллисекунды. Это среднее время операции
  внутри каждого Go benchmark, не p50/p95/p99 запросов production.

Fixtures различаются: Collections — 1000 entries и одна коллекция, Promo —
100 промокодов и 1000 начальных активаций, Calendar — один календарь/шаг.
Набор проверок, receipt/snapshot и размер ответа тоже различаются. Это сравнение
порядка стоимости существующих public paths, не строго эквивалентных workloads.
HasItem hit/miss означает найденное право/его отсутствие: оба выполняют DB query.
Admin cache-hit измеряется отдельно; его нельзя сравнивать с актуальным DB read.

## Все измеренные пути

| Сервис / операция | мс/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Collections/User.HasItem/hit | 0.40713 | 5608 | 142 |
| Collections/User.HasItem/miss | 0.38999 | 5525 | 141 |
| Collections/User.HasItems/1 | 0.37983 | 5570 | 141 |
| Collections/User.HasItems/100 | 0.74249 | 163699 | 1709 |
| Collections/User.HasItems/1000 | 4.62299 | 1772892 | 15273 |
| Collections/User.GetCollection | 2.39703 | 227744 | 2612 |
| Collections/User.ListCollections | 1.10197 | 7613 | 156 |
| Collections/User.ListOwned | 0.42213 | 4259 | 73 |
| Collections/Admin.GetStats | 0.32376 | 2912 | 48 |
| Collections/Internal.SetOwned/change | 2.72750 | 40200 | 820 |
| Collections/Internal.SetOwned/noop | 1.92668 | 15295 | 305 |
| Collections/Internal.SetOwned/replay | 1.51961 | 9017 | 154 |
| Collections/Internal.SetOwned/conflict | 1.51896 | 8466 | 148 |
| Collections/Internal.SetOwnedBatch/1 | 3.05143 | 39302 | 819 |
| Collections/Internal.SetOwnedBatch/100 | 14.34783 | 2062739 | 8733 |
| Collections/Internal.SetOwnedBatch/1000 | 176.00526 | 21239608 | 78141 |
| Collections/Admin.ListCatalog/cache_hit_true | 0.00252 | 1881 | 30 |
| Collections/Admin.ListCatalog/cache_hit_false | 0.86754 | 9944 | 189 |
| Collections/Internal.SetOwnedParallel/shared_owner_false | 0.85542 | 38950 | 820 |
| Collections/Internal.SetOwnedParallel/shared_owner_true | 2.75856 | 38585 | 821 |
| PromoServiceMethods/User.Apply/success | 1.84713 | 13758 | 225 |
| PromoServiceMethods/User.Apply/already_applied | 1.17627 | 12165 | 195 |
| PromoServiceMethods/Admin.GetPromo | 0.00725 | 5688 | 85 |
| PromoServiceMethods/Admin.ListPromos | 0.00641 | 23785 | 32 |
| PromoServiceMethods/Admin.GetStats | 0.29447 | 2831 | 47 |
| CalendarServiceMethods/User.Record/idempotent | 1.65761 | 31124 | 457 |
| CalendarServiceMethods/User.Record/success | 2.45748 | 42242 | 559 |
| CalendarServiceMethods/User.Next | 0.25614 | 8714 | 149 |
| CalendarServiceMethods/User.ListActive | 0.00287 | 2664 | 40 |
| CalendarServiceMethods/User.GetCalendar | 0.00295 | 2312 | 36 |
| CalendarServiceMethods/User.GetProgress | 0.32284 | 6599 | 114 |

## Выводы и ограничения

- Новая запись Collections ≈2.73 ms против Promo ≈1.85 ms и Calendar ≈2.46 ms:
  соответственно примерно +48% и +11%. Это не универсальное отношение быстродействия
  сервисов: бизнес-инварианты и число SQL statements различаются.
- Replay Collections ≈1.52 ms против Promo ≈1.18 ms и Calendar ≈1.66 ms.
  В Collections перед lookup receipt выполняются две transaction locks.
- Точечный HasItem ≈0.407 ms против свежего Calendar.GetProgress ≈0.323 ms.
  Cached Admin paths находятся в отдельных единицах микросекунд.
- Collections SetOwned: ≈39.3 KiB / 820 allocations; Promo.Apply: ≈13.4 KiB / 225;
  Calendar.Record: ≈41.3 KiB / 559. По allocations Collections тяжелее обоих.
- HasItems/1000 ≈4.62 ms, ≈1.69 MiB; SetOwnedBatch/1000 ≈176 ms, ≈20.3 MiB.
  B/op — суммарно выделенные Go bytes, не peak RSS/живой heap.
- Для batch/1000 было всего 3–4 итерации на повтор: вывод о latency пока грубый.
  Первый baseline на 50ms дал ≈90ms, поэтому его нельзя принимать как устойчивое
  значение. Таблицы растут от stateful benchmarks; меньший размер batch не означает
  пропорционально меньшую нагрузку на PostgreSQL/WAL и GC.
- Число SQL statements batch постоянно (8 без BEGIN/COMMIT), но объём данных,
  receipt/outbox serialization, row/index writes и allocations растут с batch.
  Конкретная доля CPU/GC/DB здесь не профилировалась.
- Parallel ns/op — агрегированная стоимость на завершённую операцию под нагрузкой,
  не latency одного запроса. shared owner сериализуется и смешивает change/no-op.

Следующий шаг оптимизации, если он нужен: CPU/heap profiles batch paths и более
длинный изолированный прогон на фиксированных размерах таблиц; затем проверить
сокращение round-trips GetCollection/replay без ослабления snapshot/idempotency.
В этом сравнении реализация не оптимизировалась и новые SLO не назначались.
