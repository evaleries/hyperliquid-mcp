package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/config"
	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// testPrivateKey is Hardhat/Anvil well-known account #0. It is a public test
// vector, not a secret; it must never control real funds.
const testPrivateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// testAccountAddress is derived from testPrivateKey.
const testAccountAddress = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

// readGoldenFixture loads the Python list_tools golden fixture. The fixture
// is tracked (it is derived from static literals in the reference server, so
// it carries no secrets) and its absence is a hard failure: skipping would
// turn the repo's parity gate into a no-op.
//
// Provenance: edkdev/hyperliquid-mcp @ 7f39651, src/hyperliquid_mcp/server.py,
// list_tools() in declaration order. Regenerate by AST-extracting the
// Tool(name=, description=, inputSchema=) literals from that function into
// [{name, description, inputSchema}] with 2-space indent.
func readGoldenFixture(t testing.TB) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tools.python.json"))
	if err != nil {
		t.Fatalf("read golden fixture (the parity gate cannot run without it): %v", err)
	}
	return raw
}

// testMetaFixture is the /info meta response served at client construction
// (the SDK eagerly fetches meta and spotMeta). Indices: BTC=0, ETH=1, SOL=2.
func testMetaFixture() map[string]any {
	return map[string]any{
		"universe": []any{
			map[string]any{"name": "BTC", "szDecimals": 5, "maxLeverage": 40, "marginTableId": 1},
			map[string]any{"name": "ETH", "szDecimals": 4, "maxLeverage": 25, "marginTableId": 2, "onlyIsolated": true},
			map[string]any{"name": "SOL", "szDecimals": 2, "maxLeverage": 20, "marginTableId": 3},
		},
		"marginTables":    []any{},
		"collateralToken": 0,
	}
}

// recordedRequest captures one HTTP call to the fake API.
type recordedRequest struct {
	Path    string
	Payload map[string]any
}

// fakeAPI is an httptest server that answers the SDK's eager meta/spotMeta
// fetches and delegates everything else to the test's handler.
type fakeAPI struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	started  bool // client construction finished; later meta calls are tool traffic
	requests []recordedRequest
	// handler returns the JSON body to write for non-startup requests.
	handler func(path string, payload map[string]any) any
}

// newFakeAPI starts the fake API and builds an hl.Client against it.
// The handler may be nil for tests that never reach the network.
func newFakeAPI(t *testing.T, handler func(path string, payload map[string]any) any) (*fakeAPI, *hl.Client) {
	t.Helper()
	f := &fakeAPI{t: t, handler: handler}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	cfg, err := config.LoadFromEnv(func(key string) string {
		if key == config.EnvPrivateKey {
			return testPrivateKey
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.BaseURL = f.srv.URL

	c, err := hl.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("hl.New: %v", err)
	}
	f.mu.Lock()
	f.started = true
	f.mu.Unlock()
	return f, c
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var payload map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			f.t.Errorf("request body is not JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	f.mu.Lock()
	started := f.started
	f.mu.Unlock()

	// SDK startup fetches (NewExchange's internal NewInfo) are served but not
	// recorded. After startup, a "meta" request is tool traffic: recorded AND
	// served the fixture so get_meta wire bodies stay observable.
	if r.URL.Path == "/info" && !started {
		switch payload["type"] {
		case "meta":
			writeJSONResponse(w, testMetaFixture())
			return
		case "spotMeta":
			writeJSONResponse(w, map[string]any{"universe": []any{}, "tokens": []any{}})
			return
		}
	}

	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{Path: r.URL.Path, Payload: payload})
	f.mu.Unlock()

	if r.URL.Path == "/info" && payload["type"] == "meta" {
		// Main-dex meta stays pinned to the fixture so get_meta wire bodies
		// stay observable; dex-qualified meta (HIP-3) falls through to the
		// test's handler.
		if dex, _ := payload["dex"].(string); dex == "" {
			writeJSONResponse(w, testMetaFixture())
			return
		}
	}

	if f.handler == nil {
		f.t.Errorf("unexpected %s request: %v", r.URL.Path, payload)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, f.handler(r.URL.Path, payload))
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

// requestsSnapshot returns a copy of recorded non-startup requests.
func (f *fakeAPI) requestsSnapshot() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// lastRequest returns the most recent non-startup request.
func (f *fakeAPI) lastRequest(t *testing.T) recordedRequest {
	t.Helper()
	reqs := f.requestsSnapshot()
	if len(reqs) == 0 {
		t.Fatal("no API requests recorded")
	}
	return reqs[len(reqs)-1]
}

// lastRequestOfType returns the most recent recorded /info request with the
// given payload "type" (e.g. "meta" for a get_meta wire assertion).
func (f *fakeAPI) lastRequestOfType(t *testing.T, typ string) recordedRequest {
	t.Helper()
	reqs := f.requestsSnapshot()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].Path == "/info" && reqs[i].Payload["type"] == typ {
			return reqs[i]
		}
	}
	t.Fatalf("no recorded /info request of type %q", typ)
	return recordedRequest{}
}

// callTool invokes a tool handler and decodes the JSON text envelope.
// It fails the test on Go-level handler errors (envelope errors are returned,
// not thrown).
func callTool(t *testing.T, st server.ServerTool, args map[string]any) (map[string]any, bool) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := st.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned Go error (envelope error expected): %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly 1 content item, got %d", len(res.Content))
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("envelope is not JSON: %v\ntext: %s", err, text.Text)
	}
	return out, res.IsError
}

// findTool locates a registered tool by name.
func findTool(t *testing.T, tools []server.ServerTool, name string) server.ServerTool {
	t.Helper()
	for _, st := range tools {
		if st.Tool.Name == name {
			return st
		}
	}
	t.Fatalf("tool %s not registered", name)
	return server.ServerTool{}
}

// envelopeText returns the raw JSON text of a tool result (for
// indentation/passthrough assertions).
func envelopeText(t *testing.T, st server.ServerTool, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := st.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res.Content[0].(mcp.TextContent).Text
}
