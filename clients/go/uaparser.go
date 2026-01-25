package uaparser

import (
	"github.com/Octanium91/ua-parser/pkg/core"
)

// Re-export types for easier usage
type Parser = core.Parser
type Config = core.Config
type Result = core.Result

// New creates a new Parser instance.
// This is a wrapper around core.New to provide a cleaner public API.
func New(cfg Config) (*Parser, error) {
	return core.New(cfg)
}
