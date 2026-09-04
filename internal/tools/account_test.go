package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// clearinghouseStateFixture mirrors a real API response, including fields the
// SDK structs would drop ("time") to prove data passthrough fidelity.
func clearinghouseStateFixture() map[string]any {
	return map[string]any{
		"assetPositions": []any{
			map[string]any{
				"position": map[string]any{
					"coin": "BTC", "szi": "0.5", "entryPx": "50000.0",
					"positionValue": "25000.0", "unrealizedPnl": "10.5",
					"returnOnEquity": "0.001", "marginUsed": "500.0",
					"leverage":      map[string]any{"type": "cross", "value": 10},
					"liquidationPx": nil, "cumFunding": map[string]any{"allTime": "1.0", "sinceChange": "0.1", "sinceOpen": "0.2"},
				},
				"type": "oneWay",
			},
		},
		"marginSummary": map[string]any{
			"accountValue": "10000.0", "totalMarginUsed": "50.25",
			"totalNtlPos": "25000.0", "totalRawUsd": "9950.0",
		},
		"crossMarginSummary": map[string]any{
			"accountValue": "10000.0", "totalMarginUsed": "50.25",
			"totalNtlPos": "25000.0", "totalRawUsd": "9950.0",
		},
		"withdrawable": "9949.75",
		"time":         1756900000000.0, // extra field: must survive passthrough
	}
}

func TestAccountInfo(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return clearinghouseStateFixture()
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_account_info")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Account information retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["accountValue"] != "10000.0" || summary["totalMarginUsed"] != "50.25" {
		t.Errorf("summary margin fields: %v", summary)
	}
	if summary["withdrawable"] != "9949.75" {
		t.Errorf("summary withdrawable: %v", summary)
	}
	if summary["numberOfPositions"] != 1.0 {
		t.Errorf("numberOfPositions: %v", summary["numberOfPositions"])
	}
	// data passthrough: extra API field "time" survives.
	data := out["data"].(map[string]any)
	if data["time"] != 1756900000000.0 {
		t.Errorf("data lost the extra field: %v", data)
	}
	if data["crossMarginSummary"] == nil {
		t.Errorf("data lost crossMarginSummary")
	}

	// Wire format: Python SDK always sends "dex", default user is the account.
	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{"type": "clearinghouseState", "user": testAccountAddress, "dex": ""}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestAccountInfoUserAddressOverride(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return clearinghouseStateFixture()
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_account_info")

	_, isErr := callTool(t, st, map[string]any{"userAddress": "0xabc", "dex": "xyz"})
	if isErr {
		t.Fatal("unexpected error")
	}
	req := fake.lastRequest(t)
	if req.Payload["user"] != "0xabc" || req.Payload["dex"] != "xyz" {
		t.Errorf("payload = %v", req.Payload)
	}
}

func TestGetPositions(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		fixture := clearinghouseStateFixture()
		delete(fixture, "crossMarginSummary") // exercises the null branch
		return fixture
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_positions")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %v", out)
	}
	if out["message"] != "Positions retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	data := out["data"].(map[string]any)
	if len(data) != 4 {
		t.Errorf("data should be narrowed to 4 keys, got %d: %v", len(data), data)
	}
	for _, k := range []string{"assetPositions", "marginSummary", "crossMarginSummary", "withdrawable"} {
		if _, ok := data[k]; !ok {
			t.Errorf("data missing key %s", k)
		}
	}
	if data["crossMarginSummary"] != nil {
		t.Errorf("absent crossMarginSummary should be null, got %v", data["crossMarginSummary"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfPositions"] != 1.0 || summary["accountValue"] != "10000.0" || summary["totalMarginUsed"] != "50.25" {
		t.Errorf("summary: %v", summary)
	}
}

func TestGetBalance(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return clearinghouseStateFixture()
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_balance")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %v", out)
	}
	data := out["data"].(map[string]any)
	if len(data) != 5 {
		t.Errorf("data should have 5 keys: %v", data)
	}
	summary := out["summary"].(map[string]any)
	// 10000.0 - 50.25 = 9949.75
	if summary["availableBalance"] != "9949.75" {
		t.Errorf("availableBalance: %v", summary["availableBalance"])
	}
	if summary["accountValue"] != "10000.0" || summary["withdrawable"] != "9949.75" {
		t.Errorf("summary: %v", summary)
	}
}

func TestGetBalanceIntegralAvailableBalance(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		f := clearinghouseStateFixture()
		f["marginSummary"] = map[string]any{
			"accountValue": "10000.0", "totalMarginUsed": "0.0",
			"totalNtlPos": "0.0", "totalRawUsd": "10000.0",
		}
		return f
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_balance")

	out, _ := callTool(t, st, map[string]any{})
	summary := out["summary"].(map[string]any)
	// Python str(10000.0 - 0.0) == "10000.0", never "10000"
	if summary["availableBalance"] != "10000.0" {
		t.Errorf("availableBalance should render Python-style: %v", summary["availableBalance"])
	}
}

func TestEnvelopeErrorOnAPIFailure(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"unexpected": "shape"} // no marginSummary
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_account_info")

	out, isErr := callTool(t, st, map[string]any{"userAddress": "0xabc"})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_account_info" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	// arguments echo (Python passes the original arguments dict).
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["userAddress"] != "0xabc" {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestEnvelopePrettyFormat(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return clearinghouseStateFixture()
	})
	st := findTool(t, accountTools(c), "hyperliquid_get_account_info")

	text := envelopeText(t, st, map[string]any{})
	if !strings.Contains(text, "\n  \"") {
		t.Errorf("expected 2-space indented JSON:\n%s", text)
	}
	if strings.HasSuffix(text, "\n") {
		t.Errorf("Python json.dumps has no trailing newline")
	}
}

// jsonEqual compares two JSON-compatible structures via marshal round-trip
// (key-order insensitive).
func jsonEqual(t *testing.T, got, want any) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	var gn, wn any
	if err := json.Unmarshal(g, &gn); err != nil {
		t.Fatalf("normalize got: %v", err)
	}
	if err := json.Unmarshal(w, &wn); err != nil {
		t.Fatalf("normalize want: %v", err)
	}
	gg, _ := json.Marshal(gn)
	ww, _ := json.Marshal(wn)
	return string(gg) == string(ww)
}
