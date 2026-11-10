//go:build wasm && wasip1 && go1.24

// This is the WASI (wasip1) reactor build of the UA parser.
//
// It requires Go 1.24+: //go:wasmexport and -buildmode=c-shared for wasip1
// arrived in 1.24. Older toolchains silently ignore the directive and emit a
// module without exports, so the go1.24 build tag makes them fail loudly
// instead. The rest of the module only needs the go.mod minimum (1.23).
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

// updateCorrections lets the HOST push a corrections.yaml payload into the
// engine: WASI preview1 has no sockets, so the Java/Node clients fetch the
// file themselves and call this export (init-time + periodic). Returns 0 on
// success, -1 on rejection (last good rules are kept either way).
//
//go:wasmexport updateCorrections
func updateCorrections(ptr uint32, length uint32) int32 {
	if ptr == 0 || length == 0 {
		return -1
	}
	if parser == nil {
		if initUA(0, 0) != 0 {
			return -1
		}
	}
	input := (*[1 << 30]byte)(unsafe.Pointer(uintptr(ptr)))[:length:length]
	// Copy out of WASM linear memory: the host frees ptr after the call, and
	// the compiled rule set must not alias host-owned memory.
	data := make([]byte, length)
	copy(data, input)
	if err := parser.ApplyCorrectionsYAML(data); err != nil {
		return -1
	}
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
		Signals *core.Signals     `json:"signals"`
	}

	// Try to parse as JSON payload (which allows passing headers)
	// Fallback to treating the entire input as a raw User-Agent string
	if err := json.Unmarshal(input, &payload); err != nil || payload.UA == "" {
		payload.UA = string(input)
		payload.Headers = nil
		payload.Signals = nil
	}

	result := parser.ParseFull(payload.UA, payload.Headers, payload.Signals)
	resBytes, _ := json.Marshal(result)

	// Allocate buffer for the result to be read by the host
	resPtr := malloc(uint32(len(resBytes)))
	copy(registry[resPtr], resBytes)

	// Return packed (length << 32) | ptr
	return (uint64(len(resBytes)) << 32) | uint64(resPtr)
}

func main() {}
