package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// trailingArgs are valid trailing-stop arguments (SOL sell, 1.5% retracement
// with activation), matching the README example shape.
func trailingArgs() map[string]any {
	return map[string]any{
		"asset":           2,
		"isBuy":           false,
		"size":            "4.12",
		"retracement":     "1.5",
		"activationPrice": "250.5",
		"reduceOnly":      true,
	}
}

// TestTrailingStopRequest checks the argument→SDK-request mapping. The wire
// format itself (floatToWire, "1.5000%" rendering, action key order) is
// pinned in the SDK's own tests.
func TestTrailingStopRequest(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want func(*testing.T, map[string]any) // assertions on the built request
	}{
		{
			name: "percent with activation",
			args: trailingArgs(),
		},
		{
			name: "quote retracement, no activation",
			args: map[string]any{
				"size": "0.1", "retracement": "10", "retracementUnit": "quote",
			},
		},
		{
			name: "unit defaults to percent",
			args: map[string]any{"size": "1", "retracement": "0.25"},
		},
		{
			name: "blank activation means immediate",
			args: map[string]any{"size": "1", "retracement": "2", "activationPrice": "   "},
		},
		{
			name: "numeric size and activation accepted",
			args: map[string]any{
				"size": 0.25, "retracement": "10", "retracementUnit": "quote", "activationPrice": 30000.0,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isBuy, _ := tc.args["isBuy"].(bool)
			req, err := trailingStopRequest(tc.args, isBuy)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			wantSize, _ := coerceFloat(tc.args["size"])
			if req.Size != wantSize {
				t.Errorf("Size = %v, want %v", req.Size, wantSize)
			}
			wantRetr, _ := coerceFloat(tc.args["retracement"])
			if unit, _ := tc.args["retracementUnit"].(string); unit == "quote" {
				if req.Retracement.PriceDistance == nil || *req.Retracement.PriceDistance != wantRetr {
					t.Errorf("PriceDistance = %v, want %v", req.Retracement.PriceDistance, wantRetr)
				}
				if req.Retracement.Percent != nil {
					t.Errorf("Percent must be unset for quote unit: %v", *req.Retracement.Percent)
				}
			} else {
				if req.Retracement.Percent == nil || *req.Retracement.Percent != wantRetr {
					t.Errorf("Percent = %v, want %v", req.Retracement.Percent, wantRetr)
				}
				if req.Retracement.PriceDistance != nil {
					t.Errorf("PriceDistance must be unset for percent unit: %v", *req.Retracement.PriceDistance)
				}
			}
			if av, present := tc.args["activationPrice"]; present {
				if s, isStr := av.(string); !isStr || strings.TrimSpace(s) != "" {
					wantAct, _ := coerceFloat(av)
					if req.ActivationPx == nil || *req.ActivationPx != wantAct {
						t.Errorf("ActivationPx = %v, want %v", req.ActivationPx, wantAct)
					}
				} else if req.ActivationPx != nil {
					t.Errorf("blank activation must map to nil, got %v", *req.ActivationPx)
				}
			} else if req.ActivationPx != nil {
				t.Errorf("absent activation must map to nil, got %v", *req.ActivationPx)
			}
			if ro, _ := tc.args["reduceOnly"].(bool); req.ReduceOnly != ro {
				t.Errorf("ReduceOnly = %v, want %v", req.ReduceOnly, ro)
			}
		})
	}
}

// TestTrailingStopRequestValidation mirrors the reference's
// _build_trailing_stop_action validation messages.
func TestTrailingStopRequestValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"invalid unit", func(a map[string]any) { a["retracementUnit"] = "pct" },
			"Invalid retracementUnit: pct. Must be 'percent' or 'quote'."},
		{"non-string unit", func(a map[string]any) { a["retracementUnit"] = true },
			"Invalid retracementUnit: true. Must be 'percent' or 'quote'."},
		{"null unit", func(a map[string]any) { a["retracementUnit"] = nil },
			"Invalid retracementUnit: <nil>. Must be 'percent' or 'quote'."},
		{"zero size", func(a map[string]any) { a["size"] = "0" },
			"Invalid size: 0. Must be positive."},
		{"negative size", func(a map[string]any) { a["size"] = "-1" },
			"Invalid size: -1. Must be positive."},
		{"missing size", func(a map[string]any) { delete(a, "size") },
			"missing required parameter: size"},
		{"non-numeric size", func(a map[string]any) { a["size"] = "abc" },
			"invalid size parameter: abc. Must be a valid number."},
		{"zero retracement", func(a map[string]any) { a["retracement"] = "0" },
			"Invalid retracement: 0. Must be positive."},
		{"missing retracement", func(a map[string]any) { delete(a, "retracement") },
			"missing required parameter: retracement"},
		{"non-numeric activation", func(a map[string]any) { a["activationPrice"] = "abc" },
			"invalid activationPrice parameter: abc. Must be a valid number."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := trailingArgs()
			tc.mutate(args)
			_, err := trailingStopRequest(args, false)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPlaceTrailingStopOrder(t *testing.T) {
	fake, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{
			"status": "ok",
			"response": map[string]any{
				"type": "trailingStop",
				"data": map[string]any{"statuses": []any{map[string]any{"resting": map[string]any{"oid": 777}}}},
			},
		}
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_trailing_stop_order")

	out, isErr := callTool(t, st, trailingArgs())
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if got, want := out["message"], "Trailing stop order placed for SOL"; got != want {
		t.Errorf("message = %v, want %v", got, want)
	}
	info, ok := out["orderInfo"].(map[string]any)
	if !ok {
		t.Fatalf("orderInfo missing: %v", out)
	}
	if info["status"] != "resting" {
		t.Errorf("orderInfo.status = %v, want resting", info["status"])
	}
	if info["orderId"].(float64) != 777 {
		t.Errorf("orderInfo.orderId = %v, want 777", info["orderId"])
	}
	// data is the rebuilt Python-shaped map (D-2), as for place_order.
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %v", out)
	}
	if digString(data, "status") != "ok" {
		t.Errorf("data.status = %v, want ok", data["status"])
	}
	if resp, _ := data["response"].(map[string]any); resp["type"] != "trailingStop" {
		t.Errorf("data.response.type = %v, want trailingStop", data["response"])
	}
	// requestParams echoes the (int-normalized) arguments, like Python.
	params, ok := out["requestParams"].(map[string]any)
	if !ok {
		t.Fatalf("requestParams missing: %v", out)
	}
	if params["size"] != "4.12" || params["retracement"] != "1.5" {
		t.Errorf("requestParams echo wrong: %v", params)
	}

	// The SDK posts the signed trailingStop action; the raw body pins the
	// frontend's key order end to end through the SDK's wire struct.
	req := fake.lastRequest(t)
	if req.Path != "/exchange" {
		t.Fatalf("path = %s, want /exchange", req.Path)
	}
	wantAction := `"action":{"type":"trailingStop","asset":2,"isBuy":false,"sz":"4.12","reduceOnly":true,"retracement":{"pct":"1.5000%"},"activationPx":"250.5"}`
	if !strings.Contains(req.Raw, wantAction) {
		t.Errorf("action wire bytes wrong:\nraw:  %s\nwant: %s", req.Raw, wantAction)
	}
	assertSignedPayload(t, fake)
}

