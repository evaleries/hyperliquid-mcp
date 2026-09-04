package tools

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// ServerName and ServerVersion match the Python reference (server.py:
// Server("hyperliquid-mcp"); package version 0.1.0).
const (
	ServerName    = "hyperliquid-mcp"
	ServerVersion = "0.1.0"
)

// All returns every tool with its handler: the 23 Python-parity tools in the
// reference server's list_tools order — account (§1), orders (§2), queries
// (§3), market (§4), vault (§5), utility (§6) — followed by the post-parity
// extension groups (HIP-3/4).
func All(c *hl.Client) []server.ServerTool {
	var out []server.ServerTool
	out = append(out, accountTools(c)...)
	out = append(out, orderTools(c)...)
	out = append(out, twapTools()...)
	out = append(out, queryTools(c)...)
	out = append(out, marketTools(c)...)
	out = append(out, vaultTools(c)...)
	out = append(out, utilTools(c)...)
	out = append(out, hip3Tools(c)...)
	return out
}

// RegisterAll registers every tool (23 parity + extensions) on the MCP server.
func RegisterAll(s *server.MCPServer, c *hl.Client) {
	s.AddTools(All(c)...)
}

// NewServer builds the MCP server.
func NewServer(c *hl.Client) *server.MCPServer {
	s := server.NewMCPServer(
		ServerName,
		ServerVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	RegisterAll(s, c)
	return s
}

// schemaSpec is marshaled into mcp.Tool.RawInputSchema. mcp-go's structured
// InputSchema always emits "required": [] for empty lists, which the Python
// server never sends; the raw form keeps the wire schema byte-equivalent to
// Python's.
type schemaSpec struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// schema builds a tool input schema from explicit JSON-schema property maps.
// Property maps are written to match the Python server's schemas
// (verified by the golden test against testdata/tools.python.json).
func schema(props map[string]any, required ...string) json.RawMessage {
	if props == nil {
		props = map[string]any{}
	}
	b, err := json.Marshal(schemaSpec{Type: "object", Properties: props, Required: required})
	if err != nil {
		// Intentional panic: props are static literals compiled into the
		// binary, so a marshal failure is a programming invariant violation
		// discovered at startup — not a runtime condition to handle.
		panic(fmt.Sprintf("invalid tool schema: %v", err))
	}
	return b
}

// Property helpers keep schema declarations terse while emitting exactly the
// keys the Python schemas emit.

func strProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// strPropDefault is a string property with a default value.
func strPropDefault(description string, def string) map[string]any {
	return map[string]any{"type": "string", "description": description, "default": def}
}

// intProp is an integer property; min is included only when non-nil.
func intProp(description string, min *int64) map[string]any {
	p := map[string]any{"type": "integer", "description": description}
	if min != nil {
		p["minimum"] = *min
	}
	return p
}

func boolPropDefault(description string, def bool) map[string]any {
	return map[string]any{"type": "boolean", "description": description, "default": def}
}

func objPropDefault(description string, def map[string]any) map[string]any {
	return map[string]any{"type": "object", "description": description, "default": def}
}

func int64ptr(v int64) *int64 { return &v }

// tool assembles a ServerTool from name, description, schema, and handler.
func tool(name, description string, in json.RawMessage, h handlerFunc) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:           name,
			Description:    description,
			RawInputSchema: in,
		},
		Handler: wrap(name, h),
	}
}
