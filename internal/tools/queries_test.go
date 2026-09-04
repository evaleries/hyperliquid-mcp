package tools

import (
	"encoding/json"
	"reflect"
	"testing"
)

// openOrdersFixture mirrors a real openOrders response, including a field
// ("triggerCondition") the SDK structs would normalize, to prove data
// passthrough fidelity.
func openOrdersFixture() []any {
	return []any{
		map[string]any{
			"coin": "BTC", "side": "B", "limitPx": "95000.0", "sz": "0.1",
			"oid": 12345, "timestamp": 1756900000000, "triggerCondition": "N/A",
		},
		map[string]any{
			"coin": "ETH", "side": "A", "limitPx": "4200.5", "sz": "1.5",
			"oid": 12346, "timestamp": 1756900001000, "origSz": "2.0",
		},
	}
}

// orderStatusFixture mirrors the orderStatus response shape.
func orderStatusFixture() map[string]any {
	return map[string]any{
		"status": "order",
		"order": map[string]any{
			"order": map[string]any{
				"coin": "BTC", "side": "B", "limitPx": "95000.0", "sz": "0.1",
				"oid": 12345, "timestamp": 1756900000000,
			},
			"status":          "open",
			"statusTimestamp": 1756900000001,
		},
	}
}

func userFillsFixture() []any {
	return []any{
		map[string]any{
			"coin": "BTC", "px": "95000.5", "sz": "0.1", "side": "B",
			"time": 1756900000000, "startPosition": "0.0", "dir": "Open Long",
			"closedPnl": "0.0", "hash": "0xabc", "oid": 12345, "fee": "0.05",
		},
		map[string]any{
			"coin": "ETH", "px": "4200.0", "sz": "1.0", "side": "A",
			"time": 1756900002000, "startPosition": "1.0", "dir": "Close Long",
			"closedPnl": "12.5", "hash": "0xdef", "oid": 12346, "fee": "0.02",
		},
	}
}

func userFundingFixture() []any {
	return []any{
		map[string]any{
			"time": 1756900000000, "coin": "BTC",
			"usdc": "-0.123", "szi": "0.1", "fundingRate": "0.0001",
		},
	}
}

func TestOpenOrders(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return openOrdersFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_open_orders")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Open orders retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfOrders"] != 2.0 {
		t.Errorf("numberOfOrders: %v", summary["numberOfOrders"])
	}
	// data passthrough: SDK-dropped fields survive untouched.
	data := out["data"].([]any)
	if data[0].(map[string]any)["triggerCondition"] != "N/A" {
		t.Errorf("data lost passthrough field: %v", data[0])
	}
	if data[1].(map[string]any)["limitPx"] != "4200.5" {
		t.Errorf("data number string formatting not preserved: %v", data[1])
	}

	// Wire format: Python SDK always sends "dex"; default user is the account.
	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	if _, ok := req.Payload["dex"]; !ok {
		t.Errorf("payload must always contain dex key: %v", req.Payload)
	}
	want := map[string]any{"type": "openOrders", "user": testAccountAddress, "dex": ""}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestOpenOrdersUserAddressOverride(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return openOrdersFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_open_orders")

	_, isErr := callTool(t, st, map[string]any{"userAddress": "0xabc", "dex": "xyz"})
	if isErr {
		t.Fatal("unexpected error")
	}
	req := fake.lastRequest(t)
	if req.Payload["user"] != "0xabc" || req.Payload["dex"] != "xyz" {
		t.Errorf("payload = %v", req.Payload)
	}
}

func TestOpenOrdersNullResponse(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return nil // JSON null body
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_open_orders")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfOrders"] != 0.0 {
		t.Errorf("null response should count 0 orders: %v", summary)
	}
	if out["data"] != nil {
		t.Errorf("data should pass null through, got %v", out["data"])
	}
}

func TestOpenOrdersErrorEnvelope(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"unexpected": "shape"} // not an array
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_open_orders")

	out, isErr := callTool(t, st, map[string]any{"dex": "xyz"})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_open_orders" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["dex"] != "xyz" {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestGetOrderStatus(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return orderStatusFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_order_status")

	out, isErr := callTool(t, st, map[string]any{"oid": 12345})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order status retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	if out["orderId"] != 12345.0 {
		t.Errorf("top-level orderId: %v", out["orderId"])
	}
	data := out["data"].(map[string]any)
	if data["status"] != "order" {
		t.Errorf("data passthrough: %v", data)
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{"type": "orderStatus", "user": testAccountAddress, "oid": 12345}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
	if _, hasDex := req.Payload["dex"]; hasDex {
		t.Errorf("orderStatus must not send dex: %v", req.Payload)
	}
}

