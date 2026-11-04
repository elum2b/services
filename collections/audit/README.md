# Регрессионные проверки аудита

Все восемь исправлений закреплены обычными TestRegression* в collections_test.go.
Они входят в go test ./... и больше не требуют overlay:

```sh
python3 collections/audit/run_probes.py
python3 collections/audit/check_archive_api.py
```

Первый runner запускает положительные regression tests с race detector.
Второй компилирует настоящий внешний Go module с local replace и реализацией
Collections.Archive / ArchiveLister через публичные типы; никаких internal imports.
Созданный temporary module удаляется. Root tests используют изолированные test БД.

`defect-probes-before-fixes.go.txt` — исторический снимок отрицательных проб из
аудита до исправлений. Он не компилируется и не запускается текущим runner/CI:
старые ожидания утверждали наличие дефектов и использовали прежний import contract.
Доказанные до исправления результаты сохранены в ../SECURITY_RELIABILITY_AUDIT.md.
