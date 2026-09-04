package tools

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// perpDexsFixture mirrors the live mainnet shape (recorded 2026-09-04):
// index 0 is the null main-DEX slot, builder DEXs follow.
func perpDexsFixture() []any {
	return []any{
		nil,
		map[string]any{
			"name":     "xyz",
			"fullName": "XYZ",
			"deployer": "0x88806a71d74ad0a510b350545c9ae490912f0888",
		},
		map[string]any{
			"name":     "abc",
			"fullName": "ABC Markets",
			"deployer": "0x1111111111111111111111111111111111111111",
		},
	}
}

// dexMetaFixture mirrors the live xyz meta: dex-prefixed names plus HIP-3
// extras (growthMode) that enumeration must ignore.
func dexMetaFixture() map[string]any {
	return map[string]any{
		"universe": []any{
			map[string]any{"name": "xyz:XYZ100", "szDecimals": 4, "maxLeverage": 30, "marginTableId": 30, "growthMode": "enabled"},
			map[string]any{"name": "xyz:TSLA", "szDecimals": 3, "maxLeverage": 20, "marginTableId": 20, "growthMode": "enabled", "onlyIsolated": true},
		},
		"marginTables": []any{},
	}
}

// hip3Handler serves the HIP-3 fixtures: perpDexs always; any dex-qualified
// meta gets the builder-DEX fixture (main-dex meta is pinned by the fake API
// before this handler is consulted).
func hip3Handler(perpDexsCalls *atomic.Int32) func(string, map[string]any) any {
	return func(_ string, payload map[string]any) any {
		switch payload["type"] {
		case "perpDexs":
			perpDexsCalls.Add(1)
			return perpDexsFixture()
		case "meta":
			if payload["dex"] != "" {
				return dexMetaFixture()
			}
			return testMetaFixture()
		}
		return testMetaFixture()
	}
}

func TestGetPerpDexs(t *testing.T) {
	var perpDexsCalls atomic.Int32
	fake, c := newFakeAPI(t, hip3Handler(&perpDexsCalls))
	st := findTool(t, hip3Tools(c), "hyperliquid_get_perp_dexs")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if got := perpDexsCalls.Load(); got != 1 {
		t.Errorf("perpDexs called %d times, want 1", got)
	}
	// Wire body: exactly {"type":"perpDexs"} — no dex key.
	req := fake.lastRequestOfType(t, "perpDexs")
	if want := map[string]any{"type": "perpDexs"}; !jsonEqual(t, req.Payload, want) {
		t.Errorf("perpDexs wire body = %v, want %v", req.Payload, want)
	}
	if out["message"] != "Retrieved 2 builder perp DEXs" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfDexs"] != 2.0 {
		t.Errorf("numberOfDexs: %v", summary["numberOfDexs"])
	}
	dexs := summary["dexs"].([]any)
	// Null main-DEX slot skipped in the summary, but array indices preserved
	// (index doubles as perpDexIndex for hip3-trade asset-ID math). All
	// summary fields pinned, not just index/name.
	first := dexs[0].(map[string]any)
	if first["index"] != 1.0 || first["name"] != "xyz" ||
		first["fullName"] != "XYZ" || first["deployer"] != "0x88806a71d74ad0a510b350545c9ae490912f0888" {
		t.Errorf("dexs[0]: %v", first)
	}
	second := dexs[1].(map[string]any)
	if second["index"] != 2.0 || second["name"] != "abc" ||
		second["fullName"] != "ABC Markets" || second["deployer"] != "0x1111111111111111111111111111111111111111" {
		t.Errorf("dexs[1]: %v", second)
	}
	// data is the raw body: null-first array preserved through the decode.
	data := out["data"].([]any)
	if len(data) != 3 || data[0] != nil {
		t.Errorf("data: %v", data)
	}
}

func TestGetDexMetaDefaultXyz(t *testing.T) {
	var perpDexsCalls atomic.Int32
	fake, c := newFakeAPI(t, hip3Handler(&perpDexsCalls))
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// Default dex = "xyz": one perpDexs resolution call, then the meta call.
	if got := perpDexsCalls.Load(); got != 1 {
		t.Errorf("perpDexs called %d times, want 1", got)
	}
	req := fake.lastRequestOfType(t, "meta")
	if want := map[string]any{"type": "meta", "dex": "xyz"}; !jsonEqual(t, req.Payload, want) {
		t.Errorf("meta wire body = %v, want %v", req.Payload, want)
	}
	if out["message"] != "Meta for perp dex xyz retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["dex"] != "xyz" || summary["perpDexIndex"] != 1.0 || summary["assetIdBase"] != 110000.0 {
		t.Errorf("summary identity: %v", summary)
	}
	if summary["numberOfAssets"] != 2.0 {
		t.Errorf("numberOfAssets: %v", summary["numberOfAssets"])
	}
	assets := summary["assetsWithIndices"].([]any)
	// xyz:XYZ100 lacks onlyIsolated → false; xyz:TSLA carries true.
	xyz100 := assets[0].(map[string]any)
	if xyz100["index"] != 0.0 || xyz100["name"] != "xyz:XYZ100" || xyz100["onlyIsolated"] != false || xyz100["maxLeverage"] != 30.0 {
		t.Errorf("assets[0]: %v", xyz100)
	}
	tsla := assets[1].(map[string]any)
	if tsla["onlyIsolated"] != true {
		t.Errorf("assets[1]: %v", tsla)
	}
}

