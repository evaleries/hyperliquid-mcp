package tools

import (
	"encoding/json"
	"reflect"
	"testing"
)

// testVaultAddress is a 42-character hexadecimal address fixture.
const testVaultAddress = "0x1713904a06694c11bd81a2b16d6a41fe6b9a5044"

// vaultDetailsFixture mirrors a real vaultDetails API response, including an
// unknown field the SDK structs would drop, to prove data passthrough
// fidelity.
func vaultDetailsFixture() map[string]any {
	return map[string]any{
		"name":          "Test Vault",
		"vaultAddress":  testVaultAddress,
		"leader":        testAccountAddress,
		"description":   "A vault used by the Go parity tests",
		"apr":           0.123,
		"isClosed":      false,
		"allowDeposits": true,
		"followers": []any{
			map[string]any{"user": testAccountAddress, "vaultEquity": "1000.5"},
		},
		"relationship":    map[string]any{"type": "normal"},
		"someNewApiField": "preserved-verbatim",
	}
}

// assertVaultDetailsWire checks the raw wire body shared by both vault tools:
// {"type":"vaultDetails","vaultAddress":V,"user":null} — user key present and
// null, and never any startTime/endTime keys.
func assertVaultDetailsWire(t *testing.T, payload map[string]any, vaultAddress string) {
	t.Helper()
	if payload["type"] != "vaultDetails" {
		t.Errorf("payload type: %v", payload["type"])
	}
	if payload["vaultAddress"] != vaultAddress {
		t.Errorf("payload vaultAddress: %v", payload["vaultAddress"])
	}
	user, ok := payload["user"]
	if !ok {
		t.Error("payload missing user key (Python SDK sends it)")
	} else if user != nil {
		t.Errorf("payload user should be null, got %v", user)
	}
	for _, k := range []string{"startTime", "endTime"} {
		if _, ok := payload[k]; ok {
			t.Errorf("payload must not contain %s (endpoint takes no time range)", k)
		}
	}
}

func TestVaultDetails(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_details")

	out, isErr := callTool(t, st, map[string]any{"vaultAddress": testVaultAddress})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Vault details for "+testVaultAddress+" retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	if out["vaultAddress"] != testVaultAddress {
		t.Errorf("vaultAddress: %v", out["vaultAddress"])
	}
	// data passthrough: unknown API field survives untouched.
	data := out["data"].(map[string]any)
	if data["someNewApiField"] != "preserved-verbatim" {
		t.Errorf("data lost the extra field: %v", data)
	}
	if data["name"] != "Test Vault" || data["apr"] != 0.123 {
		t.Errorf("data: %v", data)
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	assertVaultDetailsWire(t, req.Payload, testVaultAddress)
}

func TestVaultDetailsMissingAddress(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_details")

	out, isErr := callTool(t, st, map[string]any{})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_vault_details" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] != "missing required parameter: vaultAddress" {
		t.Errorf("error: %v", out["error"])
	}
	if got := len(fake.requestsSnapshot()); got != 0 {
		t.Errorf("no API request expected, got %d", got)
	}
}

