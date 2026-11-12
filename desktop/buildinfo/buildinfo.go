// Package buildinfo — версия ПК-версии. Отдельный пакет, чтобы установщик знал версию, не подключая ядро Xray.
package buildinfo

const (
	// VersionCode растёт с каждым выпуском; по нему решается, новее ли опубликованная версия.
	VersionCode = 7
	VersionName = "1.6"
)
