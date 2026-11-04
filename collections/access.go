package collections

// AccessDefinition is registered by the host application through Control's
// manifest contract; the Collections package does not depend on Control.
type AccessDefinition struct {
	Key           string
	Service       string
	GroupKey      string
	Position      int32
	TitleRU       string
	TitleEN       string
	DescriptionRU string
	DescriptionEN string
}

func AccessManifest() []AccessDefinition {
	actions := [][3]string{
		{"catalog.read", "Чтение каталога", "Read catalog"},
		{"collection.create", "Создание коллекции", "Create collection"},
		{"collection.update", "Изменение коллекции", "Update collection"},
		{"collection.archive", "Архивация коллекции", "Archive collection"},
		{"entry.add", "Добавление элемента", "Add entry"},
		{"entry.remove", "Удаление элемента", "Remove entry"},
		{"entry.reorder", "Перестановка элементов", "Reorder entries"},
		{"ownership.correct", "Корректировка наличия", "Correct ownership"},
		{"journal.read", "Чтение журнала", "Read journal"},
		{"stats.read", "Чтение статистики", "Read statistics"},
		{"stats.refresh", "Пересчёт статистики", "Refresh statistics"},
		{"archive.export", "Экспорт каталога", "Export catalog"},
		{"archive.import", "Импорт каталога", "Import catalog"},
		{"archive.read", "Чтение архивных заданий", "Read archive jobs"},
		{"archive.download", "Скачивание архива", "Download archive"},
		{"callback.read", "Чтение уведомлений", "Read callbacks"},
		{"callback.retry", "Повтор уведомления", "Retry callback"},
		{"callback.accept", "Подтверждение уведомления", "Accept callback"},
		{"callback.reject", "Отклонение уведомления", "Reject callback"},
		{"callback.reset", "Сброс истёкшей обработки", "Reset expired callback processing"},
	}
	out := make([]AccessDefinition, 0, len(actions))
	for i, a := range actions {
		out = append(out, AccessDefinition{
			Key:           "collections." + a[0],
			Service:       "collections",
			GroupKey:      "admin",
			Position:      int32(i),
			TitleRU:       a[1],
			TitleEN:       a[2],
			DescriptionRU: a[1],
			DescriptionEN: a[2],
		})
	}
	return out
}
