package tools

import (
	"strings"
	"testing"
)

// exchangeOrderFixture is an /exchange order-action response body with the
// given statuses (the Python-passed-through shape).
func exchangeOrderFixture(statuses ...any) map[string]any {
	return map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "order",
			"data": map[string]any{"statuses": statuses},
		},
	}
}

// cancelSuccessFixture is an /exchange cancel-action success body.
func cancelSuccessFixture() map[string]any {
	return map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "cancel",
			"data": map[string]any{"statuses": []any{"success"}},
		},
	}
}

// exchangeAction returns the action object of the last recorded request,
// asserting it was a POST to /exchange.
func exchangeAction(t *testing.T, fake *fakeAPI) map[string]any {
	t.Helper()
	req := fake.lastRequest(t)
	if req.Path != "/exchange" {
		t.Fatalf("path = %s, want /exchange", req.Path)
	}
	action, ok := req.Payload["action"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing action object: %v", req.Payload)
	}
	return action
}

// assertSignedPayload checks the /exchange envelope keys every signed action
// shares: numeric nonce, {r,s,v} signature, and no vaultAddress when no vault
// is configured.
func assertSignedPayload(t *testing.T, fake *fakeAPI) {
	t.Helper()
	req := fake.lastRequest(t)
	if _, ok := req.Payload["nonce"].(float64); !ok {
		t.Errorf("nonce missing or not a number: %v", req.Payload["nonce"])
	}
	sig, ok := req.Payload["signature"].(map[string]any)
	if !ok {
		t.Fatalf("signature missing: %v", req.Payload)
	}
	for _, k := range []string{"r", "s", "v"} {
		if _, ok := sig[k]; !ok {
			t.Errorf("signature missing %q: %v", k, sig)
		}
	}
	if _, ok := req.Payload["vaultAddress"]; ok {
		t.Errorf("vaultAddress must be absent when no vault is configured: %v", req.Payload)
	}
}

// limitGtcWire is the wire form of the default order type.
func limitGtcWire() map[string]any {
	return map[string]any{"limit": map[string]any{"tif": "Gtc"}}
}

func TestPlaceOrder(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(map[string]any{"resting": map[string]any{"oid": 12345}})
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{
		"asset": 0.0, "isBuy": true, "size": "0.1", "price": "50000",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order placed for BTC" {
		t.Errorf("message: %v", out["message"])
	}
	wantData := map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "order",
			"data": map[string]any{"statuses": []any{
				map[string]any{"resting": map[string]any{"oid": 12345}},
			}},
		},
	}
	if !jsonEqual(t, out["data"], wantData) {
		t.Errorf("data = %v, want %v", out["data"], wantData)
	}
	wantInfo := map[string]any{
		"status": "resting", "orderId": 12345, "message": "Order placed and resting on order book",
	}
	if !jsonEqual(t, out["orderInfo"], wantInfo) {
		t.Errorf("orderInfo = %v, want %v", out["orderInfo"], wantInfo)
	}
	rp, ok := out["requestParams"].(map[string]any)
	if !ok {
		t.Fatalf("requestParams missing: %v", out)
	}
	if rp["asset"] != 0.0 || rp["isBuy"] != true || rp["size"] != "0.1" || rp["price"] != "50000" {
		t.Errorf("requestParams echo: %v", rp)
	}

	// Wire: Python order action with a/b/p/s/r/t keys (BTC = asset 0).
	action := exchangeAction(t, fake)
	if action["type"] != "order" {
		t.Errorf("action type: %v", action["type"])
	}
	orders, ok := action["orders"].([]any)
	if !ok || len(orders) != 1 {
		t.Fatalf("action orders: %v", action["orders"])
	}
	wantOrder := map[string]any{
		"a": 0, "b": true, "p": "50000", "s": "0.1", "r": false, "t": limitGtcWire(),
	}
	if !jsonEqual(t, orders[0], wantOrder) {
		t.Errorf("orders[0] = %v, want %v", orders[0], wantOrder)
	}
	assertSignedPayload(t, fake)
}

