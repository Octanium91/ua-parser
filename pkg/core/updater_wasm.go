//go:build js && wasm

package core

// startUpdater in the WASM version does nothing, as updates are managed by the browser cache/CDN.
// This avoids importing the "net/http" package, which significantly reduces the file size.
func (p *Parser) startUpdater() {
	return
}