func TestVaultDetailsAPIFailure(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_details")
	fake.srv.Close() // force a transport failure on the next request

	out, isErr := callTool(t, st, map[string]any{"vaultAddress": testVaultAddress})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_vault_details" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["vaultAddress"] != testVaultAddress {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestVaultPerformance(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_performance")

	// 123.0/456.0 arrive as floats and must be int-normalized before dispatch.
	out, isErr := callTool(t, st, map[string]any{
		"vaultAddress": testVaultAddress,
		"startTime":    123.0,
		"endTime":      456.0,
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Vault performance for "+testVaultAddress+" retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	data := out["data"].(map[string]any)
	if data["someNewApiField"] != "preserved-verbatim" {
		t.Errorf("data lost the extra field: %v", data)
	}
	summary := out["summary"].(map[string]any)
	if summary["vaultAddress"] != testVaultAddress {
		t.Errorf("summary vaultAddress: %v", summary["vaultAddress"])
	}
	timeRange := summary["timeRange"].(map[string]any)
	if timeRange["startTime"] != 123.0 {
		t.Errorf("startTime: %v", timeRange["startTime"])
	}
	if timeRange["endTime"] != 456.0 {
		t.Errorf("endTime: %v", timeRange["endTime"])
	}

	// Same wire call as vault_details: no time range reaches the API.
	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	assertVaultDetailsWire(t, req.Payload, testVaultAddress)
}

func TestVaultPerformanceEndTimeOmitted(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_performance")

	out, isErr := callTool(t, st, map[string]any{
		"vaultAddress": testVaultAddress,
		"startTime":    123.0,
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	timeRange := out["summary"].(map[string]any)["timeRange"].(map[string]any)
	if timeRange["startTime"] != 123.0 {
		t.Errorf("startTime: %v", timeRange["startTime"])
	}
	if timeRange["endTime"] != "current" {
		t.Errorf("omitted endTime should render \"current\", got %v", timeRange["endTime"])
	}
}

func TestVaultPerformanceEndTimeZero(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_performance")

	// Python `end_time or "current"`: 0 is falsy and renders as "current".
	out, isErr := callTool(t, st, map[string]any{
		"vaultAddress": testVaultAddress,
		"startTime":    123.0,
		"endTime":      0.0,
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	timeRange := out["summary"].(map[string]any)["timeRange"].(map[string]any)
	if timeRange["endTime"] != "current" {
		t.Errorf("zero endTime should render \"current\", got %v", timeRange["endTime"])
	}
}

func TestVaultPerformanceMissingStartTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_performance")

	out, isErr := callTool(t, st, map[string]any{"vaultAddress": testVaultAddress})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_vault_performance" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] != "missing required parameter: startTime" {
		t.Errorf("error: %v", out["error"])
	}
	if got := len(fake.requestsSnapshot()); got != 0 {
		t.Errorf("no API request expected, got %d", got)
	}
}

func TestVaultPerformanceAPIFailure(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return vaultDetailsFixture()
	})
	st := findTool(t, vaultTools(c), "hyperliquid_vault_performance")
	fake.srv.Close() // force a transport failure on the next request

	out, isErr := callTool(t, st, map[string]any{
		"vaultAddress": testVaultAddress,
		"startTime":    123.0,
	})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_vault_performance" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	// arguments echo carries the normalized integer (Python parity).
	if argsEnv["startTime"] != 123.0 {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

// TestVaultUtilGoldenSchemaParity checks the slice's tools against the Python
// golden fixture directly, since TestGoldenSchemaParity only sees All() once
// the groups are integrated into tools.go.
func TestVaultUtilGoldenSchemaParity(t *testing.T) {
	raw := readGoldenFixture(t)
	var golden []map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden fixture: %v", err)
	}
	goldenByName := make(map[string]map[string]any, len(golden))
	for _, g := range golden {
		goldenByName[g["name"].(string)] = g
	}

	// Handlers are never invoked; a nil client is safe here (see
	// TestGoldenSchemaParity).
	registered := append(vaultTools(nil), utilTools(nil)...)
	if len(registered) != 3 {
		t.Fatalf("registered %d tools, want 3", len(registered))
	}
	for _, st := range registered {
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", st.Tool.Name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("normalize tool %s: %v", st.Tool.Name, err)
		}
		name := got["name"].(string)
		want, ok := goldenByName[name]
		if !ok {
			t.Errorf("tool %s not present in Python golden set", name)
			continue
		}
		if got["description"] != want["description"] {
			t.Errorf("tool %s description drift:\n got: %v\nwant: %v", name, got["description"], want["description"])
		}
		if !reflect.DeepEqual(normalizeJSON(got["inputSchema"]), normalizeJSON(want["inputSchema"])) {
			gotSchema, _ := json.MarshalIndent(got["inputSchema"], "", "  ")
			wantSchema, _ := json.MarshalIndent(want["inputSchema"], "", "  ")
			t.Errorf("tool %s inputSchema drift:\n got: %s\nwant: %s", name, gotSchema, wantSchema)
		}
	}
}