func TestGetDexMetaMainDex(t *testing.T) {
	var perpDexsCalls atomic.Int32
	fake, c := newFakeAPI(t, hip3Handler(&perpDexsCalls))
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	// Explicit empty dex selects the main DEX and skips perpDexs resolution.
	out, isErr := callTool(t, st, map[string]any{"dex": ""})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if got := perpDexsCalls.Load(); got != 0 {
		t.Errorf("perpDexs called %d times for main dex, want 0", got)
	}
	req := fake.lastRequestOfType(t, "meta")
	if want := map[string]any{"type": "meta", "dex": ""}; !jsonEqual(t, req.Payload, want) {
		t.Errorf("meta wire body = %v, want %v", req.Payload, want)
	}
	if out["message"] != "Meta for perp dex main retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["perpDexIndex"] != 0.0 || summary["assetIdBase"] != 100000.0 || summary["numberOfAssets"] != 3.0 {
		t.Errorf("summary: %v", summary)
	}
}

func TestGetDexMetaFetchesConcurrently(t *testing.T) {
	// The dex-index resolution and meta fetch must overlap (perf decision:
	// saves one round trip per call). Deterministic: the handler blocks each
	// request briefly, so sequential execution can never reach depth 2.
	var inFlight, maxInFlight atomic.Int32
	_, c := newFakeAPI(t, func(_ string, payload map[string]any) any {
		cur := inFlight.Add(1)
		for {
			max := maxInFlight.Load()
			if cur <= max || maxInFlight.CompareAndSwap(max, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		defer inFlight.Add(-1)
		switch payload["type"] {
		case "perpDexs":
			return perpDexsFixture()
		default:
			return dexMetaFixture()
		}
	})
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	if _, isErr := callTool(t, st, map[string]any{}); isErr {
		t.Fatal("unexpected error envelope")
	}
	if got := maxInFlight.Load(); got != 2 {
		t.Errorf("max concurrent requests = %d, want 2 (perpDexs + meta overlap)", got)
	}
}

func TestGetDexMetaUnknown(t *testing.T) {
	var perpDexsCalls atomic.Int32
	_, c := newFakeAPI(t, hip3Handler(&perpDexsCalls))
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	out, isErr := callTool(t, st, map[string]any{"dex": "nope"})
	if !isErr {
		t.Fatalf("expected error envelope, got: %v", out)
	}
	// The full message is spec-locked; assert it exactly.
	if want := `unknown perp dex "nope" (not in perpDexs)`; out["error"] != want {
		t.Errorf("error = %q, want %q", out["error"], want)
	}
}

func TestGetDexMetaSecondDex(t *testing.T) {
	// resolvePerpDex must scan past the first builder DEX.
	var perpDexsCalls atomic.Int32
	_, c := newFakeAPI(t, hip3Handler(&perpDexsCalls))
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	out, isErr := callTool(t, st, map[string]any{"dex": "abc"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	summary := out["summary"].(map[string]any)
	if summary["dex"] != "abc" || summary["perpDexIndex"] != 2.0 || summary["assetIdBase"] != 120000.0 {
		t.Errorf("summary: %v", summary)
	}
}

func TestGetPerpDexsMalformed(t *testing.T) {
	// decodePerpDexs error path via the get_perp_dexs tool itself.
	_, c := newFakeAPI(t, func(_ string, _ map[string]any) any {
		return "not-an-array"
	})
	st := findTool(t, hip3Tools(c), "hyperliquid_get_perp_dexs")

	out, isErr := callTool(t, st, map[string]any{})
	if !isErr {
		t.Fatalf("expected error envelope, got: %v", out)
	}
	if !strings.Contains(out["error"].(string), "failed to parse API response") {
		t.Errorf("error: %v", out["error"])
	}
}

func TestGetDexMetaMalformedPerpDexs(t *testing.T) {
	// Wrong-shape JSON (string instead of array) hits the rawToSlice error
	// path inside resolvePerpDex.
	_, c := newFakeAPI(t, func(_ string, payload map[string]any) any {
		if payload["type"] == "perpDexs" {
			return "not-an-array"
		}
		return testMetaFixture()
	})
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	out, isErr := callTool(t, st, map[string]any{"dex": "xyz"})
	if !isErr {
		t.Fatalf("expected error envelope, got: %v", out)
	}
	if !strings.Contains(out["error"].(string), "failed to parse API response") {
		t.Errorf("error: %v", out["error"])
	}
}

func TestGetDexMetaMalformedMeta(t *testing.T) {
	// Wrong-shape meta body (string instead of object) hits rawToMap.
	_, c := newFakeAPI(t, func(_ string, payload map[string]any) any {
		switch payload["type"] {
		case "perpDexs":
			return perpDexsFixture()
		case "meta":
			return "not-an-object"
		}
		return testMetaFixture()
	})
	st := findTool(t, hip3Tools(c), "hyperliquid_get_dex_meta")

	out, isErr := callTool(t, st, map[string]any{"dex": "xyz"})
	if !isErr {
		t.Fatalf("expected error envelope, got: %v", out)
	}
	if !strings.Contains(out["error"].(string), "failed to parse API response") {
		t.Errorf("error: %v", out["error"])
	}
}
