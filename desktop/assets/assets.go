// Package assets — значок приложения: тот же рисунок, что у Android-версии (исходник icon-source.webp,
// PNG делает tools/icon). Из него собираются значок exe, ярлык Linux, значок в трее и вкладки браузера.
package assets

import _ "embed"

//go:embed vpnrouting.png
var IconPNG []byte
