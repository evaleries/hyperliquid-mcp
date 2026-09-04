package tools

import "testing"

// BenchmarkCandlesPipeline measures the full large-payload path as production
// runs it: streaming count for the summary + indented envelope render with
// the raw body passed through. The ~2.7MB body matches the largest realistic
// /info responses (20k 1m candles). Baseline before the streaming count
// (rawToSlice): 77.7MB and 641k allocs per 2.7MB call.
func BenchmarkCandlesPipeline(b *testing.B) {
	raw := candlesBody(20000)
	b.ReportAllocs()
	for b.Loop() {
		n, err := rawArrayLen(raw)
		if err != nil {
			b.Fatal(err)
		}
		env := map[string]any{
			"message": "Candles for BTC (1m) retrieved successfully",
			"data":    raw,
			"summary": map[string]any{"coin": "BTC", "interval": "1m", "numberOfCandles": n},
		}
		if _, err := marshalPretty(env); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRawArrayLen isolates the streaming count itself.
func BenchmarkRawArrayLen(b *testing.B) {
	raw := candlesBody(20000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rawArrayLen(raw); err != nil {
			b.Fatal(err)
		}
	}
}
