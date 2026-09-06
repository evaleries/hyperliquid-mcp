package tools

import (
	"strings"
	"testing"
)

// HIP-3 builder-DEX order paths (stack bug #1): place/cancel/modify used to
// validate against the main-DEX meta only, rejecting builder asset IDs
// (110000+) and dex-prefixed coins ("xyz:CL"). The fixtures below model a
// two-entry xyz DEX: universe index 0 = xyz:XYZ100 (asset 110000), index 1 =
// xyz:CL (asset 110001).

// perpDexsBuilderFixture is a perpDexs body: index 0 is the null main-DEX
// slot, index 1 is xyz.
func perpDexsBuilderFixture() []any {
	return []any{
		nil,
		map[string]any{"name": "xyz", "fullName": "XYZ", "deployer": "0x0000000000000000000000000000000000000001"},
	}
}

// xyzMetaFixture is the dex-qualified meta body for "xyz".
func xyzMetaFixture() map[string]any {
	return map[string]any{
		"universe": []any{
			map[string]any{"name": "xyz:XYZ100", "szDecimals": 4, "maxLeverage": 20, "marginTableId": 1},
			map[string]any{"name": "xyz:CL", "szDecimals": 3, "maxLeverage": 20, "marginTableId": 2},
		},
		"marginTables":    []any{},
		"collateralToken": 0,
	}
}

// builderDexInfoHandler answers the builder path's /info fetches (perpDexs +
// dex-qualified meta) and serves exchangeBody for /exchange.
func builderDexInfoHandler(exchangeBody any) func(path string, payload map[string]any) any {
	return func(path string, payload map[string]any) any {
		if path == "/info" {
			switch payload["type"] {
			case "perpDexs":
				return perpDexsBuilderFixture()
			case "meta":
				if payload["dex"] == "xyz" {
					return xyzMetaFixture()
				}
			}
		}
		return exchangeBody
	}
}

// requestTypes summarizes recorded requests as "path:type" for sequence
// assertions (/exchange requests have no "type" key at the top level).
func requestTypes(fake *fakeAPI) []string {
	reqs := fake.requestsSnapshot()
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		typ, _ := r.Payload["type"].(string)
		out = append(out, r.Path+":"+typ)
	}
	return out
}

func TestPlaceOrderBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(
		exchangeOrderFixture(map[string]any{"resting": map[string]any{"oid": 424242}}),
	))
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{
		"asset": 110001.0, "isBuy": true, "size": "0.12", "price": "88",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order placed for xyz:CL" {
		t.Errorf("message: %v", out["message"])
	}

	// Fresh perpDexs + dex-meta fetches precede the signed order (D-5
	// freshness posture extended to the builder path).
	gotTypes := requestTypes(fake)
	wantTypes := []string{"/info:perpDexs", "/info:meta", "/exchange:"}
	if !jsonEqual(t, gotTypes, wantTypes) {
		t.Errorf("request sequence = %v, want %v", gotTypes, wantTypes)
	}
	metaReq := fake.lastRequestOfType(t, "meta")
	if metaReq.Payload["dex"] != "xyz" {
		t.Errorf("meta request dex = %v, want xyz", metaReq.Payload)
	}

	// Wire: the builder asset ID lands in the order untouched.
	action := exchangeAction(t, fake)
	orders := action["orders"].([]any)
	wantOrder := map[string]any{
		"a": 110001, "b": true, "p": "88", "s": "0.12", "r": false, "t": limitGtcWire(),
	}
	if !jsonEqual(t, orders[0], wantOrder) {
		t.Errorf("orders[0] = %v, want %v", orders[0], wantOrder)
	}
	assertSignedPayload(t, fake)
}

func TestPlaceOrderBuilderDexOutOfRange(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(nil))
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	// The xyz universe has 2 assets, so 110005 is past the end.
	out, isErr := callTool(t, st, map[string]any{"asset": 110005.0, "isBuy": true, "size": "0.1"})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	want := "invalid asset index 110005: xyz dex meta universe has 2 assets"
	if out["error"] != want {
		t.Errorf("error = %v, want %v", out["error"], want)
	}
	for _, rt := range requestTypes(fake) {
		if strings.HasPrefix(rt, "/exchange") {
			t.Errorf("no /exchange request must be sent on validation failure: %v", rt)
		}
	}
}

func TestPlaceOrderBuilderReservedSlot(t *testing.T) {
	// 100000-109999 is the unassigned perpDexIndex-0 slot; rejected before
	// any network call (nil handler fails the test on traffic).
	_, c := newFakeAPI(t, nil)
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	out, isErr := callTool(t, st, map[string]any{"asset": 100000.0, "isBuy": true, "size": "0.1"})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if msg := out["error"].(string); !strings.Contains(msg, "invalid asset index 100000") ||
		!strings.Contains(msg, "unassigned main-DEX slot") {
		t.Errorf("error: %v", out["error"])
	}
}

func TestPlaceOrderBuilderDexIndexMissing(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(nil))
	st := findTool(t, orderTools(c), "hyperliquid_place_order")

	// perpDexIndex 5 with only 2 perpDexs entries.
	out, isErr := callTool(t, st, map[string]any{"asset": 150000.0, "isBuy": true, "size": "0.1"})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	want := "invalid asset index 150000: no perp dex at index 5 (perpDexs has 2 entries)"
	if out["error"] != want {
		t.Errorf("error = %v, want %v", out["error"], want)
	}
	if got := requestTypes(fake); len(got) != 1 || got[0] != "/info:perpDexs" {
		t.Errorf("requests = %v, want only the perpDexs fetch", got)
	}
}

func TestBracketOrderBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(
		exchangeOrderFixture(
			map[string]any{"resting": map[string]any{"oid": 1}},
			map[string]any{"resting": map[string]any{"oid": 2}},
			map[string]any{"resting": map[string]any{"oid": 3}},
		),
	))
	st := findTool(t, orderTools(c), "hyperliquid_place_bracket_order")

	out, isErr := callTool(t, st, map[string]any{
		"asset": 110001.0, "isBuy": true, "size": "0.12",
		"entryPrice": "88", "takeProfitPrice": "95", "stopLossPrice": "80",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	// All three legs carry the builder asset ID; TP/SL flip to the sell side.
	action := exchangeAction(t, fake)
	orders := action["orders"].([]any)
	wantOrders := []any{
		map[string]any{"a": 110001, "b": true, "p": "88", "s": "0.12", "r": false, "t": limitGtcWire()},
		map[string]any{
			"a": 110001, "b": false, "p": "95", "s": "0.12", "r": true,
			"t": map[string]any{"trigger": map[string]any{"isMarket": false, "triggerPx": "95", "tpsl": "tp"}},
		},
		map[string]any{
			"a": 110001, "b": false, "p": "80", "s": "0.12", "r": true,
			"t": map[string]any{"trigger": map[string]any{"isMarket": false, "triggerPx": "80", "tpsl": "sl"}},
		},
	}
	if !jsonEqual(t, orders, wantOrders) {
		t.Errorf("orders = %v, want %v", orders, wantOrders)
	}
	assertSignedPayload(t, fake)
}

func TestCancelOrderBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(cancelSuccessFixture()))
	st := findTool(t, orderTools(c), "hyperliquid_cancel_order")

	out, isErr := callTool(t, st, map[string]any{"coin": "xyz:CL", "oid": 123.0})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order 123 cancelled for xyz:CL" {
		t.Errorf("message: %v", out["message"])
	}
	action := exchangeAction(t, fake)
	wantAction := map[string]any{"type": "cancel", "cancels": []any{map[string]any{"a": 110001, "o": 123}}}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestCancelOrderUnknownBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(cancelSuccessFixture()))
	st := findTool(t, orderTools(c), "hyperliquid_cancel_order")

	out, isErr := callTool(t, st, map[string]any{"coin": "nope:CL", "oid": 123.0})
	if !isErr {
		t.Fatalf("expected isError envelope: %v", out)
	}
	if out["error"] != `unknown perp dex "nope" (not in perpDexs)` {
		t.Errorf("error: %v", out["error"])
	}
	for _, rt := range requestTypes(fake) {
		if strings.HasPrefix(rt, "/exchange") {
			t.Errorf("no /exchange request must be sent for an unknown dex: %v", rt)
		}
	}
}

func TestModifyOrderBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, builderDexInfoHandler(map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "batchModify",
			"data": map[string]any{"statuses": []any{
				map[string]any{"resting": map[string]any{"oid": 123}},
			}},
		},
	}))
	st := findTool(t, orderTools(c), "hyperliquid_modify_order")

	out, isErr := callTool(t, st, map[string]any{
		"oid": 123.0, "coin": "xyz:CL", "isBuy": true, "size": "0.2", "price": "91",
	})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Order 123 modified successfully" {
		t.Errorf("message: %v", out["message"])
	}
	action := exchangeAction(t, fake)
	wantAction := map[string]any{
		"type": "batchModify",
		"modifies": []any{map[string]any{
			"oid":   123,
			"order": map[string]any{"a": 110001, "b": true, "p": "91", "s": "0.2", "r": false, "t": limitGtcWire()},
		}},
	}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestCancelAllOrdersBuilderDex(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		if path == "/info" {
			switch payload["type"] {
			case "openOrders":
				return []any{
					map[string]any{"coin": "xyz:CL", "oid": 12345, "limitPx": "88.0", "side": "B", "sz": "0.12"},
					map[string]any{"coin": "xyz:XYZ100", "oid": 12346, "limitPx": "950.0", "side": "A", "sz": "1.0"},
				}
			case "perpDexs":
				return perpDexsBuilderFixture()
			case "meta":
				if payload["dex"] == "xyz" {
					return xyzMetaFixture()
				}
			}
		}
		return cancelSuccessFixture()
	})
	st := findTool(t, orderTools(c), "hyperliquid_cancel_all_orders")

	out, isErr := callTool(t, st, map[string]any{"dex": "xyz"})
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Cancelled 2 orders" {
		t.Errorf("message: %v", out["message"])
	}

	reqs := fake.requestsSnapshot()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want openOrders + perpDexs + meta + cancel: %v", len(reqs), requestTypes(fake))
	}
	wantLookup := map[string]any{"type": "openOrders", "user": testAccountAddress, "dex": "xyz"}
	if !jsonEqual(t, reqs[0].Payload, wantLookup) {
		t.Errorf("lookup = %v, want %v", reqs[0].Payload, wantLookup)
	}
	action := exchangeAction(t, fake)
	wantAction := map[string]any{
		"type": "cancel",
		"cancels": []any{
			map[string]any{"a": 110001, "o": 12345},
			map[string]any{"a": 110000, "o": 12346},
		},
	}
	if !jsonEqual(t, action, wantAction) {
		t.Errorf("action = %v, want %v", action, wantAction)
	}
	assertSignedPayload(t, fake)
}