func TestPlaceOrderDefaults(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(map[string]any{"resting": map[string]any{"oid": 777}})
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	// No price/orderType/reduceOnly: price 0 (market), limit Gtc, false.
	out, isErr := callTool(t, st, map[string]any{"asset": 1.0, "isBuy": false, "size": "2"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order placed for ETH" {
		t.Errorf("message: %v", out["message"])
	}
	action := exchangeAction(t, fake)
	orders := action["orders"].([]any)
	wantOrder := map[string]any{
		"a": 1, "b": false, "p": "0", "s": "2", "r": false, "t": limitGtcWire(),
	}
	if !jsonEqual(t, orders[0], wantOrder) {
		t.Errorf("orders[0] = %v, want %v", orders[0], wantOrder)
	}
}

func TestPlaceOrderTrigger(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(map[string]any{"filled": map[string]any{"totalSz": "0.1", "avgPx": "48000.5", "oid": 888}})
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{
		"asset": 0.0, "isBuy": true, "size": "0.1", "price": "48000",
		"orderType": map[string]any{
			"trigger": map[string]any{"triggerPx": "48000.5", "isMarket": true, "tpsl": "tp"},
		},
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	wantInfo := map[string]any{
		"status": "filled", "orderId": 888, "totalSize": "0.1", "averagePrice": "48000.5",
		"message": "Order filled successfully",
	}
	if !jsonEqual(t, out["orderInfo"], wantInfo) {
		t.Errorf("orderInfo = %v, want %v", out["orderInfo"], wantInfo)
	}

	// triggerPx arrived as a string and must be coerced before sending; the
	// requestParams echo carries the coerced value (Python in-place mutation).
	rp := out["requestParams"].(map[string]any)
	trigger, ok := rp["orderType"].(map[string]any)["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("requestParams orderType.trigger: %v", rp)
	}
	if trigger["triggerPx"] != 48000.5 {
		t.Errorf("echoed triggerPx = %v (%T), want coerced 48000.5", trigger["triggerPx"], trigger["triggerPx"])
	}

	action := exchangeAction(t, fake)
	orders := action["orders"].([]any)
	wantType := map[string]any{
		"trigger": map[string]any{"isMarket": true, "triggerPx": "48000.5", "tpsl": "tp"},
	}
	if !jsonEqual(t, orders[0].(map[string]any)["t"], wantType) {
		t.Errorf("wire order type = %v, want %v", orders[0].(map[string]any)["t"], wantType)
	}
}

func TestPlaceOrderErrorStatus(t *testing.T) {
	// An API-level order error inside statuses is a successful tool
	// result carrying the error status (Python post() parity).
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(map[string]any{"error": "Insufficient balance"})
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{"asset": 0.0, "isBuy": true, "size": "0.1", "price": "50000"})
	if isErr {
		t.Fatalf("expected success envelope for status-level error: %v", out)
	}
	wantInfo := map[string]any{
		"status": "error", "error": "Insufficient balance", "message": "Order placement failed",
	}
	if !jsonEqual(t, out["orderInfo"], wantInfo) {
		t.Errorf("orderInfo = %v, want %v", out["orderInfo"], wantInfo)
	}
	data := out["data"].(map[string]any)
	if data["status"] != "ok" {
		t.Errorf("data.status: %v", data["status"])
	}
	statuses := data["response"].(map[string]any)["data"].(map[string]any)["statuses"].([]any)
	if statuses[0].(map[string]any)["error"] != "Insufficient balance" {
		t.Errorf("data status entry: %v", statuses[0])
	}
}

func TestPlaceOrderParamErrors(t *testing.T) {
	_, c := newFakeAPI(t, nil) // no request may reach the API handler
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing asset", map[string]any{"isBuy": true, "size": "0.1"}, "missing required parameter: asset"},
		{"missing isBuy", map[string]any{"asset": 0.0, "size": "0.1"}, "missing required parameter: isBuy"},
		{"non-bool isBuy", map[string]any{"asset": 0.0, "isBuy": "yes", "size": "0.1"}, "missing required parameter: isBuy"},
		{"missing size", map[string]any{"asset": 0.0, "isBuy": true}, "missing required parameter: size"},
		{"bad size", map[string]any{"asset": 0.0, "isBuy": true, "size": "abc"}, "invalid size parameter: abc. Must be a valid number."},
		{"orderType not object", map[string]any{"asset": 0.0, "isBuy": true, "size": "0.1", "orderType": "nope"}, "invalid orderType parameter: must be an object"},
		{"orderType empty", map[string]any{"asset": 0.0, "isBuy": true, "size": "0.1", "orderType": map[string]any{}}, `invalid orderType: must contain "limit" or "trigger"`},
		{"bad triggerPx", map[string]any{
			"asset": 0.0, "isBuy": true, "size": "0.1",
			"orderType": map[string]any{"trigger": map[string]any{"triggerPx": "abc", "isMarket": true, "tpsl": "tp"}},
		}, "invalid triggerPx parameter: abc. Must be a valid number."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := callTool(t, st, tc.args)
			if !isErr {
				t.Fatalf("expected isError envelope: %v", out)
			}
			if out["error"] != tc.want {
				t.Errorf("error = %v, want %v", out["error"], tc.want)
			}
			if out["tool"] != "hyperliquid_place_order" {
				t.Errorf("tool: %v", out["tool"])
			}
		})
	}
}

