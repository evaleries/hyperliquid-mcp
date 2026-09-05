package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/server"

	"github.com/edkdev/hyperliquid-mcp-go/internal/hl"
)

// Account & Position Management (server.py §1). All three tools are backed by
// clearinghouseState; the data field passes the API response through
// untouched.

func accountTools(c *hl.Client) []server.ServerTool {
	return []server.ServerTool{
		tool(
			"hyperliquid_get_account_info",
			"Get user's perpetual account summary including positions and margin",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"dex":         strPropDefault("Perp dex name (optional, defaults to empty string)", ""),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return accountInfo(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_positions",
			"Get user's open positions with margin summary",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"dex":         strPropDefault("Perp dex name (optional, defaults to empty string)", ""),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getPositions(ctx, c, args)
			},
		),
		tool(
			"hyperliquid_get_balance",
			"Get user's account balance and withdrawable amount",
			schema(map[string]any{
				"userAddress": strProp("User address (optional, defaults to configured account)"),
				"dex":         strPropDefault("Perp dex name (optional, defaults to empty string)", ""),
			}),
			func(ctx context.Context, args map[string]any) (map[string]any, error) {
				return getBalance(ctx, c, args)
			},
		),
	}
}

// userStateRaw posts the clearinghouseState query. Info.user_state always
// includes the "dex" key, even when empty.
func userStateRaw(ctx context.Context, c *hl.Client, args map[string]any) (json.RawMessage, error) {
	return c.RawInfo(ctx, map[string]any{
		"type": "clearinghouseState",
		"user": UserAddress(args, c),
		"dex":  Dex(args),
	})
}

func accountInfo(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	raw, err := userStateRaw(ctx, c, args)
	if err != nil {
		return nil, err
	}
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	margin, ok := digMap(result, "marginSummary")
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing marginSummary")
	}
	return map[string]any{
		"message": "Account information retrieved successfully",
		"data":    raw,
		"summary": map[string]any{
			"accountValue":      digString(margin, "accountValue"),
			"totalMarginUsed":   digString(margin, "totalMarginUsed"),
			"withdrawable":      digString(result, "withdrawable"),
			"numberOfPositions": digLen(result, "assetPositions"),
		},
	}, nil
}

func getPositions(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	raw, err := userStateRaw(ctx, c, args)
	if err != nil {
		return nil, err
	}
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	margin, ok := digMap(result, "marginSummary")
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing marginSummary")
	}
	// Python narrows data to these four keys; crossMarginSummary may be null.
	data, err := pickFields(raw, []string{"assetPositions", "marginSummary", "withdrawable"}, []string{"crossMarginSummary"})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message": "Positions retrieved successfully",
		"data":    data,
		"summary": map[string]any{
			"numberOfPositions": digLen(result, "assetPositions"),
			"accountValue":      digString(margin, "accountValue"),
			"totalMarginUsed":   digString(margin, "totalMarginUsed"),
		},
	}, nil
}

func getBalance(ctx context.Context, c *hl.Client, args map[string]any) (map[string]any, error) {
	raw, err := userStateRaw(ctx, c, args)
	if err != nil {
		return nil, err
	}
	result, err := rawToMap(raw)
	if err != nil {
		return nil, err
	}
	margin, ok := digMap(result, "marginSummary")
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing marginSummary")
	}
	accountValue := digString(margin, "accountValue")
	totalMarginUsed := digString(margin, "totalMarginUsed")
	withdrawable := digString(result, "withdrawable")

	// Python: str(float(accountValue) - float(totalMarginUsed))
	av, err := strconv.ParseFloat(accountValue, 64)
	if err != nil {
		return nil, fmt.Errorf("unexpected accountValue %q: %v", accountValue, err)
	}
	mu, err := strconv.ParseFloat(totalMarginUsed, 64)
	if err != nil {
		return nil, fmt.Errorf("unexpected totalMarginUsed %q: %v", totalMarginUsed, err)
	}

	return map[string]any{
		"message": "Balance retrieved successfully",
		"data": map[string]any{
			"accountValue":    accountValue,
			"totalMarginUsed": totalMarginUsed,
			"totalNtlPos":     digString(margin, "totalNtlPos"),
			"totalRawUsd":     digString(margin, "totalRawUsd"),
			"withdrawable":    withdrawable,
		},
		"summary": map[string]any{
			"accountValue":     accountValue,
			"withdrawable":     withdrawable,
			"availableBalance": PyFloatStr(av - mu),
		},
	}, nil
}

// pickFields narrows a raw API response to selected top-level keys without
// re-marshaling their values (byte parity). Required keys must be present;
// optional keys marshal as null when absent (Python dict.get semantics).
func pickFields(raw json.RawMessage, required, optional []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}
	out := make(map[string]json.RawMessage, len(required)+len(optional))
	for _, k := range required {
		v, ok := fields[k]
		if !ok {
			return nil, fmt.Errorf("unexpected response: missing %s", k)
		}
		out[k] = v
	}
	for _, k := range optional {
		if v, ok := fields[k]; ok {
			out[k] = v
		} else {
			out[k] = json.RawMessage("null")
		}
	}
	return out, nil
}
