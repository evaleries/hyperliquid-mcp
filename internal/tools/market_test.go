package tools

import (
	"testing"
)

// orderBookFixture mirrors a real l2Book response: two level arrays plus the
// "time" field the API adds (passthrough proof).
func orderBookFixture() map[string]any {
	level := func(px, sz string, n int) map[string]any {
		return map[string]any{"px": px, "sz": sz, "n": n}
	}
	return map[string]any{
		"coin": "BTC",
		"time": 1756900000000,
		"levels": []any{
			[]any{level("49999.0", "0.5", 3), level("49998.0", "1.2", 1), level("49997.0", "0.7", 2)},
			[]any{level("50001.0", "0.4", 1), level("50002.0", "2.0", 5)},
		},
	}
}

func TestGetMeta(t *testing.T) {
	// Post-startup {"type":"meta"} calls are tool traffic: the fake records
	// them (startup SDK fetches are not recorded) and serves the fixture.
	fake, c := newFakeAPI(t, nil)
	st := findTool(t, marketTools(c), "hyperliquid_get_meta")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// Wire body parity with the Python SDK: meta always carries "dex" ("").
	metaReq := fake.lastRequestOfType(t, "meta")
	if want := map[string]any{"type": "meta", "dex": ""}; !jsonEqual(t, metaReq.Payload, want) {
		t.Errorf("meta wire body = %v, want %v", metaReq.Payload, want)
	}
	if out["message"] != "Exchange metadata retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfAssets"] != 3.0 {
		t.Errorf("numberOfAssets: %v", summary["numberOfAssets"])
	}
	// BTC and SOL lack onlyIsolated -> false; ETH carries it as true.
	want := []any{
		map[string]any{"index": 0.0, "name": "BTC", "maxLeverage": 40.0, "onlyIsolated": false},
		map[string]any{"index": 1.0, "name": "ETH", "maxLeverage": 25.0, "onlyIsolated": true},
		map[string]any{"index": 2.0, "name": "SOL", "maxLeverage": 20.0, "onlyIsolated": false},
	}
	if !jsonEqual(t, summary["assetsWithIndices"], want) {
		t.Errorf("assetsWithIndices = %v, want %v", summary["assetsWithIndices"], want)
	}
	// data passthrough: fields the summary drops survive untouched.
	data := out["data"].(map[string]any)
	if data["collateralToken"] != 0.0 {
		t.Errorf("data lost collateralToken: %v", data)
	}
	universe := data["universe"].([]any)
	if universe[0].(map[string]any)["szDecimals"] != 5.0 {
		t.Errorf("data universe lost szDecimals: %v", universe[0])
	}
}

func TestAllMids(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"BTC": "50000.5", "ETH": "3000.25", "PURR": "0.42"}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_all_mids")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "All mid prices retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["numberOfAssets"] != 3.0 {
		t.Errorf("numberOfAssets: %v", summary["numberOfAssets"])
	}
	data := out["data"].(map[string]any)
	if data["PURR"] != "0.42" {
		t.Errorf("data passthrough lost an entry: %v", data)
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{"type": "allMids", "dex": ""}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestAllMidsEmpty(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_all_mids")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["summary"].(map[string]any)["numberOfAssets"] != 0.0 {
		t.Errorf("numberOfAssets: %v", out["summary"])
	}
}

func TestOrderBook(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return orderBookFixture()
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_order_book")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order book for BTC retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["coin"] != "BTC" || summary["bidsCount"] != 3.0 || summary["asksCount"] != 2.0 {
		t.Errorf("summary: %v", summary)
	}
	// data passthrough: the API's "time" field survives.
	if out["data"].(map[string]any)["time"] != 1756900000000.0 {
		t.Errorf("data lost the time field: %v", out["data"])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{"type": "l2Book", "coin": "BTC"}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestOrderBookNoLevels(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"coin": "BTC", "time": 1756900000000}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_order_book")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	summary := out["summary"].(map[string]any)
	if summary["bidsCount"] != 0.0 || summary["asksCount"] != 0.0 {
		t.Errorf("absent levels must count 0/0: %v", summary)
	}
}

func TestOrderBookMissingCoin(t *testing.T) {
	// nil handler: reaching the network would fail the test.
	_, c := newFakeAPI(t, nil)
	st := findTool(t, marketTools(c), "hyperliquid_get_order_book")

	out, isErr := callTool(t, st, map[string]any{"stray": "arg"})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_order_book" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["stray"] != "arg" || len(argsEnv) != 1 {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestRecentTrades(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{
			map[string]any{"coin": "BTC", "side": "B", "px": "50000.0", "sz": "0.1", "time": 1756900000000, "unexpectedField": "kept"},
			map[string]any{"coin": "BTC", "side": "A", "px": "50001.0", "sz": "0.2", "time": 1756900001000},
		}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_recent_trades")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Recent trades for BTC retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["coin"] != "BTC" || summary["numberOfTrades"] != 2.0 {
		t.Errorf("summary: %v", summary)
	}
	trades := out["data"].([]any)
	if trades[0].(map[string]any)["unexpectedField"] != "kept" {
		t.Errorf("data passthrough lost the extra field: %v", trades[0])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	want := map[string]any{"type": "recentTrades", "coin": "BTC"}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestRecentTradesEmpty(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_recent_trades")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["summary"].(map[string]any)["numberOfTrades"] != 0.0 {
		t.Errorf("numberOfTrades: %v", out["summary"])
	}
}

