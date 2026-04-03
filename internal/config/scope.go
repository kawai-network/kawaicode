package config

// Scope determines which config file is targeted for read/write operations.
type Scope int

const (
	// ScopeGlobal targets the global data config (paths.UserDataDir()/crush.json).
	ScopeGlobal Scope = iota
	// ScopeWorkspace targets the workspace config (<data-dir>/crush.json).
	ScopeWorkspace
)
