package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sonirico/go-hyperliquid"
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

// TestBuildTrailingStopActionWireJSON pins the exact wire JSON, including key
// order: the API recomputes the L1 action hash from the posted JSON, so the
// field order must match the frontend format the Python reference emits.
func TestBuildTrailingStopActionWireJSON(t *testing.T) {
	cases := []struct {
		name  string
		args  map[string]any
		asset int64
		isBuy bool
		want  string
	}{
		{
			name: "percent with activation",
			args: trailingArgs(), asset: 2, isBuy: false,
			want: `{"type":"trailingStop","asset":2,"isBuy":false,"sz":"4.12","reduceOnly":true,"retracement":{"pct":"1.5000%"},"activationPx":"250.5"}`,
		},
		{
			name: "quote retracement, no activation",
			args: map[string]any{
				"size": "0.1", "retracement": "10", "retracementUnit": "quote",
			},
			asset: 0, isBuy: true,
			want: `{"type":"trailingStop","asset":0,"isBuy":true,"sz":"0.1","reduceOnly":false,"retracement":{"px":"10"},"activationPx":null}`,
		},
		{
			name:  "unit defaults to percent",
			args:  map[string]any{"size": "1", "retracement": "0.25"},
			asset: 1, isBuy: false,
			want: `{"type":"trailingStop","asset":1,"isBuy":false,"sz":"1","reduceOnly":false,"retracement":{"pct":"0.2500%"},"activationPx":null}`,
		},
		{
			name: "blank activation means immediate",
			args: map[string]any{
				"size": "1", "retracement": "2", "activationPrice": "   ",
			},
			asset: 1, isBuy: false,
			want: `{"type":"trailingStop","asset":1,"isBuy":false,"sz":"1","reduceOnly":false,"retracement":{"pct":"2.0000%"},"activationPx":null}`,
		},
		{
			name: "numeric size and activation accepted",
			args: map[string]any{
				"size": 0.25, "retracement": "10", "retracementUnit": "quote", "activationPrice": 30000.0,
			},
			asset: 0, isBuy: true,
			want: `{"type":"trailingStop","asset":0,"isBuy":true,"sz":"0.25","reduceOnly":false,"retracement":{"px":"10"},"activationPx":"30000"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, err := buildTrailingStopAction(tc.args, tc.asset, tc.isBuy)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			got, err := json.Marshal(action)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("wire JSON:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestBuildTrailingStopActionValidation mirrors the reference's
// _build_trailing_stop_action validation messages.
func TestBuildTrailingStopActionValidation(t *testing.T) {
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
		{"size needing >8 decimals", func(a map[string]any) { a["size"] = "4.123456789" },
			"float_to_wire causes rounding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := trailingArgs()
			tc.mutate(args)
			_, err := buildTrailingStopAction(args, 2, false)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want substring %q", err, tc.want)
			}
		})
	}
}

// TestTrailingStopSignatureMatchesPython signs builder output with the SDK
// and compares against vectors produced by the Python reference
// (hyperliquid-python-sdk sign_l1_action, hardhat test key). A match proves
// the msgpack action serialization — field order, null handling, nested
// retracement map, vault and mainnet/testnet paths — is byte-identical.
func TestTrailingStopSignatureMatchesPython(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(testPrivateKey, "0x"))
	if err != nil {
		t.Fatalf("test key: %v", err)
	}
	vectors := []struct {
		name    string
		args    map[string]any
		asset   int64
		isBuy   bool
		vault   string
		nonce   int64
		mainnet bool
		r, s    string
		v       int
	}{
		{
			name: "percent with activation, mainnet",
			args: map[string]any{
				"size": "4.12", "retracement": "1.5", "retracementUnit": "percent",
				"activationPrice": "250.5", "reduceOnly": true,
			},
			asset: 5, isBuy: false, nonce: 1757000000000, mainnet: true,
			r: "0x15c385af4d595b07dcb0c204a3da263a6ef5cb263fd09f3ecc36caece61555b3",
			s: "0x60ac69422a0372a8dd946b4b91e2376a7d8300cd5c285cc473aed25f6967e02",
			v: 27,
		},
		{
			name: "quote retracement, no activation, testnet",
			args: map[string]any{
				"size": "0.1", "retracement": "10", "retracementUnit": "quote",
			},
			asset: 0, isBuy: true, nonce: 1757000001234, mainnet: false,
			r: "0x1f27366df952e6cda9c662015623bb0349a43280ddd607769df0d25d14d0043c",
			s: "0x79afcdd511a61641c406768f440126adbfbe77c5308058951664fb1dfd44b63d",
			v: 28,
		},
		{
			name:    "vault, mainnet",
			args:    map[string]any{"size": "100", "retracement": "0.25", "reduceOnly": true},
			asset:   2,
			isBuy:   false,
			vault:   "0x00000000000000000000000000000000deadbeef",
			nonce:   1757000009999,
			mainnet: true,
			r:       "0x64e04e37257f7970e1375e6489bde0f7e06790f21c631e89086f16c25f51b9ff",
			s:       "0x5471d1519c0541731003fa52e10c521dc4a4d50bec3d1a537af6f7fc494974ac",
			v:       27,
		},
	}
	for _, tv := range vectors {
		t.Run(tv.name, func(t *testing.T) {
			action, err := buildTrailingStopAction(tv.args, tv.asset, tv.isBuy)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			sig, err := hyperliquid.SignL1Action(key, action, tv.vault, tv.nonce, nil, tv.mainnet)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			if sig.R != tv.r || sig.S != tv.s || sig.V != tv.v {
				t.Errorf("signature mismatch vs Python:\n got {r: %s, s: %s, v: %d}\nwant {r: %s, s: %s, v: %d}",
					sig.R, sig.S, sig.V, tv.r, tv.s, tv.v)
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
	// data passes the API body through (status/type preserved).
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

	// The /exchange request carries the signed trailingStop action.
	action := exchangeAction(t, fake)
	if action["type"] != "trailingStop" {
		t.Errorf("action.type = %v, want trailingStop", action["type"])
	}
	if action["asset"].(float64) != 2 || action["isBuy"] != false || action["sz"] != "4.12" || action["reduceOnly"] != true {
		t.Errorf("action fields wrong: %v", action)
	}
	if retr, _ := action["retracement"].(map[string]any); retr["pct"] != "1.5000%" {
		t.Errorf("action.retracement = %v, want pct 1.5000%%", action["retracement"])
	}
	if action["activationPx"] != "250.5" {
		t.Errorf("action.activationPx = %v, want 250.5", action["activationPx"])
	}

	// Python _post_action parity: vaultAddress/expiresAfter keys present,
	// null when unset (the SDK's own order path omits them instead — see
	// assertSignedPayload).
	req := fake.lastRequest(t)
	if v, ok := req.Payload["vaultAddress"]; !ok || v != nil {
		t.Errorf("vaultAddress = %v (present: %v), want present null", v, ok)
	}
	if v, ok := req.Payload["expiresAfter"]; !ok || v != nil {
		t.Errorf("expiresAfter = %v (present: %v), want present null", v, ok)
	}
	// Raw body keeps the frontend's key order (load-bearing for the hash).
	wantAction := `"action":{"type":"trailingStop","asset":2,"isBuy":false,"sz":"4.12","reduceOnly":true,"retracement":{"pct":"1.5000%"},"activationPx":"250.5"}`
	if !strings.Contains(req.Raw, wantAction) {
		t.Errorf("action wire order wrong:\nraw:  %s\nwant: %s", req.Raw, wantAction)
	}
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
// validation (and a bad asset index) fail before any exchange call.
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