func TestPlaceTrailingStopOrderRejected(t *testing.T) {
	_, c := newFakeAPI(t, func(path string, payload map[string]any) any {
		return map[string]any{"status": "err", "response": "Insufficient margin"}
	})
	st := findTool(t, orderTools(c), "hyperliquid_place_trailing_stop_order")

	out, isErr := callTool(t, st, trailingArgs())
	if !isErr {
		t.Fatalf("expected error envelope, got %v", out)
	}
	if out["error"] != "order rejected: Insufficient margin" {
		t.Errorf("error = %v, want order rejected: Insufficient margin", out["error"])
	}
}

// TestPlaceTrailingStopOrderValidationSkipsNetwork checks that argument
// validation (and a bad asset index or wire-rounding failure) fail without
// an exchange call.
func TestPlaceTrailingStopOrderValidationSkipsNetwork(t *testing.T) {
	fake, c := newFakeAPI(t, nil)
	st := findTool(t, orderTools(c), "hyperliquid_place_trailing_stop_order")

	bad := trailingArgs()
	bad["retracement"] = "-1"
	out, isErr := callTool(t, st, bad)
	if !isErr || !strings.Contains(out["error"].(string), "Invalid retracement: -1. Must be positive.") {
		t.Fatalf("want retracement validation error, got %v", out)
	}

	asset := trailingArgs()
	asset["asset"] = 99
	out, isErr = callTool(t, st, asset)
	if !isErr || !strings.Contains(out["error"].(string), "invalid asset index 99") {
		t.Fatalf("want asset bounds error, got %v", out)
	}

	// Sizes needing >8 decimals fail inside the SDK's floatToWire before any
	// POST is made.
	rounding := trailingArgs()
	rounding["size"] = "4.123456789"
	out, isErr = callTool(t, st, rounding)
	if !isErr || !strings.Contains(out["error"].(string), "float_to_wire causes rounding") {
		t.Fatalf("want float_to_wire rounding error, got %v", out)
	}

	for _, req := range fake.requestsSnapshot() {
		if req.Path == "/exchange" {
			t.Errorf("no /exchange call expected on validation failure: %v", req.Payload)
		}
	}
}

// TestTrailingStopToolSchema pins the tool's MCP surface: name, required
// list, enum/default values.
func TestTrailingStopToolSchema(t *testing.T) {
	st := findTool(t, orderTools(nil), "hyperliquid_place_trailing_stop_order")
	if st.Tool.Description == "" {
		t.Error("description empty")
	}
	var sch struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(st.Tool.RawInputSchema, &sch); err != nil {
		t.Fatalf("schema unmarshal: %v", err)
	}
	wantRequired := []string{"asset", "isBuy", "size", "retracement"}
	if len(sch.Required) != len(wantRequired) {
		t.Fatalf("required = %v, want %v", sch.Required, wantRequired)
	}
	for i, r := range wantRequired {
		if sch.Required[i] != r {
			t.Errorf("required[%d] = %v, want %v", i, sch.Required[i], r)
		}
	}
	unit := sch.Properties["retracementUnit"]
	if unit["default"] != "percent" {
		t.Errorf("retracementUnit.default = %v, want percent", unit["default"])
	}
	enum, ok := unit["enum"].([]any)
	if !ok || len(enum) != 2 || enum[0] != "percent" || enum[1] != "quote" {
		t.Errorf("retracementUnit.enum = %v, want [percent quote]", unit["enum"])
	}
	if ro := sch.Properties["reduceOnly"]; ro["default"] != false {
		t.Errorf("reduceOnly.default = %v, want false", ro["default"])
	}
	if a := sch.Properties["asset"]; a["minimum"].(float64) != 0 {
		t.Errorf("asset.minimum = %v, want 0", a["minimum"])
	}
	if _, optional := sch.Properties["activationPrice"]; !optional {
		t.Error("activationPrice property missing")
	}
}