func TestGetOrderStatusUserAddressOverride(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return orderStatusFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_order_status")

	_, isErr := callTool(t, st, map[string]any{"oid": 7, "userAddress": "0xabc"})
	if isErr {
		t.Fatal("unexpected error")
	}
	req := fake.lastRequest(t)
	if req.Payload["user"] != "0xabc" || req.Payload["oid"] != 7.0 {
		t.Errorf("payload = %v", req.Payload)
	}
}

func TestGetOrderStatusOidNormalization(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return orderStatusFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_order_status")

	// LLM clients may send integers as floats (123.0); the wire must carry 123.
	out, isErr := callTool(t, st, map[string]any{"oid": 123.0})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	req := fake.lastRequest(t)
	if req.Payload["oid"] != float64(123) {
		t.Errorf("wire oid = %v (%T), want 123", req.Payload["oid"], req.Payload["oid"])
	}
	if out["orderId"] != 123.0 {
		t.Errorf("orderId: %v", out["orderId"])
	}
}

func TestGetOrderStatusMissingOid(t *testing.T) {
	_, c := newFakeAPI(t, nil)
	st := findTool(t, queryTools(c), "hyperliquid_get_order_status")

	out, isErr := callTool(t, st, map[string]any{})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_order_status" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
}

func TestGetOrderStatusAPIFailure(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return orderStatusFixture()
	})
	fake.srv.Close() // transport failure on the next request
	st := findTool(t, queryTools(c), "hyperliquid_get_order_status")

	out, isErr := callTool(t, st, map[string]any{"oid": 42})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_order_status" {
		t.Errorf("tool: %v", out["tool"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["oid"] != 42.0 {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestUserFills(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return userFillsFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	args := map[string]any{"startTime": 1756900000000, "endTime": 1756900100000, "aggregateByTime": true}
	out, isErr := callTool(t, st, args)
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "User fills retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfFills"] != 2.0 {
		t.Errorf("numberOfFills: %v", summary["numberOfFills"])
	}
	timeRange := summary["timeRange"].(map[string]any)
	if timeRange["startTime"] != 1756900000000.0 || timeRange["endTime"] != 1756900100000.0 {
		t.Errorf("timeRange: %v", timeRange)
	}
	data := out["data"].([]any)
	if data[0].(map[string]any)["closedPnl"] != "0.0" {
		t.Errorf("data number string formatting not preserved: %v", data[0])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{
		"type": "userFillsByTime", "user": testAccountAddress,
		"startTime": 1756900000000, "endTime": 1756900100000, "aggregateByTime": true,
	}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestUserFillsDefaults(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return userFillsFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1756900000000})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// Python always sends the endTime key, null when omitted.
	req := fake.lastRequest(t)
	v, ok := req.Payload["endTime"]
	if !ok {
		t.Errorf("fills payload must contain endTime key: %v", req.Payload)
	}
	if v != nil {
		t.Errorf("omitted endTime should be null on the wire, got %v", v)
	}
	if req.Payload["aggregateByTime"] != false {
		t.Errorf("aggregateByTime default: %v", req.Payload["aggregateByTime"])
	}
	// Omitted endTime renders as "current" in the summary.
	summary := out["summary"].(map[string]any)
	timeRange := summary["timeRange"].(map[string]any)
	if timeRange["startTime"] != 1756900000000.0 {
		t.Errorf("timeRange.startTime: %v", timeRange)
	}
	if timeRange["endTime"] != "current" {
		t.Errorf("timeRange.endTime: %v", timeRange["endTime"])
	}
}

func TestUserFillsZeroEndTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return userFillsFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1000, "endTime": 0})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// Python `end_time or "current"`: 0 is falsy in the summary...
	summary := out["summary"].(map[string]any)
	if summary["timeRange"].(map[string]any)["endTime"] != "current" {
		t.Errorf("zero endTime should render as current: %v", summary)
	}
	// ...but is sent verbatim on the wire.
	req := fake.lastRequest(t)
	if req.Payload["endTime"] != 0.0 {
		t.Errorf("wire endTime: %v", req.Payload["endTime"])
	}
}

func TestUserFillsNullResponse(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return nil // JSON null body
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1000})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfFills"] != 0.0 {
		t.Errorf("null response should count 0 fills: %v", summary)
	}
}