func TestPlaceOrderNormalizedAssetEcho(t *testing.T) {
	// The test meta fixture has 3 assets, so asset 5 is out of range; the
	// error envelope must still echo the normalized integer.
	_, c := newFakeAPI(t, nil)
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	for _, asset := range []any{5.0, "5"} {
		out, isErr := callTool(t, st, map[string]any{"asset": asset, "isBuy": true, "size": "0.1"})
		if !isErr {
			t.Fatalf("expected isError envelope: %v", out)
		}
		if !strings.Contains(out["error"].(string), "invalid asset index 5") {
			t.Errorf("error: %v", out["error"])
		}
		argsEnv := out["arguments"].(map[string]any)
		if argsEnv["asset"] != 5.0 {
			t.Errorf("arguments echo asset = %v (%T), want normalized 5", argsEnv["asset"], argsEnv["asset"])
		}
	}
}

func TestPlaceOrderTransportFailure(t *testing.T) {
	// A 200 body the SDK cannot parse ("ok" without response.data) makes
	// BulkOrders return a nil response, exactly like a transport/HTTP
	// failure — a tool error results.
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"status": "ok", "response": map[string]any{"type": "order"}}
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{"asset": 0.0, "isBuy": true, "size": "0.1", "price": "50000"})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_place_order" {
		t.Errorf("tool: %v", out["tool"])
	}
	if out["error"] == nil || out["error"] == "" {
		t.Errorf("error field: %v", out["error"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["asset"] != 0.0 || argsEnv["size"] != "0.1" {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestBracketOrder(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(
			map[string]any{"resting": map[string]any{"oid": 111}},
			map[string]any{"filled": map[string]any{"totalSz": "0.1", "avgPx": "50100.5", "oid": 222}},
			map[string]any{"error": "Insufficient balance"},
		)
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_bracket_order")

	out, isErr := callTool(t, st, map[string]any{
		"asset": 0.0, "isBuy": true, "size": "0.1",
		"entryPrice": "50000", "takeProfitPrice": "55000", "stopLossPrice": "45000",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Bracket order placed successfully" {
		t.Errorf("message: %v", out["message"])
	}

	// Wire: exactly 3 orders — entry, opposite-side reduce-only TP trigger,
	// opposite-side reduce-only SL trigger.
	action := exchangeAction(t, fake)
	if action["type"] != "order" {
		t.Errorf("action type: %v", action["type"])
	}
	orders, ok := action["orders"].([]any)
	if !ok || len(orders) != 3 {
		t.Fatalf("action orders: %v", action["orders"])
	}
	wantOrders := []any{
		map[string]any{"a": 0, "b": true, "p": "50000", "s": "0.1", "r": false, "t": limitGtcWire()},
		map[string]any{
			"a": 0, "b": false, "p": "55000", "s": "0.1", "r": true,
			"t": map[string]any{"trigger": map[string]any{"isMarket": false, "triggerPx": "55000", "tpsl": "tp"}},
		},
		map[string]any{
			"a": 0, "b": false, "p": "45000", "s": "0.1", "r": true,
			"t": map[string]any{"trigger": map[string]any{"isMarket": false, "triggerPx": "45000", "tpsl": "sl"}},
		},
	}
	if !jsonEqual(t, orders, wantOrders) {
		t.Errorf("orders = %v, want %v", orders, wantOrders)
	}
	assertSignedPayload(t, fake)

	// Envelope: parsed statuses labeled entry/take-profit/stop-loss in
	// submission order.
	wantParsed := []any{
		map[string]any{"status": "resting", "orderId": 111, "message": "Order placed and resting on order book", "orderType": "entry"},
		map[string]any{"status": "filled", "orderId": 222, "totalSize": "0.1", "averagePrice": "50100.5", "message": "Order filled successfully", "orderType": "take-profit"},
		map[string]any{"status": "error", "error": "Insufficient balance", "message": "Order placement failed", "orderType": "stop-loss"},
	}
	if !jsonEqual(t, out["orders"], wantParsed) {
		t.Errorf("orders = %v, want %v", out["orders"], wantParsed)
	}
	rp := out["requestParams"].(map[string]any)
	if rp["takeProfitPrice"] != "55000" || rp["stopLossPrice"] != "45000" {
		t.Errorf("requestParams echo: %v", rp)
	}
}

func TestBracketOrderDefaults(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return exchangeOrderFixture(
			map[string]any{"resting": map[string]any{"oid": 1}},
			map[string]any{"resting": map[string]any{"oid": 2}},
			map[string]any{"resting": map[string]any{"oid": 3}},
		)
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_bracket_order")

	// entryPrice/entryOrderType/reduceOnly omitted: market entry, limit Gtc.
	out, isErr := callTool(t, st, map[string]any{
		"asset": 2.0, "isBuy": false, "size": "4.96",
		"takeProfitPrice": "150", "stopLossPrice": "250",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Bracket order placed successfully" {
		t.Errorf("message: %v", out["message"])
	}
	action := exchangeAction(t, fake)
	orders := action["orders"].([]any)
	wantEntry := map[string]any{"a": 2, "b": false, "p": "0", "s": "4.96", "r": false, "t": limitGtcWire()}
	if !jsonEqual(t, orders[0], wantEntry) {
		t.Errorf("entry = %v, want %v", orders[0], wantEntry)
	}
	// Short bracket: TP/SL flip to the buy side.
	wantTP := map[string]any{
		"a": 2, "b": true, "p": "150", "s": "4.96", "r": true,
		"t": map[string]any{"trigger": map[string]any{"isMarket": false, "triggerPx": "150", "tpsl": "tp"}},
	}
	if !jsonEqual(t, orders[1], wantTP) {
		t.Errorf("take-profit = %v, want %v", orders[1], wantTP)
	}
}

func TestCancelOrder(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return cancelSuccessFixture()
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_order")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC", "oid": 123.0})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order 123 cancelled for BTC" {
		t.Errorf("message: %v", out["message"])
	}
	if !jsonEqual(t, out["data"], cancelSuccessFixture()) {
		t.Errorf("data = %v", out["data"])
	}
	wantCancelled := map[string]any{"coin": "BTC", "orderId": 123}
	if !jsonEqual(t, out["cancelledOrder"], wantCancelled) {
		t.Errorf("cancelledOrder = %v, want %v", out["cancelledOrder"], wantCancelled)
	}

	// Wire: cancel action keyed by asset index + oid.
	action := exchangeAction(t, fake)
	wantAction := map[string]any{"type": "cancel", "cancels": []any{map[string]any{"a": 0, "o": 123}}}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestCancelOrderTransportFailure(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return cancelSuccessFixture()
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_order")
	fake.srv.Close() // transport failure on the next request

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC", "oid": 123.0})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["tool"] != "hyperliquid_cancel_order" {
		t.Errorf("tool: %v", out["tool"])
	}
	argsEnv := out["arguments"].(map[string]any)
	if argsEnv["oid"] != 123.0 {
		t.Errorf("arguments echo: %v", argsEnv)
	}
}

func TestCancelAllOrders(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		if path == "/info" {
			return openOrdersFixture() // BTC oid 12345, ETH oid 12346
		}
		return cancelSuccessFixture()
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_all_orders")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Cancelled 2 orders" {
		t.Errorf("message: %v", out["message"])
	}
	if out["cancelledCount"] != 2.0 {
		t.Errorf("cancelledCount: %v", out["cancelledCount"])
	}
	if !jsonEqual(t, out["data"], cancelSuccessFixture()) {
		t.Errorf("data = %v", out["data"])
	}

	reqs := fake.requestsSnapshot()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want openOrders + cancel", len(reqs))
	}
	// The openOrders lookup mirrors the Python SDK body (dex always present).
	wantLookup := map[string]any{"type": "openOrders", "user": testAccountAddress, "dex": ""}
	if reqs[0].Path != "/info" || !jsonEqual(t, reqs[0].Payload, wantLookup) {
		t.Errorf("lookup = %s %v, want /info %v", reqs[0].Path, reqs[0].Payload, wantLookup)
	}
	action, ok := reqs[1].Payload["action"].(map[string]any)
	if !ok {
		t.Fatalf("cancel payload missing action: %v", reqs[1].Payload)
	}
	wantAction := map[string]any{
		"type": "cancel",
		"cancels": []any{
			map[string]any{"a": 0, "o": 12345},
			map[string]any{"a": 1, "o": 12346},
		},
	}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestCancelAllOrdersEmpty(t *testing.T) {
	for name, body := range map[string]any{
		"empty list": []any{},
		"null body":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
				return body
			})
			st := findTool(t, orderTools(c), "hyperliquid_cancel_all_orders")

			out, isErr := callTool(t, st, map[string]any{})
			if isErr {
				t.Fatalf("unexpected error envelope: %v", out)
			}
			if out["message"] != "No open orders to cancel" {
				t.Errorf("message: %v", out["message"])
			}
			if out["cancelledCount"] != 0.0 {
				t.Errorf("cancelledCount: %v", out["cancelledCount"])
			}
			// Exact Python-synthesized shape: no "type" key inside response.
			wantData := map[string]any{
				"status":   "ok",
				"response": map[string]any{"data": map[string]any{"statuses": []any{}}},
			}
			if !jsonEqual(t, out["data"], wantData) {
				t.Errorf("data = %v, want %v", out["data"], wantData)
			}
			// No cancel request was sent.
			if reqs := fake.requestsSnapshot(); len(reqs) != 1 || reqs[0].Path != "/info" {
				t.Errorf("requests = %v, want only the openOrders lookup", reqs)
			}
		})
	}
}

