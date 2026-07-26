package uaparser

import (
	"github.com/Octanium91/ua-parser/pkg/core"
)

// Re-export types for easier usage, so callers never need to import the
// internal core package directly.
type Parser = core.Parser
type Config = core.Config
type Result = core.Result
type Signals = core.Signals
type ScreenInfo = core.ScreenInfo
type BotInfo = core.BotInfo
type GPUInfo = core.GPUInfo

// New creates a new Parser instance.
// This is a wrapper around core.New to provide a cleaner public API.
func New(cfg Config) (*Parser, error) {
	return core.New(cfg)
}
