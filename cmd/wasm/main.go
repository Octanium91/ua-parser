//go:build wasm && wasip1

// This is the WASI (wasip1) reactor build of the UA parser.
//
// Memory contract with the host:
//   - malloc/free manage buffers that cross the WASM boundary. Allocated
//     buffers are pinned in the registry map: Go's GC is non-moving, so a
//     buffer that is kept reachable via the registry neither gets collected
//     nor changes address while the host holds its pointer. free removes the
//     registry entry, allowing the GC to reclaim the buffer.
//   - parseUA returns a packed uint64: (length << 32) | ptr. The host reads
//     length bytes at ptr and must release the buffer with free(ptr).
//   - The registry map is deliberately unsynchronized: hosts guarantee
//     single-threaded access to the module's exports (standard for WASI
//     preview 1 reactors).
package main

import (
	"encoding/json"
	"unsafe"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var parser *core.Parser

// registry keeps track of allocated buffers to prevent GC from collecting them.
var registry = make(map[uint32][]byte)

//go:wasmexport malloc
func malloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	registry[ptr] = buf
	return ptr
}

//go:wasmexport free
func free(ptr uint32) {
	delete(registry, ptr)
}

//go:wasmexport initUA
func initUA(ptr uint32, length uint32) int32 {
	cfg := core.Config{
		LRUCacheSize:      1000,
		DisableAutoUpdate: true, // WASM environment usually doesn't have network access
	}

	if ptr != 0 && length > 0 {
		configBytes := (*[1 << 30]byte)(unsafe.Pointer(uintptr(ptr)))[:length:length]
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			// If invalid JSON, we'll just use the default config instead of failing hard
		}
	}

	p, err := core.New(cfg)
	if err != nil {
		return -1
	}
	parser = p
	return 0
}

//go:wasmexport parseUA
func parseUA(ptr uint32, length uint32) uint64 {
	// Guard against a null pointer or empty input before constructing the
	// unsafe slice below; a zero-length slice at address 0 is undefined.
	if ptr == 0 || length == 0 {
		return 0
	}

	if parser == nil {
		if initUA(0, 0) != 0 {
			return 0
		}
	}

	// Read input from WASM memory
	// Safe to use 1<<30 as a max limit for the slice header, won't actually allocate that much.
	input := (*[1 << 30]byte)(unsafe.Pointer(uintptr(ptr)))[:length:length]

	var payload struct {
		UA      string            `json:"ua"`
		Headers map[string]string `json:"headers"`
	}

	// Try to parse as JSON payload (which allows passing headers)
	// Fallback to treating the entire input as a raw User-Agent string
	if err := json.Unmarshal(input, &payload); err != nil || payload.UA == "" {
		payload.UA = string(input)
		payload.Headers = nil
	}

	result := parser.Parse(payload.UA, payload.Headers)
	resBytes, _ := json.Marshal(result)

	// Allocate buffer for the result to be read by the host
	resPtr := malloc(uint32(len(resBytes)))
	copy(registry[resPtr], resBytes)

	// Return packed (length << 32) | ptr
	return (uint64(len(resBytes)) << 32) | uint64(resPtr)
}

func main() {}
