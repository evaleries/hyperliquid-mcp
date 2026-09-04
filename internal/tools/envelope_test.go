package tools

import (
	"strings"
	"testing"

	"github.com/sonirico/go-hyperliquid"
)

func TestParseOrderStatus(t *testing.T) {
	tests := []struct {
		name   string
		status map[string]any
		check  func(t *testing.T, out map[string]any)
	}{
		{
			name:   "resting",
			status: map[string]any{"resting": map[string]any{"oid": 12345.0}},
			check: func(t *testing.T, out map[string]any) {
				if out["status"] != "resting" || out["orderId"] != 12345.0 {
					t.Errorf("out: %v", out)
				}
				if out["message"] != "Order placed and resting on order book" {
					t.Errorf("message: %v", out["message"])
				}
			},
		},
		{
			name: "filled",
			status: map[string]any{"filled": map[string]any{
				"oid": 999.0, "totalSz": "0.5", "avgPx": "50000.0",
			}},
			check: func(t *testing.T, out map[string]any) {
				if out["status"] != "filled" || out["orderId"] != 999.0 {
					t.Errorf("out: %v", out)
				}
				if out["totalSize"] != "0.5" || out["averagePrice"] != "50000.0" {
					t.Errorf("filled fields: %v", out)
				}
				if out["message"] != "Order filled successfully" {
					t.Errorf("message: %v", out["message"])
				}
			},
		},
		{
			name:   "error",
			status: map[string]any{"error": "Insufficient margin"},
			check: func(t *testing.T, out map[string]any) {
				if out["status"] != "error" || out["error"] != "Insufficient margin" {
					t.Errorf("out: %v", out)
				}
				if out["message"] != "Order placement failed" {
					t.Errorf("message: %v", out["message"])
				}
			},
		},
		{
			name:   "unknown",
			status: map[string]any{"somethingElse": true},
			check: func(t *testing.T, out map[string]any) {
				if out["status"] != "unknown" {
					t.Errorf("out: %v", out)
				}
				raw, ok := out["rawStatus"].(map[string]any)
				if !ok || raw["somethingElse"] != true {
					t.Errorf("rawStatus: %v", out["rawStatus"])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, ParseOrderStatus(tt.status))
		})
	}
}

func TestParseOrderResponse(t *testing.T) {
	data := map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "order",
			"data": map[string]any{
				"statuses": []any{map[string]any{"resting": map[string]any{"oid": 7.0}}},
			},
		},
	}
	out, err := ParseOrderResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "resting" || out["orderId"] != 7.0 {
		t.Errorf("out: %v", out)
	}

	// Missing statuses key → unknown (Python's .get default [{}][0] applies).
	out, err = ParseOrderResponse(map[string]any{"status": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "unknown" {
		t.Errorf("missing statuses should be unknown: %v", out)
	}

	// Present-but-empty statuses → error (Python IndexError on [][0] →
	// error envelope).
	empty := map[string]any{
		"response": map[string]any{"data": map[string]any{"statuses": []any{}}},
	}
	if _, err := ParseOrderResponse(empty); err == nil {
		t.Error("empty statuses must error (Python IndexError parity)")
	}
}

func TestOrderStatusToMap(t *testing.T) {
	oid := int64(42)
	cloid := "0x1234567890abcdef1234567890abcdef"
	s := hyperliquid.OrderStatus{Resting: &hyperliquid.OrderStatusResting{Oid: oid, ClientID: &cloid}}
	m := OrderStatusToMap(s)
	resting, ok := m["resting"].(map[string]any)
	if !ok {
		t.Fatalf("out: %v", m)
	}
	if resting["oid"] != int64(42) || resting["cloid"] != cloid {
		t.Errorf("resting: %v", resting)
	}

	s = hyperliquid.OrderStatus{Filled: &hyperliquid.OrderStatusFilled{Oid: 7, TotalSz: "0.5", AvgPx: "100.0"}}
	m = OrderStatusToMap(s)
	filled := m["filled"].(map[string]any)
	if filled["oid"] != 7 || filled["totalSz"] != "0.5" || filled["avgPx"] != "100.0" {
		t.Errorf("filled: %v", filled)
	}

	errStr := "bad"
	s = hyperliquid.OrderStatus{Error: &errStr}
	m = OrderStatusToMap(s)
	if m["error"] != "bad" {
		t.Errorf("error: %v", m)
	}
}

func TestMarshalPretty(t *testing.T) {
	text, err := marshalPretty(map[string]any{"a": 1, "b": "<tag>"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "\n  \"a\": 1") {
		t.Errorf("expected 2-space indent:\n%s", text)
	}
	if !strings.Contains(text, "<tag>") {
		t.Errorf("HTML escaping must be disabled (Python parity); expected literal <tag> in:\n%s", text)
	}
	if strings.HasSuffix(text, "\n") {
		t.Error("no trailing newline (Python json.dumps parity)")
	}
}