func TestRecentTradesErrorOnBadShape(t *testing.T) {
	// An object where an array is expected fails decoding -> error envelope.
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"unexpected": "shape"}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_recent_trades")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC"})
	if !isErr {
		t.Fatalf("expected isError result: %v", out)
	}
	if out["tool"] != "hyperliquid_get_recent_trades" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["coin"] != "BTC" {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestHistoricalFunding(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{
			map[string]any{"coin": "BTC", "fundingRate": "0.0001", "premium": "0.0002", "time": 1700000000000, "extraApiField": "kept"},
			map[string]any{"coin": "BTC", "fundingRate": "0.00011", "premium": "0.00021", "time": 1700003600000},
		}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_historical_funding")

	// startTime as float64 exercises the pre-dispatch int normalization.
	out, isErr := callTool(t, st, map[string]any{"coin": "BTC", "startTime": float64(1700000000000)})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Historical funding for BTC retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["coin"] != "BTC" || summary["numberOfEntries"] != 2.0 {
		t.Errorf("summary: %v", summary)
	}
	entries := out["data"].([]any)
	if entries[0].(map[string]any)["extraApiField"] != "kept" {
		t.Errorf("data passthrough lost the extra field: %v", entries[0])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	// endTime omitted -> key absent entirely (hyperliquid-python-sdk parity).
	if _, ok := req.Payload["endTime"]; ok {
		t.Errorf("endTime key must be absent when omitted: %v", req.Payload)
	}
	want := map[string]any{"type": "fundingHistory", "coin": "BTC", "startTime": float64(1700000000000)}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestHistoricalFundingWithEndTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_historical_funding")

	out, isErr := callTool(t, st, map[string]any{
		"coin":      "BTC",
		"startTime": float64(1700000000000),
		"endTime":   float64(1700003600000),
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["summary"].(map[string]any)["numberOfEntries"] != 0.0 {
		t.Errorf("numberOfEntries: %v", out["summary"])
	}

	req := fake.lastRequest(t)
	want := map[string]any{
		"type":      "fundingHistory",
		"coin":      "BTC",
		"startTime": float64(1700000000000),
		"endTime":   float64(1700003600000),
	}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

// TestHistoricalFundingZeroEndTime pins the Python falsy remap
// (server.py: `... if arguments.get("endTime") else None`): endTime=0 omits
// the key exactly like an absent endTime.
func TestHistoricalFundingZeroEndTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_historical_funding")

	if _, isErr := callTool(t, st, map[string]any{
		"coin": "BTC", "startTime": float64(1700000000000), "endTime": 0.0,
	}); isErr {
		t.Fatal("unexpected error envelope")
	}
	req := fake.lastRequest(t)
	if _, present := req.Payload["endTime"]; present {
		t.Errorf("endTime=0 must be omitted (Python falsy remap): %v", req.Payload)
	}
}

// TestGetCandlesZeroEndTime: same falsy remap, but candleSnapshot keeps the
// key and sends null.
func TestGetCandlesZeroEndTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_candles")

	if _, isErr := callTool(t, st, map[string]any{
		"coin": "BTC", "interval": "1h", "startTime": float64(1700000000000), "endTime": 0.0,
	}); isErr {
		t.Fatal("unexpected error envelope")
	}
	req := fake.lastRequest(t)
	inner, ok := req.Payload["req"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing req object: %v", req.Payload)
	}
	v, present := inner["endTime"]
	if !present {
		t.Errorf("candleSnapshot must always carry endTime: %v", inner)
	}
	if v != nil {
		t.Errorf("endTime=0 must become null (Python falsy remap), got %v", v)
	}
}

func TestGetCandles(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		candle := func(ts int64) map[string]any {
			return map[string]any{
				"t": ts, "T": ts + 3599999, "s": "BTC", "i": "1h",
				"o": "50000.0", "c": "50100.0", "h": "50200.0", "l": "49900.0",
				"v": "12.5", "n": 42, "extraApiField": "kept",
			}
		}
		return []any{candle(1700000000000), candle(1700003600000)}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_candles")

	out, isErr := callTool(t, st, map[string]any{
		"coin": "BTC", "interval": "1h", "startTime": float64(1700000000000),
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Candles for BTC (1h) retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	summary := out["summary"].(map[string]any)
	if summary["coin"] != "BTC" || summary["interval"] != "1h" || summary["numberOfCandles"] != 2.0 {
		t.Errorf("summary: %v", summary)
	}
	candles := out["data"].([]any)
	if candles[0].(map[string]any)["extraApiField"] != "kept" {
		t.Errorf("data passthrough lost the extra field: %v", candles[0])
	}

	req := fake.lastRequest(t)
	if req.Path != "/info" {
		t.Errorf("path: %s", req.Path)
	}
	// req nesting; endTime present and null when omitted.
	reqBody, ok := req.Payload["req"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing req object: %v", req.Payload)
	}
	if v, ok := reqBody["endTime"]; !ok || v != nil {
		t.Errorf("req.endTime must be present and null when omitted: %v", reqBody)
	}
	want := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      "BTC",
			"interval":  "1h",
			"startTime": float64(1700000000000),
			"endTime":   nil,
		},
	}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}

func TestGetCandlesWithEndTime(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return []any{}
	})
	st := findTool(t, marketTools(c), "hyperliquid_get_candles")

	out, isErr := callTool(t, st, map[string]any{
		"coin": "ETH", "interval": "4h",
		"startTime": float64(1700000000000), "endTime": float64(1700086400000),
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["summary"].(map[string]any)["numberOfCandles"] != 0.0 {
		t.Errorf("numberOfCandles: %v", out["summary"])
	}

	req := fake.lastRequest(t)
	want := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      "ETH",
			"interval":  "4h",
			"startTime": float64(1700000000000),
			"endTime":   float64(1700086400000),
		},
	}
	if !jsonEqual(t, req.Payload, want) {
		t.Errorf("payload = %v, want %v", req.Payload, want)
	}
}