func TestModifyOrder(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{
			"status": "ok",
			"response": map[string]any{
				"type": "batchModify",
				"data": map[string]any{"statuses": []any{
					map[string]any{"resting": map[string]any{"oid": 123}},
				}},
			},
		}
	})
	st := findTool(t, orderTools(c), "hyperliquid_modify_order")

	out, isErr := callTool(t, st, map[string]any{
		"oid": 123.0, "coin": "BTC", "isBuy": true, "size": "0.2", "price": "51000",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order 123 modified successfully" {
		t.Errorf("message: %v", out["message"])
	}
	wantData := map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "batchModify",
			"data": map[string]any{"statuses": []any{
				map[string]any{"resting": map[string]any{"oid": 123}},
			}},
		},
	}
	if !jsonEqual(t, out["data"], wantData) {
		t.Errorf("data = %v, want %v", out["data"], wantData)
	}
	wantModified := map[string]any{"orderId": 123, "coin": "BTC", "newPrice": 51000, "newSize": 0.2}
	if !jsonEqual(t, out["modifiedOrder"], wantModified) {
		t.Errorf("modifiedOrder = %v, want %v", out["modifiedOrder"], wantModified)
	}

	// Wire: the Python SDK's modify_order delegates to bulk_modify_orders_new.
	action := exchangeAction(t, fake)
	wantAction := map[string]any{
		"type": "batchModify",
		"modifies": []any{map[string]any{
			"oid":   123,
			"order": map[string]any{"a": 0, "b": true, "p": "51000", "s": "0.2", "r": false, "t": limitGtcWire()},
		}},
	}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestModifyOrderAPIError(t *testing.T) {
	// Modify exception: a top-level non-ok body surfaces as a tool error
	// because the SDK hides the body behind a Go error.
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"status": "err", "response": "unknown oid"}
	})
	st := findTool(t, orderTools(c), "hyperliquid_modify_order")

	out, isErr := callTool(t, st, map[string]any{
		"oid": 123.0, "coin": "BTC", "isBuy": true, "size": "0.2", "price": "51000",
	})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if !strings.Contains(out["error"].(string), "unknown oid") {
		t.Errorf("error: %v", out["error"])
	}
	if out["tool"] != "hyperliquid_modify_order" {
		t.Errorf("tool: %v", out["tool"])
	}
}

