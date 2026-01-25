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
	parser *core.Parser
	once   sync.Once
)

//export Init
func Init(configJSON *C.char) *C.char {
	var errStr string
	once.Do(func() {
		var cfg core.Config
		if configJSON != nil {
			err := json.Unmarshal([]byte(C.GoString(configJSON)), &cfg)
			if err != nil {
				errStr = "Failed to unmarshal config: " + err.Error()
				return
			}
		}

		if cfg.LRUCacheSize == 0 {
			cfg.LRUCacheSize = 1000
		}

		p, err := core.New(cfg)
		if err != nil {
			errStr = "Failed to initialize parser: " + err.Error()
			return
		}
		parser = p
	})

	if errStr != "" {
		return C.CString(errStr)
	}
	return nil
}

type ParsePayload struct {
	UA      string            `json:"ua"`
	Headers map[string]string `json:"headers"`
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

	result := parser.Parse(payload.UA, payload.Headers)
	resBytes, err := json.Marshal(result)
	if err != nil {
		return C.CString(`{"error": "Failed to marshal result"}`)
	}

	return C.CString(string(resBytes))
}

//export FreeString
func FreeString(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
