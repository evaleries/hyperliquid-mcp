package tools

import (
	"testing"
	"time"
)

func TestServerTime(t *testing.T) {
	// The handler must not reach the network; a nil handler fails the test
	// on any delegated request.
	fake, c := newFakeAPI(t, nil)
	st := findTool(t, utilTools(c), "hyperliquid_get_server_time")

	before := time.Now().UnixMilli()
	out, isErr := callTool(t, st, map[string]any{})
	after := time.Now().UnixMilli()
	if isErr {
		t.Fatalf("unexpected error envelope: %v", out)
	}
	if out["message"] != "Server time retrieved successfully" {
		t.Errorf("message: %v", out["message"])
	}
	data := out["data"].(map[string]any)
	serverTime, ok := data["serverTime"].(float64)
	if !ok {
		t.Fatalf("serverTime should be a number, got %T: %v", data["serverTime"], data["serverTime"])
	}
	if serverTime <= 0 {
		t.Errorf("serverTime should be positive, got %v", serverTime)
	}
	// Local-clock parity: within a generous bound of the test's own clock
	// (the contract pins 60s; the observed delta is normally sub-second).
	if d := after - before + 1; serverTime < float64(before)-60_000 || serverTime > float64(after)+60_000 {
		t.Errorf("serverTime %v not within 60s of now (window %d ms)", serverTime, d)
	}
	if got := len(fake.requestsSnapshot()); got != 0 {
		t.Errorf("server time must not hit the API, got %d requests", got)
	}
}