func TestTwapStubs(t *testing.T) {
	tools := twapTools() // stubs never reach the network

	place := findTool(t, tools, "hyperliquid_place_twap_order")
	out, isErr := callTool(t, place, map[string]any{
		"coin": "BTC", "isBuy": true, "size": "0.1", "minutes": 5.0,
	})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["error"] != "TWAP orders require additional implementation" {
		t.Errorf("error: %v", out["error"])
	}
	if out["tool"] != "hyperliquid_place_twap_order" {
		t.Errorf("tool: %v", out["tool"])
	}
	if argsEnv := out["arguments"].(map[string]any); argsEnv["minutes"] != 5.0 {
		t.Errorf("arguments echo minutes: %v", argsEnv["minutes"])
	}

	cancel := findTool(t, tools, "hyperliquid_cancel_twap_order")
	out, isErr = callTool(t, cancel, map[string]any{"twapId": 7.0})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["error"] != "TWAP cancellation requires additional implementation" {
		t.Errorf("error: %v", out["error"])
	}
	if argsEnv := out["arguments"].(map[string]any); argsEnv["twapId"] != 7.0 {
		t.Errorf("arguments echo twapId: %v", argsEnv["twapId"])
	}

	// Integer normalization runs before the stub, so a malformed
	// twapId/minutes yields the coercion error instead of the stub text.
	out, _ = callTool(t, cancel, map[string]any{"twapId": "abc"})
	if out["error"] != "Invalid twapId parameter: abc. Must be a valid integer." {
		t.Errorf("coercion error: %v", out["error"])
	}
	out, _ = callTool(t, place, map[string]any{
		"coin": "BTC", "isBuy": true, "size": "0.1", "minutes": "abc",
	})
	if out["error"] != "Invalid minutes parameter: abc. Must be a valid integer." {
		t.Errorf("coercion error: %v", out["error"])
	}
}