func TestUserFillsMissingStartTime(t *testing.T) {
	_, c := newFakeAPI(t, nil)
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	out, isErr := callTool(t, st, map[string]any{})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_user_fills" {
		t.Errorf("tool: %v", out["tool"])
	}
}

func TestUserFillsErrorEnvelope(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"unexpected": "shape"} // not an array
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_fills")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1000})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_user_fills" {
		t.Errorf("tool: %v", out["tool"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["startTime"] != 1000.0 {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestUserFunding(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return userFundingFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_funding")

	args := map[string]any{"startTime": 1756900000000, "endTime": 1756900100000, "userAddress": "0xabc"}
	out, isErr := callTool(t, st, args)
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "User funding retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfEntries"] != 1.0 {
		t.Errorf("numberOfEntries: %v", summary["numberOfEntries"])
	}
	timeRange := summary["timeRange"].(map[string]any)
	if timeRange["startTime"] != 1756900000000.0 || timeRange["endTime"] != 1756900100000.0 {
		t.Errorf("timeRange: %v", timeRange)
	}
	data := out["data"].([]any)
	if data[0].(map[string]any)["fundingRate"] != "0.0001" {
		t.Errorf("data number string formatting not preserved: %v", data[0])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{
		"type": "userFunding", "user": "0xabc",
		"startTime": 1756900000000, "endTime": 1756900100000,
	}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestUserFundingDefaults(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return userFundingFixture()
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_funding")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1756900000000})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// Python omits the endTime key entirely when not provided.
	req := fake.lastRequest(t)
	if _, ok := req.Payload["endTime"]; ok {
		t.Errorf("funding payload must NOT contain endTime key: %v", req.Payload)
	}
	want := map[string]any{"type": "userFunding", "user": testAccountAddress, "startTime": 1756900000000}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
	summary := out["summary"].(map[string]any)
	timeRange := summary["timeRange"].(map[string]any)
	if timeRange["endTime"] != "current" {
		t.Errorf("timeRange.endTime: %v", timeRange["endTime"])
	}
}

func TestUserFundingNullResponse(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return nil // JSON null body
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_funding")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1000})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfEntries"] != 0.0 {
		t.Errorf("null response should count 0 entries: %v", summary)
	}
}

func TestUserFundingErrorEnvelope(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"unexpected": "shape"} // not an array
	})
	st := findTool(t, queryTools(c), "hyperliquid_get_user_funding")

	out, isErr := callTool(t, st, map[string]any{"startTime": 1000})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_user_funding" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
}

// TestQueryToolsSchemaParity checks this file's tools against the Python
// golden fixture now, without waiting for the All() integration (the
// project-wide TestGoldenSchemaParity gates the final wiring).
func TestQueryToolsSchemaParity(t *testing.T) {
	raw := readGoldenFixture(t)
	var golden []map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden fixture: %v", err)
	}
	goldenByName := make(map[string]map[string]any, len(golden))
	for _, g := range golden {
		goldenByName[g["name"].(string)] = g
	}

	wantNames := []string{
		"hyperliquid_get_open_orders",
		"hyperliquid_get_order_status",
		"hyperliquid_get_user_fills",
		"hyperliquid_get_user_funding",
	}
	tools := queryTools(nil) // handlers are never invoked; nil client is safe
	if len(tools) != len(wantNames) {
		t.Fatalf("queryTools returned %d tools, want %d", len(tools), len(wantNames))
	}
	for i, name := range wantNames {
		if tools[i].Tool.Name != name {
			t.Errorf("tool %d = %s, want %s (Python list_tools order)", i, tools[i].Tool.Name, name)
		}
		st := findTool(t, tools, name)
		b, err := json.Marshal(st.Tool)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("normalize tool %s: %v", name, err)
		}
		want, ok := goldenByName[name]
		if !ok {
			t.Errorf("tool %s not present in Python golden set", name)
			continue
		}
		if m["description"] != want["description"] {
			t.Errorf("tool %s description drift:\n got: %q\nwant: %q", name, m["description"], want["description"])
		}
		if !reflect.DeepEqual(normalizeJSON(m["inputSchema"]), normalizeJSON(want["inputSchema"])) {
			gotSchema, _ := json.MarshalIndent(m["inputSchema"], "", "  ")
			wantSchema, _ := json.MarshalIndent(want["inputSchema"], "", "  ")
			t.Errorf("tool %s inputSchema drift:\n got: %s\nwant: %s", name, gotSchema, wantSchema)
		}
	}
}
