package tools

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Utility (server.py §6).

func utilTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_get_server_time",
			"Get estimated server time",
			schema(map[string]any{}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getServerTime(ctx, c, args)
			},
		),
	}
}

// getServerTime returns the local clock with no network call (Python parity).
func getServerTime(_ context.Context, _ *hl.Client, _ map[string]any) (map[string]any, error) {
	return map[string]any{
		"message": "Server time retrieved successfully",
		"data": map[string]any{
			"serverTime": time.Now().UnixMilli(),
		},
	}, nil
}
