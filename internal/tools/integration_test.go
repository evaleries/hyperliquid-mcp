//go:build integration

package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/edkdev/hyperliquid-mcp-go/internal/config"
	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// TestIntegrationPlaceAndCancelOrder is the manual testnet smoke test:
// places a far-off-market limit order on TESTNET (rests,
// never fills), then cancels it. Requires a funded testnet account:
//
//	HYPERLIQUID_PRIVATE_KEY=0x... go test -tags=integration ./internal/tools/ -run Integration -v
//
// Never runs in CI; skipped without the env var. Testnet is forced regardless
// of the ambient HYPERLIQUID_TESTNET value, and HYPERLIQUID_BASE_URL is
// ignored so an ambient endpoint override can never redirect this live-order
// smoke test away from testnet.
func TestIntegrationPlaceAndCancelOrder(t *testing.T) {
	if os.Getenv("HYPERLIQUID_PRIVATE_KEY") == "" {
		t.Skip("HYPERLIQUID_PRIVATE_KEY not set; skipping testnet integration test")
	}
	cfg, err := config.LoadFromEnv(func(key string) string {
		switch key {
		case config.EnvTestnet:
			return "true"
		case config.EnvBaseURL:
			return ""
		}
		return os.Getenv(key)
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	c, err := hl.New(ctx, cfg)
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	// Sanity: read-only market data works against the real API.
	midsTool := findTool(t, marketTools(c), "hyperliquid_get_all_mids")
	mids, isErr := callTool(t, midsTool, map[string]any{})
	if isErr {
		t.Fatalf("get_all_mids failed: %v", mids["error"])
	}

	// ETH (asset 1) buy at a price far below any plausible mark: rests on the
	// book, never fills, value 0.01 * 1100 = $11 >= $10 exchange minimum.
	place := findTool(t, orderTools(c), "hyperliquid_place_order")
	placed, isErr := callTool(t, place, map[string]any{
		"asset": 1, "isBuy": true, "size": "0.01", "price": "1100",
	})
	if isErr {
		t.Fatalf("place_order failed: %v", placed["error"])
	}
	info, ok := placed["orderInfo"].(map[string]any)
	if !ok {
		t.Fatalf("orderInfo missing: %v", placed)
	}
	if info["status"] != "resting" {
		t.Fatalf("expected resting order, got %v (full: %v)", info["status"], placed)
	}
	oidF, ok := info["orderId"].(float64)
	if !ok {
		t.Fatalf("orderId missing: %v", info)
	}
	oid := int64(oidF)
	t.Logf("resting order placed: oid=%d", oid)

	// Cancel it.
	cancelTool := findTool(t, orderTools(c), "hyperliquid_cancel_order")
	cancelled, isErr := callTool(t, cancelTool, map[string]any{"coin": "ETH", "oid": oid})
	if isErr {
		t.Fatalf("cancel_order failed: %v", cancelled["error"])
	}
	co, ok := cancelled["cancelledOrder"].(map[string]any)
	if !ok || co["coin"] != "ETH" || int64(co["orderId"].(float64)) != oid {
		t.Fatalf("unexpected cancelledOrder: %v", cancelled)
	}
	t.Logf("order %d cancelled", oid)
}
