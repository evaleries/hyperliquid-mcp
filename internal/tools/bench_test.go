package tools

import (
	"encoding/json"
	"fmt"
	"testing"
)

// The per-call Go-side cost of this server is expected to sit orders of
// magnitude below the Hyperliquid API round trip (~100ms). These benchmarks
// exist to prove that — and to catch a regression if a future change makes
// envelope building or argument normalization non-trivially expensive.

// candlesBody builds a candleSnapshot-style body with n entries, matching the
// wire shape of realistic /info responses.
func candlesBody(n int) json.RawMessage {
	buf := []byte{'['}
	for i := range n {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, []byte(fmt.Sprintf(
			`{"t":1700000000000,"T":1700000003599,"s":"BTC","i":"1m","o":"%d.1","h":"%d.9","l":"%d.0","c":"%d.5","v":"123.456","n":42}`,
			27000+i%100, 27000+i%100, 27000+i%100, 27000+i%100))...)
	}
	return append(buf, ']')
}

// candlesEnvelope returns a realistic large envelope: a candles response with
// 1500 entries passed through as json.RawMessage, like getCandles produces.
func candlesEnvelope() map[string]any {
	return map[string]any{
		"message": "Retrieved 1500 candles for BTC",
		"data":    candlesBody(1500),
		"summary": map[string]any{"coin": "BTC", "count": 1500},
	}
}

func BenchmarkMarshalPrettyLarge(b *testing.B) {
	envelope := candlesEnvelope()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := marshalPretty(envelope); err != nil {
			b.Fatalf("marshalPretty: %v", err)
		}
	}
}

func BenchmarkMarshalPrettySmall(b *testing.B) {
	envelope := map[string]any{
		"message": "Server time retrieved successfully",
		"data":    map[string]any{"serverTime": 1700000000000},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := marshalPretty(envelope); err != nil {
			b.Fatalf("marshalPretty: %v", err)
		}
	}
}

func BenchmarkNormalizeIntParams(b *testing.B) {
	// NormalizeIntParams mutates the map; build a fresh one per iteration so
	// the string-coercion path is measured every time (map construction is
	// part of the measured cost here).
	b.ReportAllocs()
	for b.Loop() {
		args := map[string]any{
			"asset":     float64(3),
			"oid":       "12345678",
			"startTime": float64(1700000000000),
			"coin":      "BTC",
		}
		if err := NormalizeIntParams(args); err != nil {
			b.Fatalf("NormalizeIntParams: %v", err)
		}
	}
}

func BenchmarkParseOrderResponse(b *testing.B) {
	data := map[string]any{
		"status": "ok",
		"response": map[string]any{
			"type": "order",
			"data": map[string]any{
				"statuses": []any{
					map[string]any{"filled": map[string]any{"totalSz": "0.5", "avgPx": "27000.0", "oid": float64(12345)}},
				},
			},
		},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseOrderResponse(data); err != nil {
			b.Fatalf("ParseOrderResponse: %v", err)
		}
	}
}
