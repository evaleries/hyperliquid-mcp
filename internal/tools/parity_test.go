package tools

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestMarshalPrettyMatchesStdlib pins the sonic encoder (envelopeJSON) to
// byte-parity with the stdlib encoder it replaced — the repo's hard rule #1
// (byte-compatible envelopes). If a sonic upgrade or config change ever alters
// wire bytes (float formatting, key order, escaping beyond the documented
// U+2028/2029 divergence), this test fails before it ships.
func TestMarshalPrettyMatchesStdlib(t *testing.T) {
	stdlib := func(v any) string {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			t.Fatalf("stdlib encode: %v", err)
		}
		return string(bytes.TrimRight(buf.Bytes(), "\n"))
	}

	shapes := map[string]any{
		"success with raw passthrough": map[string]any{
			"message": "Order book for BTC retrieved successfully",
			"data":    json.RawMessage(`{"coin":"BTC","levels":[{"px":"27000.5","sz":"1.25"}],"time":1788490557996}`),
			"summary": map[string]any{"coin": "BTC", "bidsCount": 3, "asksCount": 2},
		},
		"error envelope": map[string]any{
			"error":     "missing required parameter: size",
			"tool":      "hyperliquid_place_order",
			"arguments": map[string]any{"asset": 5.0, "isBuy": true},
		},
		"html and unicode": map[string]any{
			"data": map[string]any{"html": "<b>&amp;</b>", "accent": "café", "cjk": "文字"},
		},
		"empty containers and nulls": map[string]any{
			"m": map[string]any{}, "s": []any{}, "z": nil, "list": []any{nil, ""},
		},
		"numbers": map[string]any{
			"int": int64(1<<62 + 123), "float": 0.1, "big": 1e20, "neg": -3.5,
			"time": int64(1788490557996), "zero": 0,
		},
		"deep nesting": map[string]any{
			"a": map[string]any{"b": map[string]any{"c": []any{map[string]any{"d": []any{1, 2}}}}},
		},
	}

	for name, v := range shapes {
		t.Run(name, func(t *testing.T) {
			got, err := marshalPretty(v)
			if err != nil {
				t.Fatalf("marshalPretty: %v", err)
			}
			if want := stdlib(v); got != want {
				t.Errorf("byte divergence:\n--- stdlib ---\n%s\n--- sonic ---\n%s", want, got)
			}
		})
	}
}
