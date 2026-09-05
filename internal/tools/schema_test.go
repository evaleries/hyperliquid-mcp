package tools

import (
	"encoding/json"
	"reflect"
	"testing"
)

// extensionTools is the post-parity surface: tools
// beyond the Python 23, checked additively — the golden file stays untouched.
// Registering a new tool without adding it here fails the parity gate, which
// is deliberate: the extension set is explicit.
var extensionTools = map[string]bool{
	"hyperliquid_get_perp_dexs": true,
	"hyperliquid_get_dex_meta":  true,
}

// TestGoldenSchemaParity is the parity gate: the tools this server registers
// must match the Python reference's list_tools output
// (testdata/tools.python.json, extracted from server.py) in name,
// description, and inputSchema. Extension tools (extensionTools) have no
// golden counterpart and are validated structurally instead.
//
// Comparison is order-insensitive on purpose: mcp-go sorts tools by name
// before answering tools/list (server.go: sort.Strings(toolNames)), so
// registration order is not observable by a client and asserting it would
// pin an internal detail.
func TestGoldenSchemaParity(t *testing.T) {
	raw := readGoldenFixture(t)
	var golden []map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden fixture: %v", err)
	}
	goldenByName := make(map[string]map[string]any, len(golden))
	for _, g := range golden {
		goldenByName[g["name"].(string)] = g
	}

	// Handlers are never invoked; a nil client is safe here.
	var got []map[string]any
	for _, st := range All(nil) {
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", st.Tool.Name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("normalize tool %s: %v", st.Tool.Name, err)
		}
		got = append(got, m)
	}

	if len(got) != 23+len(extensionTools) {
		t.Errorf("registered %d tools, want 23 parity + %d extensions", len(got), len(extensionTools))
	}

	seen := map[string]bool{}
	extensionsSeen := map[string]bool{}
	for _, g := range got {
		name, _ := g["name"].(string)
		seen[name] = true
		want, ok := goldenByName[name]
		if !ok {
			if !extensionTools[name] {
				t.Errorf("tool %s not present in Python golden set or extension set", name)
				continue
			}
			extensionsSeen[name] = true
			// Extension tools: structural check only (no golden counterpart).
			desc, _ := g["description"].(string)
			if desc == "" {
				t.Errorf("extension tool %s has empty description", name)
			}
			schemaMap, _ := normalizeJSON(g["inputSchema"]).(map[string]any)
			if schemaMap["type"] != "object" {
				t.Errorf("extension tool %s schema type = %v, want object", name, schemaMap["type"])
			}
			continue
		}
		if gotDesc, _ := g["description"].(string); gotDesc != want["description"] {
			t.Errorf("tool %s description drift:\n got: %q\nwant: %q", name, gotDesc, want["description"])
		}
		if !reflect.DeepEqual(normalizeJSON(g["inputSchema"]), normalizeJSON(want["inputSchema"])) {
			gotSchema, _ := json.MarshalIndent(g["inputSchema"], "", "  ")
			wantSchema, _ := json.MarshalIndent(want["inputSchema"], "", "  ")
			t.Errorf("tool %s inputSchema drift:\n got: %s\nwant: %s", name, gotSchema, wantSchema)
		}
	}
	for name := range goldenByName {
		if !seen[name] {
			t.Errorf("missing tool %s (present in Python golden set)", name)
		}
	}
	for name := range extensionTools {
		if !extensionsSeen[name] {
			t.Errorf("extension tool %s declared but not registered", name)
		}
	}
}

// normalizeJSON re-parses a value so both sides share float64/map[string]any
// representations before DeepEqual.
func normalizeJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
