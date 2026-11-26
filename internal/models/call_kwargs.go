package models

// CallKwargHit is one call in repo code that passes a keyword (Python) or object property
// (TypeScript) from a closed table with a value the engine judges: a literal true or false,
// or the whole parent environment. It is the fact behind repo_call_kwarg_matches
// (AGCFG-019, 020, 021). The table is: env on StdioServerParameters / StdioClientTransport
// (value environ, recorded only when the launch command is not a literal), warn_only and ssrf_protection_enabled, and trust_remote_code (values true or
// false). Any other keyword, any other value shape and any call under a test path is not recorded.
type CallKwargHit struct {
	FilePath string   `json:"file_path"`
	Line     int      `json:"line"`
	Language Language `json:"language"`
	Callee   string   `json:"callee"` // last name of the call, e.g. "from_pretrained"
	Kwarg    string   `json:"kwarg"`
	Value    string   `json:"value"` // "environ" | "true" | "false"
}