// TestPlaceOrderTopLevelErr pins review finding 1: a top-level HTTP-200
// {"status":"err","response":"reason"} body makes Python's
// _parse_order_response raise (the "response" value is a string), so the tool
// must return an error envelope carrying the API's reason — not a fabricated
// success with an empty statuses list.
func TestPlaceOrderTopLevelErr(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"status": "err", "response": "Multi-sig required"}
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{"asset": 0, "isBuy": true, "size": "0.1", "price": "50000"})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "Multi-sig required") {
		t.Errorf("error should carry the API reason: %v", out["error"])
	}
	if out["tool"] != "hyperliquid_place_order" {
		t.Errorf("tool: %v", out["tool"])
	}
}

// TestCancelOrderTopLevelErr pins the other half of finding 1: Python's
// cancel handler never parses statuses, so a top-level err body passes
// through as SUCCESSFUL data with "response" as a string.
func TestCancelOrderTopLevelErr(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"status": "err", "response": "Multi-sig required"}
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_order")

	out, isErr := callTool(t, st, map[string]any{"coin": "BTC", "oid": 123})
	if isErr {
		t.Fatalf("Python parity: cancel passes the err body through as success: %v", out)
	}
	if out["message"] != "Order 123 cancelled for BTC" {
		t.Errorf("message: %v", out["message"])
	}
	data := out["data"].(map[string]any)
	if data["status"] != "err" || data["response"] != "Multi-sig required" {
		t.Errorf("data passthrough: %v", data)
	}
	co := out["cancelledOrder"].(map[string]any)
	if co["coin"] != "BTC" || co["orderId"].(float64) != 123 {
		t.Errorf("cancelledOrder: %v", co)
	}
}

// TestCancelAllTopLevelErr covers the same passthrough on the bulk path.
func TestCancelAllTopLevelErr(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		if path == "/info" {
			return openOrdersFixture()
		}
		return map[string]any{"status": "err", "response": "Multi-sig required"}
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_all_orders")

	out, isErr := callTool(t, st, map[string]any{})
	if isErr {
		t.Fatalf("Python parity: bulk cancel passes the err body through: %v", out)
	}
	data := out["data"].(map[string]any)
	if data["status"] != "err" || data["response"] != "Multi-sig required" {
		t.Errorf("data passthrough: %v", data)
	}
	if out["cancelledCount"].(float64) != 2 {
		t.Errorf("cancelledCount: %v", out["cancelledCount"])
	}
}
