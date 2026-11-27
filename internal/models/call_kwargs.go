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

// RepoFlow is one flow found by the one-function taint pass over repo code that is not a tool
// handler: a model-output value reaching a shell or code-execution call (RAW-003), or a function
// parameter reaching the resume keyword of ClaudeAgentOptions (CSDK-209). It is the fact behind
// repo_source_reaches_sink. Python only; any function under a test path is not walked.
type RepoFlow struct {
	FilePath   string     `json:"file_path"`
	Func       string     `json:"func,omitempty"`
	Source     SourceKind `json:"source_kind"`
	Param      string     `json:"param,omitempty"` // the parameter, or the model call for llm_output
	SourceLine int        `json:"source_line"`
	SinkClass  string     `json:"sink_class"`
	SinkCallee string     `json:"sink_callee"`
	SinkLine   int        `json:"sink_line"`
}
