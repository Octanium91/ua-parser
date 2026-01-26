//go:build js && wasm

package core

// startUpdater у WASM версії нічого не робить, оскільки оновленнями керує браузерний кеш/CDN.
// Це дозволяє уникнути імпорту пакету "net/http", що суттєво зменшує розмір файлу.
func (p *Parser) startUpdater() {
	return
}
