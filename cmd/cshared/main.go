package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/Octanium91/ua-parser/pkg/core"
)

var (
	parser      *core.Parser
	initMu      sync.Mutex
	initialized bool
)

// Init initializes the process-global parser. The engine is a process
// singleton: the FIRST call wins and later calls are no-ops that return
// success — a second Init with a different config (e.g. corrections_url or
// lru_cache_size) is silently ignored, not applied. Host wrappers that need
// distinct configs must run in separate processes.
//
//export Init
func Init(configJSON *C.char) *C.char {
	initMu.Lock()
	defer initMu.Unlock()

	if initialized {
		return nil
	}

	var cfg core.Config
	if configJSON != nil {
		err := json.Unmarshal([]byte(C.GoString(configJSON)), &cfg)
		if err != nil {
			return C.CString("Failed to unmarshal config: " + err.Error())
		}
	}

	if cfg.LRUCacheSize == 0 {
		cfg.LRUCacheSize = 1000
	}

	p, err := core.New(cfg)
	if err != nil {
		return C.CString("Failed to initialize parser: " + err.Error())
	}

	parser = p
	initialized = true
	return nil
}

type ParsePayload struct {
	UA      string            `json:"ua"`
	Headers map[string]string `json:"headers"`
	Signals *core.Signals     `json:"signals"`
}

//export Parse
func Parse(payloadJSON *C.char) *C.char {
	if parser == nil {
		return C.CString(`{"error": "Parser not initialized"}`)
	}

	var payload ParsePayload
	err := json.Unmarshal([]byte(C.GoString(payloadJSON)), &payload)
	if err != nil {
		return C.CString(`{"error": "Invalid payload: ` + err.Error() + `"}`)
	}

	result := parser.ParseFull(payload.UA, payload.Headers, payload.Signals)
	resBytes, err := json.Marshal(result)
	if err != nil {
		return C.CString(`{"error": "Failed to marshal result"}`)
	}

	return C.CString(string(resBytes))
}

// UpdateCorrections lets the host push a new corrections.yaml payload into
// the engine (validated + self-tested; whole-file reject keeps last good).
// Returns nil on success or an error message (free with FreeString). Useful
// when the background updater is disabled and the host manages delivery.
//
//export UpdateCorrections
func UpdateCorrections(yamlPayload *C.char) *C.char {
	if parser == nil {
		return C.CString("Parser not initialized")
	}
	if yamlPayload == nil {
		return C.CString("nil corrections payload")
	}
	if err := parser.ApplyCorrectionsYAML([]byte(C.GoString(yamlPayload))); err != nil {
		return C.CString("Failed to apply corrections: " + err.Error())
	}
	return nil
}

//export FreeString
func FreeString(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
