package models

// MCPCodeKind names the code pattern behind one MCPCodeFact.
type MCPCodeKind string

const (
	// MCPCodePrefixContainment is a startsWith containment check on a path with a root that has
	// no separator appended (MCP-204).
	MCPCodePrefixContainment MCPCodeKind = "prefix_containment"
	// MCPCodeStdioCommand is an MCP stdio client built with a command that is not a literal, or
	// is a shell given -c (MCP-205).
	MCPCodeStdioCommand MCPCodeKind = "stdio_command"
	// MCPCodeBearerNoAudience is AuthSettings with a token_verifier and no audience check (MCP-206).
	MCPCodeBearerNoAudience MCPCodeKind = "bearer_no_audience"
	// MCPCodeClientOpensURL is an OAuth redirect handler that passes the server-supplied URL to an
	// OS opener with no scheme or origin check (MCP-207).
	MCPCodeClientOpensURL MCPCodeKind = "client_opens_url"
	// MCPCodeSharedTransport is a module-scope server or transport used inside a handler (MCP-208).
	MCPCodeSharedTransport MCPCodeKind = "shared_transport"
	// MCPCodeDNSRebindingOff is TransportSecuritySettings with the protection switched off and no
	// authentication in the file (MCP-209).
	MCPCodeDNSRebindingOff MCPCodeKind = "dns_rebinding_off"
	// MCPCodePkgInstallSpec is an npm or pnpm install whose specifier is built from a value, with
	// no validation in the file (MCP-210).
	MCPCodePkgInstallSpec MCPCodeKind = "pkg_install_spec"
)

// MCPCodeFact is one site in MCP server or client source code that matches an MCPCodeKind. Only
// the site is stored; the source text behind the judgment is not.
type MCPCodeFact struct {
	Kind     MCPCodeKind `json:"kind"`
	FilePath string      `json:"file_path"`
	Line     int         `json:"line"`
	Language Language    `json:"language"`
}
