package codeanchor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ParserConfigHash identifies resolved settings that can change parsed code.
func (c Config) ParserConfigHash() string {
	parser := struct {
		Enabled           bool
		AutomaticScope    bool
		DisabledLanguages []string
		PythonRoots       []string
		GoRoots           []string
		TSRoots           []string
		CSharpRoots       []string
		PHPRoots          []string
		TSParseTimeout    time.Duration
	}{
		Enabled: c.Enabled, AutomaticScope: c.AutomaticScope, DisabledLanguages: c.DisabledLanguages,
		PythonRoots: c.PythonRoots, GoRoots: c.GoRoots, TSRoots: c.TSRoots,
		CSharpRoots: c.CSharpRoots, PHPRoots: c.PHPRoots, TSParseTimeout: c.TSParseTimeout,
	}
	data, _ := json.Marshal(parser)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
