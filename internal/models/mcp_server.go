package models

// MCPServerBind is one place where MCP server code starts an HTTP transport bound to every
// network interface (a literal 0.0.0.0, :: or [::] host) with no authentication and no
// host or origin allow-list in the same file. It is the fact behind MCP-203. Only these hits
// are recorded: the host text is reduced to this judgment during discovery and is never
// stored, and a host from a variable, an f-string or an environment lookup is not judged.
type MCPServerBind struct {
	FilePath  string   `json:"file_path"`
	Line      int      `json:"line"`
	Language  Language `json:"language"`
	Transport string   `json:"transport"` // "sse" | "http" | "streamable-http" | "express"
	Reason    string   `json:"reason"`
}
