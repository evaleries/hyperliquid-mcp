package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Vault Management (server.py §5). Both tools post vaultDetails and pass the
// API response through untouched. vault_performance accepts startTime/endTime
// for schema parity, but the upstream endpoint takes no time range (the
// Python implementation passes them positionally into vault_details' user
// parameter — a latent bug), so Go sends the same request as vault_details
// and echoes the range only in the summary.

func vaultTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_vault_details",
			"Get detailed information about a specific vault",
			schema(map[string]any{
				"vaultAddress": strProp("Vault address in 42-character hexadecimal format"),
			}, "vaultAddress"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return vaultDetails(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_vault_performance",
			"Get performance metrics for a specific vault",
			schema(map[string]any{
				"vaultAddress": strProp("Vault address in 42-character hexadecimal format"),
				"startTime":    intProp("Start time in milliseconds", nil),
				"endTime":      intProp("End time in milliseconds (optional, defaults to current time)", nil),
			}, "vaultAddress", "startTime"),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return vaultPerformance(ctx, c, args)
			},
		),
	}
}

// vaultDetailsRaw posts the vault_details query. The Python SDK always
// includes the "user" key (hyperliquid-python-sdk 0.24.0
// vault_details(vault_address, user=None)), null for these tools.
func vaultDetailsRaw(ctx context.Context, c *hl.Client, vaultAddress string) (json.RawMessage, error) {
	return c.RawInfo(ctx, map[string]any{
		"type":         "vaultDetails",
		"vaultAddress": vaultAddress,
		"user":         nil,
	})
}

func vaultDetails(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	vaultAddress, err := RequireString(args, "vaultAddress")
	if err != nil {
		return nil, err
	}
	raw, err := vaultDetailsRaw(ctx, c, vaultAddress)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":      fmt.Sprintf("Vault details for %s retrieved successfully", vaultAddress),
		"data":         raw,
		"vaultAddress": vaultAddress,
	}, nil
}

func vaultPerformance(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	vaultAddress, err := RequireString(args, "vaultAddress")
	if err != nil {
		return nil, err
	}
	startTime, err := RequireInt(args, "startTime")
	if err != nil {
		return nil, err
	}
	// Same wire call as vault_details: the time
	// range never reaches the API.
	raw, err := vaultDetailsRaw(ctx, c, vaultAddress)
	if err != nil {
		return nil, err
	}
	// Python `"endTime": end_time or "current"`: absent, null, and 0 all
	// render as "current".
	var endTime any = "current"
	if v, ok := args["endTime"]; ok {
		if n, isInt := v.(int64); isInt && n != 0 {
			endTime = n
		}
	}
	return map[string]any{
		"message": fmt.Sprintf("Vault performance for %s retrieved successfully", vaultAddress),
		"data":    raw,
		"summary": map[string]any{
			"vaultAddress": vaultAddress,
			"timeRange": map[string]any{
				"startTime": startTime,
				"endTime":   endTime,
			},
		},
	}, nil
}
